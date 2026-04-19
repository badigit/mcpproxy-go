package index

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
	"github.com/smart-mcp-proxy/mcpproxy-go/internal/storage"
)

func TestCollectAliases_Empty(t *testing.T) {
	assert.Equal(t, "", CollectAliases(nil, "any", nil))
	assert.Equal(t, "", CollectAliases(&config.ServerConfig{Name: "x"}, "any", nil))
}

func TestCollectAliases_ServerLevel(t *testing.T) {
	cfg := &config.ServerConfig{
		Name:          "b24",
		SearchAliases: []string{"битрикс", "b24"},
		DomainTags:    []string{"crm", "sales"},
	}
	got := CollectAliases(cfg, "list_items_tool", nil)
	for _, want := range []string{"битрикс", "b24", "crm", "sales"} {
		assert.True(t, strings.Contains(got, want), "aliases must contain %q, got %q", want, got)
	}
}

func TestCollectAliases_PerToolOverride(t *testing.T) {
	cfg := &config.ServerConfig{
		Name:          "b24",
		SearchAliases: []string{"битрикс"},
		ToolAliases: map[string][]string{
			"list_items_tool":  {"сделки", "deals"},
			"list_tasks_tool":  {"задачи"},
		},
	}
	deals := CollectAliases(cfg, "list_items_tool", nil)
	tasks := CollectAliases(cfg, "list_tasks_tool", nil)

	assert.Contains(t, deals, "сделки")
	assert.Contains(t, deals, "deals")
	assert.NotContains(t, deals, "задачи", "list_items_tool must not inherit list_tasks_tool aliases")

	assert.Contains(t, tasks, "задачи")
	assert.NotContains(t, tasks, "сделки")

	// Both inherit server-level aliases
	assert.Contains(t, deals, "битрикс")
	assert.Contains(t, tasks, "битрикс")
}

func TestCollectAliases_EnrichmentMerge(t *testing.T) {
	cfg := &config.ServerConfig{
		Name:          "b24",
		SearchAliases: []string{"b24"},
	}
	enriched := &storage.EnrichedToolMeta{
		Keywords:       []string{"recent deals", "last CRM items"},
		ExampleQueries: []string{"список сделок за неделю"},
		Domain:         "crm",
	}

	got := CollectAliases(cfg, "list_items_tool", enriched)
	for _, want := range []string{"b24", "recent deals", "last CRM items", "список сделок", "crm"} {
		assert.Contains(t, got, want)
	}
}

func TestResolveDomain_Priority(t *testing.T) {
	cfg := &config.ServerConfig{DomainTags: []string{"crm", "sales"}}
	enriched := &storage.EnrichedToolMeta{Domain: "reporting"}

	// Enriched wins over config
	assert.Equal(t, "reporting", ResolveDomain(cfg, enriched))

	// Config first entry used when no enrichment
	assert.Equal(t, "crm", ResolveDomain(cfg, nil))

	// Enriched empty → fall back to config
	assert.Equal(t, "crm", ResolveDomain(cfg, &storage.EnrichedToolMeta{Domain: ""}))

	// No config, no enrichment → empty
	assert.Equal(t, "", ResolveDomain(nil, nil))
	assert.Equal(t, "", ResolveDomain(&config.ServerConfig{}, nil))
}
