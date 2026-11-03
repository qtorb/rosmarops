# AGENTS.md

RosmarOps is a local security auditor for AI agents. It reads configuration at rest and reports what each agent on the computer can reach. People run it on their own computers, so a bug here is a bug on their machine.

## Review guidelines

Treat as P0 (must be fixed before merging):
- Any network use outside internal/spacenet: an import of net, net/*, crypto/tls, or a socket call anywhere else. Inside internal/spacenet, any request made without the person's recorded permission.
- Any write outside ~/.rosmarops/ and the configured reports folder.
- A secret, key or token printed, logged or written in full. Only the first 4 and last 4 characters may ever appear.
- A change that weakens or removes a test in tests/trust/, or a forced exclusion or resource limit from CLAUDE.md.
- A new third-party Go dependency, or any build step that downloads or runs a tool from outside the repository.

Treat as P1:
- A code path that can turn an error into a green report (fail-secure: an error must give at least yellow).
- Windows-only code (cmd/rosmarops/*_windows.go, PowerShell scripts) without a test that runs on Windows in CI.
- User-visible text that does not come from the message catalogs (internal/catalog/messages).
- Missing tests for a changed behaviour.

Do not comment on: formatting (gofumpt handles it), the wording of user-facing texts (they are fixed elsewhere), or the release notes.

Write each comment in English, name the file and line, say what can go wrong in one concrete scenario, and propose the smallest fix.
