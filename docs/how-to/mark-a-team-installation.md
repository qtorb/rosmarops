# How to mark an installation as the team's

The usage statistics (`usage.v1`, see [what is sent](../usage-statistics.md)) count people's checks. The RosmarOps team runs RosmarOps too, on its own computers, and those checks must not be counted as people's. An installation the team uses is marked once, and its summaries are then counted apart.

## Mark it

```
rosmarops usage team on
```

It prints «This installation is now marked as RosmarOps team. Its usage statistics will be counted apart.» and records `usage_team: true` in `~/.rosmarops/state.json`.

To remove the mark:

```
rosmarops usage team off
```

It prints «This installation is no longer marked as RosmarOps team.».

The command is for the team, so the general help does not show it.

## What changes

- While the mark is on, each `usage.v1` summary carries the channel `internal` instead of the real one (`store`, `homebrew` or `other`). No other field changes.
- The mark is not the answer to the usage question. If usage statistics are off, or the person has not answered yet, nothing is sent, mark or no mark. Marking an installation never answers the question, and `rosmarops usage on` or `off` never changes the mark.
- The mark belongs to the installation's state. A new state (a new computer, or a state started from scratch) starts unmarked.

## Tests never turn usage statistics on

Test computers are not the team's installations: they must send nothing. Every journey answers no to the usage question, and the journeys and the realistic end-to-end run check at the end that their test computers have usage statistics off and built no usage summary ([journeys](journeys.md)). A trust test fails if a steps file, workflow or script turns them on or marks an installation.

## Where it is

- The command: `runUsageTeam` in `cmd/rosmarops/cmd_usage.go`; the channel is replaced in `sendChannel`, just before the summary is built.
- The state field: `usage_team` in `schemas/state.v2.json`; the channel value: `internal` in `schemas/usage.v1.schema.json`.
- The service (`app.rosmarops.com`) accepts `internal` once its own copy of `usage.v1` has it. Until then it refuses a team installation's summaries (400), and the sends log says so; nothing else breaks.
- The decision: [ADR 0024](../decisions/0024-team-installations-counted-apart.md).
