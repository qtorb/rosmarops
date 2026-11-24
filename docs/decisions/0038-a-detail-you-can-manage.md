# 0038. A detail you can manage, and the icons that were missing

Date: 2026-11-24

## Context

In a real report (9 agents, 64 projects, 35 problems, 135 findings) the detail showed combinations, not causes:
- inside «Cline can change the settings of other agents» there were 18 lines, one per pair;
- the last four problems had the same title, word for word;
- all 35 problems were open, one after another.

The icon rule (docs/design/iconos.md in the internal repository) also named two places of the report where an icon was missing.

## Decision

Signed by Albert on 24-11 at 09:22, with the advisor's answers to the reading (24-11 13:20). The texts are the signed ones, in the catalogs and in `docs/copy`.

- **One folded line per agent** inside the single «between agents» problem.
  - Each line has the agent's name, its number of findings and, when a saved date exists, «open since» with the oldest date of its findings. In the first review there is none and nothing is written.
  - Inside: whom it can change, the agent's score gain, and its findings. Three or more of one kind (settings, instructions, a program another agent runs) are joined in one row that holds them.
  - The kind comes from the rule and the resource type, not from the text.
  - The first agent's line is open.
- **Problems with the same title, the same rule and the same level are one.**
  - Each finding stays its own row, with its own «where» line and steps.
  - The title says how many: «{title} · {n} times», or, for secrets the report calls «A secret» with the same readers, «{n} secrets within reach of {agents} without asking you». The count is of distinct rows, so a secret split across its readers counts once.
  - Problems are joined only when their rows are told apart by their «where» line.
  - `package_known_malicious` is never joined: a malicious MCP server package has no location, so two of them could not be told apart.
  - `cred_read_and_send` and `cred_readable` are never joined, because their level differs.
- **Only the first six problems are open**: the same six the table shows. The others are folded lines under «The other {n} problems». Each line has its number, a 28×28 px family box whose text alternative is the family's name, its title on one line, its count and its level.
  - A link to a folded problem opens it. A `<details>` opens on its own in the browsers that support it, and a local script does it on a click on a `#p-N` link, on `hashchange` and on load. The script uses no network and no storage.
- **Icons:**
  - The open problem's header: the 36×36 px box of ADR 0037.
  - The «Start here» card: the box of what it proposes. That is the family of the problem it leads to (the key for a key in git), or, for the template, the family of the first problem it fixes. Agent when it leads to none.
  - «Your orchestrators»: a neutral orchestrator box in its header.
  - «What each agent reaches» does not exist in the report with the five resources, so nothing is added there. The risk strip and the templates' rows are unchanged.

## Consequences

- The findings, their number, the score, the rules, the index and every schema do not change. The problems are drawn only while the HTML page is made. The export for your AI, the findings window, the space view, the Markdown, usage statistics and the company summary count findings.
- «Doesn't affect me» and «I accept it» stay per finding. An accepted finding leaves its problem, folded or not.
- A report with one agent that changes others and fewer than seven problems looks as before, but for the icons.
