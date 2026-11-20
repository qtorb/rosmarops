# 0023. Paseo, Claude Squad, Orca and Claude Code's subagents, read at rest

Date: 2026-11-20

## Context

ADR 0022 added the `orchestrator` agent type and the `launches` edge for Paperclip. CTO-76 read four more orchestrators and Claude Code's subagents:

- **Paseo** and **Claude Squad** keep, in plain JSON files, the mode in which they start each agent. For them RosmarOps can read whether an agent's approvals are skipped. With Paperclip it could only infer that.
- **Orca** keeps that setting in its SQLite database, which RosmarOps does not open. Its factory default, `full-access`, starts Claude Code, Codex, Gemini CLI, Cursor, GitHub Copilot and Grok Build skipping their approvals. Orca also writes hooks into those agents' configuration.
- **Claude Code's subagents** are Markdown files with a YAML header. The header can give them every tool, a permissive mode, hooks and their own MCP servers.

`launches` could only be inferred. That was right for Paperclip, but it would understate what Paseo and Claude Squad show in their own files.

## Decision

Signed by Albert on 2026-11-20 at 07:20 (BV-268). It is an addition inside `graph.v2`, version 2.

- **`launches` can be observed.** It is observed with `basis: configured` when the launched agent's mode is read at rest from one of the orchestrator's own files. Its source is then that file. Otherwise it is inferred with `basis: product_default`, as in ADR 0022. The schema accepts no other pairing.
- **The knowledge base** `internal/knowledge/orchestrators.json` covers Paseo v0.16.2, Claude Squad v1.0.20 and Orca v1.4.266. For each it holds:
  - which agents it starts;
  - in which of its modes it skips their approvals;
  - whether they get its environment;
  - Paseo's OpenAI keys, known by where they are.

  Every fact cites the release tag and the date it was checked.
- **Paseo** (`PASEO_HOME` or `~/.paseo`):
  - **What it reads:** `config.json` (`daemon.listen`, and which `apiKey` fields hold a value), each agent's provider and effective mode, each schedule's status and the mode of the agent it starts, and the projects' roots.
  - **Approvals:** these modes skip them: Claude's `bypassPermissions`, Codex's `full-access` and Copilot's `allow-all`.
  - **Schedules:** an active schedule wakes Paseo.
  - **Keys:** its OpenAI keys are credentials identified by their path.
  - **Never opened:** `daemon-keypair.json`, `local-credential` and `push-tokens.json`. The value of `launchEnv` is never kept.
- **Claude Squad** (`~/.claude-squad`):
  - **What it reads:** `auto_yes`, the default program and the profiles' programs. Of `state.json`, only each session's program and `auto_yes`, never `diff_stats`. It also sees whether `daemon.pid` exists.
  - **What `auto_yes` covers:** it answers Claude Code's and Gemini CLI's prompts, not Codex's.
- **Orca:**
  - **Presence:** its `userData` folder (`Orca`) or `~/.orca`.
  - **Approvals:** each `launches` edge is inferred from its factory default, with the assumption `orchestrator_settings_not_read`.
  - **Hooks:** its hooks in Claude Code's, Gemini CLI's, Codex's or Cursor's configuration are `writes_config_of` edges.
  - **Never opened:** `profile-state.db` and the files of `~/.orca`.
- **Claude Code's subagents:** `<claude config folder>/agents/*.md` and each repository's `.claude/agents/*.md`. Only the header is read, up to the line that closes it. Of it RosmarOps keeps:
  - whether `tools` is written;
  - `permissionMode`;
  - how many hooks are of type `command`;
  - the names of the MCP servers defined inline.

  The body is never read.
- **The rules.** `orchestrator_skips_approvals` now reads the `launches` edges of every orchestrator:
  - an observed edge is high confidence, and its finding points at the file that set the mode;
  - Orca's is medium confidence, because its assumption is a premise.

  `cred_readable`, `acts_without_you` and `foreign_agent_config_writable` apply as for any agent.

## Consequences

- A computer with Paseo or Claude Squad shows which agents they run without asking, from their own files. A computer with Orca shows it from its default, saying it is the default.
- The subagents are read, but no finding uses them yet. Their texts, and the one of `orchestrator_settings_not_read`, wait for the owner.
- Claude Squad's acceptance of Claude's «new MCP server» prompt, which CTO-76 reported, is no longer in its code (removed in `a4ab698`, 2026-03-28). It is not reported.
- Paradev and Dev Containers (CTO-76 §7, priority 2) fit the same model and are not in this change.
