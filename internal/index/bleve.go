package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/custom"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/keyword"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/standard"
	"github.com/blevesearch/bleve/v2/analysis/lang/en"
	"github.com/blevesearch/bleve/v2/analysis/lang/ru"
	"github.com/blevesearch/bleve/v2/analysis/token/lowercase"
	"github.com/blevesearch/bleve/v2/analysis/tokenizer/unicode"
	"go.uber.org/zap"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
)

// enruAnalyzerName is the custom analyzer chained as
//
//	unicode tokenizer → lowercase → stop_en → stop_ru → stemmer_en → stemmer_ru.
//
// Applied to all text fields that may contain Russian or English content
// (description, aliases, searchable_text, tags). This fixes BM25 ranking on
// inflected Russian queries — "лицензий" and "лицензии" reduce to the same
// stem and now match. English words pass through Porter/Snowball stemming
// (-s, -ed, -ing) which keeps EN matching at parity with the previous
// standard analyzer for typical tool descriptions.
const enruAnalyzerName = "enru"

// bleveMappingVersion identifies the current field-mapping schema. Bumped
// when analyzers or field types change — older indexes are detected via the
// version marker file and rebuilt from scratch on next startup. Version 1 =
// pre-enru (standard analyzer on text fields). Version 2 = enru analyzer.
// Version 3 = stored alias_hash field (differential re-index on alias edits).
const bleveMappingVersion = 3

// mappingVersionFile is the sentinel written next to the bleve directory.
// We deliberately do NOT put it inside index.bleve/ so wiping the index
// directory does not also drop the marker (we want the marker to survive
// only as long as the index it describes).
const mappingVersionFile = "index.bleve.mapping_version"

// BleveIndex wraps Bleve index operations
type BleveIndex struct {
	index  bleve.Index
	logger *zap.Logger
}

// ToolDocument represents a tool document in the index
type ToolDocument struct {
	ToolName     string `json:"tool_name"`      // Just the tool name (without server prefix)
	FullToolName string `json:"full_tool_name"` // Complete server:tool format
	ServerName   string `json:"server_name"`
	Description  string `json:"description"`
	ParamsJSON   string `json:"params_json"`
	Hash         string `json:"hash"`
	Tags         string `json:"tags"`
	// Aliases collects keywords from per-server SearchAliases, per-tool
	// ToolAliases, DomainTags, and LLM enrichment (keywords +
	// example_queries). Indexed with boost=3 via SearchTools so a match
	// here outranks a match in Description. Stored so we can return it
	// to debug consumers; empty when no aliases are configured.
	Aliases string `json:"aliases,omitempty"`
	// AliasHash fingerprints Aliases (see index.AliasHash). Stored but not
	// indexed: it exists so the runtime's differential update can notice an
	// aliases-only change and re-index the tool even when its upstream hash
	// is untouched. Empty when the tool has no aliases.
	AliasHash      string `json:"alias_hash,omitempty"`
	SearchableText string `json:"searchable_text"` // Combined searchable content
}

// NewBleveIndex creates a new Bleve index, rebuilding it from scratch when the
// stored mapping version does not match bleveMappingVersion. Rebuilds are safe:
// upstream tools are re-indexed automatically by the runtime on startup, so
// dropping the directory only costs a few seconds of cold-start latency.
func NewBleveIndex(dataDir string, logger *zap.Logger) (*BleveIndex, error) {
	indexPath := filepath.Join(dataDir, "index.bleve")
	versionPath := filepath.Join(dataDir, mappingVersionFile)

	// If an index already exists, check whether its mapping version matches.
	// A mismatch (or a missing marker on an existing index) means the schema
	// changed since this index was built — drop it so we can recreate with
	// the current mapping.
	if _, err := os.Stat(indexPath); err == nil {
		storedVersion, vErr := readMappingVersion(versionPath)
		if vErr != nil || storedVersion != bleveMappingVersion {
			logger.Info("Bleve mapping version mismatch — rebuilding index",
				zap.Int("stored_version", storedVersion),
				zap.Int("expected_version", bleveMappingVersion),
				zap.String("path", indexPath),
				zap.NamedError("marker_error", vErr),
			)
			if rmErr := os.RemoveAll(indexPath); rmErr != nil {
				return nil, fmt.Errorf("failed to remove stale bleve index for migration: %w", rmErr)
			}
			// Also drop any stale marker so the rebuild path below writes a fresh one.
			_ = os.Remove(versionPath)
		}
	}

	// Try to open existing index
	index, err := bleve.Open(indexPath)
	if err != nil {
		// If index doesn't exist, create a new one
		logger.Info("Creating new Bleve index", zap.String("path", indexPath))
		index, err = createBleveIndex(indexPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create Bleve index: %w", err)
		}
		if wErr := writeMappingVersion(versionPath, bleveMappingVersion); wErr != nil {
			// Non-fatal: index is usable. We'll just rebuild again on next start.
			logger.Warn("Failed to write bleve mapping version marker — index will rebuild next start",
				zap.String("path", versionPath), zap.Error(wErr))
		}
	} else {
		logger.Info("Opened existing Bleve index", zap.String("path", indexPath))
	}

	return &BleveIndex{
		index:  index,
		logger: logger,
	}, nil
}

