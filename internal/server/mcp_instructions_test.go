package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
)

func TestBuildInstructions_DefaultTemplate_NoServers(t *testing.T) {
	result := buildInstructions("", nil)
	// Default template carries the workflow guidance.
	assert.Contains(t, result, "retrieve_tools")
	assert.Contains(t, result, "call_tool_read")
	assert.Contains(t, result, "upstream_servers")
	// Empty catalog → placeholder is replaced with the empty string, but
	// the surrounding template still renders.
	assert.NotContains(t, result, serverListPlaceholder)
	assert.NotContains(t, result, "Available upstream servers:")
}

func TestBuildInstructions_DefaultTemplate_WithServers(t *testing.T) {
	servers := []*config.ServerConfig{
		{Name: "github", Description: "GitHub repos and issues", Enabled: true},
		{Name: "b24", Description: "Bitrix24 CRM", Enabled: true,
			SearchAliases: []string{"битрикс", "crm"},
			DomainTags:    []string{"crm", "sales"}},
		{Name: "disabled-srv", Description: "should not appear", Enabled: false},
	}
	result := buildInstructions("", servers)
	// Workflow guidance preserved.
	assert.Contains(t, result, "retrieve_tools")
	// Catalog rendered, sorted alphabetically.
	assert.Contains(t, result, "Available upstream servers:")
	assert.Contains(t, result, "- b24 — Bitrix24 CRM")
	assert.Contains(t, result, "[domains: crm, sales]")
	assert.Contains(t, result, "(search: битрикс, crm)")
	assert.Contains(t, result, "- github — GitHub repos and issues")
	// Disabled servers excluded.
	assert.NotContains(t, result, "disabled-srv")
	// Order: b24 before github.
	assert.Less(t, strings.Index(result, "- b24"), strings.Index(result, "- github"))
}

func TestBuildInstructions_CustomWithPlaceholder(t *testing.T) {
	custom := "Project rules.\n\n{{ServerList}}\n\nThanks."
	servers := []*config.ServerConfig{
		{Name: "obsidian", Description: "Personal notes", Enabled: true},
	}
	result := buildInstructions(custom, servers)
	assert.Contains(t, result, "Project rules.")
	assert.Contains(t, result, "Thanks.")
	assert.Contains(t, result, "- obsidian — Personal notes")
	assert.NotContains(t, result, serverListPlaceholder)
	// Default workflow text NOT included when custom template is used.
	assert.NotContains(t, result, "This is mcpproxy-go")
}

func TestBuildInstructions_CustomWithoutPlaceholder_AppendsCatalog(t *testing.T) {
	custom := "Use only the obsidian server."
	servers := []*config.ServerConfig{
		{Name: "obsidian", Description: "Notes", Enabled: true},
	}
	result := buildInstructions(custom, servers)
	assert.True(t, strings.HasPrefix(result, "Use only the obsidian server."))
	assert.Contains(t, result, "Available upstream servers:")
	assert.Contains(t, result, "- obsidian — Notes")
}

func TestBuildInstructions_CustomWithoutPlaceholder_NoServers(t *testing.T) {
	custom := "Static instructions."
	result := buildInstructions(custom, nil)
	// No catalog to append → custom returned verbatim.
	assert.Equal(t, custom, result)
}

func TestBuildServerCatalog_OmitsEmptyOptionalFields(t *testing.T) {
	servers := []*config.ServerConfig{
		{Name: "bare", Enabled: true}, // no description/aliases/tags
	}
	result := buildServerCatalog(servers)
	assert.Equal(t, "Available upstream servers:\n- bare", result)
}

func TestBuildServerCatalog_NilSafe(t *testing.T) {
	assert.Equal(t, "", buildServerCatalog(nil))
	assert.Equal(t, "", buildServerCatalog([]*config.ServerConfig{nil, nil}))
}
