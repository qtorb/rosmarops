# 0012. Factory scores and sandbox templates P0–P3

Date: 2026-11-08
Amended: 2026-11-14
Amended: 2026-11-16
Amended: 2026-11-18

## Context

A person's score mixes two things: what the product does by default and what the person changed. To compare products fairly, and to show a person what their own settings are worth, RosmarOps needs the score of each product at its defaults on a fixed computer. And once the report says what is wrong, people asked for a ready-made way to fix it: a settings block they can paste, measured by the same engine.

## Decision

**Factory scores.** A reference computer is a repository fixture in `tests/trust/r2_factory_scores_test.go`: one Windows-shaped user, one git project with `AGENTS.md` and an `.env` ignored by git (a medium key), and a user-level `~/.aws/credentials` (a high key). Each agent is scored alone with the real engine. `TestR2FactoryScores` pins the scores: a score that moves without a code or knowledge change fails the test. The release job writes `factory-scores.json` from that test. In each person's report, `pipeline.Factory` (`internal/pipeline/factory.go`) rebuilds the same computer with the person's settings taken out, and the report shows that factory score next to theirs (`index` `Factory`).

**Templates.** The user-facing word is "template"; the codes stay `P0`–`P3` (`internal/profiles`):
- `P0` Read only and `P1` Local development: JSON blocks for Claude Code's user settings, measured by adding them to this computer's settings.
- `P2` Sandboxed development: `P1` plus Claude Code's `sandbox` object, with `"autoAllowBashIfSandboxed": false`. Measured on macOS and Linux only (`MeasuredOn`): on native Windows Claude Code runs commands without a sandbox. Grok Build also has a measured `P2` on macOS and Linux.
- `P3` Unattended: a launch command, not measured.
- Cursor and Grok Build have guided templates (`internal/profiles/guided.go`): the steps and text are shown. Cursor's are measured through its declared answers (ADR 0017).

Templates use each agent's own permissions. RosmarOps never writes them: the person pastes them, or asks their agent to. `TestProfileScores` (`tests/trust/bv249_release_test.go`) writes `profile-scores.json` for the release, validated against `schemas/release/profile-scores.v1.schema.json`.

**2026-11-18: P2 without a project block.** Claude Code does not count `allowRead` or `allowWrite` from repository settings while `blockReadsOutsideWorkingDirectories` is on, but it does count their `denyRead`. The old P2 project block (`denyRead ["~/"]`, `allowRead ["."]`) therefore left the sandboxed terminal unable to read the project at all. The decision: P2 has no project block, since the user-settings block already denies the home folder and re-opens the working directory; the report warns people who already pasted the old block; and the engine applies the same rule when it measures. **This is decided and not yet in the code:** at this commit `profiles.ProjectBlock(P2)` still returns the project block.

## Consequences

- Factory scores and template scores are reproducible from the source at the tagged commit.
- A template's class must agree with its band (`tests/trust/bv249_coherence_test.go`, see ADR 0011).
- Changing a template's text changes published numbers; `profile-scores.json` must be regenerated.
