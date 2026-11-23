# 0030. Claude Desktop scored, Continue detected

Date: 2026-11-23

## Context

CTO-79 (§3.1) puts Continue and Claude Desktop in the first batch of agents to score, with OpenCode and Kilo Code (ADR 0027). The reading before any code (`docs/research/bv-277/reading.md`) found two things the batch had not taken into account:
- Continue's repository says it is no longer maintained: it is read-only, and 2.0.0 is the last release of the extension, the CLI and the JetBrains plugin (continuedev/continue `5522c6f`, `README.md:19`, `README.md:27-29`);
- Claude Desktop has an official Linux build, in beta, since v1.17377.1 (2026-06-30). RosmarOps did not look for it on Linux.

Both keep part of what decides their permissions in the app's own storage, not in a file:
- Continue's per-tool choices are in the extension's webview localStorage;
- Claude Desktop's «Allow always» approvals, its connectors and Cowork's approval mode are in its databases (IndexedDB, Local Storage).

## Decision

Signed by Albert on 2026-11-23 (BV-277), aimed at 0.19.

**Continue: detected, not scored.**
- The reason is a new not-evaluated code, `continue_unmaintained`, added to the report.v2 reason enum.
- What is read:
  - the global folder `~/.continue`, or `CONTINUE_GLOBAL_DIR` when it is an absolute path;
  - its MCP servers only, through the shared MCP pipeline, with the treatment of keys and versions of every agent (ADR 0026). They come from `config.yaml`, or `config.json` when there is no `config.yaml`, and from every YAML or JSON file under each project's `.continue/mcpServers`.
- A `${{ secrets.X }}` or `${VAR}` value is a reference, not a secret. A `uses:` entry is a block from Continue's hub, fetched over the network, so it is not read.
- **Never opened:** `permissions.yaml`, the `.env` files and the other block folders.
- There is no registry entry, knowledge base, template or reference-computer case.

**Claude Desktop: scored, also on Linux (`~/.config/Claude`).**
- **What it can do:** outside Cowork, it acts on the computer only through MCP tools, from the servers of `claude_desktop_config.json` and its desktop extensions (`.mcpb`).
- **Factory mode:** it asks before each MCP tool call. The source is the Model Context Protocol documentation, «Connect to local MCP servers», version 2026-07-28, read for v6.28899.6. This is one capability, `invoke_mcp_tool` `ask`, from the knowledge base `claude-desktop.json`.
- **Assumption:** what lives only in its databases is never opened and is declared as an assumption: it asks before each tool, as from the factory. Cowork is outside the score and the report says it is not read. The Code tab is not scored apart: it is Claude Code.
- **Desktop extensions:** RosmarOps lists only the file names in `Claude Extensions Settings`, to know which extensions are installed. Those files can hold an extension's secrets and are never opened. That they are active is an assumption declared in the report.
- **Managed policy:** `isLocalDevMcpEnabled`, `isDesktopExtensionEnabled` and `mcpPersistentAlwaysAllowEnabled`.
  - **Windows:** read from `HKLM`, else `HKCU`, `\SOFTWARE\Policies\Claude`, with the registry reader GitHub Copilot and Codex already use. When `HKLM` holds any of these values, `HKCU` is ignored.
  - **macOS:** read from the managed preferences of `com.anthropic.claudefordesktop` in `/Library/Managed Preferences/` (the computer's, then the user's).
  - **Linux:** read from `/etc/claude-desktop/managed-settings.json`.
  - **The files** are read only after the administrator ownership check of the company configuration (ADR 0018). On Linux and macOS: a regular file, not a link, which with every folder up to the root belongs to root with no write for group or others. macOS still has no company mode. A file that fails the check is not read, and the report lists it as not read.
  - **Effects:**
    - `isLocalDevMcpEnabled` false: the servers of the config file stay on disk (their keys are still credentials), but load nothing;
    - `isDesktopExtensionEnabled` false: no extension is assumed active;
    - `mcpPersistentAlwaysAllowEnabled` false: the MCP capability is configured by that policy.
- **Plist reader:** managed preferences are often binary property lists. `internal/plistsubset` reads the small part RosmarOps needs, XML or binary, with no dependency: the top-level booleans, integers and strings. Any other value is skipped, and a malformed file is refused whole.
- **Reference computer:** Claude Desktop's factory score and its class on the exposure map are computed, not pinned: the owner signs them. It is not on the full test machine yet, because that would move the reference figures.

## Consequences

- One more scored agent and one more detected one. The knowledge review date moves to 2026-11-23.
- The texts the person reads for the new reason, Claude Desktop's assumptions, its Cowork line and its extensions line come from the owner. Until then they are marked `[pending copy]`.
- The ownership check now runs on macOS too, for files of other products. The company configuration and the administrator fingerprint list are still not looked for there.
