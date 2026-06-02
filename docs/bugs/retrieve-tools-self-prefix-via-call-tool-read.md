# Bug: built-in tools fail when the host namespaces them with the connection name

## Status: FIXED

**Reported:** 2026-06-02
**Severity:** High (breaks the primary `retrieve_tools` entry point on hosts with lazy tool loading)

## Summary

Some MCP hosts (e.g. claude.ai connectors) expose the gateway's own built-in tools
namespaced with the **connection name**, so the model sees them as
`dimba-mcp-gateway:retrieve_tools`, `dimba-mcp-gateway:call_tool_read`, etc.

The gateway uses the *same* `server:tool` syntax for **upstream** addressing. When an
agent invokes `call_tool_read` with `name = "dimba-mcp-gateway:retrieve_tools"`, the
dispatcher splits on `:`, treats `dimba-mcp-gateway` as an upstream server, finds no
client, and returns:

```
No client found for server: dimba-mcp-gateway. Available servers: [...].
IMPORTANT: Use 'retrieve_tools' first to discover tools ...
```

The advice is self-contradictory: it tells the agent to use `retrieve_tools` — which is
exactly what it tried, and which it often cannot load directly because the host's tool
search does not surface it.

## Root cause (two layers)

1. **Discoverability (host side).** On hosts with lazy/deferred tool loading + BM25 tool
   search, `retrieve_tools` — the documented entry point — is not reliably returned by the
   host's own search. The agent cannot load it and improvises by routing it through
   `call_tool_read` with a server prefix.
2. **Namespace collision (gateway side).** The connection-name prefix the host attaches to
   built-in tools is indistinguishable, by syntax, from the gateway's `server:tool` upstream
   addressing. The dispatcher routes the built-in to a non-existent upstream.

Layer 2 is the part within the gateway's control, and fixing it also rescues layer 1.

## Reproduction

```
call_tool_read(name="dimba-mcp-gateway:retrieve_tools", args={query:"...", limit:15})
-> Error: No client found for server: dimba-mcp-gateway. Available servers: [...]
```

`dimba-mcp-gateway` is neither a configured upstream nor a registry — it is the name under
which the host registered the gateway itself.

## Expected behavior

When the prefix is **not** a connected upstream and the suffix names a built-in tool, the
gateway should drop the prefix and run the built-in with the nested `args`, instead of
failing. The fix is read-safe: `call_tool_*` variants are excluded as self-heal targets to
avoid recursive self-dispatch.

## Fix

`internal/server/mcp.go`, `handleCallToolVariant`:

- Added `isSelfHealableBuiltin(name)` — the built-in set minus `call_tool_*`.
- Added `dispatchBuiltinTool(ctx, request, name)` — name→handler routing shared with intent.
- Before upstream routing: if `serverName` is not a connected upstream and
  `actualToolName` is a self-healable built-in, rebuild the request with the nested args and
  dispatch to the built-in handler (with a `Warn` log noting the self-prefix).
- Extracted `noUpstreamClientError(serverName)` and used it in both the variant path and the
  legacy `handleCallTool` path so the "no client found" message is identical everywhere.

## Regression test

`internal/server/mcp_self_prefix_test.go`:

- `isSelfHealableBuiltin` returns true for built-ins, false for `call_tool_*` and upstream
  tool names.
- `call_tool_read(name="any-gateway:list_registries")` returns the `list_registries`
  result (not a "No client found" error) when `any-gateway` is not an upstream.
