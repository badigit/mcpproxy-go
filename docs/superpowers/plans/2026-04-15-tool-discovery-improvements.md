# Tool Discovery Improvements — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve AI agent tool discovery by reducing keyword competition between MCPProxy tools, and adding a fallback catalog when BM25 search returns zero results.

**Architecture:** Three independent commits: (C) integration tests documenting real-world search scenarios, (A) rewrite tool descriptions to give `retrieve_tools` unique keyword dominance, (B) add server/tool catalog fallback in `handleRetrieveToolsWithMode()` when search returns 0 results.

**Tech Stack:** Go 1.24, Bleve BM25 index, mcp-go, existing test patterns from `internal/index/bleve_test.go`

**Spec:** `docs/superpowers/specs/2026-04-15-tool-discovery-improvements-design.md`

---

### Task 1: BM25 Discovery Integration Tests

**Files:**
- Create: `internal/index/bleve_discovery_test.go`

- [ ] **Step 1: Create test file with English tool discovery test**

```go
package index

import (
	"os"
	"testing"
	"time"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// createTestB24Tools returns tools resembling real Bitrix24 CRM server (English descriptions)
func createTestB24Tools() []*config.ToolMetadata {
	now := time.Now()
	return []*config.ToolMetadata{
		{
			Name:        "b24:list_items_tool",
			ServerName:  "b24",
			Description: "List CRM items with full filter, order, pagination support",
			ParamsJSON:  `{"type":"object","properties":{"entityTypeId":{"type":"integer","description":"CRM entity type ID"}},"required":["entityTypeId"]}`,
			Hash:        "b24h1",
			Created:     now,
			Updated:     now,
		},
		{
			Name:        "b24:get_item_tool",
			ServerName:  "b24",
			Description: "Get single CRM item with enriched details, product rows, custom fields",
			ParamsJSON:  `{"type":"object","properties":{"entityTypeId":{"type":"integer"},"id":{"type":"integer"}},"required":["entityTypeId","id"]}`,
			Hash:        "b24h2",
			Created:     now,
			Updated:     now,
		},
		{
			Name:        "b24:create_item_tool",
			ServerName:  "b24",
			Description: "Create CRM item with JSON fields (supports productRows)",
			ParamsJSON:  `{"type":"object","properties":{"entityTypeId":{"type":"integer"},"fields":{"type":"object"}},"required":["entityTypeId","fields"]}`,
			Hash:        "b24h3",
			Created:     now,
			Updated:     now,
		},
		{
			Name:        "b24:list_requisites_tool",
			ServerName:  "b24",
			Description: "List requisites (legal details: INN, KPP, OGRN) for entity",
			ParamsJSON:  `{"type":"object","properties":{"entityTypeId":{"type":"integer"},"entityId":{"type":"integer"}},"required":["entityTypeId","entityId"]}`,
			Hash:        "b24h4",
			Created:     now,
			Updated:     now,
		},
		{
			Name:        "b24:get_requisite_tool",
			ServerName:  "b24",
			Description: "Get single requisite with bank details and addresses",
			ParamsJSON:  `{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}`,
			Hash:        "b24h5",
			Created:     now,
			Updated:     now,
		},
	}
}

// createTestDadataTools returns tools resembling real Dadata server (Russian descriptions)
func createTestDadataTools() []*config.ToolMetadata {
	now := time.Now()
	return []*config.ToolMetadata{
		{
			Name:        "dadata:suggest_party",
			ServerName:  "dadata",
			Description: "Подсказки по организациям и ИП по названию, ИНН, ОГРН",
			ParamsJSON:  `{"type":"object","properties":{"query":{"type":"string","description":"Текст для поиска"},"count":{"type":"integer"}},"required":["query"]}`,
			Hash:        "ddh1",
			Created:     now,
			Updated:     now,
		},
		{
			Name:        "dadata:suggest_address",
			ServerName:  "dadata",
			Description: "Подсказки по адресам",
			ParamsJSON:  `{"type":"object","properties":{"query":{"type":"string"},"count":{"type":"integer"}},"required":["query"]}`,
			Hash:        "ddh2",
			Created:     now,
			Updated:     now,
		},
		{
			Name:        "dadata:find_by_id",
			ServerName:  "dadata",
			Description: "Поиск организации по ИНН или ОГРН",
			ParamsJSON:  `{"type":"object","properties":{"query":{"type":"string","description":"ИНН или ОГРН"}},"required":["query"]}`,
			Hash:        "ddh3",
			Created:     now,
			Updated:     now,
		},
		{
			Name:        "dadata:suggest_bank",
			ServerName:  "dadata",
			Description: "Подсказки по банкам (БИК, название)",
			ParamsJSON:  `{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`,
			Hash:        "ddh4",
			Created:     now,
			Updated:     now,
		},
	}
}

func TestBM25_EnglishToolDiscovery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bleve_discovery_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	idx, err := NewBleveIndex(tmpDir, zap.NewNop())
	require.NoError(t, err)
	defer idx.Close()

	err = idx.BatchIndex(createTestB24Tools())
	require.NoError(t, err)

	tests := []struct {
		name          string
		query         string
		expectedTools []string
		minResults    int
	}{
		{
			name:          "requisites by description keywords",
			query:         "requisites legal details",
			expectedTools: []string{"b24:list_requisites_tool", "b24:get_requisite_tool"},
			minResults:    1,
		},
		{
			name:          "create CRM item",
			query:         "create CRM item",
			expectedTools: []string{"b24:create_item_tool"},
			minResults:    1,
		},
		{
			name:          "exact tool name match",
			query:         "list_items_tool",
			expectedTools: []string{"b24:list_items_tool"},
			minResults:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := idx.SearchTools(tt.query, 10)
			require.NoError(t, err)
			assert.GreaterOrEqual(t, len(results), tt.minResults, "query %q: expected at least %d results, got %d", tt.query, tt.minResults, len(results))

			foundTools := make(map[string]bool)
			for _, r := range results {
				foundTools[r.Tool.Name] = true
			}
			for _, expected := range tt.expectedTools {
				assert.True(t, foundTools[expected], "query %q: expected tool %s in results, got %v", tt.query, expected, foundTools)
			}
		})
	}
}
```

- [ ] **Step 2: Add mixed-language discovery test**

Append to the same file:

```go
func TestBM25_MixedLanguageToolDiscovery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bleve_mixed_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	idx, err := NewBleveIndex(tmpDir, zap.NewNop())
	require.NoError(t, err)
	defer idx.Close()

	// Index both b24 (English) and dadata (Russian) tools
	allTools := append(createTestB24Tools(), createTestDadataTools()...)
	err = idx.BatchIndex(allTools)
	require.NoError(t, err)

	t.Run("exact tool name finds dadata tool", func(t *testing.T) {
		results, err := idx.SearchTools("suggest_party", 10)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(results), 1)
		assert.Equal(t, "dadata:suggest_party", results[0].Tool.Name)
	})

	t.Run("Russian keywords find Russian-described tools", func(t *testing.T) {
		results, err := idx.SearchTools("организация ИНН", 10)
		require.NoError(t, err)
		// Document actual behavior — standard analyzer may or may not handle Russian
		if len(results) > 0 {
			foundTools := make(map[string]bool)
			for _, r := range results {
				foundTools[r.Tool.Name] = true
			}
			// At least one dadata tool should match
			hasDadata := foundTools["dadata:suggest_party"] || foundTools["dadata:find_by_id"]
			assert.True(t, hasDadata, "Russian query should find dadata tools, got %v", foundTools)
		} else {
			t.Log("NOTE: Standard analyzer returned 0 results for Russian query — cross-language gap confirmed")
		}
	})

	t.Run("server name filter finds all server tools", func(t *testing.T) {
		results, err := idx.SearchTools("dadata", 10)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 2, "server name query should find multiple dadata tools")
		for _, r := range results {
			assert.Equal(t, "dadata", r.Tool.ServerName)
		}
	})

	t.Run("English query does not find Russian-only described tools", func(t *testing.T) {
		// This documents the cross-language gap that future work should address
		results, err := idx.SearchTools("company", 10)
		require.NoError(t, err)
		foundSuggestParty := false
		for _, r := range results {
			if r.Tool.Name == "dadata:suggest_party" {
				foundSuggestParty = true
			}
		}
		// Document current behavior: "company" likely won't find "suggest_party"
		// because description is "Подсказки по организациям" (Russian)
		t.Logf("English 'company' found suggest_party: %v (results: %d)", foundSuggestParty, len(results))
	})
}
```

- [ ] **Step 3: Add zero-results baseline test**

Append to the same file:

```go
func TestBM25_ZeroResultsBaseline(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bleve_zero_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	idx, err := NewBleveIndex(tmpDir, zap.NewNop())
	require.NoError(t, err)
	defer idx.Close()

	allTools := append(createTestB24Tools(), createTestDadataTools()...)
	err = idx.BatchIndex(allTools)
	require.NoError(t, err)

	t.Run("nonsense query returns zero results", func(t *testing.T) {
		results, err := idx.SearchTools("completely_unrelated_nonsense_xyz_42", 10)
		require.NoError(t, err)
		assert.Equal(t, 0, len(results), "nonsense query should return 0 results")
	})

	t.Run("partial name suggest finds dadata tools", func(t *testing.T) {
		results, err := idx.SearchTools("suggest", 10)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 2, "'suggest' should match suggest_party, suggest_address, suggest_bank")
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/index/ -run "TestBM25_" -v`

Expected: All tests PASS (they document current behavior, not assert future improvements).

- [ ] **Step 5: Commit**

```bash
git add internal/index/bleve_discovery_test.go
git commit -m "test: add BM25 discovery tests for real-world b24/dadata scenarios

Document BM25 search behavior with English (b24-style) and Russian
(dadata-style) tool descriptions. Tests cover exact name match,
description keyword search, cross-language gap, server name filtering,
and zero-results baseline for fallback feature."
```

---

### Task 2: Rewrite Tool Descriptions

**Files:**
- Modify: `internal/server/mcp.go:421-453` (buildCallToolVariantTool descriptions)
- Modify: `internal/server/mcp.go:486` (retrieve_tools description)
- Modify: `internal/server/mcp.go:648` (search_servers description)
- Modify: `internal/server/mcp.go:671` (list_registries description)

- [ ] **Step 1: Rewrite retrieve_tools description**

In `internal/server/mcp.go`, replace the description at line 486:

Old:
```go
mcp.WithDescription("🔍 CALL THIS FIRST to discover relevant tools! This is the primary tool discovery mechanism that searches across ALL upstream MCP servers using intelligent BM25 full-text search. Always use this before attempting to call any specific tools. Use natural language to describe what you want to accomplish (e.g., 'create GitHub repository', 'query database', 'weather forecast'). Results include 'annotations' (tool behavior hints like destructiveHint) and 'call_with' recommendation indicating which tool variant to use (call_tool_read/write/destructive). Then use the recommended variant with an 'intent' parameter. NOTE: Quarantined servers are excluded from search results for security. Use 'quarantine_security' tool to examine and manage quarantined servers. TO ADD NEW SERVERS: Use 'list_registries' then 'search_servers' to find and add new MCP servers."),
```

New:
```go
mcp.WithDescription("PRIMARY TOOL DISCOVERY — must be called before using any upstream tools. Searches all connected MCP servers using BM25 full-text search. Describe what you need in natural language (e.g., 'create GitHub issue', 'find company by INN', 'list CRM deals'). Returns matching tools with exact names, inputSchema, annotations, and call_with recommendation indicating which variant to use (call_tool_read/write/destructive). Quarantined servers excluded from results. To add NEW servers: use list_registries + search_servers instead."),
```

- [ ] **Step 2: Rewrite call_tool_read description**

In `internal/server/mcp.go`, replace line 427:

Old:
```go
description = "Execute a READ-ONLY tool. WORKFLOW: 1) Call retrieve_tools first to find tools, 2) Use the exact 'name' field from results. DECISION RULE: Use this when the tool name contains: search, query, list, get, fetch, find, check, view, read, show, describe, lookup, retrieve, browse, explore, discover, scan, inspect, analyze, examine, validate, verify. Examples: search_files, get_user, list_repositories, query_database, find_issues, check_status. This is the DEFAULT choice when unsure - most tools are read-only."
```

New:
```go
description = "Execute a read-only upstream tool. Pass the exact 'server:tool' name from retrieve_tools results. For data retrieval operations without side effects. This is the DEFAULT choice when unsure."
```

- [ ] **Step 3: Rewrite call_tool_write description**

In `internal/server/mcp.go`, replace line 434:

Old:
```go
description = "Execute a STATE-MODIFYING tool. WORKFLOW: 1) Call retrieve_tools first to find tools, 2) Use the exact 'name' field from results. DECISION RULE: Use this when the tool name contains: create, update, modify, add, set, send, edit, change, write, post, put, patch, insert, upload, submit, assign, configure, enable, register, subscribe, publish, move, copy, rename, merge. Examples: create_issue, update_file, send_message, add_comment, set_status, edit_page. Use only when explicitly modifying state."
```

New:
```go
description = "Execute a state-modifying upstream tool. Pass the exact 'server:tool' name from retrieve_tools results. For create, update, send, and other write operations."
```

- [ ] **Step 4: Rewrite call_tool_destructive description**

In `internal/server/mcp.go`, replace line 441:

Old:
```go
description = "Execute a DESTRUCTIVE tool. WORKFLOW: 1) Call retrieve_tools first to find tools, 2) Use the exact 'name' field from results. DECISION RULE: Use this when the tool name contains: delete, remove, drop, revoke, disable, destroy, purge, reset, clear, unsubscribe, cancel, terminate, close, archive, ban, block, disconnect, kill, wipe, truncate, force, hard. Examples: delete_repo, remove_user, drop_table, revoke_access, clear_cache, terminate_session. Use for irreversible or high-impact operations."
```

New:
```go
description = "Execute a destructive upstream tool. Pass the exact 'server:tool' name from retrieve_tools results. For delete, remove, and other irreversible operations."
```

- [ ] **Step 5: Rewrite search_servers description**

In `internal/server/mcp.go`, replace line 648:

Old:
```go
mcp.WithDescription("🔍 Discover MCP servers from known registries with repository type detection. Search and filter servers from embedded registry list to find new MCP servers that can be added as upstreams. Features npm/PyPI package detection for enhanced install commands. WORKFLOW: 1) Call 'list_registries' first to see available registries, 2) Use this tool with a registry ID to search servers. Results include server URLs and repository information ready for direct use with upstream_servers add command."),
```

New:
```go
mcp.WithDescription("Browse external registries to find and add NEW upstream MCP servers. Not for discovering existing tools — use retrieve_tools for that. Call list_registries first to see available registries, then search within a registry by name or description."),
```

- [ ] **Step 6: Rewrite list_registries description**

In `internal/server/mcp.go`, replace line 671:

Old:
```go
mcp.WithDescription("📋 List all available MCP registries. Use this FIRST to discover which registries you can search with the 'search_servers' tool. Each registry contains different collections of MCP servers that can be added as upstreams."),
```

New:
```go
mcp.WithDescription("List available external MCP server registries for use with search_servers."),
```

- [ ] **Step 7: Run existing tests to verify nothing broke**

Run: `go test ./internal/server/ -run "TestMCP" -v -count=1 -timeout 120s`

Expected: All existing tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/server/mcp.go
git commit -m "feat: rewrite MCP tool descriptions to reduce keyword competition

Remove emoji from all tool descriptions. Shorten call_tool_* descriptions
from ~350 chars to ~120 chars, removing decision-rule keyword lists that
competed with retrieve_tools in ToolSearch ranking. Move decision rules
to usage_instructions (already in retrieve_tools response). Add explicit
'Not for discovering existing tools' to search_servers description.

Addresses production issue where AI agents could not reliably load
retrieve_tools through Claude Code ToolSearch due to keyword overlap
with call_tool_read, search_servers, and list_registries."
```

---

### Task 3: Zero-Results Fallback Catalog

**Files:**
- Modify: `internal/server/mcp.go:1006-1154` (handleRetrieveToolsWithMode — add fallback after search)

- [ ] **Step 1: Add buildServerCatalog helper method**

Add this method to `internal/server/mcp.go`, before `handleRetrieveToolsWithMode`:

```go
// buildServerCatalog returns a catalog of connected servers and their tool names.
// Used as fallback when BM25 search returns zero results.
// Each server entry includes at most maxToolsPerServer tool names.
func (p *MCPProxyServer) buildServerCatalog(ctx context.Context, maxToolsPerServer int) []map[string]interface{} {
	clients := p.upstreamManager.GetAllClients()
	var catalog []map[string]interface{}

	for name, client := range clients {
		cfg := client.GetConfig()
		if cfg == nil || !cfg.Enabled || cfg.Quarantined {
			continue
		}
		if !client.IsConnected() {
			continue
		}

		tools, err := client.ListTools(ctx)
		if err != nil || len(tools) == 0 {
			continue
		}

		toolNames := make([]string, 0, len(tools))
		for _, t := range tools {
			// Extract tool name without server prefix
			toolName := t.Name
			if idx := strings.Index(toolName, ":"); idx >= 0 {
				toolName = toolName[idx+1:]
			}
			toolNames = append(toolNames, toolName)
		}

		entry := map[string]interface{}{
			"server":     name,
			"tool_count": len(toolNames),
		}

		if len(toolNames) > maxToolsPerServer {
			entry["tools"] = toolNames[:maxToolsPerServer]
		} else {
			entry["tools"] = toolNames
		}

		catalog = append(catalog, entry)
	}

	return catalog
}
```

- [ ] **Step 2: Wire fallback into handleRetrieveToolsWithMode**

In `internal/server/mcp.go`, after the search results filtering (around line 1149 where the response map is built), add the fallback logic. Replace the response building block:

Old (lines 1149-1154):
```go
	response := map[string]interface{}{
		"tools":              mcpTools,
		"query":              query,
		"total":              len(results),
		"usage_instructions": usageInstructions,
	}
```

New:
```go
	response := map[string]interface{}{
		"tools":              mcpTools,
		"query":              query,
		"total":              len(results),
		"usage_instructions": usageInstructions,
	}

	// Fallback: when BM25 returns zero results, include a server catalog
	// so the agent can see what tools exist and retry with exact names.
	if len(results) == 0 {
		catalog := p.buildServerCatalog(ctx, 10)
		if len(catalog) > 0 {
			response["fallback"] = "no_results"
			response["hint"] = "No tools matched your query. Browse the catalog below and retry with an exact tool name, or try different keywords."
			response["catalog"] = catalog
		}
	}
```

- [ ] **Step 3: Run all tests**

Run: `go test ./internal/server/ -v -count=1 -timeout 120s && go test ./internal/index/ -run "TestBM25_" -v`

Expected: All tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/server/mcp.go
git commit -m "feat: add server catalog fallback when retrieve_tools finds no results

When BM25 search returns zero results, retrieve_tools now includes a
catalog of all connected servers with their tool names (max 10 per
server). This helps agents discover available tools even when their
search query has no keyword overlap with tool descriptions.

Addresses the dadata discovery problem where agents search for 'company
requisites' but dadata tool 'suggest_party' has Russian-only description
with no English keyword match."
```

---

### Task 4: Verify Full Suite

- [ ] **Step 1: Run full test suite**

Run: `go test ./internal/... -v -count=1 -timeout 300s`

Expected: All tests PASS, no regressions.

- [ ] **Step 2: Build both editions**

```bash
go build ./cmd/mcpproxy
go build -tags server ./cmd/mcpproxy
```

Expected: Both compile without errors.

- [ ] **Step 3: Run linter**

Run: `./scripts/run-linter.sh`

Expected: No new lint warnings.
