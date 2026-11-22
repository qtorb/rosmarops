# 0028. A product default may carry its rule when it comes from the knowledge base

Date: 2026-11-22

## Context

The terminal-read capability (BV-245) is a `read_files` capability on the whole computer, identified by its `rule`, which starts with `terminal: `. Until now `graph.v2` allowed a `rule` only on a `configured` capability. So the terminal read could come only from a setting a file writes.

OpenCode runs any command without asking by default (ADR 0027). Without a terminal read from its factory default, a factory OpenCode reached the credentials of its projects only, and its factory score (75) said less than it does.

## Decision

Signed by Albert on 2026-11-22 (BV-276). It is a change inside `graph.v2`, version 2.

- **The rule:** a capability with `basis: product_default` may carry `rule` when its `source` starts with `kb:`, the knowledge base. This is the same criterion as an inferred `wakes`, whose source must be `kb:`.
  - The brief names `derived_from`, but a capability has only `source`, so the criterion is on `source`.
- **What else stays the same:**
  - `configured` still requires `rule`;
  - `declared`, and a `product_default` from anywhere else, still may not have one.
- **The test:** the differential test checks six cases against the oracle, and `TestSchemaCTO3_*` keeps the case that stays invalid.
- **OpenCode** uses it: its factory `bash: allow` gives the terminal read, with the rule `terminal: bash: allow` and the source `kb:opencode/shell_allow@<version>`.
- **Kilo Code** does not: it asks before commands except an allowlist of file commands, which ask outside the project.

## Consequences

- A factory OpenCode reaches every credential on the computer through commands. Its factory score on R2 moves from 75 (`open_gaps`) to 31 (`exposed`); the owner signs it before it is pinned.
- Another agent whose documented default runs commands without asking can get the same terminal read from its knowledge base, without a file.
