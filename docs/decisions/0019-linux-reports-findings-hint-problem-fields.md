# 0019. The reports folder on Linux, no findings question at the end of run, and the problem list's agent and system

Date: 2026-11-19

## Context

A person used RosmarOps 0.15.0 on Linux and found three things that did not work as intended:

- Firefox installed as a snap could not open the report. A snap browser can read only the files in the person's folder that are not hidden, and the reports were in `~/.rosmarops/reports/`.
- At the end of every `run` in a console, RosmarOps asked which findings to dismiss or accept. On a first review every finding is new, so it listed all of them, and the question read like a step of the review.
- The problem list (format problems.v1) always sent an empty `agent` and an empty `os_version`. The agent was looked up by the wrong field, and no system had a reader for its version.

## Decision

- **Reports on Linux.** On Linux the default reports folder is `~/RosmarOps/`. macOS and Windows keep `~/.rosmarops/reports/`. A `report_directory` in the person's configuration still wins, and a company configuration cannot set one. The first time RosmarOps runs with the new default, it moves the old folder whole, `data/` included, to `~/RosmarOps/`, and says so once (`console.reports_moved`). It moves nothing when the person's configuration sets `report_directory`, when `~/.rosmarops/reports/` does not exist, or when `~/RosmarOps/` already exists. The snap hint (`console.report_snap_hint`) is shown only when the reports folder in use is inside a hidden folder. The write rule (invariant 2, [ADR 0002](0002-four-write-targets.md)) keeps one reports folder; only its default changes.
- **No question at the end of `run`.** `run` no longer asks about the findings, on any system, the Windows window included. Under the same conditions as the old question (a console, not a scheduled review, the person's own configuration, at least one new finding), it prints one line: `To dismiss or accept findings, run: rosmarops findings`. `rosmarops findings` and the double click still offer the choice.
- **The problem list's agent and system.** A `config_unreadable` problem names the agent whose configuration folder holds the file that could not be read. When no agent's folder holds it, the summary shown to the person says «one of your agents» instead. `os_version` is the system's name and version, read without running a program: `ID` and `VERSION_ID` of `/etc/os-release` on Linux, `RtlGetVersion` on Windows, and `ProductVersion` of `/System/Library/CoreServices/SystemVersion.plist` on macOS. The fields and the schema do not change: the list now carries what the schema already described.

## Consequences

- A person on Linux opens the report with any browser. Their history, findings memory, accepted configuration, problem notebook and pending packages move with the folder, so nothing starts from scratch.
- When both folders exist, `~/RosmarOps/` is used and `~/.rosmarops/reports/` is left as it was.
- `tests/trust/no_writes_out_of_scope_test.go` allows `~/.rosmarops/` and the reports folder in use, and nothing else.
- The Fleet queries in [docs/company.md](../company.md) read `/home/%/RosmarOps/data/rosmarops-summary.json` on Linux.
- A problem list sent after this change carries the agent's product id and the system's version. Neither identifies the person or the computer.
