package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
)

// TestSplitExactToolName covers the parser used by the exact server:tool resolution
// branch (Bug: retrieve-tools-exact-name-lookup-returns-no-results).
func TestSplitExactToolName(t *testing.T) {
	tests := []struct {
		query      string
		wantOK     bool
		wantServer string
		wantTool   string
	}{
		{"obsidian:search_notes", true, "obsidian", "search_notes"},
		{"  obsidian:search_notes  ", true, "obsidian", "search_notes"},
		{"b24:list_items_tool", true, "b24", "list_items_tool"},
		// Natural-language queries with a colon are NOT exact tool names.
		{"list events: today", false, "", ""},
		{"search notes read note", false, "", ""},
		// Malformed / ambiguous forms.
		{"nocolon", false, "", ""},
		{":search_notes", false, "", ""},
		{"obsidian:", false, "", ""},
		{"a:b:c", false, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			server, tool, ok := splitExactToolName(tt.query)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantServer, server)
			assert.Equal(t, tt.wantTool, tool)
		})
	}
}

// indexObsidianSearchNotes indexes a single read-only obsidian tool that carries NO
// annotations object, mirroring the mcpvault/obsidian upstream from the bug report.
func indexObsidianSearchNotes(t *testing.T, proxy *MCPProxyServer) {
	t.Helper()
	now := time.Now()
	tools := []*config.ToolMetadata{
		{
			Name:        "obsidian:search_notes",
			ServerName:  "obsidian",
			Description: "Search notes in the Obsidian vault by query",
			ParamsJSON:  `{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`,
			Hash:        "obs-search-1",
			Created:     now,
			Updated:     now,
			// Annotations deliberately nil — upstream emits none.
		},
		{
			Name:        "obsidian:create_note",
			ServerName:  "obsidian",
			Description: "Create a new note in the Obsidian vault",
			ParamsJSON:  `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`,
			Hash:        "obs-create-1",
			Created:     now,
			Updated:     now,
		},
	}
	require.NoError(t, proxy.index.BatchIndexTools(tools))
}

// retrieveTools is a small helper that invokes the retrieve_tools handler and decodes
// its JSON response.
func retrieveTools(t *testing.T, proxy *MCPProxyServer, args map[string]interface{}) map[string]interface{} {
	t.Helper()
	request := mcp.CallToolRequest{}
	request.Params.Arguments = args

	result, err := proxy.handleRetrieveTools(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError, "retrieve_tools should not error")
	require.NotEmpty(t, result.Content)

	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok, "expected text content")

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(text.Text), &resp))
	return resp
}

func toolNamesFromResponse(resp map[string]interface{}) []string {
	var names []string
	if tools, ok := resp["tools"].([]interface{}); ok {
		for _, tr := range tools {
			if tm, ok := tr.(map[string]interface{}); ok {
				if name, ok := tm["name"].(string); ok {
					names = append(names, name)
				}
			}
		}
	}
	return names
}

// TestRetrieveTools_ExactName_ResolvesIndexedTool is the primary regression for
// retrieve-tools-exact-name-lookup-returns-no-results: an exact "server:tool" query
// must return the indexed tool even though BM25 phrase ranking alone misses it.
func TestRetrieveTools_ExactName_ResolvesIndexedTool(t *testing.T) {
	proxy := createTestMCPProxyServer(t)
	indexObsidianSearchNotes(t, proxy)

	resp := retrieveTools(t, proxy, map[string]interface{}{
		"query": "obsidian:search_notes",
	})

	names := toolNamesFromResponse(resp)
	assert.Contains(t, names, "obsidian:search_notes",
		"exact server:tool query must resolve the indexed tool")
	assert.Equal(t, "obsidian:search_notes", names[0],
		"exact match should be ranked first")
}

// TestRetrieveTools_ExactName_WithAnnotationFilters verifies the exact-name result
// still survives read_only_only (the tool is read-only by name even with no
// annotations) and is dropped only when it would genuinely be filtered out.
func TestRetrieveTools_ExactName_WithAnnotationFilters(t *testing.T) {
	proxy := createTestMCPProxyServer(t)
	indexObsidianSearchNotes(t, proxy)

	// read_only_only: search_notes is read-only by verb classification → kept.
	resp := retrieveTools(t, proxy, map[string]interface{}{
		"query":          "obsidian:search_notes",
		"read_only_only": true,
	})
	assert.Contains(t, toolNamesFromResponse(resp), "obsidian:search_notes",
		"read-only exact tool must survive read_only_only filter")

	// A write tool requested exactly under read_only_only must be excluded.
	resp = retrieveTools(t, proxy, map[string]interface{}{
		"query":          "obsidian:create_note",
		"read_only_only": true,
	})
	assert.NotContains(t, toolNamesFromResponse(resp), "obsidian:create_note",
		"write exact tool must be excluded by read_only_only filter")
}
