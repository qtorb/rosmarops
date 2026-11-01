# BV-141 visual check — the report's three columns

The report's three steps live in one `.flow` grid (see
`internal/report/html.go`), so the three columns start at the same height.
`TestRenderHTML_SingleFlowThreeColumns` proves the structure in CI; this
folder is the visual corroboration, kept out of CI because it needs a
browser.

## Run

```sh
# 1) render the example report, four pages (en/es × with/without «Your space»)
go run ./tests/manual/flow-columns/gen -out /tmp/flowcols

# 2) measure the columns at 1280 px and save screenshots (1280 and 390 px)
node tests/manual/flow-columns/measure.mjs /tmp/flowcols
```

Chromium and a matching Playwright are pre-installed in the build container,
so `measure.mjs` needs no `npm install`.

## Expected output

```
report.en.nospace.html @1280: column tops = [53, 53, 53] px, spread 0px — OK
report.en.space.html    @1280: column tops = [53, 53, 53] px, spread 0px — OK
report.es.nospace.html  @1280: column tops = [53, 53, 53] px, spread 0px — OK
report.es.space.html    @1280: column tops = [53, 53, 53] px, spread 0px — OK

RESULT: OK (all columns aligned within 2px)
```

Screenshots are written to `<htmlDir>/shots/` as
`report.<lang>.<space|nospace>.<1280|390>.png`.
