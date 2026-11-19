# 0020. Journey tests before publishing

Date: 2026-11-19

## Context

RosmarOps 0.15.0 passed every check in `ci.yml` and still failed a person on Linux on the first day:
- the report could not be opened in Firefox installed as a snap;
- after an update, several commands stopped in English instead of asking;
- `run` ended asking about each of 700 findings;
- two reviews in a row gave different totals.

None of that is visible to a unit test: it needs the real binary, a terminal, a browser and a computer that has been used before.

## Decision

- **Journeys.** `.github/workflows/journeys.yml` runs three journeys on a candidate binary:
  - R1, the first time;
  - R2, an update from the last published version, command by command;
  - R3, a computer with 60 projects, two reviews in a row.

  They run on ubuntu-24.04 with Firefox as a snap, on windows-latest with the `.exe` and the MSIX test package, and on macos-latest. Each journey drives RosmarOps through a pseudo-terminal (ConPTY on Windows) with planned answers. A process that waits 30 seconds for an answer nobody planned fails the journey.
- **The previous version.** It is rebuilt from its tag with the Go its own `release.yml` pinned, and must reproduce the release's `SHA256SUMS.txt` byte for byte. The published binaries are never downloaded, so the tests do not add to the public download count.
- **Publishing.** `release.yml` builds, runs the journeys on exactly those binaries, and then publishes. Publishing checks every file against the build's `SHA256SUMS.txt` before attesting and creating the release, so what was tested is what is published. The journeys block the publication only once the owner sets the repository variable `JOURNEYS_GATE` to `on`. Until then they run and are reported.
- **Pictures.** Each journey keeps its console and report as pictures with a manifest, for the website. A picture of a failed journey is never marked `ok`.
- **RosmarOps's windows on Windows.** A journey cannot click a window, so on Windows the commands that open the connect or findings window run with that window's self-test hook: the real window opens and answers itself, RosmarOps logs `window self-test: shown` with the window's name, and the row checks that line and exit 0. Without the hook nothing changes. For this, the connect window now closes when its input ends, as the findings window does, and RosmarOps waits for no window without a limit: past its limit a window is closed as «Not now» (or, after «Save to my space», with the connection's result) and the run goes on. A person's click on Windows is not tested; a UI Automation journey can come later.
- **No new trust.** No new dependency, credential or network access: the tools are Go and Python's standard library, `GITHUB_TOKEN` reads the public release list, and no journey connects to a space.

## Consequences

- A release takes about 15 to 20 minutes longer.
- A failure a person can meet becomes a journey row or a check. [The how-to](../how-to/journeys.md) says how to add one.
- Two things are not reproduced on GitHub and stay manual steps of the owner for each version: an update from the Microsoft Store, which needs the Store's identity, and the macOS Gatekeeper prompt for a downloaded binary.
- The R3 repository that runs out of time on purpose, R3 on Windows and macOS, a visual review by a model and a test space are later phases.
