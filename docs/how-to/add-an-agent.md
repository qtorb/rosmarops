# How to add an AI agent

This guide shows the full path to support a new AI agent: read its configuration, put it in the capability graph, score it, and show it in the report. The worked example is Cline (the VS Code extension `saoudrizwan.claude-dev`). Every step names the place where Cline already lives, so you can copy its shape.

Read [ARCHITECTURE.md](../../ARCHITECTURE.md), [the trust model](../trust-model.md) and [the capability graph decision](../decisions/0009-capability-graph-v2.md) first.

## Before you start

- Find the agent's official documentation for every file and setting that decides what it may do. Each fact you encode needs a source URL and the date you read it (step 2).
- Decide whether the agent is **scored** (RosmarOps reads its permissions and gives it a score) or **detected only** (the report lists it as «detected, not evaluated yet»). Cline the extension is scored; the Cline CLI is detected only. A detected-only agent needs only steps 1, 9 and 11.
- The reader only reads. It never writes, never runs the agent, never opens a socket. If the agent keeps a setting that you can only read over the network or by running it, the report says it cannot read it. Ask the owner before you do anything else.
- Pick one stable product code, lowercase with hyphens (Cline: `cline`). It is the `platform` of the agent node, the knowledge file name and the key of every per-agent text.

## Steps

### 1. Folders and detection: `internal/agentdirs/agentdirs.go`

Add a function that returns the agent's configuration folder on each layout. Honour the environment variables the agent documents.

- Cline: `ClineDataDir` returns `CLINE_DATA_DIR`, else `CLINE_DIR\data`, else `<home>\.cline\data`. `Layout.ClineDir` is the old VS Code global storage folder.
- For a detected-only product, add a product code constant (Cline CLI: `ClineCLI = "cline-cli"`) and a marker file check in `Layout.Detect`.

Tests: `internal/agentdirs/agentdirs_test.go` and `internal/agentdirs/linux_test.go`.

### 2. Knowledge base: `internal/knowledge/<product>.json`

Write what the agent does by default, with a source for each entry. Cline: `internal/knowledge/cline.json`. Then, in `internal/knowledge/knowledge.go`:

- add the product constant (`ClineProduct = "cline"`) and the version constant (`ClineVersion`);
- embed the file (`//go:embed cline.json`) and add a loader (`func Cline() (Base, error)`) that checks the version;
- add the loader to `All()`;
- add `<product>@<version>` to `Composite`, in sorted order;
- if the agent is a VS Code extension, add its id to `AgentExtensions`.

See [update-agent-knowledge.md](update-agent-knowledge.md) for the fields and the source rule. For Cline the graph builder (step 7) writes the same factory defaults in Go; keep the entries and the builder in agreement.

### 3. Scan type and platform method: `internal/platform/interface.go`

Add a scan struct with what you read, and a method on the `Platform` interface.

- Cline: `ClineScan` (with `Present`, `ConfigDir`, `GlobalStateStatus`, the five auto-approval actions, `Hooks []ClineHook`) and `ListCline(ctx context.Context, repoRoots []string) (ClineScan, error)`.
- `ConfigDir` is the agent's identity in the graph. Choose it once; changing it later changes every node id.

### 4. Reader: `adapters/windows/<product>.go`

Implement the method on `WindowsPlatform`. The macOS and Linux platforms (`adapters/darwin/platform.go`, `adapters/linux/platform.go`) embed `WindowsPlatform`, so the same reader runs there on the layout of each system.

Every path the reader looks in must come from the layout (`w.layout`, through `internal/agentdirs`), never from a Windows variable such as `%APPDATA%` alone, and must cite where the agent documents it for each system. A path that only exists on Windows is read on Windows only. BV-293 fixed four readers that used the Windows path on Linux too (ADR 0036): for example, VS Code's user folder is `layout.VSCodeUserDir`, and Codex's system `requirements.toml` is `/etc/codex/requirements.toml` off Windows. When the agent documents no path for a system, leave that system unread rather than guess.

- Cline: `adapters/windows/cline.go`. `ListCline` resolves the home and calls `ReadCline`, a pure function that takes the layout, home, environment and repository roots, so tests can call it on a temporary folder.
- Read files through the bounded reader the other readers use (`readBounded`). Record the read status of every file you open; a file that could not be read must show up as not read, never as clean.
- Never keep a secret value in the scan. Credentials go through `internal/scan/credfiles` (step 5).

Tests: add a reader test next to the reader. Cline: `adapters/windows/cline_internal_test.go`, `adapters/windows/newagents_test.go`, `adapters/darwin/darwin_test.go`. A test that runs a review and then reads its output finds it with `report.DefaultReportDirIn(home)`, never `~/.rosmarops/reports`: on Linux the reports are in `~/RosmarOps`.

