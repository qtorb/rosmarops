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
| 0014 | [Usage statistics, asked once](0014-usage-statistics-asked-once.md): decided, not built yet; ask once, then send a closed summary. | 2026-11-18 |
| 0015 | [RosmarOps inside Claude Code](0015-rosmarops-inside-claude-code.md): a session check, a status line and the plugin that carries the hook. | 2026-11-12 |
| 0016 | [Agent memory files are never opened](0016-agent-memory-files-never-opened.md): memory is recorded from file metadata only. | 2026-11-17 |
| 0017 | [Declared answers: Grok Bot and Cursor](0017-declared-answers.md): closed questions for settings that cannot be read on disk; no answer means the least controlled case. | 2026-11-14 |
| 0018 | [Company mode](0018-company-mode.md): a company configuration only administrators can change, a summary of counts and codes, and the administrator's fingerprint list. | 2026-11-12 |
