# 0036. Codex's read-only sandbox, and each system's real paths off Windows

Date: 2026-11-24

## Context

A reading of the engine (BV-290) found three gaps.

- **Codex read-only:** with `sandbox_mode = "read-only"` in Codex's user config, the score did not change. The read-only layer added its capabilities, but the knowledge base's factory write to the workspace (`write_workspace`: write files, allow, working directory) stayed in force beside them. So Codex kept writing its own instructions (`AGENTS.md`) without asking.
- **Windows paths on Linux:** several readers in `adapters/windows`, which also run on macOS and Linux, used a Windows path on Linux.
  - GitHub Copilot's VS Code settings, and Claude Code's VS Code mode, were looked for in `~/AppData/Roaming/Code/User`, which does not exist there. VS Code keeps them in `~/.config/Code/User`.
  - Codex's system `requirements.toml` was looked for only under `%ProgramData%`.
- **Stale comments:** comments said the guided templates are not measured, while Cursor's P0 and P1 and Grok Build's P2 are.

Signed by Albert with BV-293 (24-11).

## Decision

- **Codex read-only.** When the user config's effective sandbox is read-only (`sandbox_mode = "read-only"`, or `default_permissions = ":read-only"`), the factory capability `write_workspace` is not added: what is configured overrides the product's default (`internal/graph/codex.go`, `withoutCodexReadOnlyWrite`). This is as Cursor's run mode already does in `ProductDefaults`.
  - **Source:** Codex's documentation agrees that in this mode Codex does not edit files without approval: «`read-only`: The agent can inspect files, but it can't edit files or run commands without approval» (https://learn.chatgpt.com/docs/sandboxing, read 2026-11-24).
  - **Not changed:** commands and network keep their defaults, because two official pages disagree on commands and neither gives a network rule of its own.
  - **Project configs:** a trusted project's config never removes the default, because it applies only inside that project.
  - **Graph schema:** unchanged. No new reason for a capability that is not in force: the default is simply not added.
- **Real paths off Windows.** Every path comes from the layout (`internal/agentdirs`):
  - **VS Code's user folder,** for GitHub Copilot and for Claude Code's VS Code mode: `layout.VSCodeUserDir`.
    - macOS: `~/Library/Application Support/Code/User`.
    - Linux: `$XDG_CONFIG_HOME/Code/User` or `~/.config/Code/User`.
    - Source: «Settings file locations», https://code.visualstudio.com/docs/configure/settings, read 2026-11-24.
  - **Codex's system `requirements.toml`:** `/etc/codex/requirements.toml` on Linux and macOS. Source: «System requirements.toml (/etc/codex/requirements.toml on Unix systems, including Linux and macOS…)», https://learn.chatgpt.com/docs/enterprise/managed-configuration, read 2026-11-24. It is only read, as on Windows.
  - **Grok Bot:** its documentation says the app runs on Linux (`.deb`, `.rpm`, AppImage), but gives no install folder (https://docs.x.ai/grok-bot/get-started and /grok-bot/deployment, read 2026-11-24). Its app folder is therefore not looked for on Linux. `~/.grokbot/settings.json` is read there, as before.
- **The knowledge base:** Codex's moved page is cited at its new address, `/docs/config-file/config-reference`.
- **Comments:** the stale comments, and the how-to row that named `p2ProjectJSON`, say what the code does.

## Consequences

- **What the program reads that it did not read before:**
  - on Linux, VS Code's `settings.json`, its profiles, `mcp.json` and `globalStorage` folder under `~/.config/Code/User`, for Copilot and for Claude Code's VS Code mode;
  - on Linux and macOS, `/etc/codex/requirements.toml`.

  All are read only. The program still opens no token store, secret store or database.
- **What changes in the score:**
  - **Codex with `read-only`:** on the reference computer, 73 becomes 74. The write to `AGENTS.md` (`own_instructions_writable`) goes, and the readable key (`cred_readable`) stays.
  - **Copilot and Claude Code on Linux:** their scores now follow the person's VS Code settings, as on Windows and macOS.
  - **Factory scores:** none of the twelve changes. `TestR2FactoryScores`, which pins them, passes unchanged; the factory pass drops Codex's sandbox setting.
- **The index version stays 7:** no weight, K, band or coverage condition changes.
- **Tests:**
  - `tests/trust/bv293_codex_readonly_test.go`;
  - `adapters/windows/bv293_paths_internal_test.go`: VS Code's folder on each system, Copilot's and Claude Code's settings read in it on Linux and macOS, and Codex's requirements off Windows.
