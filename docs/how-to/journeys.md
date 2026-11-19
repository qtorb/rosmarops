# Journey tests

The journey tests run a candidate RosmarOps the way a person does, on the machines people have, before it is published: `.github/workflows/journeys.yml`. They catch what unit tests cannot see: a question nobody planned for, a text left in English, a report a browser cannot open, a version that stops instead of asking. The decision is [ADR 0021](../decisions/0021-journey-tests-before-publishing.md).

## What runs

| Journey | Machines | What a person does | What must hold |
|---|---|---|---|
| R1 | ubuntu-24.04 (Firefox as a snap), windows-latest (the `.exe` and the MSIX test package), macos-latest | Runs RosmarOps with no arguments, the first time, in Spanish and in English | It ends without an unplanned question, writes the report, and on Linux the Firefox snap can open it. |
| R2 | the same | Updates from the last published version, then uses `rosmarops` with no arguments, `run`, `run --unattended`, `usage`, `status`, `findings`, `problems ask`, `baseline`, `connect --help` and `--version` | Each command asks in the person's language and goes on, or (without a terminal) stops with a red report in their language; `local_salt` never changes, so the computer stays the same one in the space. |
| R3 | ubuntu-24.04 | Reviews a computer with 60 projects twice in a row | Both reviews give the same findings, rules and score, and neither ends asking about each finding. |

On every journey the fixed checks run: no `{param}` left in the console or the report, no English sentence of the catalog in a Spanish run, the binary's version in `--version`, in `report.v2.json` and in the page, and no runner path or machine name in the output. The program runs from the test home's `Downloads` folder, as a person keeps a downloaded binary, because the report names where the program lives (the Claude Code hook command); with the MSIX package, its alias is the one path outside the test home that may appear.

On Windows, a double click and `rosmarops findings` open RosmarOps's own windows (connect, findings), which a journey cannot click. Those rows set each window's self-test hook (`ROSMAROPS_CONNECT_WINDOW_SELFTEST`, `ROSMAROPS_FINDINGS_WINDOW_SELFTEST`): the real window opens and answers itself with «Not now», and the row checks the window's `window self-test: shown` line in the run's log (`data/last-run-log.txt` in the reports folder; for `findings`, which has no run log, on the console with its other log lines) and that the command ends with 0. So on Windows the windows are tested by their self-test, not by a person's click. A journey that clicks them through UI Automation can come later if it is needed.

Only on Linux does the locale choose RosmarOps's language. On Windows and macOS the language follows the system's display language, which is English on the runners: there the Spanish leg of R1 is `rosmarops run --lang es`, and R2 runs in English.

## The pieces

- `tests/journeys/makehome`: builds a test computer in a new folder, deterministically (fixed dates, authors, contents and fake secrets). `scripts/e2e/make-home.ps1` calls it too.
- `tests/journeys/drive/drive.py` (Linux, macOS, a pseudo-terminal) and `tests/journeys/drive/conpty` (Windows, ConPTY): run a command and walk it through a steps file in `tests/journeys/steps/`.
- `tests/journeys/check`: the fixed checks.
- `tests/journeys/journey.py`: R1, R2 and R3, on any of the three systems.
- `tests/journeys/capture.py`: the pictures and their manifest.
- `.github/scripts/pack-test-msix.ps1`: the MSIX test package of a given exe, for the Windows legs.

## A steps file

One step per line:

```
expect      console.binary_confirm   timeout 60s
send        s\r
if-asked    usage.ask.prompt send n\r
expect-exit 0                        timeout 600s
```

- `expect` waits for a catalog key's text in the run's language (its `{params}` match anything), or for literal text.
- `send` types; `\r` is Enter.
- `if-asked` answers a question each time it appears, from that line on: for a question one system asks and another does not.
- `expect-exit` waits for the exit code. A process that writes nothing for 30 seconds while it is awaited fails with «waiting for an answer the journey did not expect»: that is how an unplanned question is caught.

## When a beta tester finds a failure

1. Reproduce it on a test computer: `go run ./tests/journeys/makehome -home /tmp/jh -manifest /tmp/jh.json -medium 0 -large 0`, then run RosmarOps with `HOME=/tmp/jh` (and `USERPROFILE`, `APPDATA`, `LOCALAPPDATA` on Windows).
2. Decide where it belongs:
   - a command or an answer people use: a row in `R2_ROWS` in `journey.py`, or a new steps file;
   - a computer shape (many projects, an agent, a file): a `makehome` option;
   - a text that is wrong whatever the command: a fixed check in `tests/journeys/check`.
3. Write it so that it fails on the version with the bug. Run it locally with `python3 tests/journeys/journey.py r2 --bin <candidate> --prev <previous> --version X.Y.Z --tools <dir with makehome and check> --out /tmp/out`.
4. If the fix comes in another pull request, list the check's name in `JOURNEY_EXPECTED_RED` in `journeys.yml`, with the pull request in `JOURNEY_EXPECTED_RED_WHY`. Its failure is then reported as «expected red» and does not fail the journey; empty the list when the fix is merged.
5. Never answer an unplanned question with `if-asked` to make a journey pass: if a person would have been stuck, the journey must say so.

## The pictures

Each R1 and R2 job leaves an artifact `captures-<journey>-<os>-<install>`, and the `captures` job joins them into `journeys-captures`, with one `manifest.json`:

| Picture | What it is |
|---|---|
| `<os>-r1-first-run-<lang>-console.txt` and `-console-1280.png` | The console of the first run, from start to end, without colours. |
| `<os>-r1-first-run-<lang>-report-1280.png`, `-390.png` | The report page in that language, as wide as a laptop and as a phone. |
| `<os>-r2-after-update-…` | The same, for the first run after the update. |
| `<os>-r2-question-<lang>-console.txt` and `.png` | The update question alone, as the person sees it. |

Each manifest entry has the system, journey, step, language, width, candidate version, UTC date, the run's link and `status`: `ok` when the journey passed, `fallido` when it did not. Everything comes from a test computer with fixed fake secrets; the journeys check that no runner path or machine name appears. Nothing is uploaded to the website from here.

## The gate

`release.yml` runs `build`, then `journeys` on exactly those binaries, then `publish`, which checks every file against `build`'s `SHA256SUMS.txt`. The journeys block the publication only when the repository variable `JOURNEYS_GATE` is `on` (Settings > Secrets and variables > Actions > Variables). Until the owner sets it, they run and are reported, and `publish` goes on.