// readMappingVersion returns the integer version stored in the marker file,
// or (0, error) when the file is missing or unparseable. We treat any error
// as "unknown version" → triggers a rebuild, which is the safe default.
func readMappingVersion(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var version int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &version); err != nil {
		return 0, fmt.Errorf("parse mapping version: %w", err)
	}
	return version, nil
}

func writeMappingVersion(path string, version int) error {
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", version)), 0o644)
}

// createBleveIndex creates a new Bleve index with proper mapping
func createBleveIndex(indexPath string) (bleve.Index, error) {
	// Create index mapping
	indexMapping := bleve.NewIndexMapping()

	// Register the bilingual analyzer BEFORE field mappings reference it,
	// otherwise AddFieldMappingsAt → ValidateCustomAnalyzer would not find it.
	if err := indexMapping.AddCustomAnalyzer(enruAnalyzerName, map[string]interface{}{
		"type":      custom.Name,
		"tokenizer": unicode.Name,
		"token_filters": []string{
			lowercase.Name,
			en.StopName,
			ru.StopName,
			en.SnowballStemmerName,
			ru.SnowballStemmerName,
		},
	}); err != nil {
		return nil, fmt.Errorf("register enru analyzer: %w", err)
	}

	// Create document mapping for tools
	toolMapping := bleve.NewDocumentMapping()

	// Tool name field (both keyword and standard analyzers for different search types)
	toolNameFieldKeyword := bleve.NewTextFieldMapping()
	toolNameFieldKeyword.Analyzer = keyword.Name
	toolNameFieldKeyword.Store = true
	toolNameFieldKeyword.Index = true
	toolMapping.AddFieldMappingsAt("tool_name", toolNameFieldKeyword)

	// Full tool name field (keyword analyzer for exact matches)
	fullToolNameField := bleve.NewTextFieldMapping()
	fullToolNameField.Analyzer = keyword.Name
	fullToolNameField.Store = true
	fullToolNameField.Index = true
	toolMapping.AddFieldMappingsAt("full_tool_name", fullToolNameField)

	// Server name field (keyword analyzer)
	serverNameField := bleve.NewTextFieldMapping()
	serverNameField.Analyzer = keyword.Name
	serverNameField.Store = true
	serverNameField.Index = true
	toolMapping.AddFieldMappingsAt("server_name", serverNameField)

	// Description field — bilingual analyzer for full-text search across EN/RU.
	descriptionField := bleve.NewTextFieldMapping()
	descriptionField.Analyzer = enruAnalyzerName
	descriptionField.Store = true
	descriptionField.Index = true
	toolMapping.AddFieldMappingsAt("description", descriptionField)

	// Parameters JSON field — kept on standard analyzer. These are API
	// parameter names ("limit", "offset", etc.), always ASCII, and stemming
	// could distort them into less-useful tokens.
	paramsField := bleve.NewTextFieldMapping()
	paramsField.Analyzer = standard.Name
	paramsField.Store = true
	paramsField.Index = true
	toolMapping.AddFieldMappingsAt("params_json", paramsField)

	// Hash field (keyword analyzer)
	hashField := bleve.NewTextFieldMapping()
	hashField.Analyzer = keyword.Name
	hashField.Store = true
	hashField.Index = false // Don't index hash for search
	toolMapping.AddFieldMappingsAt("hash", hashField)

	// Alias hash field (keyword analyzer, stored only). Used by the runtime
	// to detect aliases-only changes; never queried by users.
	aliasHashField := bleve.NewTextFieldMapping()
	aliasHashField.Analyzer = keyword.Name
	aliasHashField.Store = true
	aliasHashField.Index = false
	toolMapping.AddFieldMappingsAt("alias_hash", aliasHashField)

	// Tags field — bilingual analyzer (operators may put russian domain tags).
	tagsField := bleve.NewTextFieldMapping()
	tagsField.Analyzer = enruAnalyzerName
	tagsField.Store = true
	tagsField.Index = true
	toolMapping.AddFieldMappingsAt("tags", tagsField)

	// Aliases field — user-configured search aliases and LLM-enriched
	// keywords/example_queries. Bilingual analyzer so RU aliases match
	// inflected user queries ("лицензий" → "лицензи" matches "лицензии").
	// Boost is applied at query time (see SearchTools), not here.
	aliasesField := bleve.NewTextFieldMapping()
	aliasesField.Analyzer = enruAnalyzerName
	aliasesField.Store = true
	aliasesField.Index = true
	toolMapping.AddFieldMappingsAt("aliases", aliasesField)

	// Searchable text field — bilingual analyzer. Combines tool name,
	// description, params and aliases, so the analyzer choice must match
	// what is used for the more specific fields.
	searchableTextField := bleve.NewTextFieldMapping()
	searchableTextField.Analyzer = enruAnalyzerName
	searchableTextField.Store = false // Don't store, just index for search
	searchableTextField.Index = true
	toolMapping.AddFieldMappingsAt("searchable_text", searchableTextField)

	// Add document mapping to index
	indexMapping.AddDocumentMapping("tool", toolMapping)
	indexMapping.DefaultMapping = toolMapping

	// Create the index
	return bleve.New(indexPath, indexMapping)
}

