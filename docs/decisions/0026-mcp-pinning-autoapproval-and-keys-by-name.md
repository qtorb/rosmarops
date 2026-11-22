# 0026. MCP servers: unpinned launches, project auto-approval, and keys known by their name

Date: 2026-11-21

## Context

CTO-79 (§2) read how RosmarOps treats MCP servers and found four gaps:

- **Unpinned launches.** A local server started with `npx`, `uvx` or a similar launcher fetches its code each time it starts. Without an exact version, that code can change from one start to the next. RosmarOps parsed those launches only to compare an exact version with the threat data, and dropped every launch without one without saying so. It did not look at `docker run` at all.
- **Claude Code's project auto-approval.** With `enableAllProjectMcpServers: true`, Claude Code approves on its own every server a project's `.mcp.json` brings. RosmarOps already worked out each project server's approval, but no rule read it. A server the person had rejected still gave findings.
- **Keys without a known prefix.** A secret in an MCP server's `env` or `headers` was recognised only if its value matched one of about twenty prefixes. A random key with no prefix went unseen.
- **Codex.** A server with `enabled = false` counted as loaded.

## Decision

The direction was signed by Albert on 2026-11-21 at 18:04, and the codes, weights, texts and index version 7 at 18:10 (BV-274). `graph.v2` does not change.

- **`mcp_unpinned_launch`** (yellow).
  - **What counts:** a local MCP server started by `npx`, `bunx`, `pnpm dlx`, `uvx`, `pipx run` or `docker run` whose package or image has no exact version. That is no version, a range (`^`, `~`, `>=`, `*`, `x`), a tag (`latest`, `next`), or, for Docker, no `@sha256:` digest. `pkg@1.2.3`, `pkg==1.2.3` and `image@sha256:…` are exact.
  - **How the command is read:** the arguments are parsed in memory, also behind a `cmd /c` wrapper and the `--package`, `--from` and `--spec` flags. They are never stored. As with `package_known_malicious`, the mark rides on the server's node in memory and carries only the launcher and the package or image name.
  - **Findings:** one per agent and server it loads.
  - **Weight:** 6, ×1.5 when the server loads in every project (user scope), as `mcp_external_action`. Group `package`.
- **`mcp_project_autoapprove`** (yellow).
  - **What counts:** Claude Code with `enableAllProjectMcpServers: true` in a layer where it takes effect, with the precedence RosmarOps already uses for project servers:
    - a managed or user file always takes effect, because in a folder the person has not trusted a `true` there is enough;
    - a project or local file takes effect only in its trusted folder, and only when no file of higher precedence sets the key first.
  - **Findings:** one per file. `{scope}` in its sentence is that file.
  - **Weight:** 8. Group `agent_config`.
- **Keys known by their variable's name.** In an MCP server's `env` and `headers`, a value with no known prefix is recorded as a credential when its variable's name contains `TOKEN`, `KEY`, `SECRET`, `PASSWORD` or `PASSWD`, in any case.
  - **What is not:** a value under 8 characters, a reference (`${…}`), `true`, `false`, `null`, or a pure number.
  - **What is recorded:** a generic secret of unknown service, of class medium, with only its salted fingerprint. The value is never kept.
  - **Effect:** it feeds `cred_readable` and `cred_read_and_send` as any other key. There is no new rule.
- **Fixes.**
  - **Claude Code:** a project server the person rejected (`disabledMcpjsonServers`) is not loaded, so it gives no MCP finding.
  - **Codex:** `enabled = false` in `[mcp_servers.<id>]` means the server is not loaded.
  - **Grok Bot:** its app is also found on macOS (`/Applications/Grok Bot.app` or `~/Applications/Grok Bot.app`). Linux doesn't change.
- **Index and report.** `index_version` goes to 7, and the new weights count only from version 7. The two rule codes enter `report.v2`'s rule enum (signed with this direction). Like the other rules drawn again on every review, they are not kept in the findings memory.

## Consequences

- An MCP server whose code can change at every start shows as a finding in the agents that load it. So does Claude Code's automatic approval of project servers.
- A key with no known prefix but a telling name now counts as within reach of the agents that read that configuration file.
- A server the person rejected, or turned off in Codex, no longer gives findings it shouldn't.
- The space computes its own copy of the score: it must take the weights above and `index_version` 7 (a later BA).
