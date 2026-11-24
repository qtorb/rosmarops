# 0037. A family per problem, and one row for agents that change others

Date: 2026-11-24

## Context

In a real report, five of the first six problems said the same thing with a different agent: «Cline can change the settings of other agents», «Claude Code can…», «Cursor can…». Only the credential problems had a tag saying what they were about, and its 12 px icon inside the tag did not read as an icon.

## Decision

Signed by Albert on 24-11 at 08:51, with the advisor's answers to the reading (24-11 09:10) and the icon box he signed afterwards (it replaces the tag). The texts are the signed ones, in the catalogs and in `docs/copy`.

- **A family per problem.** Each rule belongs to one of eight families:
  - Key: the credential rules and `n8n_key_with_data`;
  - Between agents: an agent can change other agents' settings or instructions;
  - Agent: what an agent does or can change of itself;
  - MCP: `mcp_*` and `n8n_mcp_bridge`;
  - Orchestrator;
  - Subagent;
  - Project: a project's hook that runs its own script;
  - Desktop: `computer_use_app_allowed`.

  `package_known_malicious` has no family. A grouped problem takes its kind's family: Key for a service's credentials, Between agents for agents that change others. A problem of its own takes its rule's family.
- **The icon box.** A problem with a family has, to the left of its text, a 36×36 px box with the family's 20 px icon. The box is red for an urgent problem and amber for one to review. The family's name goes above the title, in small capitals. The same box is in the header of each problem's section, after its number. The findings inside a problem, and each agent's part of the single row, have no box: an icon is not repeated on every line. A problem without a family has no box.
- **The «Key» tag** of 0.19 is no longer used in the table or in the problems' sections.
- **One row for agents that change others.** With two or more agents that can change other agents' settings, the table has one row: «{n} of your agents can change other agents' settings: {agents}.»
  - The agents are listed in the order their problems had: more findings first.
  - «Findings» is the sum and «Level» the highest.
  - «How to fix it» keeps today's logic over all its findings.
  - Its section has one part per agent, each with that agent's title, detail line, score gain and findings.
  - With one such agent, the row is today's.

## Consequences

- The rules, the findings, their number, the score, the index and every schema do not change. The grouping happens only while the HTML report is drawn: usage statistics, the company summary and the space count findings, never problems.
- The header's problem count, the problems' numbers and their links move together, because they come from the same list.
- «Doesn't affect me» and «I accept it» are stored per finding, so they keep working per agent.
- The export for your AI, the findings window, the space view and the Markdown do not use problems and are unchanged.
