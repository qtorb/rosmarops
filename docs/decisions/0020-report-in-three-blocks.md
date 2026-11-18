# 0020. The report's first screen in three blocks, findings grouped by problem

Date: 2026-11-18

## Context

The report opened with a dark summary sheet and, below it, «What to do first» and «What remains»: one finding at a time, in a narrow column with half the page empty. On a real computer (125 findings, 55 of them urgent) the same problem showed as dozens of rows: one Supabase project's keys, read by five agents and also in git, were four findings; one agent able to change five other agents' settings was five. The owner signed a new first screen on 18-11-2026: a result, the findings grouped by problem, and the template that keeps them from coming back.

## Decision

- **Three blocks.** Block 1 is the result: the computer's score, its class and the factory reference. Block 2 is the findings grouped by problem, at most six in a table with a link to the rest; each problem says how to fix it. Block 3 is «Choose a template», the only filled button. The per-agent views, the template picker and every finding with its steps stay behind their links, unchanged.
- **Grouping at draw time, with no new field.** The report groups when it draws the HTML (`internal/report/issues.go`), from data `report.v2` and `graph.v2` already carry. A credential problem joins the credential findings whose credentials open the same service, by project when a credential names one; a service with one known project takes the keys that name none, and a service with several keeps those apart. A settings problem joins the findings of one agent that can change other agents' configuration or instructions. Everything else is one finding per problem. The findings, their codes and their count do not change, so usage statistics and the company summary are unchanged.
- **Supabase database URLs.** A database URL to `db.<ref>.supabase.co`, or to a `*.pooler.supabase.com` host with the user `postgres.<ref>`, is that Supabase project's credential. Only the host and the user are read; the password never is.
- **«How to fix it» with certainty.** Each Claude Code template pass returns its findings. The report counts as fixed by «Choose a template» only what every offered template takes away, so the line holds whichever template the person picks. A key in git is always rotated, never fixed by a template.
- **«If you fix this» per problem.** The score with every finding of the problem fixed at once (`index.WithoutSet`), shown in the problem's steps. Gains are never added up across problems.

## Consequences

- A space or another reader of the JSON does not see the groups: that would need a new field in `report.v2` and `space-view.v1`, a schema change the owner decides.
- Problems the signed texts cannot word (a key of an unknown service, a key no agent reads, a key whose project is not identified among several) stay one finding each, with its own text.
- Recognising Supabase database URLs changes the graph for people who have one: the credential gets its project, so its findings may merge with the project's other keys and the score may move.
