# How to change a user-facing text

Every sentence RosmarOps shows to a person (the report, the console, the CLI help, the finding texts) comes from the message catalog, by a stable key. Code never holds user-facing prose. This guide changes a text, or adds a new one.

Read [the message catalog decision](../decisions/0008-message-catalog-and-languages.md) first.

## The rules

- **English is the source.** `internal/catalog/messages/en.json` holds every key. Any other language falls back to English for a missing or empty key.
- **Translations come from the owner only.** Never write or edit translated copy, not even to fix a typo. Today the only translation is Spanish, `internal/catalog/messages/es.json`.
- **The catalog files are copies of signed files** in `docs/copy/`, and a test compares them (see «Tests» below).
- **JSON outputs carry no prose.** The graph, the report JSON and every other JSON file carry ids, enums and codes. If you want a sentence in a JSON file, you want a key instead.

## The files

| File | What it is |
|---|---|
| [`internal/catalog/messages/en.json`](../../internal/catalog/messages/en.json) | The English catalog the program embeds. |
| [`internal/catalog/messages/es.json`](../../internal/catalog/messages/es.json) | The Spanish catalog the program embeds. |
| [`docs/copy/report-v2-en.json`](../copy/report-v2-en.json) | Signed English copy, part one (mostly the Markdown report, the console and the CLI). |
| [`docs/copy/report-html-en.json`](../copy/report-html-en.json) | Signed English copy, part two (mostly the HTML report). |
| [`docs/copy/report-html-es.json`](../copy/report-html-es.json) | Signed Spanish copy. |

`en.json` must be exactly the two English copies joined. A key lives in one of the two English copies, not in both (unless both have the same text). `es.json` must be byte for byte the same as `docs/copy/report-html-es.json`.

## Text format

- Each file is flat JSON: key to text.
- `{name}` is a placeholder, filled from the parameters the code passes. A placeholder with no parameter stays as written. A value read from disk is never expanded, so a `{agent}` inside it stays literal.
- Plurals are separate keys: `<key>.one` and `<key>.other`.
- A missing or empty key renders as `[key]` when English has no text either.

## Change an existing English text

1. Find the key: `grep -n '"<key>"' internal/catalog/messages/en.json docs/copy/*.json`.
2. Change the text in `internal/catalog/messages/en.json`.
3. Make the same change in the English file under `docs/copy/` that holds the key.
4. Keep every placeholder the code passes. Do not add a placeholder the code does not pass.
5. Search the tests and scripts for the old text, because some check rendered words: `grep -rn "<old text>" --include=*_test.go .`, `scripts/sample-report/generate.ps1` (it checks English phrases in the sample report on Windows) and `.github/workflows/ci.yml` (the Linux smoke run checks Spanish phrases).
6. If the text appears in the golden report pages, update them (see «Golden pages» below).
7. Tell the owner the key changed, so they can update the Spanish text. Until they do, Spanish keeps its old translation.

## Add a new key

1. Choose a key that follows its neighbours (`finding.<rule>.title`, `html.<section>.<name>`, `cli.cmd.<command>.short`).
2. Add it to `internal/catalog/messages/en.json` and to one English file in `docs/copy/`.
3. Make the code emit it, and list it where the coverage test looks. The test builds the list from:
   - `report.CatalogKeys` (`internal/report/v2_markdown.go`): the Markdown report and the console;
   - `report.HTMLKeys` (`internal/report/html.go`): the HTML report;
   - `cli.HelpKeys` (`internal/cli/cli.go`): the CLI help;
   - `kit.MessageKeys` (`internal/kit/kit.go`);
   - `reservedKeys` in `internal/report/v2_test.go`: signed texts the binary does not emit today.
4. Ask the owner for the Spanish text. When it arrives, put it in both `docs/copy/report-html-es.json` and `internal/catalog/messages/es.json`, exactly as given.

## Remove a key

Remove it from `en.json`, from its `docs/copy/` English file, from the list that emitted it, and ask the owner to remove it from the Spanish copy. The coverage test fails on a key that nothing emits.

## Golden pages

`TestRenderHTML_OneAgentUnchanged` (`internal/report/`) renders a one-agent report and compares it, byte for byte, with the pages in `internal/report/testdata/` (one English, one Spanish). A text on that page changes the page.

There is no update flag. The test prints where the page first differs. Check that the difference is only your change. Then replace the file with the new rendered page (for example, write the rendered string to the file once from the test, and remove that line before you commit). A change to a golden page is a change the reviewer reads.

## Tests

The catalog tests are in `internal/report/v2_test.go`:

- `TestCatalogCoverage`: every key the code can emit is in `en.json`, and `en.json` has no key nothing emits.
- `TestCatalogFilesAreTheSignedCopy`: `en.json` is the two English copies joined; `es.json` is the Spanish copy byte for byte.
- `TestSpanishCoverage`: every key the HTML report can emit, every CLI help key, and the console summary and status keys exist in Spanish, unless it is in `LegacyHTMLKeys` (`internal/report/html.go`), the list of keys allowed to fall back to English. Only the owner decides what goes in that list.
- `TestSpanishHTMLFindingCoverage`: every finding text the HTML report shows exists in Spanish, with the same exception.

So a new HTML key fails the Spanish tests until the owner's translation arrives, or the owner allows the fallback. Run:

```
go test -count=1 -run 'TestCatalog|TestSpanish|TestRenderHTML_OneAgentUnchanged|TestAssumeTextPerAgent' ./internal/report/
go test -count=1 ./internal/catalog/
```

And before you open the pull request, what CI runs:

```
go test -count=1 ./...
go vet ./...
staticcheck ./...
gofumpt -l .
```
