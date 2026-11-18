# 0003. No secrets in outputs

Date: 2026-05-21

## Context

RosmarOps looks for keys and tokens in agent configuration, repositories and environment files. Its outputs (reports, logs, state, the graph) are files people keep, share or paste into an AI. If any of them held a full secret, RosmarOps would leak the very thing it warns about.

## Decision

- A secret match is recorded as type, file, line and a redacted preview.
- The preview is the first 4 characters, then ` ... `, then the last 4 characters. Never the full value, never the middle. A value shorter than 8 characters is shown as `****`. The code is `Redact` in `internal/scan/secrets`.
- Text that may embed a secret, such as a permission rule, is kept only after every match is replaced by its preview.
- The capability graph stores a salted fingerprint of each key, never the value.
- Errors from the configuration readers carry file, line, column and a code, never the text read.
- The «Copy for your AI» export carries no secret values, previews or fingerprints.

## Consequences

- People can share a report without leaking keys. The preview is enough to recognise which key to rotate.
- `tests/trust/no_full_secret_leak_test.go` feeds known fake secrets and fails if a full value appears in any report, log or state. `tests/trust/no_secret_in_graph_test.go` does the same for the graph, and `tests/trust/decoder_errors_test.go` for reader errors. Other `tests/trust/no_*_leak_test.go` files cover specific agents' key files.
- Any new output must pass the same tests. See [the trust model](../trust-model.md).
