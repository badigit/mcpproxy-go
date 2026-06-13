# Repository Guidelines

## Project Structure & Module Organization
- `cmd/mcpproxy` hosts the core daemon entrypoint; `cmd/mcpproxy-tray` builds the CGO-based tray binary.
- Runtime logic, HTTP handlers, storage, and upstream orchestration live in `internal/` (`internal/runtime`, `internal/httpapi`, `internal/upstream`, `internal/storage`).
- Vue/Tailwind assets are kept in `frontend/`; bundled static output is embedded from `web/`.
- Integration and regression assets reside under `tests/` and `scripts/`; Go unit tests sit beside source files.

## Build, Test, and Development Commands
- `make build` — compile the proxy and tray binaries for the host platform.
- `go build ./cmd/mcpproxy` — quick core rebuild; `GOOS=darwin CGO_ENABLED=1 go build ./cmd/mcpproxy-tray` validates the macOS tray target.
- `npm install && npm run build` inside `frontend/` — install web deps and emit production assets.
- `scripts/run-web-smoke.sh` — spin up the proxy and execute the Playwright smoke suite.
- `scripts/verify-api.sh` — exercise the `/api/v1` REST surface with curated curl calls.

## Coding Style & Naming Conventions
- Run `gofmt` (tabs, goimports defaults) on all Go sources; prefer descriptive package-level names (`runtime`, `upstream`, `storage`).
- TypeScript/Vue files follow Prettier defaults (`npm run lint`); components use `PascalCase.vue`, composables use `useThing.ts`.
- Configuration and DTO structs live in shared packages (`internal/contracts`, `internal/httpapi`); avoid anonymous maps between layers.

## Testing Guidelines
- Unit tests: `go test ./internal/...` (set `GOCACHE`/`GOMODCACHE` to workspace paths when sandboxed).
- Targeted suites: `go test ./internal/server -run TestMCP -v` for lifecycle checks; `npm run test:unit` for frontend units.
- End-to-end: `scripts/run-e2e-tests.sh` after ensuring `@modelcontextprotocol/server-everything` is warmed and reachable.
- Name Go tests `TestFeatureScenario`; snapshot or fixture data belong under `tests/` with explicit prefixes.

## Commit & Pull Request Guidelines
- Use concise, imperative commit messages (e.g., `Fix upstream disable locking`); avoid AI co-author tags.
- PR descriptions should summarize impact, list verification commands, and link relevant P# items or issues; attach UI screenshots or log excerpts when behavior changes.
- Keep change scopes bounded to one surface (runtime vs. tray vs. web); document follow-up ideas in `IMPROVEMENTS.md`.

## Security & Configuration Tips
- Never hardcode secrets; load them via the tray secure store or environment lookups in `internal/secret`.
- When editing configs, prefer `runtime.SaveConfiguration()` flows so disk state and in-memory state stay aligned; regenerated files land in `~/.mcpproxy/`.

## Active Technologies
- Go 1.24 (toolchain go1.24.10) + BBolt (storage), Chi router (HTTP), Zap (logging), regexp (stdlib), existing ActivityService (026-pii-detection)
- BBolt database (`~/.mcpproxy/config.db`) - ActivityRecord.Metadata extension (026-pii-detection)

## Recent Changes
- 026-pii-detection: Added Go 1.24 (toolchain go1.24.10) + BBolt (storage), Chi router (HTTP), Zap (logging), regexp (stdlib), existing ActivityService

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:7510c1e2 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/SYNC_CONCEPTS.md for details and anti-patterns.

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->
