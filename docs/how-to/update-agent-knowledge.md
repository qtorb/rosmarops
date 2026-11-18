# How to update the agent knowledge

The knowledge base says what each agent product does by default, with a source for every fact. It is knowledge, not judgement: it says «Cline reads files without asking on a fresh install», not whether that is dangerous. The rules layer decides that.

The base is embedded JSON in `internal/knowledge/`, decoded strictly: an unknown field, an empty required field or trailing data is an error. A run with a broken base fails with a «knowledge base» error, and the tests fail.

## The files

| File | What it holds | Go type |
|---|---|---|
| `internal/knowledge/<product>.json` (one per product: `claude-code.json`, `cursor.json`, `cline.json`, `codex.json`, `gemini-cli.json`, `github-copilot.json`, `grok-build.json`, `grok-bot.json`, `hermes.json`, `antigravity-cli.json`) | The product's defaults, mitigations and command lists | `Base` in `internal/knowledge/knowledge.go` |
| `internal/knowledge/mcp_catalog.json` | Known MCP servers whose tools act outside the computer | `MCPCatalog` in `internal/knowledge/mcp_catalog.go` |
| `internal/knowledge/startup-files.json` | Files the system runs later (a terminal's startup file, autostart folders), per system | `StartupFile` in `internal/knowledge/knowledge.go` |

There is no JSON Schema for these files. The Go structs, decoded with unknown fields disallowed, and the checks in `decode`, `LoadMCPCatalog` and `StartupFiles` are the contract.

## The mandatory source

Every fact carries where it comes from and when it was read:

- `source_url`: the official documentation page or the source file of the product. Required (not empty) on every entry, mitigation, code launcher, command and startup file.
- `verified`: what was read, the date it was read (`YYYY-MM-DD`), and the sentence or the setting that proves the fact. Required everywhere.

Cline's `read_default` entry is a good model:

```json
{
  "id": "read_default",
  "type": "read_files",
  "approval": "allow",
  "target_scope": "machine",
  "source_url": "https://docs.cline.bot/features/auto-approve",
  "verified": "... read 2026-11-07: AutoApprovalSettings.ts DEFAULT_AUTO_APPROVAL_SETTINGS.actions.readFiles=true — a fresh install reads files without asking."
}
```

In `mcp_catalog.json` the source is `docs_url` and `verified` is the date it was checked. The decoder requires `verified` but not `docs_url`; fill both.

Do not write a fact you have not read in a source. If the documentation and the code disagree, write what you read in both and ask the owner.

## Fields of a product base

Top level (`Base`):

| Field | Required | Meaning |
|---|---|---|
| `product` | yes | The product code, the same as the agent node's `platform` (`cline`). |
| `version` | yes | The date of this base, `YYYY-MM-DD`. Must equal the product's version constant in `knowledge.go`. |
| `entries` | | Default behaviours (below). |
| `mitigations` | | Documented settings that cut a way to a finding (below). |
| `code_launchers` | | Command forms that run code given in their arguments. Only Claude Code's table is used: every other loader replaces its own list with Claude Code's, so a list in another file has no effect. |
| `read_commands`, `network_commands` | | Closed lists of commands that read files or send to the network (used for Cursor). Each: `program` (lowercase, no `.exe`, no spaces), `source_url`, `verified`. |
| `session_files` | | Base names of the agent's own session or account files in its configuration folder (Cline: `secrets.json`). Reading one is not a finding against its own agent, but is against any other agent that reaches it. |

An entry (`Entry`):

| Field | Required | Meaning |
|---|---|---|
| `id` | yes | Stable, unique in the base. The capability node id and its `source` (`kb:<product>/<id>@<version>`) are built from it. |
| `type` | yes | A capability type of `schemas/v2/graph.v2.schema.json`: `execute_shell`, `read_files`, `write_files`, `network`, `invoke_mcp_tool`, `permission_mode` or `operate_app`. |
| `approval` | yes | `allow`, `ask`, `deny` or `unknown`. |
| `target_scope` | yes | Where it applies, such as `machine`, `any_path`, `any_destination`, `workspace`. Reuse a value the code already reads; a new one does nothing until the rules read it. |
| `source_url`, `verified` | yes | The source, as above. |
| `paths`, `exceptions` | | A documented path list (only Claude Code's `protected_paths`). A trailing `/` marks a folder. Forward slashes only, no duplicates, `exceptions` only with `paths`. An entry with `paths` is knowledge only, not a capability. |

How entries reach the graph depends on the product:

- Claude Code, Cursor, Codex and Grok Build: each capability entry becomes a `product_default` capability on every agent of that product (`ProductDefaults` in `internal/graph/permissions.go`, called from `FromScansWithGrok`). Hermes Agent does the same in `internal/graph/hermes.go`. Changing `approval` or `type` changes the graph and can change scores.
- The other products (for example Cline, Gemini CLI, GitHub Copilot, Antigravity CLI): their graph builder in `internal/graph/` writes the factory defaults in Go (Cline: `clineEffectiveActions` in `internal/graph/cline.go`). Their entries are the sourced record of those defaults, but editing an entry alone does not change the graph. Change the builder in the same pull request, and keep the two in agreement.

For every product, the rules layer reads the base's `mitigations`, `code_launchers`, command lists and `session_files` (`baseFor` in `internal/rules/rules.go`).

A mitigation (`Mitigation`): `id`, `setting` (the settings key, `permissions.ask`, or `credential_file`), `rule_tool` (only with `permissions.ask`), `file` (under `~/`, when not the user settings file), `value` (a JSON literal), `completeness` (`full`, `partial`, `radius_only` or `by_cuts`), `cuts` and `narrows` (way codes from `mitigationWays` in `knowledge.go`), `magnitude` (above 0), `source_url`, `verified`.

## Steps to change a product base

1. Read the source. Note the URL, the date and the exact sentence or setting.
2. Edit the entry in `internal/knowledge/<product>.json`. Write the new `source_url` and `verified`.
3. Set `version` in the file to today's date.
4. Set the product's version constant in `internal/knowledge/knowledge.go` to the same date (Cline: `ClineVersion`).
5. Update `Composite` in the same file if a constant it names changed. It is built from the constants, so usually nothing to do; `TestComposite` checks it.
6. Set `ReviewedOn` in `knowledge.go` to a date not before any base's `version`. The report shows this date («Agent knowledge reviewed on …») and warns when the program is more than 14 days older than it.
7. A new `ReviewedOn` changes the date on the golden report pages in `internal/report/testdata/`. Update them as «Golden pages» in [change-a-text.md](change-a-text.md) explains.
8. If a default changed, scores may change. Run the score tests (below) and give the new figures to the owner before you change any pin.
9. Say what changed in the next release note ([release.md](release.md)).

To add a whole new product, follow [add-an-agent.md](add-an-agent.md).

## Steps to change the MCP catalog

1. Edit `internal/knowledge/mcp_catalog.json`. Each entry needs `id`, `service`, `category`, `verified`, and at least one way to recognise it: `remote_urls`, `packages`, `images` or `url_suffix`.
2. `category` is one of `AllMCPCategories` in `internal/knowledge/mcp_catalog.go`. A new category needs its `category.<code>` text (see [change-a-text.md](change-a-text.md)).
3. Set `version` in the file and `MCPCatalogVersion` in `mcp_catalog.go` to the same date.

## Steps to change the startup files

Edit `internal/knowledge/startup-files.json`. Each file needs `os` (`linux`, `darwin` or `windows`), `path`, `source_url` and `verified`. A path ending in `/` is a folder.

## Tests that must pass

- `go test -count=1 ./internal/knowledge/`: strict decoding (`TestDecode_RejectsUnknownFieldsAndGaps`, `TestDecode_PathListValidatedStrictly`, `TestDecode_RejectsBadMitigation`), `TestComposite`, `TestReviewedOn`, `TestSessionFilesPerProduct`, `TestCodeLaunchersHaveSources`, `TestCursor_LoadsStrictlyWithSources`, and the schema enum checks for Claude Code and Codex (`TestClaudeCode_EntriesUseSchemaEnums`, `TestCodex_EntriesUseSchemaEnums`).
- `go test -count=1 -run 'TestCrossMCPCatalog' ./internal/graph/`: the MCP catalog is recognised.
- `go test -count=1 -run 'TestKnowledgeReviewDate|TestRenderHTML_OneAgentUnchanged|TestAssumeTextPerAgent' ./internal/report/`.
- `go test -count=1 -run 'TestAllAgentsGraphValidates|TestR2FactoryScores|TestProfileScores' ./tests/trust/`: the graph still validates against its schema (this catches a bad `type` or `approval` for every product), and the pinned scores.

And what CI runs:

```
go test -count=1 ./...
go vet ./...
staticcheck ./...
gofumpt -l .
```
