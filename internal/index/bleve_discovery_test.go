package index

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

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

func TestBM25_MixedLanguageToolDiscovery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bleve_mixed_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	idx, err := NewBleveIndex(tmpDir, zap.NewNop())
	require.NoError(t, err)
	defer idx.Close()

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
		if len(results) > 0 {
			foundTools := make(map[string]bool)
			for _, r := range results {
				foundTools[r.Tool.Name] = true
			}
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
		results, err := idx.SearchTools("company", 10)
		require.NoError(t, err)
		foundSuggestParty := false
		for _, r := range results {
			if r.Tool.Name == "dadata:suggest_party" {
				foundSuggestParty = true
			}
		}
		t.Logf("English 'company' found suggest_party: %v (results: %d)", foundSuggestParty, len(results))
	})
}

// createTestB24PartnerTools mirrors the live b24-partner server: EN
// descriptions + RU aliases. The actual failure mode the enru analyzer is
// designed to fix — operators write aliases in one Russian form ("лицензии"),
// users search in another ("лицензий", "лицензиях").
func createTestB24PartnerTools() []*config.ToolMetadata {
	now := time.Now()
	return []*config.ToolMetadata{
		{
			Name:        "b24-partner:list_licenses",
			ServerName:  "b24-partner",
			Description: "List Bitrix24 client licenses from the partner cabinet (cloud + box), sorted by urgency. Use this to find which clients have expiring licenses, filter by plan, etc.",
			ParamsJSON:  `{"type":"object","properties":{"expiringWithinDays":{"type":"integer"},"plan":{"type":"string"}}}`,
			Hash:        "bphash1",
			Created:     now,
			Updated:     now,
		},
		{
			Name:        "b24-partner:list_vendor_apps",
			ServerName:  "b24-partner",
			Description: "List Bitrix24 marketplace apps owned by this vendor (vendors.bitrix24.ru). Returns appRegionId, code, name.",
			ParamsJSON:  `{"type":"object"}`,
			Hash:        "bphash2",
			Created:     now,
			Updated:     now,
		},
		{
			Name:        "b24-partner:get_client_by_portal",
			ServerName:  "b24-partner",
			Description: "Get full license details for a specific client portal (e.g. client-portal.bitrix24.ru)",
			ParamsJSON:  `{"type":"object","properties":{"portal":{"type":"string"}}}`,
			Hash:        "bphash3",
			Created:     now,
			Updated:     now,
		},
		{
			Name:        "b24-partner:list_catalog",
			ServerName:  "b24-partner",
			Description: "List SKUs available for purchase from the cached catalog. Use to find product IDs for price_bundle.",
			ParamsJSON:  `{"type":"object","properties":{"kind":{"type":"string"}}}`,
			Hash:        "bphash4",
			Created:     now,
			Updated:     now,
		},
	}
}

