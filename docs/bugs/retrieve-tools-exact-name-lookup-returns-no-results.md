# Bug: exact tool-name query `server:tool` returns no_results for an indexed tool

## Status: OPEN

**Reported:** 2026-06-01
**Severity:** Medium (breaks the documented "retry with an exact tool name" fallback)

## Summary

`retrieve_tools(query="obsidian:search_notes")` returns `total: 0` / `no_results`,
although the tool exists, is approved, and is returned by a natural-language query.

The parser recognizes the input as an exact tool name and splits it correctly, but the
exact-name resolution branch fails to return the tool.

## Reproduction

```
retrieve_tools(query="obsidian:search_notes")
-> total: 0, fallback: "no_results"
-> debug.query_analysis: {
     is_tool_name: true,
     has_colons: true,
     server_part: "obsidian",
     tool_part: "search_notes"
   }
```

The same tool IS present and indexed:
- natural-language query `"Obsidian vault search notes ..."` returns `search_notes`
  (score ~0.063, server `obsidian`);
- `quarantine_security inspect_tools obsidian` lists `search_notes` as approved.

## Expected Behavior

When `query_analysis.is_tool_name == true` and `server_part:tool_part` identifies an
existing, indexed tool, `retrieve_tools` MUST return that tool. The `no_results` hint
itself advises "retry with an exact tool name" — so the exact-name path must actually
resolve.

## Actual Behavior

The exact-name branch returns nothing; the user-facing hint loops back to "retry with
an exact tool name", which also fails.

## Environment

Same as `retrieve-tools-read-only-filter-drops-unannotated-tools.md`
(badigit/mcpproxy-go, obsidian/mcpvault upstream, 247 indexed tools).

## Suspected Area

The `is_tool_name` short-circuit path in the `retrieve_tools` handler — direct
lookup by `server_part`/`tool_part` against the registry/index, separate from the BM25
ranking path that works.

## Regression Test

Index a tool `obsidian:search_notes`, then assert `retrieve_tools(query="obsidian:search_notes")`
returns exactly that tool (with and without annotation filters).