// Close closes the index
func (b *BleveIndex) Close() error {
	return b.index.Close()
}

// IndexTool indexes a tool document with no aliases. Prefer
// IndexToolWithAliases when the caller has per-server / per-tool aliases
// (spec 2026-04-17). Kept for backward compatibility with existing callers.
func (b *BleveIndex) IndexTool(toolMeta *config.ToolMetadata) error {
	return b.IndexToolWithAliases(toolMeta, "")
}

// IndexToolWithAliases indexes a tool document, attaching the supplied
// aliases string. Aliases are space-separated keywords merged from
// ServerConfig.SearchAliases, ServerConfig.ToolAliases[<tool>], and any
// LLM enrichment cached for this tool.
func (b *BleveIndex) IndexToolWithAliases(toolMeta *config.ToolMetadata, aliases string) error {
	// Extract just the tool name (remove server prefix)
	toolName := toolMeta.Name
	if parts := strings.SplitN(toolMeta.Name, ":", 2); len(parts) == 2 {
		toolName = parts[1]
	}

	// Create combined searchable text for better full-text search
	searchableText := fmt.Sprintf("%s %s %s %s %s",
		toolName,
		toolMeta.Name,
		toolMeta.Description,
		toolMeta.ParamsJSON,
		aliases)

	doc := &ToolDocument{
		ToolName:       toolName,
		FullToolName:   toolMeta.Name,
		ServerName:     toolMeta.ServerName,
		Description:    toolMeta.Description,
		ParamsJSON:     toolMeta.ParamsJSON,
		Hash:           toolMeta.Hash,
		Tags:           "",
		Aliases:        aliases,
		AliasHash:      AliasHash(aliases),
		SearchableText: searchableText,
	}

	// Use server:tool format as document ID for uniqueness
	docID := fmt.Sprintf("%s:%s", toolMeta.ServerName, toolName)

	b.logger.Debug("Indexing tool", zap.String("doc_id", docID), zap.String("tool_name", toolName))
	return b.index.Index(docID, doc)
}

// DeleteTool removes a tool from the index
func (b *BleveIndex) DeleteTool(serverName, toolName string) error {
	docID := fmt.Sprintf("%s:%s", serverName, toolName)

	b.logger.Debug("Deleting tool from index", zap.String("doc_id", docID))
	return b.index.Delete(docID)
}

// DeleteServerTools removes all tools from a specific server
func (b *BleveIndex) DeleteServerTools(serverName string) error {
	// Search for all tools from this server
	query := bleve.NewTermQuery(serverName)
	query.SetField("server_name")

	searchReq := bleve.NewSearchRequest(query)
	searchReq.Size = 1000 // Assume max 1000 tools per server
	searchReq.Fields = []string{"tool_name", "server_name"}

	searchResult, err := b.index.Search(searchReq)
	if err != nil {
		return fmt.Errorf("failed to search for server tools: %w", err)
	}

	// Delete each tool
	for _, hit := range searchResult.Hits {
		if err := b.index.Delete(hit.ID); err != nil {
			b.logger.Warn("Failed to delete tool", zap.String("tool_id", hit.ID), zap.Error(err))
		}
	}

	b.logger.Info("Deleted tools from server",
		zap.Int("count", len(searchResult.Hits)),
		zap.String("server", serverName))
	return nil
}

