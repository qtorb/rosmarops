# 0024. The team's installations counted apart

Date: 2026-11-20

## Context

The usage statistics (ADR 0014) are meant to count people's checks. The RosmarOps team runs RosmarOps on its own computers every day, and those checks were counted as people's: the service could not tell them apart. Test runs are a second risk: a journey or a CI job that turned usage statistics on would send its test computer's checks as a person's.

## Decision

- **A mark for the team's installations.** `rosmarops usage team on` records `usage_team: true` in the state; `rosmarops usage team off` removes it. Each prints one line from the catalog (`console.usage_team_on`, `console.usage_team_off`). The general help does not show it: it is for the team.
- **Only the channel changes.** While the mark is on, every `usage.v1` summary carries the channel `internal` instead of the real one. No other field of `usage.v1` changes, and the channel is the only value replaced.
- **It is not consent.** The mark is kept outside the usage answer in the state, so marking an installation never answers the usage question, and nothing is sent unless the person turned usage statistics on.
- **Tests never turn usage statistics on.** Every journey answers no to the usage question; the journeys and the realistic end-to-end run check, at the end, that their test computers have usage statistics off and built no usage summary; a trust test fails if a steps file, workflow or script turns them on or sets the mark.

## Consequences

- `schemas/usage.v1.schema.json` (and its copy in `internal/schemas`): `channel` accepts `internal`. `schemas/state.v2.json` (and its copy): an optional `usage_team` boolean. Both are additions: earlier summaries and states stay valid.
- The service's own copy of `usage.v1` must accept `internal` too. Until it does, it refuses a team installation's summaries with 400, which the sends log records; the team's checks are then not counted at all, which is still not counting them as people's.
- `internal` is 8 characters, the width of the service's `channel` column.
