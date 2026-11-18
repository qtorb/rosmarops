# 0006. Self-integrity check

Date: 2026-05-21
Amended: 2026-10-26
Amended: 2026-11-12

## Context

RosmarOps is trusted to report on other programs. If its own binary or its configuration changed without the person knowing, its report could not be trusted either. A change must never be accepted in silence. But a normal update must not break the program. On 2026-10-26 the check was made to work in the normal flow: the references are recorded at setup, and a lost state counts as a change. On 2026-11-12 two things were added: Microsoft Store updates are accepted without asking, and the Claude Code hooks (`session-check`, `statusline`) check quietly.

## Decision

- On startup, RosmarOps computes the SHA-256 of its own binary and of the configuration in use. It compares them with `state.integrity.vigil_binary_sha256` and `state.integrity.config_sha256`.
- A mismatch, a lost or unreadable state, or a reference at zero after a run is a change, never a fresh start. It aborts with a minimal red report and one finding, `binary_hash_changed` or `config_hash_changed`, unless one of these accepts it:
  - **The person**, in the console. RosmarOps shows the old and new version and fingerprint and asks. Only "y" or "s" accepts. With no console, or in a scheduled review, nothing is asked and the run stops in red.
  - **A Microsoft Store update** (Windows), only if all hold: the process has RosmarOps's own package family name, fixed in `internal/winpkg`; its real executable is inside the package's installed folder as Windows reports it (`GetCurrentPackagePath`), never the `%LOCALAPPDATA%` alias; the package version equals the binary's version; and the binary's version is newer than `state.engine_version`. An older version, or the same version with another fingerprint, is never accepted this way.
- An accepted change records the new references and how it was accepted (`binary_accepted_via`: `baseline`, `console` or `store`). The report says so.
- `session-check` and `statusline` compare like every command, but never ask, write or abort. On a mismatch they print one line saying RosmarOps changed and that running it confirms the change.

## Consequences

- An unintended change to the program or configuration is caught on the next run.
- Limit: this does not stop someone who can already write as the person's user. A replaced program does not check itself. For that, people check the release attestation and checksum (see [the trust model](../trust-model.md)).
- Code: `cmd/rosmarops/integrity.go`, `cmd/rosmarops/storeaccept.go`, `internal/hash`. Tests: `cmd/rosmarops/integrity_test.go` and `tests/integration/integrity_test.go`.
- Signed releases (Authenticode, Apple) would add a signature check. Until then, the Store is the only automatic path.
