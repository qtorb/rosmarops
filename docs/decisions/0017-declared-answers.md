# 0017. Declared answers: Grok Bot and Cursor

Date: 2026-11-14
Amended: 2026-11-16

## Context

Some settings that decide what an agent can do are not in any file RosmarOps reads at rest. Cursor keeps its run mode and several protections in its app; Grok Bot keeps its account settings on the vendor's side. Reading them would mean opening app databases, sessions or cookies, or calling the vendor, which RosmarOps does not do. Without them, these agents had no honest score.

## Decision

RosmarOps asks the person, with closed questions, and marks the result as declared by them (`internal/declared`).

- **Cursor:** run mode, what the command allowlist holds, Read Access, and External-File Protection. A question is asked only when what is read does not already answer it: `approvalMode` in `permissions.json` replaces the run mode question; a `terminalAllowlist` replaces the allowlist question; a fixed `readBoundary` in `sandbox.json` replaces Read Access (`ResolveCursor`).
- **Grok Bot:** whether the person uses it (only when its app folder is not found), execution on this computer, Auto-review, a rule to ask first before sending and publishing, and connectors that send. Execution on this computer and Auto-review are read from `~/.grokbot/settings.json` when present, and not asked; no other file of `~/.grokbot` is read (`ResolveGrokBot`, `adapters/windows/grokbot.go`).
- **No answer is never the safe answer.** With no answer, or "I don't know", the engine assumes each question's least controlled option.
- **When it asks.** Only at the end of a review with a console; never in a scheduled review or a double-click window. With stored answers for the same knowledge version, it shows them and Enter keeps them (`cmd/rosmarops/declared.go`).
- **Where answers live.** In `~/.rosmarops/state.json`, under `declared`, with the time and knowledge version (`schemas/state.v2.json`). Only valid question and option codes are kept (`declared.Clean`).
- **In the report.** Cells that rest on an answer carry the origin `declared` in `report.v2.json`, and the report tags them «declared by you».

## Consequences

- Cursor and Grok Bot get a score and a class. Cursor's templates are measured with its answers (ADR 0012).
- A declared answer is only as trustworthy as `state.json`: any program that can write to the person's home folder can change it.
- An official read-only way to read these settings would replace the questions; until one exists, they stay.
- Tests: `tests/trust/bv248_declared_test.go` and `tests/trust/bv248_figures_test.go`.
