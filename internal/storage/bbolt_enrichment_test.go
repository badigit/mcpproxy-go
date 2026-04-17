package storage

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupEnrichmentStorage(t *testing.T) (*Manager, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "enrichment_test_*")
	require.NoError(t, err)

	logger := zap.NewNop().Sugar()
	manager, err := NewManager(tmpDir, logger)
	require.NoError(t, err)

	cleanup := func() {
		manager.Close()
		os.RemoveAll(tmpDir)
	}

	return manager, cleanup
}

func TestEnrichment_SaveAndGet_Roundtrip(t *testing.T) {
	mgr, cleanup := setupEnrichmentStorage(t)
	defer cleanup()

	now := time.Now().UTC().Truncate(time.Millisecond)
	desc := "List CRM items for any entity type (deals, contacts, companies)"
	hash := HashDescription(desc)

	record := &EnrichedToolMeta{
		ServerName:      "b24",
		ToolName:        "list_items_tool",
		DescriptionHash: hash,
		Keywords:        []string{"сделки", "deals", "contacts", "контакты"},
		ExampleQueries:  []string{"список сделок", "recent deals"},
		Domain:          "crm",
		PromptVersion:   1,
		Model:           "gpt-4o-mini",
		CreatedAt:       now,
	}

	require.NoError(t, mgr.SaveToolEnrichment(record))

	got, ok, err := mgr.GetToolEnrichment("b24", "list_items_tool", hash, 1)
	require.NoError(t, err)
	require.True(t, ok, "expected cache hit on matching hash+version")

	assert.Equal(t, record.Keywords, got.Keywords)
	assert.Equal(t, record.ExampleQueries, got.ExampleQueries)
	assert.Equal(t, "crm", got.Domain)
	assert.Equal(t, 1, got.PromptVersion)
	assert.Equal(t, hash, got.DescriptionHash)
}

func TestEnrichment_MissOnDescriptionChange(t *testing.T) {
	mgr, cleanup := setupEnrichmentStorage(t)
	defer cleanup()

	origHash := HashDescription("original description")
	newHash := HashDescription("rewritten description")
	require.NotEqual(t, origHash, newHash)

	require.NoError(t, mgr.SaveToolEnrichment(&EnrichedToolMeta{
		ServerName:      "b24",
		ToolName:        "list_items_tool",
		DescriptionHash: origHash,
		PromptVersion:   1,
		CreatedAt:       time.Now(),
	}))

	_, ok, err := mgr.GetToolEnrichment("b24", "list_items_tool", newHash, 1)
	require.NoError(t, err)
	assert.False(t, ok, "cache must miss when description hash differs (rewrite scenario)")
}

func TestEnrichment_MissOnPromptVersionBump(t *testing.T) {
	mgr, cleanup := setupEnrichmentStorage(t)
	defer cleanup()

	hash := HashDescription("some desc")
	require.NoError(t, mgr.SaveToolEnrichment(&EnrichedToolMeta{
		ServerName:      "b24",
		ToolName:        "list_items_tool",
		DescriptionHash: hash,
		PromptVersion:   1,
		CreatedAt:       time.Now(),
	}))

	_, ok, err := mgr.GetToolEnrichment("b24", "list_items_tool", hash, 2)
	require.NoError(t, err)
	assert.False(t, ok, "cache must miss when prompt version is bumped (global invalidation)")
}

func TestEnrichment_MissOnUnknownTool(t *testing.T) {
	mgr, cleanup := setupEnrichmentStorage(t)
	defer cleanup()

	_, ok, err := mgr.GetToolEnrichment("b24", "never_saved", "somehash", 1)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestEnrichment_ListScopedToServer(t *testing.T) {
	mgr, cleanup := setupEnrichmentStorage(t)
	defer cleanup()

	save := func(server, tool string) {
		t.Helper()
		require.NoError(t, mgr.SaveToolEnrichment(&EnrichedToolMeta{
			ServerName:      server,
			ToolName:        tool,
			DescriptionHash: HashDescription(server + ":" + tool),
			PromptVersion:   1,
			CreatedAt:       time.Now(),
		}))
	}
	save("b24", "list_items_tool")
	save("b24", "create_item_tool")
	save("dadata", "find_party")

	all, err := mgr.ListToolEnrichments("")
	require.NoError(t, err)
	assert.Len(t, all, 3)

	only24, err := mgr.ListToolEnrichments("b24")
	require.NoError(t, err)
	assert.Len(t, only24, 2)
	for _, r := range only24 {
		assert.Equal(t, "b24", r.ServerName)
	}
}

func TestEnrichment_DeleteServer(t *testing.T) {
	mgr, cleanup := setupEnrichmentStorage(t)
	defer cleanup()

	require.NoError(t, mgr.SaveToolEnrichment(&EnrichedToolMeta{
		ServerName:      "b24",
		ToolName:        "a",
		DescriptionHash: "h1",
		PromptVersion:   1,
		CreatedAt:       time.Now(),
	}))
	require.NoError(t, mgr.SaveToolEnrichment(&EnrichedToolMeta{
		ServerName:      "b24",
		ToolName:        "b",
		DescriptionHash: "h2",
		PromptVersion:   1,
		CreatedAt:       time.Now(),
	}))
	require.NoError(t, mgr.SaveToolEnrichment(&EnrichedToolMeta{
		ServerName:      "dadata",
		ToolName:        "c",
		DescriptionHash: "h3",
		PromptVersion:   1,
		CreatedAt:       time.Now(),
	}))

	require.NoError(t, mgr.DeleteServerToolEnrichments("b24"))

	all, err := mgr.ListToolEnrichments("")
	require.NoError(t, err)
	assert.Len(t, all, 1)
	assert.Equal(t, "dadata", all[0].ServerName)
}

func TestHashDescription_Deterministic(t *testing.T) {
	a := HashDescription("hello world")
	b := HashDescription("hello world")
	c := HashDescription("hello world!")
	assert.Equal(t, a, b, "same input must hash identically")
	assert.NotEqual(t, a, c)
	assert.Len(t, a, 64, "sha256 hex must be 64 chars")
}
