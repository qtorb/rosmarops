# 0018. Company mode: a company configuration, a summary, and the administrator's list

Date: 2026-11-12
Amended: 2026-11-18 (built, with the weekly review of a company install)

## Context

A small company wants to install RosmarOps on its computers with a device manager such as Fleet, and to check from there that each computer has no red findings. RosmarOps must stay what it is for the person: local, read-only, with no network, and with nothing hidden from them. The company should learn only what it needs for that check, and an update installed by the company should not stop every scheduled review in red.

## Decision

- **A company configuration** (`company.json`, schema [company.v1](../../schemas/company.v1.json)) lives in a machine folder that only administrators can change: `<ProgramData>\RosmarOps\` on Windows, resolved through the Windows API and never from the environment, or `/etc/rosmarops/` on Linux. macOS has no company mode. RosmarOps reads it only after checking that it and its folder are owned by, and writable only by, administrators, and that neither is a link. A file that fails the check, or has a setting outside the schema, is ignored, and the review says so and is never green.
- **It can only** turn the company summary on or off, and give the project folders of a person who has no RosmarOps configuration of their own. It cannot hide, dismiss or accept findings, add secret patterns, move the report, connect the computer to a space, send anything, or touch the kit.
- **Precedence:** the person's `config.json`, when it exists, stays the configuration in use. Without it, a trusted company configuration is used in memory and `config.json` is never created. With a trusted company configuration the report is always in the usual folder, so the company finds the summary at a fixed path.
- **The summary** (schema [summary.v1](../../schemas/summary.v1.json)) is one JSON line at `~/.rosmarops/reports/data/rosmarops-summary.json`, written atomically on every review with the summary on, also when the review stops for integrity or fails. It holds counts and codes only: the status, red and yellow totals, the computer's score and band, findings by rule, agents grouped by type and product, the checks that do not apply, the configuration used and how the binary was accepted. Never a path, a file's content, a key or its preview, a name chosen by the person, or an identifier derived from a path. With the summary off, the review removes a previous one. The report's «Your company» section shows it exactly as the company reads it.
- **The administrator fingerprint list** (`binaries.sha256`, same folder, same check) is a third way, after the person and before the Microsoft Store, to accept a new binary: its SHA-256 must be listed with its own version. It never accepts an older version, or the same version with another fingerprint. With the company configuration in use and no state, the first review is a first run only if the binary passes the list or the Store check. `session-check` and `statusline` never accept through the list.
- **A change to the company configuration in use** is recorded (`state.integrity.config_source = company`, `config_sha256` of `company.json`) and reported, never an abort: its trust comes from the ownership check.
- **Writes:** RosmarOps never writes, creates or changes the permissions of the company configuration, the list, or their folders.
- **The weekly review** a company installs (a scheduled task for the Users group on Windows, a global systemd user timer on Linux) is recognised as this computer's weekly review when it runs this same program.

## Consequences

- The company reads a file on the computer with its own agent. RosmarOps sends nothing: the summary is written to disk, not over the network.
- Code: `internal/company` (folder, ownership check, both files), `cmd/rosmarops/company.go` and `cmd/rosmarops/integrity.go` (precedence, list, summary), `internal/report/company.go` and `internal/report/html_company.go` (summary and «Your company»).
- Tests: `tests/trust/company_test.go` (summary schema and rules, no secret or path in it, off by default, untrusted and invalid configurations, the list, exit code after an abort, no writes outside scope), `TestNoNetwork_CompanyRuntime` and `TestNoNetwork_CompanyPackages` in `tests/trust/no_network_test.go`, and `internal/company`'s tests, with the Windows permissions on the Windows runner.
- The install scripts in [docs/company/](../company/README.md) are drafts until a pilot proves them on real computers. Their guide is [docs/company.md](../company.md).
- The wording is invariants 2 and 8 in [CLAUDE.md](../../CLAUDE.md).