A file an administrator puts under `/etc` (Claude Code's `/etc/claude-code/`, Cursor's `/etc/cursor/hooks.json`) is passed to the reader by the Linux platform (`adapters/linux/platform.go`), as macOS does with its own locations.

### 5. The shared readers

Join the agent to the readers that every agent shares, where they apply:

| What | File | Cline's entry |
|---|---|---|
| MCP servers | `adapters/windows/mcp.go` | client `cline`, `cline_mcp_settings.json` (new path and legacy copies) |
| Instruction files (AGENTS.md and similar) | `adapters/windows/instructions.go` | `instructionLister` with `knowledge.ClineProduct` |
| Skills folders | `internal/skills/skills.go` | `clineProduct`, `.cline/skills`, `.clinerules/skills` |
| Credential files, by fingerprint only | `internal/scan/credfiles/credfiles.go` | `formatClineCreds` for `secrets.json` and `providers.json` |
| Connectors and schedules | `adapters/windows/alwayson.go` | `clineAlwaysOn` |

### 6. Call the reader: `cmd/rosmarops/cmd_run.go`

In the `mcp` step of the run, call the new method, keep its scan, and join its error to `listErr`. Pass the scan to `writeGraph` and to the `report.V2Input` of the report. Cline: `plat.ListCline(ctx, repoList)` and `clineScan`.

### 7. Graph fragment: `internal/graph/<product>.go`

Write a function that turns the scan into graph nodes and edges: the `ai_client` node, its configuration file, and one capability per permission.

- Cline: `FromCline` in `internal/graph/cline.go`. Each configured capability carries the literal setting as its rule and a source pointer, or the graph fails its schema (`TestClineConfiguredCapabilitiesHaveRule` in `internal/graph/cline_configured_test.go`).
- Add an inference for what the agent reaches and writes, and its case in `Infer` (`internal/graph/infer.go`, `case knowledge.ClineProduct`).
- Add the display name to `clientDisplayNames` in `internal/graph/mcp.go`.
- If MCP servers come from a reader of their own, add a helper like `GeminiMCPServers`; Cline's come through `ListMCPServers`, so it needs none.

The graph must validate against `schemas/v2/graph.v2.schema.json`. Do not change that schema; it needs a decision from the owner.

### 8. Pipeline: `internal/pipeline/`

- `internal/pipeline/assemble.go`: add the scan to `Input` and merge the fragment in `fragment`, **before** `AddInstructions` and `AddSkills` (they only attach shared files to agents already in the fragment). Cline: `graph.Merge(frag, graph.FromCline(in.Cline, ClineExtensionPresent(in.Extensions), in.Now))`.
- `internal/pipeline/factory.go`: add a function that removes the person's settings and keeps the product defaults, and call it from `Factory`. Cline: `FactoryCline`. This is the factory pass of [the factory scores decision](../decisions/0012-factory-scores-and-sandbox-templates.md).

### 9. Rules and overview: `internal/rules/`

- `internal/rules/rules.go`: add the scan to `Input` if the overview needs facts that are not in the graph (Cline: `Cline platform.ClineScan`). Add any new assumption code to `AllAssumptions` (Cline: `AssumeAgentModelJudgesCommandSafety`).
- `internal/rules/overview.go`: add the agent's rows (Cline: `clineCells` and its five cell functions), dispatched by `v.node.Platform`. If the generic rows already fit, skip this.
- `internal/rules/kinds.go`: `mcpDispositionRead` lists the products whose MCP «who decides» is read.
- For a detected-only product: add a reason to `notEvaluatedReasonFor` and `AllNotEvaluatedReasons` only if the generic `no_inference` does not fit (Cline CLI: `NotEvaluatedClineCLI`). A new reason is a new enum value in the report schema: ask the owner.

Tests: `internal/rules/cline_overview_test.go` (one test per real case: fresh install, a changed setting, an old version, a repository hook).

### 10. Report input: `internal/report/`

- `internal/report/v2.go`: add the scan to `V2Input`.
- `internal/report/factory.go`: pass it to `rules.Input` in `evaluate`, and apply the factory function in the factory pass (`fin.Cline = pipeline.FactoryCline(in.Cline)`).
- Templates (`internal/profiles/profiles.go`): P1 and P2 ask before Claude Code edits another agent's folders (`Edit(~/.cline/**)`). Adding a folder there changes a template; follow [change-a-sandbox-template.md](change-a-sandbox-template.md).

### 11. Registry, problems and texts

- `internal/agents/registry.go`: add an entry with the configuration sources the reader opens (Cline: `~/.cline/data/globalState.json`, user layer, JSON). Declare only sources the reader really opens on the full test machine (step 12).
- `internal/problems/problems.go`: add the product to `knownAgents` and its name to `agentNames`.
- Catalog: add `html.assume.<product>` to `internal/catalog/messages/en.json` and to the English copy in `docs/copy/`. For a detected-only product with its own reason, add `html.ov.head.not_evaluated.<reason>` and list it in `internal/report/html_overview.go`. The Spanish text comes from the owner. Follow [change-a-text.md](change-a-text.md).

