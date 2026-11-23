# 0035. A verdict, «Start here» and «What it means» in the report

Date: 2026-11-23

## Context

Someone who opens the report saw a score («11/100 · Exposed»), a technical
sentence about the agent with the least control, and a table of problems.
The easiest action, choosing a template, was in block 3, below the table.
Eleven kinds of finding had only their title, with no sentence saying what
they mean.

## Decision

Signed by Albert on 23-11 at 18:36, with the corrected sentences and the
build decisions of the advisor's answers to the reading (23-11 19:05). All
the texts are the signed ones, in the catalogs and in `docs/copy`.

- **A verdict.** Block 1 starts with one plain sentence that depends on the
  level of the score block 1 already shows: exposed, open gaps or contained
  (`html.r3.verdict.<level>`). With no score (no agent evaluated) there is no
  verdict. A provisional class does not change it.
- **«Start here».** Under the score, one card with one recommended action:
  the first of three cases that holds.
  1. **A template.** When the template fixes at least one urgent finding and
     the person does not already use one: «Choose a template. It fixes {k} of
     your {n} urgent findings…», with block 3's button and line. {k} and {n}
     are counted by the same functions as blocks 2 and 3.
  2. **A key saved in git.** Otherwise, the first problem of the table with a
     key saved in git whose service is known: «Change your {service} key…»,
     and a button to that problem's steps. A key whose service is empty or
     «unknown» does not count, as in the table's «How to fix it».
  3. **Problem 1.** Otherwise: «Start with problem 1.» and its title, and a
     button to its steps.

  With no problems there is no card. Block 3 does not change.
- **«What it means».** Each of the eleven rules whose title was their only
  sentence (`computer_use_app_allowed`, and the orchestrator, subagent and
  MCP rules) now has one, where the other findings have their impact
  paragraph. Where a rule's finding covers a case the main sentence would
  overstate, a signed variant says only what the rule sees:
  - every app allowed (`*`) rather than named ones;
  - an orchestrator whose effect on approvals is unknown (yellow);
  - a copy of the orchestrator in npm's cache rather than the installed one;
  - a subagent mode that still asks for some things (yellow);
  - a project's own `enableAllProjectMcpServers` rather than the user's or the
    managed one;
  - a subagent hook, which runs when the subagent does what the hook watches,
    not every time it acts.

  The sentences are in the HTML report only. The export for your AI, the
  findings window, the space view and the Markdown keep the title alone,
  until the sentence has been seen in the report.

## Consequences

- No score, rule, index or schema changes. The report has no new URL: the
  card's buttons are links inside the page.
- Block 1's sentence next to the score is now 15 px in the second ink, under
  the 22 px verdict.
- Adding a sentence to another output, or another rule, is a new decision.