// SearchTools searches for tools using multiple query strategies for better results
func (b *BleveIndex) SearchTools(queryStr string, limit int) ([]*config.SearchResult, error) {
	if queryStr == "" {
		return nil, fmt.Errorf("search query cannot be empty")
	}

	// Create a boolean query to combine multiple search strategies
	boolQuery := bleve.NewBooleanQuery()

	// 1. Exact match on tool name (highest priority)
	exactToolNameQuery := bleve.NewTermQuery(queryStr)
	exactToolNameQuery.SetField("tool_name")
	exactToolNameQuery.SetBoost(5.0)
	boolQuery.AddShould(exactToolNameQuery)

	// 2. Exact match on full tool name
	exactFullToolNameQuery := bleve.NewTermQuery(queryStr)
	exactFullToolNameQuery.SetField("full_tool_name")
	exactFullToolNameQuery.SetBoost(4.0)
	boolQuery.AddShould(exactFullToolNameQuery)

	// 3. Prefix match on tool name for partial matches
	prefixToolNameQuery := bleve.NewPrefixQuery(queryStr)
	prefixToolNameQuery.SetField("tool_name")
	prefixToolNameQuery.SetBoost(3.0)
	boolQuery.AddShould(prefixToolNameQuery)

	// 4. Wildcard search for underscore-separated terms
	if strings.Contains(queryStr, "_") {
		wildcardQuery := bleve.NewWildcardQuery("*" + queryStr + "*")
		wildcardQuery.SetField("tool_name")
		wildcardQuery.SetBoost(2.5)
		boolQuery.AddShould(wildcardQuery)
	}

	// 5. Full-text search across all fields
	matchQuery := bleve.NewMatchQuery(queryStr)
	matchQuery.SetBoost(1.0)
	boolQuery.AddShould(matchQuery)

	// 6. Search in combined searchable text
	searchableTextQuery := bleve.NewMatchQuery(queryStr)
	searchableTextQuery.SetField("searchable_text")
	searchableTextQuery.SetBoost(1.5)
	boolQuery.AddShould(searchableTextQuery)

	// 7. Aliases — user-configured search keywords (incl. cross-language)
	// and LLM-enriched synonyms/example_queries. Boost higher than plain
	// description so an alias hit wins over coincidental description hits.
	aliasesQuery := bleve.NewMatchQuery(queryStr)
	aliasesQuery.SetField("aliases")
	aliasesQuery.SetBoost(3.0)
	boolQuery.AddShould(aliasesQuery)

	// Create search request
	searchReq := bleve.NewSearchRequest(boolQuery)
	searchReq.Size = limit
	searchReq.Fields = []string{"tool_name", "full_tool_name", "server_name", "description", "params_json", "hash", "aliases"}
	searchReq.Highlight = bleve.NewHighlight()

	b.logger.Debug("Searching tools with enhanced query", zap.String("query", queryStr), zap.Int("limit", limit))

	searchResult, err := b.index.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Convert results
	var results []*config.SearchResult
	for _, hit := range searchResult.Hits {
		toolMeta := &config.ToolMetadata{
			Name:        getStringField(hit.Fields, "full_tool_name"),
			ServerName:  getStringField(hit.Fields, "server_name"),
			Description: getStringField(hit.Fields, "description"),
			ParamsJSON:  getStringField(hit.Fields, "params_json"),
			Hash:        getStringField(hit.Fields, "hash"),
		}

		results = append(results, &config.SearchResult{
			Tool:  toolMeta,
			Score: hit.Score,
		})
	}

	b.logger.Debug("Found tools matching query", zap.Int("count", len(results)), zap.String("query", queryStr))
	return results, nil
}

// GetDocumentCount returns the number of documents in the index
func (b *BleveIndex) GetDocumentCount() (uint64, error) {
	return b.index.DocCount()
}

// Batch operations for efficiency

// BatchIndex indexes multiple tools in a single batch with no aliases.
// Kept for backward compatibility; prefer BatchIndexWithAliases.
func (b *BleveIndex) BatchIndex(tools []*config.ToolMetadata) error {
	return b.BatchIndexWithAliases(tools, nil)
}

