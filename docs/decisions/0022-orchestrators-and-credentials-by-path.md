# 0022. Orchestrators and credentials known by their path

Date: 2026-11-20

## Context

Paperclip is a local program that starts Claude Code, Codex, Cursor, Gemini CLI, Grok Build and Hermes Agent and gives them work. By default it starts most of them skipping their approvals, gives them its whole environment, and can wake them without anyone in front. It keeps a master key next to its database, so anything that reads its folder can open every secret it stores. Two of its versions before 2026.416.0 have critical, published flaws.

None of that fitted the graph:
- there was no kind of agent that starts other agents, and no edge to say it;
- `wakes` had to be read in a file, and Paperclip's scheduler is on by default but not written anywhere;
- every credential needed a fingerprint of its value, so a secret could only be in the graph if RosmarOps read it. Paperclip's master key and `.env` must never be opened.

## Decision

Signed by Albert on 2026-11-20 (CTO-75, CTO-76). These changes are additions inside `graph.v2` and `report.v2`, version 2, as before with BV-208 and BV-251.

- **Orchestrators.** An `agent` can be of type `orchestrator`.
- **`launches`.** A new edge from an orchestrator to an installed agent it supports. It says:
  - whether the orchestrator overrides the agent's approvals: `skips_all`, `keeps_agent_settings` or `unknown`;
  - whether the agent gets its environment;
  - where that comes from (`product_default` or `configured`).

  It is always inferred, with the assumption `orchestrator_agent_configured`: which agents an orchestrator has configured is in its database, which RosmarOps does not open. The facts come from `internal/knowledge/paperclip.json`, with the source and date of each.
- **`wakes` from a product default.** `wakes` may be inferred when its source is a product default (`kb:`), such as Paperclip's scheduler.
- **Credentials known by their path.** A `credential` with `identified_by: path`:
  - has no fingerprint of its value, because the value is never read;
  - has instead a fingerprint of its normalized location: a file, or a file and a JSON path for a key in a configuration file;
  - has its file's owner, relative to the user the review runs as, and its permissions where the system has them.

  The fingerprint does not change when the file's content changes. `cred_readable` is worked out on these nodes as on any other. Credentials identified by their value do not change: their id, fingerprint and findings are the same. Paperclip's `secrets/master.key` (next to `db/`), `.env`, `llm.apiKey` and `database.connectionString` are of this kind.
- **Four rules.**
  - `orchestrator_skips_approvals`: red when it skips them, yellow when it depends on how the agent was created (Codex).
  - `orchestrator_vulnerable_version`: red, from `cli/install.json` or npm's cache.
  - `orchestrator_network_listen`: red with open sign-up, yellow otherwise.
  - `orchestrator_open_signup`: only in `authenticated` mode; red when it also listens beyond the computer, yellow otherwise.

  Listening and sign-up come from `config.json` and, when there is one, a Paperclip service unit. Without a unit they carry `launch_flags_not_readable`: Paperclip started by hand with other variables leaves no trace on disk.
- **What is never opened.** Paperclip's database, data, master key, `.env`, run logs and MCP configs. Of those, RosmarOps sees only whether they exist, their permissions and their owner.
- **Telemetry.** Paperclip's telemetry, on by default, is recorded as a fact in the run log and the knowledge base, with no finding.

## Consequences

- A computer with Paperclip shows which of its agents Paperclip can put to work without asking. Its version, listen address and sign-up show when they put it at risk.
- The orchestrator findings are on Paperclip, which is not a scored agent: they do not change any agent's score.
- These findings are drawn again on every review and are not kept in the findings memory.
- Other orchestrators (Orca, Paseo, Claude Squad, Paradev, CTO-76) fit the same type and edge without another schema change.
