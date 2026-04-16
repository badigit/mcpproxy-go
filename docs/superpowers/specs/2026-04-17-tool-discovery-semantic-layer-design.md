# Tool Discovery Semantic Layer — Design Spec

**Date**: 2026-04-17
**Status**: Draft
**Scope**: Fork-only (`badigit/mcpproxy-go`), personal edition. No `//go:build server` code.
**Motivation**: Follow-up to `2026-04-15-tool-discovery-improvements-design.md`. Fallback catalog alone is insufficient — agents still can't find domain-specific tools via BM25 when descriptions and user queries are in different languages or use different vocabularies.

## Problem Recap (after 2026-04-15 rollout)

Observed on prod (`dimba-mcp-gateway`, commit `eacdce1`):

| Query | BM25 result | What user wanted |
|-------|-------------|-------------------|
| `"последние сделки битрикс24"` | `total: 0`, catalog shown | `b24:list_items_tool` with entity_type_id=2 |
| `"b24 deal list"` | `list_crm_statuses_tool` ranked first (description mentions `DEAL_*` 8 times) | Same `list_items_tool` |
| `"company requisites INN"` | unclear | `b24:list_requisites_tool` / `dadata:find_party` |

Root causes unaddressed by prior spec:

1. **No cross-language indexing.** Russian queries hit English descriptions. Catalog fallback is a consolation prize, not a fix.
2. **Universal tools lose to chatty tools.** `list_items_tool(entity_type_id)` has a minimal description; `list_crm_statuses_tool` enumerates every DEAL/CONTACT/LEAD reference book. BM25 rewards verbose descriptions.
3. **No domain hierarchy.** Agent sees a flat list of 130+ tools from 8 servers. MCP spec has no domain namespace; we must add one at the proxy level.

Constraint: prod is a resource-constrained VPS. No local ML models, no persistent embeddings. Every runtime operation is BM25 over bleve; only optional bootstrap-time work may call an external LLM.

## Solution Overview

Four layers added on top of existing BM25. First three work offline. LLM-based layer is opt-in and strictly bootstrap-time.

```
┌─────────────────────────────────────────────────────────────┐
│ 1. Config aliases (per-server + per-tool)      — zero cost  │
│ 2. Domain tags + catalog grouping in response  — zero cost  │
│ 3. LLM enrichment on server connect (cached)   — one-shot   │
│ 4. Query rewriting on zero-results             — runtime LLM│
└─────────────────────────────────────────────────────────────┘
               │
               ▼
       augmented `aliases` field in bleve index (weight ×3)
```

All four feed a single bleve schema change: one new indexed field `aliases` (analyzed, weight ×3). Everything else composes into that field.

## Component 1: Config Aliases

**File**: `internal/config/config.go`

Extend `ServerConfig`:

```go
type ServerConfig struct {
    // ... existing fields ...

    // SearchAliases are additional keywords that boost all tools on this server.
    // Useful for Russian/English pairs, short names, common misspellings.
    SearchAliases []string `json:"search_aliases,omitempty" yaml:"search_aliases,omitempty"`

    // DomainTags classify this server into high-level domains (e.g. "crm", "documents").
    // Used for catalog grouping in retrieve_tools response. Also feed aliases.
    DomainTags []string `json:"domain_tags,omitempty" yaml:"domain_tags,omitempty"`

    // ToolAliases maps tool name → extra keywords for that specific tool.
    ToolAliases map[string][]string `json:"tool_aliases,omitempty" yaml:"tool_aliases,omitempty"`

    // DisableEnrichment skips the LLM enrichment step for this server even if globally enabled.
    DisableEnrichment bool `json:"disable_enrichment,omitempty" yaml:"disable_enrichment,omitempty"`
}
```

Example:
```json
{
  "name": "b24",
  "search_aliases": ["битрикс", "b24", "bitrix24"],
  "domain_tags": ["crm", "sales"],
  "tool_aliases": {
    "list_items_tool": ["список сделок", "deals list", "список лидов", "leads", "контакты"]
  }
}
```

**What changes**: serialization only. No runtime behavior until Component 2 reads them.

## Component 2: Index and Response Enhancements

**File**: `internal/index/bleve.go`

Current bleve schema indexes: `name`, `description`, `server_name`. Add one field:

