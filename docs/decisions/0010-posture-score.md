# 0010. The posture score: formula, bands and versioned weights

Date: 2026-10-24
Amended: 2026-10-26
Amended: 2026-10-30
Amended: 2026-11-17

## Context

People asked for one number they could follow over time. A plain subtraction from 100 saturates at 0 or sticks at a ceiling. A number that disagrees with the traffic light (red, yellow, green) would confuse more than it helps. And a score is only honest if it says when it cannot be computed.

## Decision

Each evaluated agent gets an Agent Posture Score out of 100. Higher is better. It is computed by `index.Compute` in `internal/index/index.go`.

**Bands follow the agent's worst finding.** The band is the traffic light:
- any red finding: `exposed`, 0–49;
- yellow findings and no red: `open_gaps`, 50–79;
- neither: `contained`, 80–100.

**Formula.** `P` is the sum of the weights of the agent's findings, plus 2 for each medium credential readable with no way out. With `K = 80` and `f = K / (K + P)`:
- `exposed`: `⌊49 × f⌋`;
- `open_gaps`: `50 + ⌊29 × f⌋`;
- `contained`: `80 + ⌊20 × f⌋` (100 only when `P = 0`).

The score never leaves its band, and every fix moves it.

**Weights** (`weightIn`). `cred_read_and_send` 20 for a high credential, 8 otherwise; `cred_readable` 8; `foreign_agent_config_writable` and `own_config_writable` 15; `foreign_agent_instructions_writable` 6; `own_instructions_writable` 4. The two credential rules weigh ×1.5 when the radius is `any_project`. Later rules: `cred_in_git` (30 red with a remote, 20 red, 8 yellow); `mcp_external_action` (money 20 red or 6, infrastructure and database 15, other 8, ×1.5 with `any_project`); `computer_use_app_allowed` (15 red, 6 yellow); `acts_without_you` (12, 8 or 4, by who can wake the agent). Confidence and group size never change a weight.

**Version.** `index.Version` (today 5) is `index_version`. It goes up when the weights, `K`, the bands or the coverage conditions change. `weightIn(version, f)` keeps the older weights, so a run can score the same graph with the previous method and report the method's share apart (`MethodDelta`).

**Coverage.** An agent has a score only if the graph was built and no source inside its own configuration folder failed to be read. Otherwise the score is unavailable, with a reason code (`coverage`). The machine score exists only when every agent present is evaluated, and it is the worst of them. A machine with no agent is `not_applicable`, never 100.

**Comparison.** A run is compared with the last entry of `index-history.json` (`schemas/v2/index-history.v1.schema.json`, at most 104 entries, no paths or names). It is not comparable when the rules or knowledge versions differ, when the previous index version is newer, when the evaluated agents differ, or when the number of repositories drops by more than 10 %.

## Consequences

- Each finding shows its weight and the score without it, so the report can say "fix this and the score goes from X to Y".
- Removing sources or repositories cannot raise the score quietly.
- The method is public in the code; examples are synthetic.
- The share card is built only from the score, band, counts, date and method version (`internal/index/card.go`, `tests/trust/no_identity_in_card_test.go`).