// BatchIndexWithAliases indexes multiple tools in a single batch, looking
// up each tool's aliases in aliasesByFullName (key = "<server>:<tool>").
// Pass nil or an empty map to index without aliases.
func (b *BleveIndex) BatchIndexWithAliases(tools []*config.ToolMetadata, aliasesByFullName map[string]string) error {
	batch := b.index.NewBatch()

	for _, toolMeta := range tools {
		// Extract just the tool name (remove server prefix)
		toolName := toolMeta.Name
		if parts := strings.SplitN(toolMeta.Name, ":", 2); len(parts) == 2 {
			toolName = parts[1]
		}

		aliases := ""
		if aliasesByFullName != nil {
			aliases = aliasesByFullName[toolMeta.Name]
		}

		// Create combined searchable text
		searchableText := fmt.Sprintf("%s %s %s %s %s",
			toolName,
			toolMeta.Name,
			toolMeta.Description,
			toolMeta.ParamsJSON,
			aliases)

		doc := &ToolDocument{
			ToolName:       toolName,
			FullToolName:   toolMeta.Name,
			ServerName:     toolMeta.ServerName,
			Description:    toolMeta.Description,
			ParamsJSON:     toolMeta.ParamsJSON,
			Hash:           toolMeta.Hash,
			Tags:           "",
			Aliases:        aliases,
			AliasHash:      AliasHash(aliases),
			SearchableText: searchableText,
		}

		docID := fmt.Sprintf("%s:%s", toolMeta.ServerName, toolName)
		_ = batch.Index(docID, doc)
	}

	b.logger.Debug("Batch indexing tools", zap.Int("count", len(tools)))
	return b.index.Batch(batch)
}

// RebuildIndex rebuilds the entire index
func (b *BleveIndex) RebuildIndex() error {
	// Get index stats before rebuild
	count, _ := b.index.DocCount()
	b.logger.Info("Rebuilding index", zap.Uint64("current_docs", count))

	// For now, we'll just log the operation
	// In a full implementation, this would:
	// 1. Create a new index
	// 2. Re-index all tools from storage
	// 3. Atomically swap indices

	return nil
}

// GetToolsByServer retrieves all tools from a specific server
func (b *BleveIndex) GetToolsByServer(serverName string) ([]*config.ToolMetadata, error) {
	// Create a term query for the server name
	query := bleve.NewTermQuery(serverName)
	query.SetField("server_name")

	// Create search request with high limit to get all tools
	searchReq := bleve.NewSearchRequest(query)
	searchReq.Size = 10000 // Maximum tools per server
	searchReq.Fields = []string{"tool_name", "full_tool_name", "server_name", "description", "params_json", "hash", "alias_hash"}

	b.logger.Debug("Querying tools by server", zap.String("server", serverName))

	searchResult, err := b.index.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("failed to query tools by server: %w", err)
	}

	// Convert results to ToolMetadata
	var tools []*config.ToolMetadata
	for _, hit := range searchResult.Hits {
		toolMeta := &config.ToolMetadata{
			Name:        getStringField(hit.Fields, "full_tool_name"),
			ServerName:  getStringField(hit.Fields, "server_name"),
			Description: getStringField(hit.Fields, "description"),
			ParamsJSON:  getStringField(hit.Fields, "params_json"),
			Hash:        getStringField(hit.Fields, "hash"),
			AliasHash:   getStringField(hit.Fields, "alias_hash"),
		}
		tools = append(tools, toolMeta)
	}

	b.logger.Debug("Found tools for server",
		zap.String("server", serverName),
		zap.Int("count", len(tools)))

	return tools, nil
}

// GetAllIndexedServerNames returns the unique set of server names present in the index.
func (b *BleveIndex) GetAllIndexedServerNames() ([]string, error) {
	// Use a MatchAll query to scan every document, requesting only the server_name field
	query := bleve.NewMatchAllQuery()
	searchReq := bleve.NewSearchRequest(query)
	searchReq.Size = 0 // We only need facets, not results

	// Add a facet on server_name to get unique values
	facet := bleve.NewFacetRequest("server_name", 10000) // generous upper bound
	searchReq.AddFacet("servers", facet)

	searchResult, err := b.index.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("failed to query indexed server names: %w", err)
	}

	facetResult, ok := searchResult.Facets["servers"]
	if !ok {
		return nil, nil // no facet result means no documents
	}

	var names []string
	for _, term := range facetResult.Terms.Terms() {
		names = append(names, term.Term)
	}

	b.logger.Debug("Retrieved indexed server names",
		zap.Int("count", len(names)))
	return names, nil
}

// Helper function to get string field from search results
func getStringField(fields map[string]interface{}, fieldName string) string {
	if val, ok := fields[fieldName]; ok {
		if strVal, ok := val.(string); ok {
			return strVal
		}
	}
	return ""
}