### 12. Fixtures and cross-agent tests

Add the agent, with a realistic configuration, to the machines the cross-agent tests build:

| Test | File | What to add |
|---|---|---|
| `TestFullMachinePipeline` | `tests/integration/full_machine_fixture_test.go` | the folder, the scan in `fullMachineInput`, the product in the list of scored agents |
| `TestRegistrySourcesOpenedOnFullMachine` | `tests/integration/registry_sources_test.go` | the folder marker in `productOfSource` |
| `TestAllAgentsGraphValidates` | `tests/trust/all_agents_graph_test.go` | the scan in the set of every agent |
| `TestR2FactoryScores` | `tests/trust/r2_factory_scores_test.go` | a case in `r2Cases` with its factory score and band |
| reference computer with every agent | `tests/trust/r2_all_test.go` | the scan |
| `TestAssumeTextPerAgent` | in `internal/report/` | nothing: it checks `html.assume.<product>` in English and Spanish for every knowledge base |

The factory score of a new agent is a published figure. The owner signs it before you pin it in `r2Cases`.

## Two products that share one model

OpenCode and Kilo Code (ADR 0027) share their configuration model, so one reader serves both. `adapters/windows/opencode.go` describes what tells them apart in an `openCodeFamily` value (file names, dot folders, the managed folder's name), and `internal/graph/opencode.go` builds both fragments from the product's own knowledge base. A third product of the same model is a new `openCodeFamily` and a knowledge file, not a new reader.

## An agent that keeps its approvals in the app

Claude Desktop (ADR 0030) keeps its «Always allow» approvals in the app's databases, which RosmarOps never opens. Its knowledge base keeps the factory fact (it asks before each MCP tool) and the least controlled case it is scored on (its tools run without asking), as for Cursor. Its overview rows rest on `ui_mode_choice_unknown`, and its assumptions say what was not read. An administrator's managed file for such a product is read through `company.ReadAdminFile`, which checks who owns the file and its folders before reading it.

An agent that is no longer maintained (Continue) is detected with its MCP servers and gets its own not-evaluated reason. It gets no knowledge base or score.

## An orchestrator

A program that starts other agents (Paperclip, ADR 0022) is not an `ai_client`. It is an `agent` of type `orchestrator`:

1. **Its knowledge base:** a JSON of its own in `internal/knowledge/` (as `paperclip.json`), or an entry in `orchestrators.json` (ADR 0023), with a source URL at a pinned commit or release tag and a date for every fact. Record which agents it starts and whether it skips their approvals, which of its files hold secrets, and its defaults.
2. **Its reader** in `adapters/windows/`: only the configuration that says how it is exposed. Its database, secret stores and logs are never opened. Use `os.Lstat` for their existence, permissions and owner, and add their reader functions to the never-opens check of `tests/trust/paperclip_test.go`, or their names to `orchLocked` and the forbidden-names check of `tests/trust/orchestrators_test.go`.
3. **Its graph builder,** merged last in `pipeline.fragment`:
   - a `launches` edge to each installed agent it supports. Observed, with `basis: configured` and the file as its source, when its own files say the mode it starts the agent in (Paseo, Claude Squad). Inferred, with `basis: product_default` and an assumption, when that is only in its database (Paperclip: `orchestrator_agent_configured`; Orca: `orchestrator_settings_not_read`);
   - its secrets as credentials with `identified_by: path` (`graph.PathFingerprint`);
   - its port as an `inbound_channel`.
4. **The rules** in `internal/rules/orchestrator.go` read the `launches` edges and its scan.

## Tests that must pass

Run what CI runs (`.github/workflows/ci.yml`):

```
go test -count=1 ./...
go vet ./...
staticcheck ./...
gofumpt -l .
```

CI also runs `go vet` and `staticcheck` with `GOOS=windows` and `GOOS=darwin`, and runs the tests on Windows and macOS. Run them with those `GOOS` values too.

The tests that usually fail first for a new agent:

- `go test -count=1 ./internal/knowledge/`: `TestComposite`, `TestReviewedOn`.
- `go test -count=1 -run 'TestAssumeTextPerAgent|TestCatalogCoverage|TestCatalogFilesAreTheSignedCopy|TestSpanishCoverage' ./internal/report/`.
- `go test -count=1 -run 'TestFullMachinePipeline|TestRegistrySourcesOpenedOnFullMachine' ./tests/integration/`.
- `go test -count=1 -run 'TestAllAgentsGraphValidates|TestR2FactoryScores|TestFactoryScoresJSON|TestNoNetwork' ./tests/trust/`.

## Stop and ask the owner

- A new enum value in any schema (`schemas/` and its embedded copy `internal/schemas/`).
- Any read that is not a local file at rest.
- The agent's factory score and every new text.
