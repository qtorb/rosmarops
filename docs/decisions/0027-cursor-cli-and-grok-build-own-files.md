# 0027. Cursor's CLI and sandbox network, and Grok Build's own hooks, sandbox and system files

Date: 2026-11-22

## Context

CTO-79 (§4.4) listed what Cursor and Grok Build document and RosmarOps did not read:

- **Cursor.**
  - In `cli-config.json`, the CLI's run mode (`approvalMode`), its sandbox mode (`sandbox.mode`) and its network setting (`sandbox.networkAccess`).
  - In `sandbox.json`, `networkPolicy`.
  - The team hooks in `~/.cursor/managed/team_<id>/hooks/`.
- **Grok Build.**
  - Its own hooks in `~/.grok/hooks/*.json` and `.grok/hooks/*.json`.
  - `sandbox.toml`.
  - `/etc/grok/managed_config.toml` and `/etc/grok/requirements.toml`.
  - The code also assumed three vendors it reads from (Cursor, Claude Code and Codex), with six surfaces each. The documentation names two vendors with five surfaces.

## Decision

The direction is BV-275, asked for by Albert on 2026-11-22. He signed the five answers below on 2026-11-22. No rule is added, no weight changes, `index_version` stays at 7, and `graph.v2` does not change.

### Cursor

Sources: `cursor.com/docs/cli/reference/configuration.md`, `.../cli/reference/parameters.md`, `.../reference/sandbox.md`, `.../agent/security/run-modes.md` and `.../hooks.md`, all read on 2026-11-22.

1. **`approvalMode: unrestricted`** in the user's `cli-config.json` is Cursor's Run Everything. It opens the ways that already exist: `mode_run_everything` and `egress_run_everything`. When the IDE's mode and the CLI's differ, the least controlled one counts. `allowlist` and `auto-review` change nothing. A project's `cli.json` holds only permissions, so its `approvalMode` is ignored.
2. **`sandbox.mode: disabled`** is no sandbox for the CLI, as `type: insecure_none` is. Its values, `enabled` and `disabled`, are those of the `--sandbox` option.
3. **`sandbox.networkAccess`** is read but never scored, because its values are not documented. The page says only «Network access setting for sandbox mode».
4. **`networkPolicy.default: "allow"`** opens every domain inside the sandbox. A command that runs in the sandbox without a person can then send data out: under Auto-review with the sandbox, or Allowlist with the sandbox. This is a new way, `egress_sandbox_network`, within the existing egress rule, so there is no new rule. `"deny"` is the default and adds nothing.
5. **Team hooks.** RosmarOps lists the files in `~/.cursor/managed/team_<id>/hooks/` and never opens them. Each one is one of Cursor's executables. Their presence does not show what else the team's policy sets, so `team_policy_unknown` stays.

### Grok Build

Sources: `docs.x.ai/build/features/hooks`, `.../features/sandbox`, `.../settings/reference` and `.../enterprise`, all read on 2026-11-22.

- **Layers in the documented precedence.** From lowest to highest:
  1. `/etc/grok/managed_config.toml`;
  2. `~/.grok/managed_config.toml`;
  3. `~/.grok/config.toml`;
  4. `~/.grok/requirements.toml`;
  5. `/etc/grok/requirements.toml`.

  The `/etc/grok` files are read only on macOS and Linux, because Grok does not read them on Windows. The user's config now wins over the managed config, which before won over it. The requirements pin `[sandbox] profile`.
- **Its own hooks.** Each `*.json` in `~/.grok/hooks/` is one of Grok's executables. So is each one in a project's `.grok/hooks/`, but only when the folder is trusted. RosmarOps keeps no key of these files.
- **`sandbox.toml`.** The profiles of the user's file are read: `extends`, `restrict_network`, `read_write` and `deny`. A custom profile in `[sandbox] profile` counts as the built-in profile it extends, and as `workspace` when it names none. A project's `sandbox.toml` is read for coverage only.
- **Vendors.** Grok reads `[compat.cursor]` and `[compat.claude]`, each with `skills`, `rules`, `agents`, `mcps` and `hooks`. `[compat.codex]` and `sessions` are no longer read.

### Knowledge

The knowledge files of Cursor and Grok Build move to version 2026-11-22.

## Consequences

- **The score can go down** for a Cursor set to `unrestricted` in its CLI, with the CLI's sandbox off, or with `networkPolicy.default: "allow"`.
- **Grok Build's posture can change** when a managed config set a different sandbox profile from the user's config. The user's config now wins, as the documentation says.
- **First review after the update.** The knowledge version changes, so that review does not compare its score with the one before.
- **Open.** `sandbox.networkAccess` will be scored once its values are documented.
