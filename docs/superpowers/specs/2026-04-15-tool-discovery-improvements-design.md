# Tool Discovery Improvements — Design Spec

**Date**: 2026-04-15
**Status**: Draft
**Scope**: Fork-only (badigit/mcpproxy-go)
**Motivation**: AI agents using MCPProxy through Claude Code ToolSearch cannot reliably discover `retrieve_tools`, leading to broken workflows where agents guess tool names instead of following the retrieve → call_tool_* pipeline.

## Problem Statement

Observed in production (dimba-mcp-gateway, 8 upstream servers including b24, dadata, github, obsidian, kontur-diadoc, beget, remna, b24-rest-docs):

1. **`retrieve_tools` loses ToolSearch ranking** — Claude Code deferred tools system returns max 5 results per query. MCPProxy exposes ~10 tools. The descriptions of `call_tool_read`, `search_servers`, and `retrieve_tools` share too many keywords ("search", "find", "retrieve", "tools", "discover"), causing `retrieve_tools` to rank below other tools. Agent makes 3+ ToolSearch attempts before finding it.

2. **BM25 returns 0 results for valid queries** — Agent searches `"dadata company requisites"` but dadata tool `suggest_party` has no keyword overlap. Standard Bleve analyzer doesn't handle cross-language synonyms. Agent gets empty results with no guidance on what exists.

3. **Agent falls back to guessing tool names** — Without `retrieve_tools` loaded, agent calls `suggest_party` as a direct MCP tool (hitting line 4290 dispatch error) instead of `call_tool_read` with `name: "dadata:suggest_party"`. The latter works (confirmed by server logs showing successful CallTool operations).

**Root cause**: All three problems stem from the agent not loading `retrieve_tools` first. Once loaded, the workflow guidance in its response (`usage_instructions`) tells the agent how to use `call_tool_read/write/destructive`. The fallback addresses the secondary problem of BM25 not finding tools by description.

## Solution: 3 Commits

### Commit C: Integration Tests (TDD — first)

**File**: `internal/index/bleve_discovery_test.go` (new)

Tests that document current behavior and verify improvements:

**Test 1: `TestBM25_EnglishToolDiscovery`**
Index tools resembling real b24 server (English descriptions):
- `b24:list_items_tool` — "List CRM items with full filter, order, pagination support"
- `b24:get_item_tool` — "Get single CRM item with enriched details, product rows, custom fields"
- `b24:create_item_tool` — "Create CRM item with JSON fields (supports productRows)"
- `b24:list_requisites_tool` — "List requisites (legal details: INN, KPP, OGRN) for entity"
- `b24:get_requisite_tool` — "Get single requisite with bank details and addresses"

Queries and expectations:
- `"company requisites legal details"` → expects `list_requisites_tool` or `get_requisite_tool` in top 3
- `"create deal CRM"` → expects `create_item_tool` in top 3
- `"list_items_tool"` → expects exact match with highest score

**Test 2: `TestBM25_MixedLanguageToolDiscovery`**
Index tools resembling real dadata server (Russian + English descriptions):
- `dadata:suggest_party` — "Подсказки по организациям и ИП по названию, ИНН, ОГРН"
- `dadata:suggest_address` — "Подсказки по адресам"
- `dadata:find_by_id` — "Поиск организации по ИНН или ОГРН"
- `dadata:suggest_bank` — "Подсказки по банкам (БИК, название)"

Queries and expectations:
- `"suggest_party"` → exact match (must work)
- `"организация ИНН"` → expects `suggest_party` or `find_by_id`
- `"company"` → documents whether this finds `suggest_party` (likely no — this is the cross-language gap)
- `"dadata"` → expects all dadata tools via server_name field match

**Test 3: `TestBM25_ZeroResultsFallbackData`**
Index b24 + dadata tools from tests 1 and 2.
- `"completely_unrelated_nonsense_xyz"` → expects 0 results (confirms fallback trigger condition)
- `"suggest"` → expects dadata tools (confirms partial name match works)

### Commit A: Rewrite Tool Descriptions

**File**: `internal/server/mcp.go` — tool registration section

**Principle**: Each tool gets a unique keyword footprint. No two tools compete for the same query terms.

#### `retrieve_tools` — NEW description
```
PRIMARY TOOL DISCOVERY — must be called before using any upstream tools.
Searches all connected MCP servers using BM25 full-text search. Describe
what you need in natural language (e.g., 'create GitHub issue', 'find
company by INN', 'list CRM deals'). Returns matching tools with exact
names, inputSchema, annotations, and call_with recommendation indicating
which variant to use. Quarantined servers excluded from results. To add
NEW servers: use list_registries + search_servers instead.
```

