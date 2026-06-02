# Bug: `read_only_only` filter drops upstream tools that omit annotations

## Status: OPEN

**Reported:** 2026-06-01
**Severity:** High (silently hides healthy read tools from discovery)

## Summary

`retrieve_tools` with `read_only_only: true` returns **zero** tools for a healthy
upstream whose tools carry no annotations object (e.g. `mcpvault` / the `obsidian`
upstream, server version 0.11.0) — including obviously read-only tools such as
`search_notes` and `read_note`.

The BM25 index is **not** at fault: the same query *without* the filter ranks all
15 obsidian tools at the top. The annotation filter, not indexing, is the bug.

## Expected Behavior

`read_only_only: true` should return read-only tools. A tool named/typed as a read
operation (`search_notes`, `read_note`, `list_directory`) must be returned even when
the upstream does not emit an explicit `readOnlyHint` annotation.

## Actual Behavior

```
A) query="Obsidian vault search notes read note list files", read_only_only=true
   -> total: 0, fallback: "no_results"        ❌  no obsidian tools at all

B) same query, no read_only_only
   -> total: 15, obsidian tools ranked top:
      list_directory 0.122, read_note 0.091, read_multiple_notes 0.084,
      search_notes 0.063                       ✅  index is healthy
```

## Environment

- Gateway: badigit/mcpproxy-go (fork of smart-mcp-proxy/mcpproxy-go)
- Image: ghcr.io/badigit/mcpproxy-go:latest
- Upstream: `obsidian`, stdio, command `mcpvault /vault`, server `mcpvault 0.11.0`
- Upstream health: Connected (15 tools), all approved, not quarantined
- `total_indexed_tools` (debug): 247

## Root Cause Analysis

`mcpvault` tools carry **no `annotations` object at all** (no `readOnlyHint`). Compare
the raw `retrieve_tools` output: obsidian tools have no `annotations` key, whereas
e.g. beget's `backup_restore_file` has `annotations:{readOnlyHint:false,...}`.

The `read_only_only` filter keeps only tools with `readOnlyHint == true`, so
"annotation absent" is treated as "not read-only" and every tool is dropped —
including genuinely read-only ones.

**Smoking gun — internal inconsistency:** `retrieve_tools` already classifies
`search_notes` / `read_note` / `list_directory` as `call_with: "call_tool_read"` via
its verb heuristic, yet `read_only_only` excludes the very same tools. Two code paths
disagree on whether the tool is read-only.

## Suggested Fix

When an explicit `readOnlyHint` annotation is **absent**, have `read_only_only` fall
back to the *same* verb-based READ classification already used to compute `call_with`
(`search`/`read`/`list`/`get`/... → read). Only exclude when a tool is classified
write/destructive, or is *explicitly* annotated `readOnlyHint: false`.

## Regression Tests

1. Upstream emitting tools with **no** annotations + read-verb names:
   `read_only_only=true` MUST return them.
2. Tool explicitly annotated `readOnlyHint: false` MUST stay excluded under the filter.

## User Impact

Discovery-first agents that self-restrict with `read_only_only` conclude "no Obsidian
tool exists" and fall back to raw filesystem search, despite a healthy MCP upstream.
