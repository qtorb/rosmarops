# Change the report's first screen

The HTML report opens with three blocks: the result, the findings grouped by problem, and «Choose a template». This page says where each part comes from and what to keep when you change it. The decision behind it is [ADR 0020](../decisions/0020-report-in-three-blocks.md).

## Where the code is

- `internal/report/html_r3.go`: the first screen (header, the three blocks, the two folds, the footer), each problem's steps below it, its CSS and the catalog keys it emits (`r3Keys`).
- `internal/report/issues.go`: the grouping (`issuesOf`), the service each credential opens (`credServicesOf`, read from the graph) and each problem's «If you fix this» (`issueGain`, on `index.WithoutSet`).
- `internal/report/profiles.go`: the template passes; `fixedByAll` keeps the findings every offered Claude Code template takes away.
- `internal/graph/credentials.go`: `identifyService` and `supabaseDBRef`, which recognise a Supabase database URL by its host and user.

## Rules to keep

- **No new field.** The groups are drawn, never written: `report.v2`, `graph.v2`, usage statistics and the company summary count findings, not problems. A field for the groups is a schema change for the owner to decide.
- **Counts are rows.** «Findings» on the first screen counts rows as the rest of the report does: one key within reach of several agents is one finding.
- **Six problems.** The table shows at most six; the seventh row leads to the others' steps.
- **Texts are signed.** Every visible string comes from the catalog (`html.r3.*` and the existing keys). A problem the signed texts cannot word stays one finding with its own text; never write a new text.
- **Certainty.** «How to fix it» says a template fixes something only when every offered template's pass no longer has the finding. A key in git is rotated, never fixed by a template.
- **The password.** A database URL is matched by host and user. Its password is never read, kept or shown; `tests/trust/no_db_password_leak_test.go` checks it with the real binary.

## How to check a change

1. `go test ./internal/report/ ./tests/integration/ ./tests/trust/`: the grouping, the signed texts (`r3_texts_test.go`), the golden page and the browser checks.
2. If the change is visible, regenerate the golden page (`internal/report/testdata/html_one_agent_bv56.*.html`) and add a line to `TestRenderHTML_OneAgentUnchanged`'s history.
3. Look at it at 1280 and 390 px, in light and dark mode.
