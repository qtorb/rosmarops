# 0009. The capability graph v2, and the retirement of v1

Date: 2026-10-22

## Context

The first engine (v1) was a change detector. It compared each run with a baseline and reported what was new. Its finding model had a closed list of categories and no room for an agent, a permission, the reach of a credential or a relation between two agents. It could not say what an agent can do, what it can reach, or how one agent can change another.

## Decision

The engine's data model is a graph, defined by [`schemas/v2/graph.v2.schema.json`](../../schemas/v2/graph.v2.schema.json) (`schema_version` 2).

- **Nodes** have four kinds: `agent` (`ai_client`, `mcp_server`, `extension`, `bot`), `capability`, `credential` and `resource`.
- **Edges** are the relations: `has`, `reaches`, `writes_config_of`, `reads_config_of`, `loads`, `receives`, `authenticates_to`, `reads_env`, `committed_in`, `wakes` and `sends_to`.
- **Certainty** is `observed` (read as is from a file) or `inferred` (deduced). Deduced edges such as `reaches`, `writes_config_of` and `reads_config_of` are always `inferred`. They carry `derived_from` and `assumptions`: what the engine relies on and what it cannot see. There is no "confirmed" level: proving that a credential works would mean using it, and RosmarOps does not use the network for that.
- **A credential is a secret, not a place.** It has a salted `fingerprint` and a list of `locations`. The same key in several files is one node. The value is never stored.
- **The root carries `coverage`** (sources read, sources that failed, agents seen but not supported) **and `provenance`** (engine, rules and knowledge versions, host id).
- **Facts and judgement are separate.** The graph holds facts. Findings are produced by the rules (`internal/rules`), which walk the graph.

The graph is built in one place: `pipeline.Assemble` in `internal/pipeline`. The real run and the tests all call it. `internal/graph` validates every graph against the embedded schema and checks referential integrity. A graph that fails either check is rejected; no graph is produced.

v1 is being retired. The v1 collectors under `internal/scan/*` (for example `internal/scan/mcp`, `internal/scan/vscode`, `internal/scan/repos`) still run and still feed the v1 report, `report.json` (`schemas/report.v1.json`). Each one is removed only when its v2 replacement populates the graph, not before.

## Consequences

- New checks are queries over the graph, not new modules with their own data model.
- A query about absence (for example "no agent reaches this key") must not give a verdict when coverage is incomplete.
- Any change to the graph schema, or a v3, needs an explicit decision from the owner.
- For a time the code has two finding types and two reports (`report.json` and `report.v2.json`). `report.json` is not validated against `report.v1.json` before it is written, because the frozen v1 category list does not include the codes the engine now emits.
- Enforced by: `internal/graph` (validation), `tests/trust/pipeline_single_assembly_test.go` (one assembly path), `tests/trust/no_secret_in_graph_test.go` (no secret value in the graph).
