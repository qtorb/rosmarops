# Contributing

Thank you for helping. Read [ARCHITECTURE.md](ARCHITECTURE.md) first for the map of the code, and [docs/trust-model.md](docs/trust-model.md) for what the program promises.

## Requirements

- Go: the version in `go.mod` (`go 1.24`) is the minimum. CI builds with the version pinned in `.github/workflows/ci.yml`; use that one to get the same results.
- The pinned tools CI uses:

```bash
go install honnef.co/go/tools/cmd/staticcheck@2026.2.2
```

```bash
go install mvdan.cc/gofumpt@v0.7.0
```

- On Linux, `strace` for the runtime network test. Without it, that test is skipped.

## Build, test and check

Run the same checks as CI, from the repository root:

```bash
go build ./...
```

```bash
go test -count=1 ./...
```

```bash
go vet ./... && GOOS=windows go vet ./... && GOOS=darwin go vet ./...
```

```bash
staticcheck ./... && GOOS=windows staticcheck ./... && GOOS=darwin staticcheck ./...
```

```bash
test -z "$(gofumpt -l .)"
```

CI also runs the full-machine pipeline test on the three systems:

```bash
go test -count=1 -run TestFullMachinePipeline ./tests/integration/
```

## The rules that are never broken

The invariants are in [CLAUDE.md](CLAUDE.md) and are explained for users in [docs/trust-model.md](docs/trust-model.md). Each decision behind them has an ADR in [docs/decisions/](docs/decisions/README.md). The tests in `tests/trust/` prove them mechanically. Never weaken or skip one. The main groups:

- `no_network_test.go`, `no_network_proxy_test.go`, `no_problem_network_test.go`: no network outside `internal/spacenet`, checked in the build and while the program runs.
- `no_writes_out_of_scope_test.go`: the program writes only to its allowed places.
- `no_full_secret_leak_test.go`, `no_secret_in_graph_test.go` and the other `no_*_leak_test.go` and `no_*_secret_test.go` files: a secret never appears in full in any output, log, state or graph.
- `no_identity_in_*_test.go`: the share card, the notification and the status line name nothing personal.
- `no_weekly_path_leak_test.go`: the sealed results package never carries the path of the program that runs the weekly review.
- `no_external_in_html_test.go`, `no_raw_read_text_test.go`: the HTML report loads nothing from outside, and text read from disk is made safe before it is shown.
- `kit_key_permissions_test.go`: the kit key file is readable only by its owner.
- `pipeline_single_assembly_test.go`: the graph is built in one place only.
- The scenario and figure tests in the same folder check whole reviews against known results.

## Texts

Every text a person sees comes from the message catalog: `internal/catalog/messages/<lang>.json`, keyed by a stable code. English is the source and the fallback. The owner supplies every translation: do not write or edit translated text. The graph and the JSON outputs carry no prose, only ids, enums and codes. See [docs/how-to/change-a-text.md](docs/how-to/change-a-text.md).

## Dependencies

Do not add a third-party dependency without the owner's approval. RosmarOps is a security tool, and every dependency is part of what it asks people to trust. The command-line dispatcher, the schema validator and the TOML, YAML and JSONC readers are in-house for that reason.

## Opening a change

1. Create a branch from `master`.
2. Make the change, with its tests, and run the checks above.
3. Update the human documentation the change affects (see below).
4. Open a pull request. CI runs on Windows, macOS and Linux and must be green.
5. The owner reviews and merges. Contributors do not merge.

## Human documentation

Every pull request that adds or changes behaviour updates, in the same pull request and in English, the human documentation it affects:

- [ARCHITECTURE.md](ARCHITECTURE.md);
- the guides in [docs/how-to/](docs/how-to/);
- the [glossary](docs/glossary.md);
- an ADR in [docs/decisions/](docs/decisions/README.md) for any decision that changes what the product does, reads, writes or sends.

Code comments explain the why in the code's own words. They do not rely on internal ticket codes. `tests/docs/architecture_test.go` checks that every package in `internal/` is named in ARCHITECTURE.md, that every ADR is in its index, and that the relative links of these documents exist.