```go
aliasesField := bleve.NewTextFieldMapping()
aliasesField.Analyzer = "standard"
toolMapping.AddFieldMappingsAt("aliases", aliasesField)
```

**File**: `internal/index/indexer.go`

When indexing a tool, aggregate aliases from all sources:

```go
func collectAliases(serverCfg *config.ServerConfig, toolName string, enriched *storage.EnrichedToolMeta) string {
    var parts []string
    parts = append(parts, serverCfg.SearchAliases...)
    parts = append(parts, serverCfg.DomainTags...)
    if toolAliases, ok := serverCfg.ToolAliases[toolName]; ok {
        parts = append(parts, toolAliases...)
    }
    if enriched != nil {
        parts = append(parts, enriched.Keywords...)
        parts = append(parts, enriched.ExampleQueries...)
    }
    return strings.Join(parts, " ")
}
```

Bleve query: existing disjunction over `name`/`description`/`server_name` extends to include `aliases` with boost 3.0. No new query pipeline — this is a single additional `bleve.NewMatchQuery(q).SetField("aliases").SetBoost(3.0)` clause.

**Response format change (retrieve_tools)**:

Each tool result adds `domain` when known. Resolution order: (1) LLM-enriched `domain` if present, (2) first entry of server's `DomainTags` if set, (3) omitted.

```json
{
  "name": "list_items_tool",
  "server": "b24",
  "domain": "crm",
  "description": "...",
  ...
}
```

Catalog-fallback (when `total == 0`) groups by domain, falling back to server when no domain is known:

```json
"catalog_by_domain": {
  "crm": {
    "b24": ["list_items_tool", "get_item_tool", "create_item_tool", ...]
  },
  "documents": {
    "b24": ["list_document_templates_tool", "list_documents_tool", ...]
  },
  "registry_lookup": {
    "dadata": ["find_party", "clean_address", ...]
  }
}
```

The existing flat `catalog` field stays for backward compatibility; `catalog_by_domain` is additional.

## Component 3: LLM Enrichment (Bootstrap, Cached)

**New package**: `internal/enrichment/`

### Interface

```go
package enrichment

type Enricher interface {
    EnrichTool(ctx context.Context, req Request) (*Result, error)
}

type Request struct {
    ServerName  string
    ToolName    string
    Description string
    InputSchema json.RawMessage
}

type Result struct {
    Keywords       []string // search terms, both RU and EN
    ExampleQueries []string // plausible natural-language queries that should match this tool
    Domain         string   // single domain tag, e.g. "crm", "documents"
    SchemaVersion  int      // for cache invalidation if we change prompt
}
```

### Providers

