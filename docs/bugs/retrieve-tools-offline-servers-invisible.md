# Bug: offline/disconnected upstream servers are invisible to `retrieve_tools`, so an agent reads a search miss as "server does not exist"

## Status: FIXED

**Reported:** 2026-07-03
**Fixed:** 2026-07-03 in PR #2 (branch `fix/retrieve-tools-offline-visibility`, base commit `56b9d2e2`)
**Severity:** Medium-High (produces false "does not exist" conclusions; the agent actively misinforms the user)

**Fix:** two systemic changes in `handleRetrieveToolsWithMode` (`internal/server/mcp.go`):

1. An always-on epistemic guardrail appended to `usage_instructions` (all routing
   modes): a search miss is not proof of absence; offline servers are omitted from
   the index; call `upstream_servers` (list) before concluding a tool/server does
   not exist.
2. A `disconnected_servers` response field built from the StateView snapshot —
   enabled, non-quarantined servers that are not connected, with their last-known
   `tool_count`/`state`/`last_error`. Present only when something is actually
   offline (zero payload on a healthy fleet). Roster logic is the pure function
   `buildDisconnectedServers` in `internal/server/mcp_annotations.go`.

## Summary

`retrieve_tools` is a BM25 keyword search over **connected** servers only. A
disconnected/offline upstream contributes no tools to the index and no hits to the
search, so it is entirely invisible in the response. An agent that searches for
such a server's tools, sees nothing, and concludes the server "does not exist" has
no in-band signal to distinguish "absent from the results" from "not configured".

This is distinct from the ranking bugs in this folder
(`retrieve-tools-exact-name-lookup-returns-no-results.md`,
`retrieve-tools-read-only-filter-drops-unannotated-tools.md`), which all improve
matching for CONNECTED servers. This bug is on a different axis: the visibility of
UNREACHABLE-but-configured servers.

## Reproduction

Preconditions: a configured, enabled upstream that is currently disconnected — e.g.
a reverse-tunnelled local server (`task_reporter`, rathole PC→VPS) while the PC
fleet is down.

```
retrieve_tools(query="task reporter packages saved reports")
-> tools: [ ...weak/unrelated hits from OTHER connected servers, or none... ]
-> (no mention anywhere that a server named task_reporter exists but is offline)
```

The agent then reports "there is no task_reporter MCP server / no such tools",
which is false — the server exists and has ~41 tools; it is merely unreachable.

Real incident (2026-07-03): an agent twice declared "Task Reporter is not an MCP
server, no tools" while upstream `task_reporter` was in fact Ready with 41 tools;
the same agent had earlier reasoned about an offline state producing an empty index.

## Expected Behavior

- `usage_instructions` must always tell the caller that a search miss is not proof
  of absence and how to enumerate ground truth (`upstream_servers` list).
- When a configured, enabled, non-quarantined server is disconnected, the response
  must surface it under `disconnected_servers` with its last-known tool count and
  state, so the agent can say "that server exists but is currently offline" instead
  of "that server does not exist".

## Actual Behavior (before fix)

The catalog fallback fired only on `len(results) == 0` and listed only CONNECTED
servers (`buildServerCatalog` skips `!client.IsConnected()`). Offline servers
appeared nowhere, and `usage_instructions` gave no epistemic guidance. A weak but
non-empty result set suppressed even the zero-results catalog.

## Environment

badigit/mcpproxy-go fork, dimba MCP gateway (beget-vps). Offline upstreams are the
rathole reverse-tunnel servers (`task_reporter`, `dimvestor`, `zm_parser`) whose
tools disappear from the index whenever the PC-side fleet is not running.

## Regression Test

`internal/server/mcp_discovery_roster_test.go`:

- `TestBuildDisconnectedServers` — a snapshot with one connected, one enabled-offline
  (with `ToolCount`), one disabled-offline, one quarantined-offline must yield only
  the enabled-offline entry, carrying its last-known `tool_count`/`state`/`last_error`.
- `TestBuildDisconnectedServers_NilSnapshot` / `_AllHealthy` / `_Deterministic` —
  nil-safety, zero payload on a healthy fleet, and stable sorted order.
- `TestRetrieveTools_GuardrailAlwaysPresent` — the guardrail is present on both a
  matching query and an empty result set.

## Related / follow-up

- `mcpproxy-pik`: also fire the browse-catalog fallback on weak (low-score) results,
  not only zero — tune the threshold against `internal/index/discovery_lab_test.go`
  (MRR/hit-rate harness), not by guessing.
- Complementary config-side mitigations (gateway, not code): `search_aliases` to
  boost hard-to-name servers (e.g. `task-reporter` vs `task_reporter`), and
  `reconnect_on_use` for intermittent tunnelled servers (helps on call, not on
  discovery; useless while the tunnel endpoint itself is down).
