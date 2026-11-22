# Decisions

Each file here records one decision about what RosmarOps does, reads, writes or sends: the context, the decision, its consequences and its date. A decision that changes later gets an «Amended» line, or a new record that replaces it.

The invariants these decisions set are listed in [CLAUDE.md](../../CLAUDE.md) and explained for users in [docs/trust-model.md](../trust-model.md). The map of the code is in [ARCHITECTURE.md](../../ARCHITECTURE.md).

| # | Decision | Date |
|---|---|---|
| 0001 | [Network only in `internal/spacenet`, only with the person's permission](0001-network-only-in-spacenet.md): HTTPS to `app.rosmarops.com` only, with recorded or per-send permission. | 2026-05-21 |
| 0002 | [Only four write targets](0002-four-write-targets.md): `state.json`, the kit key file, `config.json` and the reports folder. | 2026-05-21 |
| 0003 | [No secrets in outputs](0003-no-secrets-in-outputs.md): a secret shows only as its first 4 and last 4 characters; the graph keeps a salted fingerprint. | 2026-05-21 |
| 0004 | [Fail-secure status](0004-fail-secure-status.md): one reducer sets the status; any error gives at least yellow. | 2026-05-21 |
| 0005 | [Resource limits and forced exclusions](0005-resource-limits-and-forced-exclusions.md): hardcoded folder exclusions, file size, per-repository and global time limits, a bounded project search. | 2026-05-21 |
| 0006 | [Self-integrity check](0006-self-integrity-check.md): the binary and configuration are checked at start; a change is accepted only by the person or a Microsoft Store update. | 2026-05-21 |
| 0007 | [No third-party dependencies in the binary](0007-no-third-party-dependencies.md): in-house CLI, schema validator and JSONC, YAML and TOML readers. | 2026-10-25 |
| 0008 | [Message catalog and languages](0008-message-catalog-and-languages.md): every text from an embedded catalog; English is the source; every run writes every language. | 2026-10-23 |
| 0009 | [The capability graph v2, and the retirement of v1](0009-capability-graph-v2.md): a validated graph of agents, capabilities, credentials and resources; v1 scanners go as each is replaced. | 2026-10-22 |
| 0010 | [The posture score](0010-posture-score.md): a score out of 100 inside the band of the worst finding, with versioned weights. | 2026-10-24 |
| 0011 | [The exposure class A-D](0011-exposure-class.md): six weighted axes give a letter that must agree with the band. | 2026-11-09 |
| 0012 | [Factory scores and sandbox templates P0-P3](0012-factory-scores-and-sandbox-templates.md): products scored at their defaults; templates measured by the same engine. | 2026-11-08 |
| 0013 | [Problem reports with permission](0013-problem-reports-with-permission.md): a closed list of problem codes, kept locally, sent only after «Send» or the opt-in. | 2026-11-10 |
| 0014 | [Usage statistics, asked once](0014-usage-statistics-asked-once.md): built in 0.15.0; ask once, then send a closed summary. | 2026-11-18 |
| 0015 | [RosmarOps inside Claude Code](0015-rosmarops-inside-claude-code.md): a session check, a status line and the plugin that carries the hook. | 2026-11-12 |
| 0016 | [Agent memory files are never opened](0016-agent-memory-files-never-opened.md): memory is recorded from file metadata only. | 2026-11-17 |
| 0017 | [Declared answers: Grok Bot and Cursor](0017-declared-answers.md): closed questions for settings that cannot be read on disk; no answer means the least controlled case. | 2026-11-14 |
| 0018 | [Company mode](0018-company-mode.md): a company configuration only administrators can change, a summary of counts and codes, and the administrator's fingerprint list. | 2026-11-12 |
| 0019 | [The reports folder on Linux, no findings question at the end of run, and the problem list's agent and system](0019-linux-reports-findings-hint-problem-fields.md): reports in ~/RosmarOps on Linux, moved once; one line instead of the question; agent and os_version filled. | 2026-11-19 |
| 0020 | [The report in three blocks](0020-report-in-three-blocks.md): result, findings grouped by problem at draw time, and the template; Supabase database URLs by host. | 2026-11-18 |
| 0021 | [Journey tests before publishing](0021-journey-tests-before-publishing.md): R1-R3 on the candidate's bytes on three machines, between build and publish; the gate turns on with JOURNEYS_GATE. | 2026-11-19 |
| 0022 | [Orchestrators and credentials known by their path](0022-orchestrators-and-credentials-by-path.md): Paperclip as an orchestrator with launches edges, four rules, and secrets identified by where they are, never opened. | 2026-11-20 |
| 0023 | [Paseo, Claude Squad, Orca and Claude Code's subagents, read at rest](0023-orchestrators-read-at-rest.md): launches observed when the mode is in the orchestrator's own file; Orca inferred from its default; subagent headers only. | 2026-11-20 |
| 0024 | [The team's installations counted apart](0024-team-installations-counted-apart.md): `rosmarops usage team on` sends the channel `internal`; tests never turn usage statistics on. | 2026-11-20 |
| 0025 | [Fixed only where the place was checked in full](0025-fixed-only-where-checked.md): findings memory v4 with the repositories each finding is in and the ones each run finished; `resolved` and `not_checked`; templates in use. | 2026-11-21 |
| 0026 | [MCP servers: unpinned launches, project auto-approval, and keys known by their name](0026-mcp-pinning-autoapproval-and-keys-by-name.md): `mcp_unpinned_launch` and `mcp_project_autoapprove`, index version 7; MCP keys recognised by their variable's name; rejected and disabled servers not loaded; Grok Bot on macOS. | 2026-11-21 |
| 0027 | [OpenCode and Kilo Code scored](0027-opencode-family-scored.md): one reader for the OpenCode family, permissions per scope, factory values from the release, not on the reference computer yet. | 2026-11-22 |
| 0028 | [A product default may carry its rule when it comes from the knowledge base](0028-product-default-rule-from-knowledge.md): `graph.v2` allows `rule` on a `product_default` capability whose source is `kb:`; OpenCode's factory terminal read. | 2026-11-22 |