Start with one: OpenAI-compatible HTTP (supports OpenAI, OpenRouter, Ollama's OpenAI endpoint, LM Studio, LocalAI). Behind the `Enricher` interface; other providers can be added later without changing call sites.

```json
"search": {
  "enrichment": {
    "enabled": false,
    "provider": "openai",
    "endpoint": "https://api.openai.com/v1",
    "api_key_env": "OPENAI_API_KEY",
    "model": "gpt-4o-mini",
    "timeout_seconds": 15,
    "max_retries": 2
  }
}
```

`api_key_env` holds the **name** of an env var, not the key itself — we never store secrets in the config file.

### Cache

BBolt bucket `tool_enrichment`. Key = `SHA256(server_name + tool_name + description + prompt_version)`. Value = JSON of `Result`.

Why description hash? If a server upgrades and rewrites its tool descriptions, hash changes, cache misses, we re-enrich. If description is stable, we never re-call the LLM for that tool.

**Prompt version** is a constant in code (`enrichment.PromptVersion = 1`). Bump when the prompt template or the expected JSON schema changes — this invalidates all cache entries on next run. Bumping is the only way to force a global re-enrichment without manual `mcpproxy enrichment clear`.

### Scheduler

Hook into `internal/upstream/managed/client.go` `updateTools()` — after a server reports its tool list:

1. For each tool, compute cache key.
2. If cache hit → feed `Keywords/ExampleQueries/Domain` into indexer (Component 2).
3. If cache miss AND enrichment enabled AND not `DisableEnrichment` → call Enricher asynchronously.
4. When enrichment returns → cache + trigger re-index for that single tool.

Async because:
- First connect with 54 tools on a 2-vCPU VPS shouldn't block for 54 × 2s API calls.
- A failed enrichment must never block the proxy from serving requests.
- Rate-limited to `min(5, max_parallel)` goroutines per server.

Failure handling: log warning, fall back to config-aliases + description. Never retry in the same process run — next restart retries from cache-miss state.

### Prompt

Hardcoded for MVP. Example:

```
You are enriching an MCP tool's metadata for search indexing.

Tool name: {toolName}
Server: {serverName}
Description: {description}

Produce JSON with:
- keywords: 5-15 search terms in both Russian and English
- example_queries: 5-10 plausible user queries this tool should match
- domain: single short tag like "crm", "documents", "infrastructure"

Respond with only valid JSON, no markdown.
```

No personalization, no few-shot. Output is small (~300 tokens). Model `gpt-4o-mini` at $0.15/1M input, $0.60/1M output → ~$0.0003 per tool. 130 tools × $0.0003 = $0.04 for a full cold-start enrichment.

## Component 4: Runtime Query Rewriting (Optional)

**File**: `internal/enrichment/rewriter.go`

Triggered only when `retrieve_tools` returns `total == 0` AND `search.query_rewriting.enabled == true`. Flow:

1. Send user query to same LLM provider: *"Translate this search intent into 3-5 alternative queries in English and Russian, comma-separated, no explanation."*
2. Join original query + rewrites with spaces, re-run BM25.
3. Cache rewrites in in-memory LRU (capacity 256, TTL 1h).
4. If second BM25 still empty → emit catalog-fallback as before.

Rewrite is one LLM call per empty-result query, cached. On a chatty agent this caps to a few calls/hour max.

**Config**:
```json
"search": {
  "query_rewriting": {
    "enabled": false
  }
}
```

Shares provider and API key with enrichment. If enrichment is disabled, rewriting must also be disabled (enforced in config validation).

## Data Flow Summary

### Cold start (new server connects, enrichment enabled):

```
server connect
  → managed/client.go.updateTools()
    → indexer.AddTool(tool) with config-aliases only (base BM25 works immediately)
    → enrichment.EnrichToolAsync(tool) (fire-and-forget goroutine)
      → cache miss → LLM call → cache put → indexer.UpdateToolAliases(tool, enriched)
```

### Warm start (restart, cache populated):

```
server connect
  → updateTools()
    → enrichment.LoadCached(tool) → found
    → indexer.AddTool(tool) with config + cached aliases (no LLM call)
```

### User query:

```
retrieve_tools("последние сделки")
  → bleve search over name+description+server_name+aliases
    → hits (aliases matched "сделки" from tool_aliases or LLM-enriched example_queries)
  → return with domain field populated
```

### Zero-result query (rewriting enabled):

```
retrieve_tools("дай мне контрагента")
  → bleve → 0 hits
  → rewriter.Rewrite("дай мне контрагента") → ["find company", "find party", "контрагент ИНН", "counterparty"]
  → bleve re-search with joined terms → hits
  → return with `query_rewritten: true` in response meta
```

## Storage Model Changes

**New BBolt bucket**: `tool_enrichment`.

```go
// internal/storage/models.go
type EnrichedToolMeta struct {
    Keywords       []string  `json:"keywords"`
    ExampleQueries []string  `json:"example_queries"`
    Domain         string    `json:"domain"`
    PromptVersion  int       `json:"prompt_version"`
    CreatedAt      time.Time `json:"created_at"`
    Model          string    `json:"model"`
}
```

Key: `sha256(server_name + "\x00" + tool_name + "\x00" + description + "\x00" + strconv.Itoa(promptVersion))`.

Bucket CRUD in `internal/storage/bbolt.go`. Iteration for admin/debug tooling.

## CLI (minimal, optional)

- `mcpproxy enrichment status` — show cache size, last fill time, providers configured.
- `mcpproxy enrichment clear [--server=name]` — purge cache (all or per server).
- `mcpproxy enrichment refresh [--server=name]` — force re-enrich.

Not required for MVP; can be added incrementally.

## Testing Plan

Unit tests (new):

- `internal/enrichment/enricher_test.go` — OpenAI client with httptest server, happy path + timeout + malformed JSON.
- `internal/enrichment/cache_test.go` — BBolt roundtrip, hash stability, prompt version invalidation.
- `internal/enrichment/rewriter_test.go` — rewriter with mock LLM.
- `internal/index/aliases_test.go` — verify that config `search_aliases` boosts BM25 rank for domain-specific queries (e.g., `"сделки"` query with b24 aliases should rank b24 tools above others).

Integration test (extend existing `internal/index/bleve_discovery_test.go`):

- `TestBM25WithAliases_Russian` — index b24 + dadata mock tools with realistic aliases, query `"последние сделки"` must return `list_items_tool` in top 3.
- `TestBM25WithAliases_MixedLanguage` — query `"company INN"` must rank `find_party` (dadata) and `list_requisites_tool` (b24) in top 3.
- `TestCatalogByDomain` — zero-results query returns `catalog_by_domain` with correct grouping.

No new E2E. Existing E2E must remain green.

## Commit Plan

1. **Commit A**: Config schema changes (Component 1) + `EnrichedToolMeta` storage model + BBolt CRUD. No behavior change yet. Tests: schema (de)serialization, BBolt roundtrip.
2. **Commit B**: Bleve `aliases` field + new indexer methods (`AddToolWithAliases`, `UpdateToolAliases`) + merge logic + `domain` field in retrieve_tools response + `catalog_by_domain` grouping. Uses only config aliases; enrichment is a no-op stub. Tests: `TestBM25WithAliases_Russian` (passes on config aliases alone), `TestCatalogByDomain`, `TestUpdateToolAliases` (post-index update doesn't lose score).
3. **Commit C**: Enrichment package (provider interface + OpenAI-compat client + async scheduler + cache integration). Disabled by default. Tests: provider unit tests with httptest, cache roundtrip, integration with mock LLM.
4. **Commit D**: Query rewriter (Component 4) + zero-result hook in `handleRetrieveToolsWithMode`. Disabled by default. Tests: rewriter unit + integration with mock LLM.
5. **Commit E**: CLI stub commands (`enrichment status/clear/refresh`). Optional — can defer.

Commits B and C are the load-bearing pair. Without B, enrichment has nowhere to feed data. Without C, B relies solely on user-authored config — functional but manual.

## Out of Scope

- Local embeddings / vector search (explicitly ruled out — VPS constraint).
- Multi-language bleve analyzers (Russian stemming etc.) — orthogonal; `aliases` field covers cross-language needs more cheaply.
- Automatic domain inference beyond what LLM returns (no clustering).
- Per-user enrichment preferences (personal edition; no users).
- Enrichment for MCP **resources** or **prompts** — only tools.
- Changes to ToolSearch ranking in Claude Code client (not our code).
- Rewriting strategy other than zero-results trigger (no score-threshold rewrite — YAGNI).

## Verification After Deploy

1. Add `search_aliases` + `tool_aliases` to `b24` server in prod config: `["битрикс", "b24"]` + `{"list_items_tool": ["сделки", "deals", "лиды", "leads"]}`.
2. Call `retrieve_tools "последние сделки"` → expect `list_items_tool` in top 3 results (was: 0 results).
3. Add `domain_tags: ["crm"]` to b24 → response includes `"domain": "crm"` per tool.
4. Zero-result query `"абракадабра"` → response has `catalog_by_domain` with b24 under `crm`.
5. Set `OPENAI_API_KEY`, enable `search.enrichment.enabled: true` → restart → wait ~60s → `mcpproxy enrichment status` reports cache size matches tool count.
6. Re-run `retrieve_tools "найти организацию по ИНН"` → expect `dadata:find_party` in top 3 (was: `total: 0` or irrelevant).

## Risk Register

| Risk | Likelihood | Mitigation |
|------|------------|-----------|
| OpenAI API key leak from config file | Medium | `api_key_env` points to env var, never value in file |
| Enrichment goroutines DOS local LLM endpoint (Ollama) | Low | Rate limit 5 parallel; skip if endpoint returns 429 |
| BBolt cache grows unbounded | Low | Key tied to description hash; stale entries invalidated on description change; CLI `clear` available |
| LLM returns garbage JSON | Medium | Strict JSON parse; on failure → log + fall back to empty enrichment; do NOT cache failure |
| Query rewriter adds latency to empty-result queries | Low | Only fires on zero-results; cached; hard timeout 5s; falls through to catalog |
| Config aliases typo makes tool un-findable | Low | Aliases *add* to index, never replace — original description still indexed |
