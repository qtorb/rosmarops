# Continue and Claude Desktop: what can be read from files (BV-277, part 1)

Consulted on 2026-11-22. Each claim cites the product's source (Continue, file:line at commit `5522c6f`) or a public page. This is the reading that comes before any code; the reader, the graph and the score follow it only once it is agreed.

## 1. What RosmarOps reads today

**Claude Desktop**
- Detected on Windows and macOS by `claude_desktop_config.json`: `%APPDATA%\Claude\`, the Microsoft Store copy under `%LOCALAPPDATA%\Packages\Claude_pzs8sxrjxfjjc\LocalCache\Roaming\Claude\`, and `~/Library/Application Support/Claude/` (`internal/agentdirs/agentdirs.go`).
- Not searched on Linux, on the grounds that no Linux build exists (BV-165). That is no longer true: see §3.
- Its `mcpServers` are read through the shared MCP pipeline: command, args, env and headers as credentials (`adapters/windows/mcp.go`, step 5). A coverage note says when the two Windows copies list different servers.
- It is shown as «detected, not evaluated» (`internal/rules/overview.go`, reason `claude_desktop`: it keeps its approvals inside the app).

**Continue**
- Known only as the VS Code extension `continue.continue` (`internal/knowledge/knowledge.go`). Nothing under `~/.continue/` is read.

**Missing for a score, both**
- a reader for each product;
- a knowledge base with its factory mode and sources;
- an entry in the agent registry (`internal/agents/registry.go`);
- a graph fragment with permissions and the factory mode;
- overview cells;
- report templates and catalog keys;
- R2 builders for the factory score.

## 2. Continue

**Versions**
- VS Code extension 2.1.0 (tag `v2.1.0-vscode`, 2026-06-18). Main's `extensions/vscode/package.json:5` still says 1.3.40. The Marketplace page is blocked from here, so I could not confirm.
- CLI `@continuedev/cli` 1.5.47 on npm (2026-06-18).

**Status**
- `README.md:19` says the repository is «no longer actively maintained and is read-only». `README.md:27-29` calls 2.0.0 the final release of the extension, the CLI and the JetBrains plugin.
- The last commit on main is 2026-07-20, docs only.

**Files**
- Global folder `~/.continue` (`%USERPROFILE%\.continue` on Windows), or `CONTINUE_GLOBAL_DIR` (`core/util/paths.ts:27-36`; the CLI too, `extensions/cli/src/env.ts:9-11`).
- `config.yaml` takes precedence. `config.json` loads only when `config.yaml` does not exist (`core/config/profile/doLoadConfig.ts:103-127`). `config.ts` and the `.continuerc.json` files apply only on the JSON path and are deprecated (`core/config/load.ts:142-148, 671-724`).
- Block folders, read recursively in the project (`.continue/<type>/`) and in the user folder (`~/.continue/<type>/`): `mcpServers`, `rules`, `prompts`, `models`, `context`, `data`, `docs`. The extensions are `.yaml` and `.yml`, plus `.md` for rules and prompts (`packages/config-yaml/src/load/getBlockType.ts:4-12`, `core/config/loadLocalAssistants.ts:103-125`).
- `*.json` files in `.continue/mcpServers/` are read in the Claude Code, Claude Desktop or Cursor shapes, or as a single server (`core/context/mcp/json/loadJsonMcpConfigs.ts:39-150`).
- The CLI also loads hooks from `~/.claude/settings.json`, `.claude/settings.json`, `~/.continue/settings.json` and `.continue/settings.json` (`extensions/cli/src/hooks/hookConfig.ts:73-91`), and skills from `.continue/skills` and `.claude/skills`.
- Secrets come from `~/.continue/.env`, then `.continue/.env`, then `<repo>/.env` (`core/config/yaml/LocalPlatformClient.ts:59-102`).

**MCP entries**
- Keys: `name`, `command`, `args`, `env`, `cwd`, `type`, `url`, `apiKey`, `requestOptions.headers` (`packages/config-yaml/src/schemas/mcp/index.ts:4-34`).
- An entry can instead be `uses: owner/package`: a hub block fetched over the network, so not readable at rest (`interfaces/slugs.ts:98-123`).
- Templates take the form `${{ secrets.X }}`.

**Factory permissions, IDE extension** (`core/tools/definitions/*.ts`)
- Allowed without asking: reading files, listing, searching, web search, viewing diffs.
- Asks first: terminal commands (`runTerminalCommand.ts:63`), creating and editing files, fetching URLs.
- Every MCP tool asks first: `DEFAULT_TOOL_SETTING = "allowedWithPermission"` (`gui/src/redux/slices/uiSlice.ts:34`).
- Reads outside the workspace are forced to «ask» (`core/tools/policies/fileAccess.ts`).
- **The person's per-tool choices are kept in the webview's localStorage** (`gui/src/redux/store.ts:55-95`), not in a file. `config.yaml` has no permission key. There is no auto mode in any file.

**Factory permissions, CLI**
- Defaults (`extensions/cli/src/permissions/defaultPolicies.ts:7-40`):
  - asks: Edit, MultiEdit, Write, Bash and MCP;
  - allows: Read, List, Search, Fetch.
- In headless mode (`-p`), Bash and MCP are allowed.
- `--auto` allows everything. `--readonly` excludes writes.
- `~/.continue/permissions.yaml` takes only the keys `allow`, `ask` and `exclude`, as lists of `Tool` or `Tool(pattern)`; any other key makes the whole file invalid (`permissionsYamlLoader.ts:11-60`).
- Precedence: flags, then `permissions.yaml`, then defaults (`precedenceResolver.ts:26-58`).
- There is no project-level permission file.

**Managed configuration**
- None on the computer: no ProgramData, `/etc` or registry path.
- The organization's policy (`schemas/policy.ts`) lives in the Continue hub, in the cloud.

## 3. Claude Desktop

**Version**
- v6.28899.6 (2026-11-20), bundling Claude Code 2.1.330 ([changelog](https://claude.com/docs/cowork/claude-desktop-changelog)).
- **Linux is official, in beta, since v1.17377.1 (2026-06-30)**, as a `.deb` for Ubuntu 22.04+ and Debian 12+ ([code.claude.com/docs/en/desktop-linux](https://code.claude.com/docs/en/desktop-linux)).

**`claude_desktop_config.json`**
- Paths:
  - macOS: `~/Library/Application Support/Claude/`;
  - Windows: `%APPDATA%\Claude\`;
  - Linux: `~/.config/Claude/`.
  - Source: the MCP document dated 2026-07-28, read from its source on GitHub because modelcontextprotocol.io is blocked from here.
- The Microsoft Store copy is described only in bug reports.
- What the file decides:
  - `mcpServers.<name>` with `command`, `args` and `env`;
  - `chromiumFlags`;
  - `preferences`, where bug reports describe `allowAllBrowserActions` and scheduled-task switches, with no official documentation;
  - remote servers are added as connectors in the app, not in this file.
- The Code tab also loads these servers into its local sessions ([code.claude.com/docs/en/desktop](https://code.claude.com/docs/en/desktop)).

**Factory mode**
- Every MCP tool call asks first: «All actions require your explicit approval before execution» (MCP document).
- «Always allow» exists per tool (changelog v1.24012.9, 2026-07-24).
- **Where those approvals are kept is not documented.** The data page lists `IndexedDB/` and `Local Storage/`: app databases, which are never read.
- No user-level file pre-approves tools.

**Extensions (.mcpb)**
- On disk:
  - `Claude Extensions/<id>/manifest.json`;
  - `extensions-installations.json`;
  - `Claude Extensions Settings/<id>.json`, with the enabled flag and `user_config` values.
- That layout is not officially documented: a third-party page describes it.
- `user_config` values marked `sensitive` go to the system keychain; the rest stay in that JSON ([desktop-extensions](https://www.anthropic.com/engineering/desktop-extensions)).
- The manifest declares the server (`server.mcp_config`) and its `tools`, with no permission field ([MANIFEST.md](https://github.com/modelcontextprotocol/mcpb/blob/main/MANIFEST.md)).

**Built-in tools**
- Cowork is in every conversation on Pro and Max since 2026-09-16. It gives shell and file tools in a local virtual machine, over folders the person connects (changelog).
- The approval mode («Manually approve», «Automatically approve», «Skip all approvals») is chosen in the app, not in a file.
- Computer use is off until the person turns it on.

**Managed configuration** ([support.claude.com/en/articles/12622667](https://support.claude.com/en/articles/12622667-enterprise-configuration); [configuration-format](https://claude.com/docs/third-party/claude-desktop/configuration-format))
- Locations:
  - Windows: `HKLM\SOFTWARE\Policies\Claude` (or HKCU);
  - macOS: `/Library/Managed Preferences/com.anthropic.claudefordesktop.plist`;
  - Linux: `/etc/claude-desktop/managed-settings.json`, owned by root.
- Keys that affect MCP and extensions:
  - `isLocalDevMcpEnabled`;
  - `isDesktopExtensionEnabled`;
  - `isDesktopExtensionSignatureRequired`;
  - `mcpPersistentAlwaysAllowEnabled`;
  - `secureVmFeaturesEnabled` (Cowork);
  - `computerUseEnabled`;
  - `managedMcpServers[].toolPolicy`.

**The Code tab**
- It shares `~/.claude/` and `~/.claude.json` with Claude Code, so its permissions are Claude Code's, which RosmarOps already scores.

## 4. YAML

The existing reader, `internal/yamlsubset` (`ParseKeysWithMapItems`), is enough. Tested with sample files:
- Read: comments, the `mcpServers` list of mappings, `args` and `env` in block style, `${{ secrets.X }}`, and `permissions.yaml`.
- Skipped without error: other sections that use anchors or block scalars.
- A flow mapping inside `mcpServers` (`env: {A: b}`) makes the whole file unread, and it is reported as unread (fail-secure). So does a file that starts with `%YAML 1.1` and `---`, which Continue's docs show for anchors.

No new dependency is needed.

## What cannot be known from files

1. **Continue, IDE:** the person's per-tool choices (localStorage). Only the factory defaults can be stated, as an assumption.
2. **Claude Desktop:** the «Always allow» approvals, connectors and Cowork's approval mode (app databases). Assumed: it asks, which is the factory default.
3. **Claude Desktop:** `Claude Extensions Settings/<id>.json` is a plain JSON file, but it can hold `user_config` values that are secrets when the extension does not mark them `sensitive`. Reading it would be reading a possible secret, so it is not read unless decided; enabled extensions would then be an assumption.
