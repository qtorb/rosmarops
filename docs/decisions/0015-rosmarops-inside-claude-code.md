# 0015. RosmarOps inside Claude Code: session check, status line and plugin

Date: 2026-11-12
Amended: 2026-11-18

## Context

A full review runs once in a while. Between reviews, opening a project in Claude Code can bring new permissions, hooks, MCP servers or skills from that project's files. People also wanted to see their last result without opening the report. Both had to fit the invariants: read only, no network, no writes outside RosmarOps's folders, and an integrity check that cannot stop a session.

## Decision

**Session check.** `rosmarops session-check` runs as a Claude Code `SessionStart` hook. It reads only Claude Code's settings (user, managed and the project's) and compares what they bring with a snapshot of the last time that project was opened (`internal/agenthooks`). It tells the person, not Claude, in one `systemMessage`: at most 5 items, names cut at 60 characters, fingerprints stored, never values. It acts only on Claude Code's own `SessionStart` input with source `startup` or `resume`; Cursor's input is ignored. It has a one-second budget: past it, it says and writes nothing. It writes only project snapshots under the reports data folder.

**Status line.** `rosmarops statusline` prints the line the last run left in `<reports>/data/statusline.txt`: red count, yellow count, score and date. Never a path, name, server or key. It is offered only when the person has no status line of their own.

**Integrity.** Both commands check integrity like every command. On a mismatch they never ask and never write: they print one line and exit 0. The next `rosmarops run` handles the change (see the [trust model](../trust-model.md)).

**Installation.** RosmarOps never edits Claude Code's settings. The report gives a text to paste into Claude Code, which shows the change and asks before saving it. A hook or status line counts as RosmarOps's own only when it is in user or managed settings, is a `command` with no matcher or condition, has the exact form `<path> session-check` or `<path> statusline` with no shell operators, and the path leads to a binary whose SHA-256 is the state's reference (`cmd/rosmarops/ownhooks.go`, `adapters/windows/ownhooks.go`). Anything else is treated as any other hook.

**Plugin (2026-11-18).** A Claude Code plugin in the public repository `qtorb/rosmarops-claude-code` carries the `SessionStart` hook. RosmarOps recognises it by reading only `installed_plugins.json`, `known_marketplaces.json` and, in its installed copy, `hooks/hooks.json` and `.claude-plugin/plugin.json` (`adapters/windows/ownplugin.go`). Its `/rosmarops` command shows the last result and the command to run in a terminal; it does not start a review.

**Decided on 2026-11-18, not yet in the code:** `session-check` will also pass the last result to Claude as `additionalContext`, so `/rosmarops` can show it when the sandbox blocks reading the reports folder.

## Consequences

- Neither command uses the network or starts a process.
- Tests: `cmd/rosmarops/bv226_hooks_test.go`, `cmd/rosmarops/ownplugin_test.go`, `internal/agenthooks/*_test.go` and `tests/trust/no_identity_in_statusline_test.go`.
