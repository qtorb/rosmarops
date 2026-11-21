# 0025. Fixed only where the place was checked in full

Date: 2026-11-21

## Context

Built in version 0.18.0 (BV-273). Until now, an open finding missing from a review was «no longer observed». That covered a real fix, but also a repository that timed out, a project the search did not reach, or an agent that was uninstalled. The report could not say «you fixed it», and the space said «fixed» when it could not know.

## Decision

- **Findings memory v4** (`schemas/v2/findings-memory.v4.schema.json`). The last checked run keeps `repos_complete`: the salted references (HMAC with the local salt, 16 hex) of the repositories whose check finished. A repository counts only if it did not time out, finished inside the global limit, and its root's project search finished. Each entry keeps `repos`: the salted references of the repositories its files are in, empty for a finding outside every repository. The second field was chosen by the owner on 21-11: without it, a finding that is gone cannot be tied to its repository. Never a path.
- **Two verdicts for a finding that is gone.** In a comparable run whose graph was built and whose agent sources were all read:
  - `resolved` when every repository it was in is in this run's `repos_complete`, and its agent is still installed;
  - `not_checked` otherwise, and the finding stays open.

  `no_longer_observed` remains only where it cannot be known: an entry from a v3 memory that was not seen again. So the first v4 run after a v3 memory resolves nothing. A finding that moved (the same rule, agent and credential appearing elsewhere) is not resolved. The rules the memory does not track are unchanged (`not_comparable`).
- **`report.v2` does not change.** Its verdict counts are a closed list, so `resolved` lives in the memory and in the report's own data, like the new agents. Adding it to `report.v2` is a separate schema decision.
- **The report.** Block 1 says «Since the last check» when something was fixed or the score went up. The «Since the last check» column counts fixed and not checked findings. The space says «fixed» only for `resolved`.
- **Templates in use.** Each review recognises the Claude Code, Grok Build and Cursor template the person's user settings already hold, from the files it already reads. Matching is by everything the template sets; entries of the person's own do not matter, and the strictest match wins. To recognise «Sandboxed development», the Claude Code settings reader now also keeps `sandbox.network.strictAllowlist` and `sandbox.credentials.files` of the same file.

## Consequences

- Versions 1 to 3 of the memory are still read; v4 is written.
- A repository that does not finish holds back only its own findings, which stay open until a review checks it in full.
- Cursor's two templates leave the same `sandbox.json` (`readBoundary: "workspace"`), and the rest of each is in the app, which RosmarOps does not read, so it cannot tell which was applied (advisor, 21-11). Block 3 then says «Read only or Local development» (`html.r3.tpl.cursor_either`), with no next template and no «strictest», and both Cursor cards carry «In use».
- `index_version` does not change.
