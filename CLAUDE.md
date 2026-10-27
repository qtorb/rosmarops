# Context for Claude Code

You are helping build **RosmarOps** — a local security auditor for AI-first builders that maps the agentic attack surface: what each AI agent on the machine can do, which credentials it can reach, and how one agent can compromise another. It reads configuration at rest (read-only, local, zero network), builds a capability graph, and reports findings with evidence. v0.1 is Windows-first, written in Go, open source under AGPL-3.0.

This file is read by Claude Code on every session. Treat its contents as fixed unless explicitly told otherwise by the human owner.

---

## Read first, before writing any code

1. `docs/trust-model.md` — non-negotiable invariants this project must satisfy.
2. `schemas/v2/graph.v2.schema.json` — the capability-graph model the engine emits. This is the contract for the v2 engine.
3. `schemas/report.v1.json`, `schemas/config.v1.json`, `schemas/state.v1.json` — the legacy v1 data model. Being retired: v2 is the product’s data model going forward, and the v1 collectors (the code that reads configs from disk, notably `internal/scan/mcp`) are migrated to populate the v2 graph and removed as each is replaced — not before.

If you have not read these in the current session, stop and read them. Do not skip.

---

## Project status

- **Phase**: 2B (engine + Windows adapter for personal dogfood use).
- **Out of scope for now**: macOS/Linux adapters, signed releases, web viewer, hosted layer, suite abstractions. (scope under review against the v2 graph model)
- **In scope**: everything listed in `README.md` under "What it audits (v0.1, Windows)".

### Note on Claude Code self-audit

