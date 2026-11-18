# 0001. Network only in `internal/spacenet`, only with the person's permission

Date: 2026-05-21
Amended: 2026-10-27
Amended: 2026-11-01
Amended: 2026-11-10

## Context

RosmarOps reads the configuration of AI agents, keys included. A tool with that reach must be able to prove where its data goes. The first rule was simple: no network at all. Later the product gained two features that need the network: connecting a computer to the person's space on app.rosmarops.com, and sending a list of problems the program ran into. On 2026-10-27 the owner accepted network use only if each feature has its own decision, is off by default and needs the person's consent. On 2026-11-01 the owner chose one program with one network package, not a second executable. On 2026-11-10 the problem list was added.

## Decision

- Only `internal/spacenet` may import `net`, `net/*` or `crypto/tls`, or open a socket.
- It speaks HTTPS to one host, `app.rosmarops.com`, port 443. The host is a compile-time constant. The dialer and the transport refuse any other address. Redirects are refused. TLS uses the system roots, version 1.2 or later. `HTTPS_PROXY` is honoured, but the tunnel still goes to the fixed host and TLS checks that name.
- Every request is capped at 30 seconds.
- It acts only with permission:
  - space: the permission recorded in the state (`spacenet.FromState`);
  - problem list: pressing «Send» at that moment, or «Send the next ones without asking» recorded in the state.
- It does only four things: connect this computer, send this computer's results package, disconnect, and send the problem list (`schemas/problems.v1.schema.json`).
- Results packages are encrypted end to end. The problem list carries only the problems.v1 fields.
- Every send is listed in `rosmarops-sends.log`, in the `data` folder of the reports folder.
- Any other network use needs a new decision from the owner.

## Consequences

- Without permission, the program opens no connection. Updates are never checked.
- `tests/trust/no_network_test.go` checks, for Windows, macOS and Linux, that only `internal/spacenet` brings in network packages, that no code calls sockets directly, and that the host cannot be changed with linker flags. It also runs the binary on Linux under strace and fails on any connection without permission.
- `tests/trust/no_network_proxy_test.go`, `tests/trust/no_problem_network_test.go` and `tests/trust/problem_consent_source_test.go` cover the proxy path and the problem list consent.
- The public promise is in [the trust model](../trust-model.md).