Keywords unique to this tool: `"discovery"`, `"must be called"`, `"natural language"`, `"matching tools"`, `"exact names"`.

#### `call_tool_read` — NEW description
```
Execute a read-only upstream tool. Pass the exact 'server:tool' name from
retrieve_tools results. For data retrieval operations without side effects.
```

#### `call_tool_write` — NEW description
```
Execute a state-modifying upstream tool. Pass the exact 'server:tool' name
from retrieve_tools results. For create, update, send, and other write operations.
```

#### `call_tool_destructive` — NEW description
```
Execute a destructive upstream tool. Pass the exact 'server:tool' name from
retrieve_tools results. For delete, remove, and other irreversible operations.
```

#### `search_servers` — NEW description
```
Browse external registries to find and add NEW upstream MCP servers.
Not for discovering existing tools — use retrieve_tools for that.
Call list_registries first to see available registries.
```

#### `list_registries` — NEW description
```
List available external MCP server registries for use with search_servers.
```

**What changes**:
- All emoji removed (noise for tokenizers)
- `call_tool_*` descriptions shortened from ~350 chars to ~120 chars each
- Decision rule keywords ("search, query, list, get, fetch, find...") moved OUT of descriptions — they don't help with ToolSearch ranking and bloat the description. The `usage_instructions` in `retrieve_tools` response already contains the full decision guide.
- `search_servers` explicitly says "Not for discovering existing tools"

**What stays the same**:
- Tool parameter schemas (inputSchema) unchanged
- `upstream_servers`, `quarantine_security`, `code_execution`, `read_cache` descriptions unchanged
- `usage_instructions` text in retrieve_tools response unchanged

### Commit B: Zero-Results Fallback Catalog

**File**: `internal/server/mcp.go` — `handleRetrieveToolsWithMode()` function

**Trigger**: `len(results) == 0` after BM25 search.

**Behavior**: Append a `catalog` field to the JSON response listing all connected servers with their tool names.

**Response structure** (when fallback triggers):
```json
{
  "tools": [],
  "total": 0,
  "query": "dadata company requisites",
  "fallback": "no_results",
  "hint": "No tools matched your query. Browse the catalog below and retry with an exact tool name, or try different keywords.",
  "catalog": [
    {
      "server": "dadata",
      "tool_count": 4,
      "tools": ["suggest_party", "suggest_address", "find_by_id", "suggest_bank"]
    },
    {
      "server": "b24",
      "tool_count": 48,
      "tools": ["list_items_tool", "get_item_tool", "create_item_tool", "list_requisites_tool", "get_requisite_tool", "list_document_templates_tool", "list_documents_tool", "get_document_tool", "create_document_tool", "update_document_tool"]
    }
  ]
}
```

**Constraints**:
- Max **10 tool names per server** in catalog (if more, `tool_count` shows the real number)
- Only **connected and enabled** servers (skip disabled, quarantined, disconnected)
- Tool names only — no descriptions, no inputSchema (token economy)
- Data source: `p.upstreamManager` — iterate connected servers, call `ListTools()` on each (data already cached in memory after connection)

**Implementation location**: After the BM25 search in `handleRetrieveToolsWithMode()`, approximately line 1006-1010 in current code. Before JSON serialization of the response map.

**Fallback test** (in `internal/server/mcp_fallback_test.go` or added to existing e2e):
- Set up proxy with mock upstream servers
- Search for nonsense query → verify catalog is present in response
- Search for valid query with results → verify catalog is NOT present
- Verify catalog respects 10-tool limit per server
- Verify catalog excludes disabled/quarantined servers

## Commit Order

1. **Commit C** — Tests first (TDD). Some tests will initially fail (documenting current behavior) or pass (baseline).
2. **Commit A** — Rewrite descriptions. Tests unchanged (descriptions don't affect BM25 index of upstream tools).
3. **Commit B** — Fallback catalog. Fallback test now passes.

## Out of Scope

- BM25 search aliases / synonyms config per server
- Russian morphology analyzer for Bleve
- Changes to `instructions` (fork feature — already works)
- Changes to frontend / Web UI
- Changes to `upstream_servers` tool
- ToolSearch ranking algorithm (Claude Code client — not our code)

## Verification

After deploying to dimba-mcp-gateway:
1. Connect Claude Code with `dimba-mcp-gateway` MCP server
2. ToolSearch for `"find tools"` → `retrieve_tools` should appear in first 5 results
3. `retrieve_tools` with query `"suggest_party"` → exact match from dadata
4. `retrieve_tools` with query `"company requisites"` → if 0 results, catalog shows dadata tools
5. Agent can call `call_tool_read` with `dadata:suggest_party` → succeeds
