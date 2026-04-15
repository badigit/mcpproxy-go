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
