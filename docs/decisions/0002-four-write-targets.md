# 0002. Only four write targets

Date: 2026-05-21
Amended: 2026-10-30
Amended: 2026-11-14
Amended: 2026-11-18 (company mode)

## Context

RosmarOps runs with the person's own account and can read much of their home folder. If it could write anywhere, a bug or a hostile input could change agent settings, repositories or keys. The first rule allowed two targets: the state and the reports folder. On 2026-10-30 the kit key file was added for the connected space. On 2026-11-14 the configuration file was named explicitly, with limits on when it is written.

## Decision

RosmarOps writes only to:

1. `~/.rosmarops/state.json`.
2. The kit key file: `~/.rosmarops/kit.dpapi` on Windows (protected with DPAPI for the current user), or `~/.rosmarops/kit.key` on macOS and Linux (mode 0600 in a 0700 folder).
3. `~/.rosmarops/config.json`: created once at install if it does not exist, and rewritten only when the person changes which findings are dismissed or accepted. If the configuration in use is another file, `rosmarops findings` writes nothing and says so.
4. The configured reports folder (default `~/.rosmarops/reports/`), including its `data` subfolder.

No other file create, write, open-for-write or mkdir call is allowed. RosmarOps never writes, creates or changes the permissions of the company configuration, the administrator fingerprint list, or their folders (see [ADR 0018](0018-company-mode.md)); the company summary is written inside the reports folder. RosmarOps never creates scheduled tasks: the report gives the command, and the person runs it.

## Consequences

- RosmarOps never changes the person's files, settings, repositories, keys or agents. It reads; the report says what to change.
- `tests/trust/no_writes_out_of_scope_test.go` runs the program in a sandbox and fails on any write elsewhere. It also checks that a review does not write the configuration, and that dismissing a finding writes only `~/.rosmarops/config.json`.
- `tests/trust/kit_key_permissions_test.go` checks the kit key file modes. The code is in `internal/kit`.
- A new write target needs the owner's decision. The current wording is invariant 2 in [CLAUDE.md](../../CLAUDE.md).
