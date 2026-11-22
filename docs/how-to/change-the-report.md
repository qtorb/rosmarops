# Change the report's first screen

The HTML report opens with three blocks: the result, the findings grouped by problem, and «Choose a template». This page says where each part comes from and what to keep when you change it. The decision behind it is [ADR 0020](../decisions/0020-report-in-three-blocks.md).

## Where the code is

- `internal/report/html_r3.go`: the first screen (header, the three blocks, the two folds, the footer), each problem's steps below it, its CSS and the catalog keys it emits (`r3Keys`).
- `internal/report/issues.go`: the grouping (`issuesOf`), the service each credential opens (`credServicesOf`, read from the graph) and each problem's «If you fix this» (`issueGain`, on `index.WithoutSet`).
- `internal/report/profiles.go`: the template passes; `fixedByAll` keeps the findings every offered Claude Code template takes away.
- `internal/graph/credentials.go`: `identifyService` and `supabaseDBRef`, which recognise a Supabase database URL by its host and user.

## «Since the last check» and templates in use (BV-273)

- Block 1 ends with «Since the last check» (`renderR3Done` in `internal/report/resolved.go`) when a finding was fixed since the last check or the sheet's score went up: how many were fixed, the score change, the three most severe fixed findings by their row's title (read from the previous run's `report.v2.json`; the rest folded) and the series when there are three or more comparable scores.
- Only `resolved` findings count as fixed, here, in the «Since the last check» column (`html.since.counts`, whose parts at 0 are left out except «still there») and in the space (`kind: "fixed"`). A `no_longer_observed` or `not_checked` finding never does.
- `resolved` lives in the findings memory and in the report's own data, not in `report.v2.json`: its verdict counts are a closed list there, and adding one is a schema change for the owner.
- Block 3's line says which template the person already uses (`tplInUseLine` in `internal/report/tplinuse.go`) and, if there is one, the next stricter template for the same use on this system; the card of the template in use has the green «In use» pill.

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
