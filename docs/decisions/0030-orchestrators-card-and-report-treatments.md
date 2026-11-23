# 0030. «Your orchestrators», and three visual treatments of the report

Date: 2026-11-23

## Context

The decision layer signed on 2026-11-21 (point 5) asks for one view per orchestrator. That view shows:
- the agents the orchestrator can start;
- whether it removes their questions;
- whether this is observed or assumed;
- each agent's own reach.

It must do this without changing the schema. Albert also asked to set some sections apart visually.

The design was signed on 2026-11-23 at 09:24 (BV-278, vigil `docs/research/2026-11-23-mapa-de-la-flota-diseno.md`). The advisor's answers to the reading were signed the same day, with three changes:
- no link to the Observatory;
- the grey note uses the catalog's assumption texts;
- the trust test covers the card.

## Decision

- **«Your orchestrators»** is a card between block 3 and the folds. It appears only when the graph has an orchestrator with a `launches` edge, and Paperclip appears like the others.
  - **One group per orchestrator,** sorted by name. Each has a table with one row per edge, sorted by agent.
  - **Columns:**
    - the agent, and a line when it gets the orchestrator's environment variables;
    - whether it asks before acting (`approval_override`);
    - how we know: «Read in <file>» with the file's base name only, read as untrusted text, or «Assumed: its factory setting»;
    - the agent's own score and level;
    - «See the warning ›» to the problem of its `orchestrator_skips_approvals` finding. The cell is empty when there is none, or when it is dismissed or accepted.
  - **Under the table:** one grey line per distinct assumption of the group's edges, with the catalog's `unknown.<code>` text. The assumptions every agent shares are left out, as in each finding's list.
  - **No URL.** The trust test `TestNoExternalInHTML` now also checks a page with the card.
- **«To understand»** (`.r3-und`) is the treatment of a card that explains and asks for no action: a brand-tinted background and an eyebrow line with its icon.
- **The «Key» tag** (`.r3-tag`) marks the credential problems: `cred_readable`, `cred_read_and_send` and `cred_in_git`. It goes before the title in the table and after it in the detail.
- **The frontier** (`.r3-detail`) separates the summary from the problems' steps, when there is at least one problem. It sits just above `main` and is as wide as it.
- **No schema change.** No rule, weight or `index_version` changes, and `graph.v2` and `report.v2` stay as they are: the card's rows are kept in memory, as the credential services already are.

While writing the card's tests, a defect came to light. The orchestrator and subagent findings were added after «No me afecta» and «Lo asumo» were applied, so a dismissed one still counted and an accepted one had no date. Both lists now apply to them too (`applyPersonLists`).

## Consequences

- **A report with an orchestrator gets the card.** Every report with a problem gets the frontier, and every credential problem gets the tag.
- **The golden page changes** only in the CSS, the frontier and the tag.
- **The sample computer gets no orchestrator.** An orchestrator that skips questions adds an urgent finding and moves the sample's reference figures, so the card is tested with its own fake graph and a real run on a fake folder.
- **A dismissed orchestrator or subagent finding now leaves the score,** as every other dismissed finding does.
- **The space's view** of the report does not change in 0.19.
