# 0032. Only published versions send usage statistics

Date: 2026-11-23

## Context

The production service counted 126 installations in a week on Linux, channel
`other`, version `0.1.0-dev`, each with two checks and the same five agents.
None was a person's: every one was a CI run.

- **The test that sent.** The trust test `TestGrokBotNoSettingsTextLeaks` runs `rosmarops run` in a pseudo-terminal and pressed Enter more times than there were questions. The last Enter reached the usage question, where Enter means yes. The test then ran a scheduled review, which sent a second summary.
- **Why it reached production.** The trust sandbox passed the runner's environment through, with no proxy, so both summaries went straight to `app.rosmarops.com`.
- **Where it ran.** It ran on every Linux `verify` job: every push to `master` or a `bv-*` branch, and every pull request.
- **Why it was missed.** ADR 0024 made the journeys answer no and made a trust test scan workflows, scripts and steps files. It did not look at Go tests, and a bare Enter cannot be told apart from text.

## Decision

- **Only a published version asks and sends.** A published version is exactly three numbers: `X.Y.Z`. Any other build never asks the usage question and never sends a summary, whatever the state records. That covers:
  - `0.1.0-dev`, the version of a build without `-X main.version`, which every `go build` and `go test` makes;
  - a journey's candidate, `X.Y.Z-journeys.N`;
  - any prerelease.

  The check is `usageBuildMaySend` in `cmd/rosmarops/cmd_usage.go`.
- **What such a build shows.** The state line, in the report and in `rosmarops usage`, is a new catalog key, `usage.state.dev_build`. Its text, signed by the owner on 23-11 for v0.19.0: «Usage statistics: this build was made from the source code, so it never asks and never sends.» (Spanish in `es.json`). `rosmarops usage on` and `off` still record the answer, but nothing is sent from such a build.
- **The trust sandbox has no way out.** Every run in it has `HTTPS_PROXY` (and `HTTP_PROXY`, in both cases) set to `http://127.0.0.1:1`, where nothing listens, and no `NO_PROXY` exception. RosmarOps reaches the network only through the proxy when one is set, so a send a test triggers by mistake fails on that computer.
- **The test that sent answers no.** `TestGrokBotNoSettingsTextLeaks` presses Enter only for the four questions about the agents. It then answers «n» to everything after, the usage question included. It checks that `state.json` does not record usage statistics on.
- **CI records where the tests go.** The Linux `verify` job runs `go test ./...` behind a local proxy that only records each CONNECT target and never opens a tunnel (`.github/scripts/go-test-recorded.sh`). The job fails if any test asks for `app.rosmarops.com`. Tests that check a send on purpose set their own local proxy, which takes precedence. The modules are already in the cache from `go vet`; anything the go command fetched would go through the recorder too.
- **The usage trust tests build a published version.** They build with `-X main.version=9.9.9`, so they still exercise the send through a local proxy. A new trust test checks that a development build with usage statistics on asks for no connection at all.

## Consequences

- Neither `usage.v1`, `state.v2` nor invariant 1 changes: a build that is not published sends less, never more.
- Released binaries (`release.yml`), the Microsoft Store package and Homebrew carry `X.Y.Z` and behave as before.
- The journeys no longer meet the usage question: their candidates are not published versions. Their steps still answer no if asked (`if-asked`), and their end check still requires usage statistics off.
- A text scan of `_test.go` files is not added to `tests/trust/usage_off_in_tests_test.go`. The Enter that turned usage statistics on was plain text, indistinguishable from any other answer. The two checks that catch it look at behaviour instead:
  - the recording proxy in CI sees the destination;
  - a test binary cannot send at all.
- The 126 installations already counted are the service's to remove, by `version = 0.1.0-dev`, `os = linux` and `channel = other`. That is the owner's decision.
- The problem list (`problems.v1`) is not changed by this decision. It is sent only when the person presses «Send», or after they chose «Send the next ones without asking». The same sandbox proxy and the same CI check cover it.
