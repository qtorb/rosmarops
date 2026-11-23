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

## «Your orchestrators», the «Key» tag and the frontier (BV-278)

- **«Your orchestrators»** (`renderFleet` in `internal/report/html_fleet.go`) sits between block 3 and the folds. It appears only when the graph has an orchestrator with a `launches` edge.
  - **Rows.** `BuildV2` keeps the edges in memory (`fleetOf`), sorted by orchestrator and then by agent. They are not in the JSON.
  - **Columns:**
    - the agent, with a line when it gets the orchestrator's environment;
    - `approval_override` as a red, green or amber pill;
    - how we know: the base name of the file it was read in, as text read from the disk, or «Assumed: its factory setting»;
    - the agent's own score pill;
    - «See the warning ›» to the `#p-N` of the problem that holds its `orchestrator_skips_approvals` finding.
  - **Under each table:** one grey line per distinct assumption of its edges, with the catalog's `unknown.<code>` text. The assumptions every agent shares are left out, as in the findings' own lists.
  - **No URL.** The card links nowhere outside the page.
- **«To understand»** (`.r3-und`, `undEyebrow`) is the treatment of a card that explains and asks for no action. «Your orchestrators» is the first one.
- **The «Key» tag** (`.r3-tag`, `keyTag`) marks the credential problems (`issueCred`: `cred_readable`, `cred_read_and_send`, `cred_in_git`). It goes before the title in the table and after it in the problem's steps.
- **The frontier** (`renderDetail`, `.r3-detail`) goes between the first screen and `main`, only when there is a problem. It is as wide as `main`, and hidden in the alternate views.
- **The person's lists apply to every finding.** «No me afecta» and «Lo asumo» apply to the orchestrator and subagent findings too (`applyPersonLists` in `internal/rules/rules.go`), so a dismissed or accepted warning is never linked from the card.

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
4. At 700 px or less the top bar (`renderBar` and `sheetRules` in `internal/report/html_sheet.go`) breaks into two lines: the logo above, and the language link and «Your space» below, 8 px apart. `TestBV282NoHorizontalScroll` (`tests/integration/bv282_test.go`) checks in a browser, when Playwright is installed, that the page has no horizontal scroll at 360 and 390 px, with and without the sample report's band, and that the bar stays one line, 76 px tall, at 1280 px. Anything you add to the bar must keep that test passing.
