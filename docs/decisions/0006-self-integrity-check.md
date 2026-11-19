# 0006. Self-integrity check

Date: 2026-05-21
Amended: 2026-10-26
Amended: 2026-11-12
Amended: 2026-11-18 (the administrator fingerprint list built)
Amended: 2026-11-19 (every command asks like run)

## Context

RosmarOps is trusted to report on other programs. If its own binary or its configuration changed without the person knowing, its report could not be trusted either. A change must never be accepted in silence. But a normal update must not break the program. On 2026-10-26 the check was made to work in the normal flow: the references are recorded at setup, and a lost state counts as a change. On 2026-11-12 two things were added: Microsoft Store updates are accepted without asking, and the Claude Code hooks (`session-check`, `statusline`) check quietly.

## Decision

- On startup, RosmarOps computes the SHA-256 of its own binary and of the configuration in use. It compares them with `state.integrity.vigil_binary_sha256` and `state.integrity.config_sha256`.
- A mismatch, a lost or unreadable state, or a reference at zero after a run is a change, never a fresh start. It aborts with a minimal red report and one finding, `binary_hash_changed` or `config_hash_changed`, unless one of these accepts it:
  - **The person**, in the console. RosmarOps shows the old and new version and fingerprint and asks. Only "y" or "s" accepts. With no console, or in a scheduled review, nothing is asked and the run stops in red.
  - **The administrator fingerprint list**, on a computer a company manages: `binaries.sha256` in the company folder (see [ADR 0018](0018-company-mode.md)) holds the running binary's SHA-256 with its version, and the list and its folder pass the ownership check. It is tried before the person is asked.
  - **A Microsoft Store update** (Windows), only if all hold: the process has RosmarOps's own package family name, fixed in `internal/winpkg`; its real executable is inside the package's installed folder as Windows reports it (`GetCurrentPackagePath`), never the `%LOCALAPPDATA%` alias; the package version equals the binary's version; and the binary's version is newer than `state.engine_version`. An older version, or the same version with another fingerprint, is never accepted this way.
- Neither the list nor the Store ever accepts a version older than `state.engine_version`, or the same version with another fingerprint. With the company configuration in use and no configuration of the person's, a lost or missing state is a first run only if the binary passes the list or the Store check.
- With the company configuration in use, `config_sha256` fingerprints `company.json` and `config_source` is `company`: a change to it is recorded and reported, never an abort.
- An accepted change records the new references and how it was accepted (`binary_accepted_via`: `baseline`, `console`, `admin_list` or `store`). The report says so.
- Every command that checks integrity (`status`, `findings`, `problems ask`, `usage`, `kit`, `connect`, `disconnect`, `allow`, `schedule`) uses the same check as `run`: in a console it asks and, if the person confirms, records the change and goes on; without one it stops in red. `init`, `baseline`, `--help` and `--version` do not check. The texts of a stop come from the catalog, in the system's language, and none sends the person to `baseline`: accepting a new version is the question every command asks.
- `session-check` and `statusline` compare like every command, but never ask, write or abort, and never accept through the list. On a mismatch they print one line saying RosmarOps changed and that running it confirms the change.

## Consequences

- An unintended change to the program or configuration is caught on the next run.
- Limit: this does not stop someone who can already write as the person's user. A replaced program does not check itself. For that, people check the release attestation and checksum (see [the trust model](../trust-model.md)).
- Code: `cmd/rosmarops/integrity.go`, `cmd/rosmarops/storeaccept.go`, `internal/hash`. Tests: `cmd/rosmarops/integrity_test.go` and `tests/integration/integrity_test.go`.
- Signed releases (Authenticode, Apple) would add a signature check. Until then, the list and the Store are the only automatic paths.
