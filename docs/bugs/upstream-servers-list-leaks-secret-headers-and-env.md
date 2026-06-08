# Bug: `upstream_servers list` leaks secret header / env values verbatim

## Status: OPEN

**Reported:** 2026-06-08
**Severity:** High (credential disclosure)

## Summary

The administrative `upstream_servers` tool with `operation: "list"` returns each
upstream's configuration including the **full, unredacted value** of secret HTTP
headers (e.g. a DaData `Authorization` / API-key header) and secret environment
variables. Any caller able to invoke the list operation — including an LLM agent
whose transcript may be logged, cached, or exfiltrated via prompt injection —
receives live credentials in plaintext.

## Expected Behavior

`upstream_servers list` should redact sensitive values:
- `headers` values (especially `Authorization`, `X-Api-Key`, `Token`, cookies)
  shown as `***` / `<redacted>` or masked (e.g. last 4 chars only).
- `env` values for keys matching secret patterns (`*_KEY`, `*_TOKEN`,
  `*_SECRET`, `PASSWORD`, `AUTHORIZATION`, etc.) redacted likewise.
- Presence of the header/env key may still be shown (so operators can confirm
  configuration), but not the value.

## Actual Behavior

The list response embeds `headers` and secret `env` values in full, verbatim.

## Reproduction

```
upstream_servers(operation="list")
-> response includes an upstream with headers: { "Authorization": "<REAL SECRET VALUE>" }
```

## Suspected Area

The serialization path for the `list` operation in the `upstream_servers` handler
(likely `internal/server/`) returns the raw config struct. A redaction pass should
be applied to the DTO before it is marshalled into the tool result — distinct from
any on-disk config, which legitimately stores the real values.

## Suggested Fix

1. Add a `redactSecrets()` transform applied to the upstream config DTO in the
   `list` (and any `get`/`inspect`) response path.
2. Redact all `headers` values; redact `env` values whose key matches a
   secret-key denylist/pattern.
3. Add a regression test asserting that a configured `Authorization` header and a
   `*_TOKEN` env var do NOT appear in the list response payload.

## User Impact

Credential disclosure to any agent/caller with access to the management tool, and
to anything that persists the conversation (logs, caches, training data).
