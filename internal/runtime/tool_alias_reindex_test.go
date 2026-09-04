package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
	"github.com/smart-mcp-proxy/mcpproxy-go/internal/index"
	"github.com/smart-mcp-proxy/mcpproxy-go/internal/storage"
)

// aliasHashOf returns the alias hash recorded in the search index for a tool.
func aliasHashOf(t *testing.T, rt *Runtime, serverName, toolName string) string {
	t.Helper()
	tools, err := rt.indexManager.GetToolsByServer(serverName)
	require.NoError(t, err)
	for _, tool := range tools {
		if extractToolName(tool.Name) == toolName {
			return tool.AliasHash
		}
	}
	t.Fatalf("tool %q not found in index for server %q", toolName, serverName)
	return ""
}

// setAliases replaces the tool_aliases of a server in the runtime config snapshot,
// which is what buildAliasesMap reads when indexing.
func setAliases(t *testing.T, rt *Runtime, serverName string, aliases map[string][]string) {
	t.Helper()
	for _, s := range rt.cfg.Servers {
		if s.Name == serverName {
			s.ToolAliases = aliases
			return
		}
	}
	t.Fatalf("server %q not found in runtime config", serverName)
}

// TestApplyDifferentialToolUpdate_AliasDiffMatrix walks the (tool hash, alias hash)
// x (same, changed) matrix and asserts what the differential update does with it.
//
// The regression this guards: aliases are merged into the bleve document only at
// (re-)index time, and the diff used to look at the tool hash alone. An alias edit
// in the config therefore never reached the index — the tool was "unchanged".
func TestApplyDifferentialToolUpdate_AliasDiffMatrix(t *testing.T) {
	const server = "b24"
	const tool = "get_crm_types_tool"

	tests := []struct {
		name          string
		newHash       string
		newAliases    []string
		wantReindexed bool
	}{
		{
			name:          "hash same, aliases same: no re-index",
			newHash:       "h1",
			newAliases:    []string{"типы", "crm"},
			wantReindexed: false,
		},
		{
			name:          "hash same, aliases changed: re-index (the bug)",
			newHash:       "h1",
			newAliases:    []string{"типы", "crm", "entitytypeid", "сущностей"},
			wantReindexed: true,
		},
		{
			name:          "hash changed, aliases same: re-index",
			newHash:       "h2",
			newAliases:    []string{"типы", "crm"},
			wantReindexed: true,
		},
		{
			name:          "hash changed, aliases changed: re-index",
			newHash:       "h2",
			newAliases:    []string{"типы", "crm", "entitytypeid"},
			wantReindexed: true,
		},
		{
			name:          "hash same, aliases removed: re-index",
			newHash:       "h1",
			newAliases:    nil,
			wantReindexed: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt := setupQuarantineRuntime(t, boolP(false), []*config.ServerConfig{
				{
					Name:        server,
					Enabled:     true,
					ToolAliases: map[string][]string{tool: {"типы", "crm"}},
				},
			})
			ctx := context.Background()

			initial := []*config.ToolMetadata{{
				ServerName:  server,
				Name:        tool,
				Description: "Returns CRM entity types",
				ParamsJSON:  `{"type":"object"}`,
				Hash:        "h1",
			}}
			require.NoError(t, rt.applyDifferentialToolUpdate(ctx, server, initial))

			before := aliasHashOf(t, rt, server, tool)
			require.Equal(t, index.AliasHash("типы crm"), before,
				"first index must persist the configured aliases")

			// Second discovery round with this case's hash and aliases.
			setAliases(t, rt, server, map[string][]string{tool: tc.newAliases})
			updated := []*config.ToolMetadata{{
				ServerName:  server,
				Name:        tool,
				Description: "Returns CRM entity types",
				ParamsJSON:  `{"type":"object"}`,
				Hash:        tc.newHash,
			}}
			require.NoError(t, rt.applyDifferentialToolUpdate(ctx, server, updated))

			after := aliasHashOf(t, rt, server, tool)
			wantAliasHash := index.AliasHash(index.CollectAliases(rt.cfg.Servers[0], tool, nil))
			assert.Equal(t, wantAliasHash, after,
				"index must carry the aliases the config currently declares")

			if tc.wantReindexed {
				// The document was rewritten: its hash follows the new upstream hash.
				indexed, err := rt.indexManager.GetToolsByServer(server)
				require.NoError(t, err)
				require.Len(t, indexed, 1)
				assert.Equal(t, tc.newHash, indexed[0].Hash)
			} else {
				assert.Equal(t, before, after, "unchanged tool must keep its alias hash")
			}
		})
	}
}

