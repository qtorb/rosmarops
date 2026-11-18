# 0008. Message catalog and languages

Date: 2026-10-23
Amended: 2026-10-27
Amended: 2026-11-12

## Context

RosmarOps is used in more than one language. Text written inline in code drifts, cannot be translated and mixes prose into machine outputs. Machine outputs (the graph, JSON) must stay stable for tools, whatever the language. The first rule (2026-10-23) used English unless the person passed `--lang`. On 2026-10-27 the default became the system's display language, and every run writes the report in every language. On 2026-11-12 Catalan, Galician and Basque were mapped to Spanish.

## Decision

- English is the default and the source language.
- Every human-readable string (report, CLI messages, finding texts) comes from a message catalog keyed by a stable code: one embedded JSON file per language in `internal/catalog/messages/` (today `en.json` and `es.json`). A missing or empty key falls back to English.
- Without `--lang`, RosmarOps follows the system's display language if a translation exists for it, and English otherwise. On Windows it reads the user's display language; on Linux, the locale (`LC_ALL`, `LC_MESSAGES`, `LANG`); on macOS it uses English. Catalan, Galician and Basque select Spanish. `--lang` always wins.
- Every run writes the report in every available language, and each page links to the others.
- The graph and every JSON output carry no user-facing prose: only ids, enums, stable codes and technical detail.
- Placeholders are filled in one pass, so a value read from disk is never expanded as a placeholder.
- Translations are supplied by the owner. Code never writes translated copy.

## Consequences

- Adding a language means adding one catalog file. No code change is needed to write the report in it.
- Tools can read the JSON outputs without parsing prose.
- Code: `internal/catalog` and `cmd/rosmarops/lang.go`. Tests: `internal/catalog/catalog_test.go` and `cmd/rosmarops/lang_test.go`.
