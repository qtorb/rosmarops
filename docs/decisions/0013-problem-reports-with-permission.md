# 0013. Problem reports with permission

Date: 2026-11-10

## Context

When RosmarOps fails on someone's computer, the owner does not find out unless the person writes in. Telling us had to be as easy as pressing a button. But RosmarOps reads the same things malware would, and its promise is that nothing leaves the computer without permission. The owner defined the rule: the program keeps locally what goes wrong, and asks before it sends anything.

## Decision

**A local notebook.** `internal/problems` records problems in `rosmarops-problems.jsonl`, in the reports data folder, at most 200 lines. Each line is a code from a closed list of seven (`run_unfinished`, `step_failed`, `time_ran_out`, `output_invalid`, `space_rejected`, `config_unreadable`, `report_not_opened`), the part of the review, the agent involved if any, the day and a count. It never records paths, file or folder names, user or computer names, file contents, keys, configuration values, the IP or the text of error messages.

**A closed format.** What is sent is `problems.v1` (`schemas/problems.v1.schema.json`): the codes above, the language, program version, system and architecture, at most 20 problems, and an optional comment and email. `problems.Marshal` validates every report against the schema; a report that does not validate is never returned.

**Sent only with permission.** `internal/spacenet` sends it with `SendProblems` to `https://app.rosmarops.com/api/v1/problems`, with no account and nothing that links it to a space or a device. It refuses without a `ProblemsConsent`, which can come only from:
- `ConsentFromSendButton`: the person pressed «Send» now;
- `ConsentFromState`: the person ticked «Send the next ones without asking», stored in the state. The box is off by default.

`rosmarops problems ask` unticks the box. A scheduled review never asks. Every send is listed in `rosmarops-sends.log`.

## Consequences

- The network invariant names this as one of the few allowed uses of `internal/spacenet` (see [the trust model](../trust-model.md)).
- `tests/trust/problem_consent_source_test.go` fails if `ConsentFromSendButton` is called from anywhere but the «Send» branch in `cmd/rosmarops/problemprompt.go`.
- `tests/trust/no_problem_leak_test.go` checks that no path, name or secret reaches the report; `tests/trust/no_problem_network_test.go` checks that no connection happens without consent.
- Adding a code or a field is a schema change for the owner.
