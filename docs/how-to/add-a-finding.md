# How to add a finding

A finding is what the rules layer concludes from the capability graph: for example, «n8n keeps the key to its credentials next to them». This guide adds a new v2 finding. The worked example is `n8n_key_with_data`, a finding with its own file and its own texts.

Read [ARCHITECTURE.md](../../ARCHITECTURE.md), [the capability graph decision](../decisions/0009-capability-graph-v2.md) and [the posture score decision](../decisions/0010-posture-score.md) first.

## Before you start

- A new finding changes what the product reports. Agree it with the owner first, and write an ADR in `docs/decisions/` if it changes what RosmarOps reads.
- The finding needs facts in the graph. If the graph does not have them yet, add them first (for `n8n_key_with_data`: the folder resource node with `N8NKey` set, from `internal/scan/n8n` and `internal/graph/n8n.go`).
- A finding carries only ids, enums, codes and technical detail. Every sentence a person reads comes from the message catalog by code.
- The new rule code must be added to two schemas (steps 3 and 4). Schema changes need the owner's explicit decision. Ask before you write the code.

## Steps

### 1. The rule: `internal/rules/<rule>.go`

Add the rule code and a function that builds the finding. `internal/rules/n8n.go` has both:

```go
const RuleN8NKeyWithData = "n8n_key_with_data"

func n8nKeyFinding(in Input, n graph.Node) Finding
```

Fill every field of `Finding`:

- `ID`: `findingID(rule, nodeIDs...)`. It must be stable across runs for the same facts, because the findings memory follows it.
- `Rule`, `RuleVersion` (the `RuleVersion` constant in `internal/rules/rules.go`).
- `Severity` (`SeverityRed` or `SeverityYellow`) and `Confidence`. Any finding raises the run's status to at least yellow ([fail-secure](../decisions/0004-fail-secure-status.md)).
- `Params`: the values the texts fill in, such as `{agent}` or `{path}`. Never a secret, a token or a full header ([no secrets in outputs](../decisions/0003-no-secrets-in-outputs.md)).
- `Evidence`: node ids, edge ids, file locations and assumption codes. Use empty slices, not nil.
- `Group` and `Actions`.

The first evidence node decides the score. If it is an evaluated agent, the finding enters that agent's score. If it is something else (the n8n folder), the finding weighs 0 and stays outside every agent's band.

### 2. Register and emit it: `internal/rules/rules.go`

- Add the code to `AllRules`.
- Call the builder in `Evaluate`. `n8n_key_with_data` is one finding per graph node with `N8NKey` set.
- If you change what an existing rule finds, raise `Version` in the same file.

### 3. The report schema

Add the code to the enum at `definitions/finding/properties/rule` in `schemas/v2/report.v2.schema.json`. Copy the same change into `internal/schemas/v2/report.v2.schema.json`; the two files must be byte-identical.

If the finding belongs to a new group (step 5), the group code is also an enum in both report schemas and in `schemas/v2/space-view.v1.schema.json`.

### 4. The findings memory: `internal/report/v2.go`

Either the memory tracks the rule (its code is in the rule enum of `schemas/v2/findings-memory.v3.schema.json` and its copy in `internal/schemas/v2/`, another schema change), or the rule is listed in `MemoryUntracked`. `n8n_key_with_data` is in `MemoryUntracked`: it is derived again on every run.

### 5. The group: `internal/index/index.go`

Return a group for the rule in `GroupOf`. `n8n_key_with_data` is in `GroupN8N`. A new group also goes in `GroupOrder`.

### 6. The texts

A rule with the generic layout needs these keys:

- `finding.<rule>.title`, `finding.<rule>.what`, `finding.<rule>.why` (Markdown report and console);
- `html.headline.<rule>` (HTML report);
- optionally `findings.when.<rule>` (when it is fine to dismiss it; otherwise `findings.when.default` is shown).

`n8n_key_with_data` has its own layout: `finding.n8n_key_with_data.title`, `.what` and `.step1`, and `findings.when.n8n_key_with_data`. Add each key to `internal/catalog/messages/en.json` and to the English copy in `docs/copy/`, as [change-a-text.md](change-a-text.md) explains. The HTML finding texts must also be in Spanish; the owner supplies them.

### 7. List the keys and render them: `internal/report/`

The catalog coverage test fails if a key exists that nothing emits, or a key is emitted that does not exist. So list every key you use:

- `CatalogKeys` in `internal/report/v2_markdown.go`. The loop over `rules.AllRules` adds the generic keys. For a rule with its own layout, add a case with its keys (see the `rules.RuleN8NKeyWithData` case).
- `HTMLKeys` in `internal/report/html.go`. The loop adds `html.headline.<rule>`; a rule with its own layout is skipped there and its keys are added below.
- `HTMLFindingKeys` in `internal/report/v2_markdown.go`, for a rule whose HTML texts are its own `finding.*` keys.
- `findings.when.<rule>` goes in the static key list at the top of `internal/report/v2_markdown.go`.

For a rule with its own layout, also render it where the other special rules are rendered: search for `rules.RuleN8NKeyWithData` in `internal/report/v2_markdown.go`, `internal/report/html.go` and `internal/report/spaceview_report.go` (`isBespokeFixRule`, `svFixStepsText`).

### 8. Tests

Write a rules test that builds a small graph and calls `rules.Evaluate`. `internal/rules/n8n_test.go` has three: `TestN8NKeyFinding` (the finding exists, with its severity and evidence), `TestN8NBridgeFinding` and `TestN8NDoesNotChangeScore` (the score does not move).

If the finding can enter an agent's score, the pinned factory scores may change (`TestR2FactoryScores` in `tests/trust/`). The new figures are published; the owner signs them.

## Tests that must pass

```
go test -count=1 ./...
go vet ./...
staticcheck ./...
gofumpt -l .
```

The ones a new finding touches:

- `go test -count=1 -run 'TestN8N' ./internal/rules/` (your own rules test);
- `go test -count=1 -run 'TestCatalogCoverage|TestCatalogFilesAreTheSignedCopy|TestSpanishCoverage|TestSpanishHTMLFindingCoverage|TestSchemaEnumsCoverCodeConstants|TestMemoryCoversEveryRule' ./internal/report/`;
- `go test -count=1 -run 'TestGroupsWithout' ./internal/index/`;
- `go test -count=1 -run 'TestSchemasInSync' ./tests/unit/`;
- `go test -count=1 ./tests/trust/` (no secret in any output, the graph and report validate, factory scores).
