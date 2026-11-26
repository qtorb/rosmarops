# 0041. A review that ends

Date: 2026-11-26

## Context

The problem reports people chose to send showed that reviews ran out of time or did not finish. The reading of BV-308 measured why:
- Within a project's minute, the two secret searches take almost all the time: in the files and in the history.
- In the file search, almost all of it is spent matching patterns. `discord_bot_token` costs the most, because the only literal every match of it contains is a dot, which nearly every file has.
- With one core, the Linux kernel and kubernetes went over the minute.
- What was found before the cut stays in the report, but the text signed in ADR 0040 said it did not.
- Ctrl+C or closing the console left the in-progress mark, and the next review reported it as a problem.

## Decision

Signed by Albert on 26-11 at 07:51 (block 202, BV-311), as specified in T1, T2 and T3 of the advisor's proposal of 26-11.

**T1. The secret search finds exactly the same, in less time.**
- Before a pattern's regex runs on a file or on a commit's added lines, the file must pass checks that every match passes. If it does not, the regex is not run.
  - Besides the existing literal, three patterns get a shape. For `discord_bot_token`: a dot with at least 24 token characters before it, exactly 6 after it, another dot, and at least 27 more. For `authorization_bearer_hardcoded`: «Bearer» in any case. For `database_url_with_credentials`: `_url` in any case, and `://`.
  - The case-insensitive literal search starts from the literal's rarest letter, so a common first letter no longer slows it down.
- No pattern, limit or exclusion changes, and no dependency is added.
- The history search is mostly the time `git log` takes to produce the diffs. That is left as it is: making git cheaper would change which lines are searched.

**T2. The text says what is really missing.** «What we could not read» now says that what was found in a project before it was cut is in the report, and that what was left to check is not. The line after those projects is 12 px apart from the list that follows.

**T3. Cancelling is not a failure.**
- A review started at a console listens for Ctrl+C and for the console or terminal closing: `SIGINT` and `SIGHUP` on macOS and Linux, `CTRL_C_EVENT` and `CTRL_CLOSE_EVENT` on Windows.
- Before it writes its results, a cancellation stops the review. It does not write the state or the report, it clears the in-progress mark and, with a console, it says «Review cancelled. The previous report has not changed.»
- Once the results are being written (the graph, the problem notebook, the report, the memory, the state), a cancellation waits for them to end and then stops, without that line, because the report has changed.
- The next review records no `run_unfinished`. A process that is killed or hangs still does.
- `session-check`, `statusline` and the weekly review are unchanged.

## Measured

Measured as in BV-308: a cloud container with Linux, 4 cores and 15 GB, no network for RosmarOps, `rosmarops init` and `rosmarops run` on a temporary home. Each review covered kubernetes (`3596eb69d25`), vscode (`ae4f67801dd`), Linux (`25faec9ce61`) and ten small repositories. One core means `taskset -c 0`. Times are in seconds.

| Repository | 4 cores, before | 4 cores, after | 1 core, before | 1 core, after |
|---|---|---|---|---|
| kubernetes | 26.2 (files 12.2, history 14.0) | 17.3 (4.4, 12.9) | 60.0, cut in the history at 822 of 1,000 commits | 49.7 (12.6, 37.0) |
| vscode | 21.5 (13.3, 8.1) | 12.5 (4.6, 7.8) | 60.0, cut in the history at 897 of 1,000 | 41.9 (14.3, 27.4) |
| Linux | 57.1 (49.5, 7.5) | 22.8 (15.4, 7.4) | 60.0, cut in the files at 42,544 of 96,453 read | 55.7 (45.2, 10.5) |

- **The findings are the same, one by one.** The 106 findings of the 4-core review before the change are found again, every one with the same id, file, preview and commit, after the change, with 4 cores and with 1.
- **The 1-core review after the change, run again:** 46.0, 38.3 and 51.5 s, all within the minute.
- **The whole review** went from 68 s to 34 s with 4 cores, and from 71 s with three projects cut to 67 s with none cut with 1 core.

## Consequences

- No finding, pattern, limit, exclusion or schema changes.
- On a slower computer, a project can still go over its minute. The report then names it (ADR 0040) and says correctly what is missing.
- A review cancelled before its writes leaves everything as it was, except its run log.
