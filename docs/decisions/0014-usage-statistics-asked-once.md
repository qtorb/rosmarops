# 0014. Usage statistics, asked once

Date: 2026-11-18

## Context

Built in version 0.15.0. What is sent, and where the code is, is described in [usage-statistics.md](../usage-statistics.md).

The owner cannot tell how many people use RosmarOps, on which systems, with which agents, or which findings are common. Problem reports (ADR 0013) only arrive when something fails. The rule from that decision still holds: nothing leaves the computer without permission. The question is how to ask without asking at every run.

## Decision

- **Ask once.** At the end of the first interactive review with a version that has this feature, if the state has no decision, the console shows what would be sent and asks once. Pressing Enter means yes (replaced by [ADR 0033](0033-enter-is-not-yes-for-usage.md): Enter alone leaves it for later). If the person says yes, each later review sends one summary without asking again. A review with no console (a scheduled review, CI, `session-check`, `statusline`) never asks and never sends.
- **Easy to turn off.** A command turns it off and on, and the report always says the current state and how to change it. If what is sent changes, the program asks again once.
- **A closed summary**, a new format `usage.v1` built from the report already computed: a random installation id created on yes, the day, the program version, system and architecture, the install channel, the agents found by product id (every product the review finds, evaluated or only detected, from a closed list), the score, class and status, finding codes with their counts, whether a space is connected and whether the Claude Code plugin is installed. Every field is an enum, a number or a boolean. Never paths, file, project or computer names, keys or configuration content.
- **Sent the same way as problem reports:** through `internal/spacenet` only, to `app.rosmarops.com`, with the permission recorded in the state. A failed send does not change the review's status. The service keeps these summaries for 400 days, not linked to any account.
- **A new public promise** replaces the old "never sends telemetry": "What it reads from your computer does not leave without your permission, and your keys never do."

## Consequences

- The network invariant, `docs/trust-model.md` and the project instructions list this use of `internal/spacenet`.
- A new schema (`schemas/usage.v1.schema.json`, the same file as the service's), a small package (`internal/usage`) and trust tests on the same pattern as problem reports: the package has exactly the schema's keys, a no-leak test with fake paths and secrets, a network test that sees no connection without permission and only `app.rosmarops.com` with it, a failed send that changes nothing, and a contract test against the service's copy of the schema.
- The install channel `store` uses the same identity check as the Microsoft Store update in the integrity check (`winpkg.IdentityIsOwn`), so the two cannot disagree.
