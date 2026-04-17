package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServerConfig_AliasesRoundtrip verifies JSON (de)serialization of the
// new search-aliases / domain-tags / tool-aliases / disable-enrichment fields.
func TestServerConfig_AliasesRoundtrip(t *testing.T) {
	original := &ServerConfig{
		Name:              "b24",
		URL:               "http://b24-mcp:8321/mcp",
		Protocol:          "http",
		Enabled:           true,
		SearchAliases:     []string{"битрикс", "b24", "bitrix24"},
		DomainTags:        []string{"crm", "sales"},
		ToolAliases:       map[string][]string{"list_items_tool": {"сделки", "deals"}},
		DisableEnrichment: true,
	}

	raw, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded ServerConfig
	require.NoError(t, json.Unmarshal(raw, &decoded))

	assert.Equal(t, original.SearchAliases, decoded.SearchAliases)
	assert.Equal(t, original.DomainTags, decoded.DomainTags)
	assert.Equal(t, original.ToolAliases, decoded.ToolAliases)
	assert.True(t, decoded.DisableEnrichment)
}

// TestServerConfig_AliasesOmittedWhenEmpty verifies the JSON emits no
// aliases fields for a config that doesn't use them — prevents churn on
// existing configs during in-place writes.
func TestServerConfig_AliasesOmittedWhenEmpty(t *testing.T) {
	minimal := &ServerConfig{Name: "simple", Enabled: true}
	raw, err := json.Marshal(minimal)
	require.NoError(t, err)

	for _, field := range []string{"search_aliases", "domain_tags", "tool_aliases", "disable_enrichment"} {
		assert.NotContains(t, string(raw), field, "field %q should be omitted when empty", field)
	}
}

// TestCopyServerConfig_AliasesDeepCopy ensures the copy does not alias the
// source's slices / maps — mutating the copy must not mutate the original.
func TestCopyServerConfig_AliasesDeepCopy(t *testing.T) {
	src := &ServerConfig{
		Name:          "b24",
		SearchAliases: []string{"битрикс"},
		DomainTags:    []string{"crm"},
		ToolAliases:   map[string][]string{"list_items_tool": {"сделки"}},
	}

	dst := CopyServerConfig(src)

	// Mutate copy.
	dst.SearchAliases[0] = "MUTATED"
	dst.DomainTags[0] = "MUTATED"
	dst.ToolAliases["list_items_tool"][0] = "MUTATED"
	dst.ToolAliases["new_tool"] = []string{"x"}

	assert.Equal(t, "битрикс", src.SearchAliases[0], "src.SearchAliases must be untouched")
	assert.Equal(t, "crm", src.DomainTags[0], "src.DomainTags must be untouched")
	assert.Equal(t, "сделки", src.ToolAliases["list_items_tool"][0], "src.ToolAliases values must be untouched")
	_, addedToSrc := src.ToolAliases["new_tool"]
	assert.False(t, addedToSrc, "keys added to dst.ToolAliases must not leak into src")
}
