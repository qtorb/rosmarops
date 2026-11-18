# 0004. Fail-secure status

Date: 2026-05-21

## Context

A security report that says "all clear" when part of the check failed is worse than no report. Each scanner can fail: a file cannot be read, a repository times out, a reader panics. If every caller had to remember to lower the status, one would forget.

## Decision

- The overall status is red, yellow or green.
- The status is computed in one place, the reducer (`finding.Reducer` in `internal/finding`), not by callers:
  - any red finding: red;
  - otherwise, any yellow finding or any scan error: yellow;
  - otherwise: green. Findings of severity `info` do not block green.
- The v2 report (`internal/report`) applies the same rule. If the graph could not be built, the status is at least yellow. If any source failed to read, it is at least yellow. The final status is the worse of the legacy and v2 statuses.
- Scanner panics are contained (`RecoverStep` in `internal/scan`) and become scan errors, so they also force at least yellow.
- What RosmarOps could not read is listed in the report and never counted as clean.

## Consequences

- Green means every check ran without error and found nothing above `info`.
- An error can make a report yellow with no real finding. That is the intended cost.
- `internal/finding/finding_test.go` tests the reducer, including that a scan error forces yellow and that green needs zero errors.