The owner uses Claude Code to build this project. Claude Code itself is therefore part of the AI-first attack surface that RosmarOps must audit from v0.1, not deferred. Treat `claude-code` as a first-class MCP host alongside Cline, Cursor, Continue, Aider. Its config locations on Windows (typically under `%USERPROFILE%\.claude\` and `<project>\.claude\settings.json`) must be discovered and hashed like any other agent.

---

## Non-negotiable invariants

These cannot be violated by any code you write. They are the product.

1. **No outbound network.** Do not import `net/http` client packages, `net.Dial`, `net.Listen` (except localhost-only in v0.4+, which is out of scope now), or anything that opens a socket. If a check seems to require network, stop and ask the human owner.

2. **No writes outside scope.** Only two write targets allowed:
   - `~/.rosmarops/state.json`
   - the configured reports directory (default `~/.rosmarops/reports/`).
   No other `os.Create`, `os.WriteFile`, `os.OpenFile`, or `os.MkdirAll` calls outside these paths.

3. **No secrets in logs or reports.** Secret matchers return `{type, file, line, redacted_preview}`. The preview must be: first 4 chars + ` ... ` + last 4 chars of the matched value. Never the full value. Never the middle. Tested.

4. **Fail-secure.** Any non-recoverable error → overall report status is at least `yellow`. Status `green` requires zero errors and zero new findings. This is enforced in the status reducer, not left to caller discipline.

5. **Forced exclusions in recursive scans.** Always skip, hardcoded, not configurable:
   ```
   node_modules, .git, .next, dist, build, coverage, target,
   .venv, venv, __pycache__, .pytest_cache, .mypy_cache,
   .ruff_cache, .turbo, .parcel-cache
   ```

6. **Resource limits, hardcoded:**
   - Files >2 MB are not opened for content scan.
   - Global scan timeout: 300 seconds. Hard cap.
   - Per-repo timeout: 60 seconds. Repo over budget → yellow finding "scan_timeout", continue with next repo.

7. **Per-repo isolation.** Each repo scan runs in its own context with `defer`/`recover` for the equivalent of try/finally. One repo failing must not abort the run.

8. **Self-integrity check.** On startup, the binary computes its own SHA-256 and compares against `state.integrity.vigil_binary_sha256`. Mismatch → abort with a minimal red report and one finding (`binary_hash_changed`). Same for `config_sha256`.

---

## Coding conventions

- **Language**: Go 1.22+, single module.
- **CLI framework**: Go standard library `flag` with an in-house dispatcher (decision 2026-10-25, G4).
- **JSON Schema validation**: in-house validator limited to the keywords our schemas use; gojsonschema only as a test oracle (decision 2026-10-25, G2–G3). Schemas are loaded on startup; fail fast on invalid config/state.
- **Hashing**: SHA-256 only, lowercase hex.
- **Time**: UTC in JSON outputs (`time.RFC3339`); local time in Markdown headers.
- **Errors**: wrapped with `fmt.Errorf("...: %w", err)`. Never swallow without logging.
- **Logging**: stdlib `log/slog`. No third-party log libraries.
- **Dependencies**: minimal. Each new third-party dep needs explicit human approval. We are a security tool — every dep is part of our attack surface.
- **Language of user-facing text**: English is the default and the source language. Every human-readable string shown to the user (report, CLI messages, finding texts) must come from a message catalog keyed by a stable code (embedded JSON, one file per language); other languages (es, …) are translations that fall back to English for missing keys. When the user does not pass `--lang`, RosmarOps follows the Windows display language if a translation exists for it, and English otherwise; `--lang` always wins. Every run writes the report in every available language, each page linking to the others. The graph and every JSON output carry no user-facing prose — only ids, enums, stable codes and technical detail. Translations are supplied by the human owner: never write or edit translated copy.

---

## Suggested file layout

```
cmd/rosmarops/main.go                 # entrypoint, command dispatch
internal/
  config/                             # load + validate config.json
  state/                              # load + persist state.json
  report/                             # build Markdown + JSON sidecar
  finding/                            # Finding type, severity reducer
  hash/                               # sha256 helpers
  scan/
    runner.go                         # orchestrates all checks with timeouts
    secrets/                          # regex matchers + redaction
    repos/                            # git status, hooks, history scan
    deps/                             # package.json, lockfile parsing
    workflows/                        # .github/workflows analysis
    vscode/                           # extensions + settings.json
    mcp/                              # cline_mcp_settings.json etc.
    shellprofile/                     # platform-agnostic interface
  platform/                           # abstract platform interface
    interface.go
adapters/
  windows/                            # PowerShell-backed implementations
    extensions.go
    profile.go
    defender.go
    scheduled_tasks.go
schemas/                              # vendored copies of the JSON schemas
  v2/graph.v2.schema.json             # v2 capability-graph model (current contract)
  config.v1.json
  state.v1.json
  report.v1.json
tests/
  trust/                              # mechanical enforcement of invariants
    no_network_test.go
    no_writes_out_of_scope_test.go
    no_full_secret_leak_test.go
  unit/
  integration/
```

---

## Quality bar

- Every check has at least one unit test using a fake filesystem (`testing/fstest` or similar).
- Trust tests in `tests/trust/` are mandatory. They mechanically prove the invariants:
  - `no_network_test.go`: builds the binary and runs it under a network namespace / mock that fails any socket; expects clean run.
  - `no_writes_out_of_scope_test.go`: runs in a sandbox, asserts only the allowed paths receive writes.
  - `no_full_secret_leak_test.go`: feeds known fake secrets, asserts the full value never appears in any output (report, logs, state).
- `go vet ./...` clean.
- `staticcheck ./...` clean.
- Formatting: `gofumpt -l -d .` returns nothing.

---

## When to STOP and ask the human owner

- adding any third-party Go dependency;
- changing any schema or creating a new schema version (the v2 graph schema at `schemas/v2/` is approved; any change to it, or a future v3, requires an explicit decision from the human owner);
- writing any code path that opens a socket or writes outside the two allowed paths;
- relaxing any of the forced-exclusion paths or the resource limits;
- removing or weakening any of the trust tests;
- adding any code that calls out to a network service "just for telemetry" / "anonymous usage" / "crash reports" — the answer is always no.

If you are unsure whether something violates an invariant, assume it does and ask.

---

## Style of human–AI collaboration here

The human owner:
- decides product direction;
- reviews and commits all code;
- holds the AGPL copyright and the brand.

You (Claude Code):
- generate code that implements what the docs and schemas specify;
- write tests alongside code, not after;
- surface tradeoffs proactively;
- never silently expand scope.

Every commit is the human's decision. You propose, they dispose.
