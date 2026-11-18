# 0016. Agent memory files are never opened

Date: 2026-11-17

## Context

Several agents keep a memory: files they write and load again in later sessions. Memory matters for exposure, because whoever can write it can steer the agent, the same way as with its instruction files. But memory holds what the agent learned about the person's work, often in free text, and can hold secrets. Reading it would mean RosmarOps handling the most private text on the computer.

## Decision

RosmarOps never opens an agent's memory files. It records only:
- the path, how many regular files it holds and their total size, from metadata (`os.Lstat` and directory listings; symbolic links are not followed);
- whether it is inside a repository.

The walk stops at 6 levels and 10,000 entries. The memory locations read today (`adapters/windows/alwayson.go`):
- Claude Code: `projects/<project>/memory` in its configuration folder, and the folder set by `autoMemoryDirectory` in its user settings;
- Codex: `memories` and `memories_v2`;
- Hermes Agent: `memories` in each Hermes folder;
- Cline: `memory-bank` in each repository;
- GitHub Copilot: `.github/copilot-memories.jsonl` in each repository.

Each memory becomes an `agent_memory` resource in the graph, with `file_count` and `total_bytes` and nothing of the content. Who can write it counts like an agent's instructions: it adds a location to the same instructions finding, not a new finding (`internal/rules`). The report says: "Agent memory: {n} files, {size}. RosmarOps doesn't open them."

## Consequences

- RosmarOps cannot say what a memory contains, or whether it holds a secret.
- The factory score keeps memory files, like instruction files.
- `tests/trust/bv251_memory_test.go` walks a file system that fails the test on any file open, and parses the memory functions' source to fail on any open or read call.
- Known gap: a memory file that is tracked by git inside a repository (Cline's `memory-bank`, Copilot's `copilot-memories.jsonl`) is still read by the repository secret check, which scans every tracked file in `internal/scan/repos`. The memory reader never opens it; that older check does not exclude it.