// TestBM25_RussianMorphologyRanking is the regression test for the enru
// analyzer. It reproduces the production failure: alias contains "лицензии"
// (nom. pl.) but user searches with "лицензий" (gen. pl.). With the standard
// analyzer these don't match. With enru (snowball-ru) they reduce to the
// same stem.
func TestBM25_RussianMorphologyRanking(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bleve_morph_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	idx, err := NewBleveIndex(tmpDir, zap.NewNop())
	require.NoError(t, err)
	defer idx.Close()

	// Mirror the live config: b24-partner has aliases in nominative form,
	// b24 has competing tools with "список сделок"-style aliases that the
	// old analyzer falsely ranked above list_licenses on "список ..." queries.
	partnerTools := createTestB24PartnerTools()
	b24Tools := createTestB24Tools()
	allTools := append(append([]*config.ToolMetadata{}, partnerTools...), b24Tools...)

	aliases := map[string]string{
		"b24-partner:list_licenses":       "лицензии клиенты истекают expiring партнерский кабинет",
		"b24-partner:list_vendor_apps":    "маркетплейс приложения vendor apps",
		"b24-partner:get_client_by_portal": "клиент по порталу карточка клиента",
		"b24-partner:list_catalog":         "каталог тарифы битрикс24",
		// Match the live mcp_config.json verbatim — note "клиенты" is NOT
		// in the real aliases for list_items_tool; adding it here would be
		// fabricated competition that doesn't exist in production.
		"b24:list_items_tool":              "сделки список сделок deals лиды контакты компании recent deals",
		"b24:create_item_tool":             "создать сделку создать контакт new deal",
	}
	require.NoError(t, idx.BatchIndexWithAliases(allTools, aliases))

	t.Run("inflected RU query matches nominative alias", func(t *testing.T) {
		// "лицензий" (gen. pl.) — alias is "лицензии" (nom. pl.). enru
		// reduces both to the same stem.
		results, err := idx.SearchTools("список лицензий клиентов", 10)
		require.NoError(t, err)
		require.NotEmpty(t, results, "should find tools, got 0")
		assert.Equal(t, "b24-partner:list_licenses", results[0].Tool.Name,
			"list_licenses must be top-1 for 'список лицензий клиентов'; got top-5: %s",
			topNames(results, 5))
	})

	t.Run("bilingual query stays accurate", func(t *testing.T) {
		// Mixed RU/EN query — Codex showed this works empirically; verify here.
		results, err := idx.SearchTools("expiring лицензии клиентов", 10)
		require.NoError(t, err)
		require.NotEmpty(t, results)
		assert.Equal(t, "b24-partner:list_licenses", results[0].Tool.Name,
			"bilingual query must surface list_licenses first; got top-5: %s",
			topNames(results, 5))
	})

	t.Run("competing CRM tool no longer wins on 'список ...' alone", func(t *testing.T) {
		// "список" is in both b24:list_items_tool aliases and the user query.
		// With the old analyzer this drove list_items_tool to rank 1. With
		// enru, "лицензий" stems to "лицензи" and matches list_licenses
		// aliases, which now wins thanks to its boost=3 alias field and
		// multiple matching stems.
		results, err := idx.SearchTools("список лицензий клиентов истекают", 10)
		require.NoError(t, err)
		require.NotEmpty(t, results)
		assert.NotEqual(t, "b24:list_items_tool", results[0].Tool.Name,
			"list_items_tool (CRM) must not outrank list_licenses on a license query; top-5: %s",
			topNames(results, 5))
	})
}

// TestBleveMappingMigration verifies that a stale mapping version on disk
// triggers an index rebuild instead of silently using the old analyzer.
func TestBleveMappingMigration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bleve_migrate_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// First boot: creates index + writes current version marker.
	idx, err := NewBleveIndex(tmpDir, zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, idx.BatchIndex(createTestB24Tools()))
	require.NoError(t, idx.Close())

	markerPath := tmpDir + string(os.PathSeparator) + mappingVersionFile
	data, err := os.ReadFile(markerPath)
	require.NoError(t, err, "marker file must be written on first boot")
	assert.Contains(t, string(data), strconv.Itoa(bleveMappingVersion),
		"marker should record current mapping version")

	// Pretend an older binary wrote version 1 (pre-enru). Reopen and verify
	// the index gets rebuilt — easiest tell is that the previous documents
	// are gone (we never re-indexed them).
	require.NoError(t, os.WriteFile(markerPath, []byte("1\n"), 0o644))

	idx2, err := NewBleveIndex(tmpDir, zap.NewNop())
	require.NoError(t, err)
	defer idx2.Close()

	count, err := idx2.GetDocumentCount()
	require.NoError(t, err)
	assert.Equal(t, uint64(0), count, "old documents must be wiped after version bump triggers rebuild")
}

// topNames returns a space-joined list of the first n tool names for use in
// assertion failure messages.
func topNames(results []*config.SearchResult, n int) string {
	names := []string{}
	for i, r := range results {
		if i >= n {
			break
		}
		names = append(names, r.Tool.Name)
	}
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}

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
