# 0027. OpenCode and Kilo Code scored

Date: 2026-11-22

## Context

CTO-79 (§3.1) lists OpenCode and Kilo Code among the agents RosmarOps detects without a score. Both are much used, and both let the agent do almost everything without asking by default. Kilo Code is a fork of OpenCode's code and keeps its configuration model, so the two can be read the same way.

## Decision

Signed by Albert on 2026-11-21 at 18:04 (first batch, BV-276), aimed at 0.19.

- **Sources, at the latest release:** OpenCode v1.19.6 (`f4e363c829`, published as «Latest») and Kilo Code v7.8.32 (`2aa002cc0a`), read on 2026-11-22. Where CTO-79 differs, the release wins:
  - MCP servers are under `mcp.<name>`, not `mcp.servers.<name>` as CTO-79 has it (`packages/core/src/v1/config/config.ts:113-115`);
  - OpenCode's default `"*": "allow"` asks in three cases, not one (`packages/opencode/src/agent/agent.ts:119-136`):
    - before reading `.env` files (`*.env`, `*.env.*`, but not `*.env.example`);
    - on `doom_loop`, the same tool call repeated in a loop;
    - outside the project (`external_directory`).
- **What is read**, JSON or JSONC, lowest precedence first:
  - **user:** `<xdg config>/opencode` (also on Windows and macOS), or `/kilo`;
  - **project:** the root files, then the dot folders `.opencode/`, `.kilocode/` and `.kilo/`;
  - **managed:** `/etc/<name>`, `/Library/Application Support/<name>` and `%ProgramData%\<name>`.

  Of each file only `permission` and `mcp`. For Kilo Code, also its legacy `{"mcpServers": …}` files: `.kilocode/mcp.json`, `.kilo/mcp.json` and the VS Code extension's `mcp_settings.json`.
- **Never opened:** the session database (`opencode.db`, `kilo.db`) and the credential files (`auth.json`, `mcp-auth.json`) of the data folder.
- **MCP servers** join the shared pipeline: keys, unpinned versions and the rest are treated as for every agent (ADR 0026). `{env:…}` and `{file:…}` references are not taken as secrets. A later file replaces a server, and `{"enabled": false}` turns it off.
- **Permissions in the graph.** Each category (read the project, read outside it, edit, run commands, reach the network, call MCP tools) gets its effective action per scope, as the product merges its files:
  - the merge order is: the default, the user, the project (only in that project), then the managed file;
  - inside a file, `"*"` applies first and then the tool's own key.

  One capability per category for the user scope, and one for each project whose own files change it:
  - a category no file sets is `product_default`, with its knowledge-base source;
  - one a file sets is `configured`, with the setting as its rule.
- **Factory values:**
  - **OpenCode:** reads, edits, runs any command, reaches the network and calls MCP tools without asking. It asks before reading outside the project.
  - **Kilo Code:** the same, but it asks before commands, except an allowlist.
- **The terminal read:** when commands run without asking, the agent gets the terminal-read capability: every credential within reach.
  - A file that allows `bash` gives it, as for the other agents.
  - Since 2026-11-22 (ADR 0028), OpenCode's factory default gives it too.
  - OpenCode asks before the file commands it recognises (`cat`, `cp`…) when their paths leave the project (`external_directory`, `packages/opencode/src/tool/shell.ts:262-279, 392-403` at v1.19.6). Any other program, such as `python -c` or `node -e`, reads anywhere without asking.
  - Kilo Code asks before commands except its allowlist (`cat`, `head`, `grep`…), and those same commands ask outside the project, so it gets no terminal read from its default.
- **Overview, knowledge and templates:**
  - the six rows are each category's user-scope decision;
  - the knowledge date `ReviewedOn` moves to 2026-11-22;
  - their templates show «in the next version», as every agent without one.

## Consequences

- OpenCode and Kilo Code get a score and a class on any computer that has them.
- Their factory score on the reference computer R2 is not pinned yet: the owner signs it first.
  - Without the factory terminal read (#159): 75 (`open_gaps`) for both.
  - With it (ADR 0028): OpenCode 31 (`exposed`) and Kilo Code 75 (`open_gaps`).
- They are on the full test computer since 2026-11-22 (Albert's signature). Claude Code's figure there goes from 11 to 10 (BV-237's 11 is replaced in the test, with a comment), because Claude Code can now change their configuration too: two more `foreign_agent_config_writable`. Their registry entries declare their user file.
- The Kilo Code extension's own VS Code settings (`kilo-code.new.*`, including `autoApprove.enabled`) are not read yet.
