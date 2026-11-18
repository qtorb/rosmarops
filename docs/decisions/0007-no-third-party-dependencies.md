# 0007. No third-party dependencies in the binary

Date: 2026-10-25
Amended: 2026-10-28
Amended: 2026-11-01

## Context

RosmarOps is a security tool. Every dependency is code we did not write that runs with the person's account, and part of our attack surface. In October 2026 three libraries brought network packages into the binary: the JSON Schema validator (gojsonschema), and the CLI framework (cobra) with its flag library (pflag). That made the public "no network" promise false, even though no socket was opened. Later, new agents needed YAML (2026-10-28) and TOML (2026-11-01) readers, and the same choice came up: library or in-house.

## Decision

- The product binary links only the Go standard library and this module.
- The CLI uses the standard `flag` package with an in-house dispatcher (`internal/cli`). Help texts come from the message catalog.
- JSON Schema validation uses an in-house validator (`internal/schemaval`), limited to the keywords our schemas use. It refuses any other keyword when it compiles a schema, so a new keyword fails in tests, not silently. Schemas are loaded at startup; invalid configuration or state fails fast.
- gojsonschema stays only as a test oracle. A differential test and a fuzz test check that both validators agree on every schema and fixture (`internal/schemaval/differential_test.go`). Test imports never reach the binary.
- Configuration formats are read by small in-house readers: `internal/jsonc` (JSON with comments), `internal/jsonkeys` (JSON with exact, case-sensitive keys), `internal/yamlsubset` and `internal/tomlsubset`. Each reads only the subset the agents use. Anything outside it makes the whole file unread, never half-read.
- Any new third-party dependency needs the owner's explicit approval.

## Consequences

- No third-party code runs in the product, and the network guarantee of [0001](0001-network-only-in-spacenet.md) can be checked on the dependency graph.
- We maintain the readers and the validator ourselves, and must keep them strict and tested.
- `go list -deps ./cmd/rosmarops` shows only standard and `github.com/qtorb/rosmarops` packages. `tests/integration/cli_surface_test.go` pins the CLI's commands, flags and exit codes.