// TestApplyDifferentialToolUpdate_AliasChangeIsSearchable is the end-to-end check
// behind the acceptance criterion: editing tool_aliases and re-running discovery
// must lift the tool in retrieve_tools, without wiping index.bleve by hand.
func TestApplyDifferentialToolUpdate_AliasChangeIsSearchable(t *testing.T) {
	const server = "b24"
	ctx := context.Background()

	rt := setupQuarantineRuntime(t, boolP(false), []*config.ServerConfig{
		{Name: server, Enabled: true},
	})

	tools := []*config.ToolMetadata{
		{
			ServerName:  server,
			Name:        "get_crm_types_tool",
			Description: "Returns CRM entity types",
			ParamsJSON:  `{"type":"object"}`,
			Hash:        "h1",
		},
		{
			ServerName:  server,
			Name:        "userfield_add_tool",
			Description: "Adds a userfield",
			ParamsJSON:  `{"type":"object"}`,
			Hash:        "h2",
		},
	}
	require.NoError(t, rt.applyDifferentialToolUpdate(ctx, server, tools))

	const query = "смартпроцессы портала"
	results, err := rt.indexManager.SearchTools(query, 5)
	require.NoError(t, err)
	for _, res := range results {
		assert.NotEqual(t, "get_crm_types_tool", extractToolName(res.Tool.Name),
			"tool must not match the query before aliases are configured")
	}

	// Operator edits tool_aliases and discovery runs again — upstream data unchanged.
	setAliases(t, rt, server, map[string][]string{
		"get_crm_types_tool": {"смартпроцессы", "портала", "entitytypeid"},
	})
	require.NoError(t, rt.applyDifferentialToolUpdate(ctx, server, tools))

	results, err = rt.indexManager.SearchTools(query, 5)
	require.NoError(t, err)
	require.NotEmpty(t, results, "alias query must return something after the alias edit")
	assert.Equal(t, "get_crm_types_tool", extractToolName(results[0].Tool.Name),
		"aliased tool must now win the alias query")
}

// TestApplyDifferentialToolUpdate_AliasChangeKeepsQuarantineApproved guards the
// reason AliasHash is stored separately instead of folded into the tool hash:
// an alias edit must not read as a rug pull and push an approved tool to "changed".
func TestApplyDifferentialToolUpdate_AliasChangeKeepsQuarantineApproved(t *testing.T) {
	const server = "b24"
	const tool = "get_crm_types_tool"
	const description = "Returns CRM entity types"
	const schema = `{"type":"object"}`
	ctx := context.Background()

	rt := setupQuarantineRuntime(t, boolP(true), []*config.ServerConfig{
		{Name: server, Enabled: true},
	})

	require.NoError(t, rt.storageManager.SaveToolApproval(&storage.ToolApprovalRecord{
		ServerName:         server,
		ToolName:           tool,
		Status:             storage.ToolApprovalStatusApproved,
		ApprovedHash:       calculateToolApprovalHash(tool, description, schema, nil),
		CurrentDescription: description,
	}))

	tools := []*config.ToolMetadata{{
		ServerName:  server,
		Name:        tool,
		Description: description,
		ParamsJSON:  schema,
		Hash:        "h1",
	}}
	require.NoError(t, rt.applyDifferentialToolUpdate(ctx, server, tools))

	setAliases(t, rt, server, map[string][]string{tool: {"смартпроцессы", "entitytypeid"}})
	require.NoError(t, rt.applyDifferentialToolUpdate(ctx, server, tools))

	record, err := rt.storageManager.GetToolApproval(server, tool)
	require.NoError(t, err)
	assert.Equal(t, storage.ToolApprovalStatusApproved, record.Status,
		"alias edit must not move an approved tool into quarantine")

	assert.Equal(t, index.AliasHash("смартпроцессы entitytypeid"),
		aliasHashOf(t, rt, server, tool), "aliases must still reach the index")
}
