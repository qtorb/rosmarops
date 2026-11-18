# 0005. Resource limits and forced exclusions

Date: 2026-05-21
Amended: 2026-11-03
Amended: 2026-11-12

## Context

RosmarOps walks the person's folders and repositories. Without limits, one huge file, a dependency folder or a slow network drive could make a review run for hours or read far more than it needs. The limits must be fixed in the code, so that a configuration file cannot widen them. The first global time limit was a flat 300 seconds. On 2026-11-03 it became proportional to the number of projects. On 2026-11-12 the search for projects got its own limits and rules.

## Decision

- Forced exclusions, hardcoded and not configurable, in every recursive scan: `node_modules`, `.git`, `.next`, `dist`, `build`, `coverage`, `target`, `.venv`, `venv`, `__pycache__`, `.pytest_cache`, `.mypy_cache`, `.ruff_cache`, `.turbo`, `.parcel-cache` (`internal/scan/repos`).
- Files over 2 MB are not opened for content scan.
- Per-repository timeout: 60 seconds. A repository over budget gets a yellow `scan_timeout` finding and the run goes on. Each repository runs in its own context with `recover`, so one failure does not abort the run.
- Global timeout: 15 seconds per project, at least 300 and at most 1,200 seconds. It is computed once per run, after the projects are found, and counts from the start of the project search (`internal/scan/runner.go`).
- Project search (`internal/scan/projects`): at most 30 seconds, 200,000 folders read, 6 levels below each root and 100 repositories found (`MaxRepos`; repositories the person lists are always kept). Over budget, it stops, keeps what it found and the previous run's repositories that still exist, and adds a yellow `project_search_incomplete` finding.
- The project search only lists folders and never opens a file. Below a root it never enters a hidden folder, the forced exclusions, a few heavy folders (such as `site-packages`), the user's system folders (`AppData` on Windows, `Library` on macOS, `snap` on Linux, and a few more), a symbolic link, junction or other reparse point, another file system, a network file system, or a cloud placeholder not on disk.

## Consequences

- A review finishes in bounded time, and a slow part shows as yellow, not as a hang.
- Some content is never read. The report says so instead of counting it as clean.
- Tests: `internal/scan/runner_test.go`, `internal/scan/repos/timeout_test.go`, `internal/scan/projects/projects_test.go` and `tests/trust/project_search_test.go`.
- Relaxing any limit or exclusion needs the owner's decision.
