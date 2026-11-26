# Glossary

The words RosmarOps uses, as the code uses them. The map of the code is in [ARCHITECTURE.md](../ARCHITECTURE.md).

## The graph

**Agent.** A program that can act on the computer: an AI client such as Claude Code or Cursor (`ai_client`), an MCP server it loads (`mcp_server`), an editor extension (`extension`) or a chat bot (`bot`). A node of kind `agent` in the graph.

**Orchestrator.** A program that starts other agents and gives them their work, such as Paperclip, Paseo, Claude Squad or Orca. It often skips their approvals and can wake them without anyone in front. In the graph it is an `agent` of type `orchestrator`, with a `launches` edge to each installed agent it supports. When the orchestrator writes in its own files the mode in which it starts each agent, the edge is observed; otherwise it is inferred from its defaults.

**«Your orchestrators».** The report's card that lists, per orchestrator, the agents it can start, whether it removes their questions, how RosmarOps knows it and each agent's own score. It explains and asks for no action, so it carries the «To understand» treatment.

**Subagent.** A helper agent of Claude Code, defined in a Markdown file in `~/.claude/agents/` or a project's `.claude/agents/`. RosmarOps reads only its header: which tools it may use, its permission mode, its hooks and the MCP servers it defines. One in the user's folder runs in every project without the folder-trust question.

**OpenCode family.** OpenCode and Kilo Code. Kilo Code is a fork of OpenCode's code and keeps its configuration model: JSON or JSONC files in the user's folder, the project and a managed folder, each overriding the one before, with a `permission` key and an `mcp` key. RosmarOps scores both with one reader (ADR 0027).

**Managed policy.** Settings an administrator sets for a product, which override the person's: a file under `/etc`, `/Library` or ProgramData, or a registry key under `SOFTWARE\Policies`. RosmarOps reads Claude Desktop's managed files only when they and their folders belong to an administrator (ADR 0030), as it does for the company configuration.

**Desktop extension.** A packaged MCP server Claude Desktop installs (`.mcpb`). RosmarOps knows an extension by the name of its settings file and never opens that file, which can hold the extension's secrets; that it is active is an assumption.

**Capability.** Something an agent can do: run shell commands, read files, write files, use the network, call an MCP tool, run in a permission mode, or operate an app. Each capability has an approval (`allow`, `ask`, `deny` or `unknown`) and a basis: set in a file (`configured`), the product's default (`product_default`), or answered by the person (`declared`).

**Team hooks.** The hooks Cursor's desktop app copies to `~/.cursor/managed/team_<id>/hooks/` from a team's dashboard. RosmarOps lists their names and treats each as one of Cursor's executables; it never opens them. They do not tell what else the team's policy sets, so `team_policy_unknown` stays.

**Credential.** A secret an agent can reach, such as an API key, a token or a private key. The graph keeps only its metadata and a salted fingerprint, never the value. A credential identified by its path is known by where it is, without its content ever being read.

**Resource.** Something an agent can reach or change: a file, a database, an external service, another agent's configuration, instructions or executable, a git repository, an inbound channel, a schedule, or an agent's memory.

**Edge certainty.** How an edge of the graph is known. `observed`: read in a file. `inferred`: worked out from the product's documented behaviour (see knowledge file).

## Findings

**Finding.** Something the report tells you to look at, with its evidence and severity. There are two kinds in the code:

- a **v1 finding** (`finding.Finding`), from the older scanners: a new VS Code extension, a new scheduled task, a changed shell profile and similar;
- a **v2 finding** (`rules.Finding`), from a rule over the graph. This is the product's main kind.

**Problem.** What the report's first screen lists: the findings one change fixes, grouped while the page is drawn. A service's credentials within reach of agents (by project when the credential names one) are one problem; one agent able to change other agents' settings is one problem; any other finding is a problem of its own. Problems exist only in the HTML: the JSON and the counts the program sends are of findings. See [the decision](decisions/0020-report-in-three-blocks.md).

**Rule.** The code that turns a pattern in the graph into v2 findings, such as `cred_read_and_send` or `cred_in_git`. Rules live in `internal/rules`.

**Unpinned launch.** An MCP server started by a launcher that fetches its code when it starts (`npx`, `bunx`, `pnpm dlx`, `uvx`, `pipx run`, `docker run`) without an exact version: its code can change every time it starts. `pkg@1.2.3`, `pkg==1.2.3` and `image@sha256:…` are pinned. The finding is `mcp_unpinned_launch` (ADR 0026).

**Project auto-approval.** Claude Code's `enableAllProjectMcpServers: true`: it approves on its own every MCP server a project brings, without asking. The finding is `mcp_project_autoapprove` (ADR 0026).

**Fixed (resolved).** A finding that was open and is gone from a place this review checked in full: each repository it was in finished its check (no time-out, inside the global limit, its root's project search finished), and its agent is still installed with its settings read. Only these count as «You fixed …» in the report and as fixed in the space. See [the decision](decisions/0025-fixed-only-where-checked.md).

**Not checked.** A finding that was open and is not in this review because its place was not checked in full: a repository that timed out, was deleted or was not reached, or an agent that is no longer installed. It stays open: it is never counted as fixed.

**No longer observed.** A finding that was open and is not in this review, where RosmarOps cannot tell whether its place was checked in full: it was recorded by a version before 0.18. It does not mean fixed.

## Scores

**Posture score.** A score out of 100 for each evaluated agent. Higher is better. It is computed from the agent's v2 findings and their weights. See [the decision](decisions/0010-posture-score.md).

**Band.** The range a posture score falls in, set by the agent's worst finding: `exposed` (a red finding, 0-49), `open_gaps` (a yellow finding, 50-79), `contained` (neither, 80-100).

**Exposure class.** A letter from A to D for each agent, from six axes: autonomy, reach, contagion, delegation, opacity and complexity. A is the least exposed. It is `provisional` while an axis has no data. See [the decision](decisions/0011-exposure-class.md).

**Factory score.** An agent's score on this computer with every agent at its product defaults, as if nobody had changed a setting.

**Template (P0-P3).** A ready-made Claude Code configuration the report offers:

- P0, read only;
- P1, local development;
- P2, sandboxed development (only where Claude Code has a sandbox: macOS and Linux);
- P3, unattended, which is a launch command.

The engine measures P0, P1 and P2 by applying them to this computer's settings. See [the decision](decisions/0012-factory-scores-and-sandbox-templates.md).

**Template in use.** A template whose every setting is already in the person's user settings: for Claude Code its allow, ask and deny entries, its single values and its sandbox keys; for Grok Build its `config.toml` keys and values; for Cursor `readBoundary: "workspace"`, which both of its templates leave, so the report says «Read only or Local development». Entries of the person's own do not change it. When several match, the strictest is the one in use. The report says so in block 3, names the next stricter one for the same use on this system (never «Sandboxed development» on Windows), and marks the card «In use».

**Declared answer.** The person's answer to a question about a setting RosmarOps cannot read on disk, for Cursor and Grok Bot. Until there is an answer, the least controlled case is assumed. See [the decision](decisions/0017-declared-answers.md).

**Knowledge file.** One of the `internal/knowledge/*.json` files: the documented behaviour of each supported product, each entry with its source and date. It says what a product does by default, not whether that is dangerous.

## Space and kit

**Space.** The person's area on `app.rosmarops.com`, where the results of all their computers can be seen together. A computer sends to it only after the person connects it.

**Kit.** The person's key, which locks their results so only they can open them. It is created on one computer and imported on the others. It is protected with a secret phrase and a recovery code.

**Results package.** One review's results, sealed with the kit key before they leave the computer. The server stores it and cannot read it.

## Inside Claude Code

**Session check.** A short check that a Claude Code hook runs when a session starts in a project. It tells what changed in that project's Claude Code setup since the last snapshot: new allow rules, a mode, hooks, MCP servers, skills or agents.

**Status line.** The line Claude Code shows below its prompt. RosmarOps can provide it with the last review's score, counts and date, and never a path or a name.

## Reports and integrity

**Reports folder.** Where each review writes its pages (`report.html` and one per language) and, in `data/`, everything else it keeps. `~/.rosmarops/reports/` on Windows and macOS, `~/RosmarOps/` on Linux, where a browser installed as a snap cannot open a hidden folder; `report_directory` in the configuration sets another. See [the decision](decisions/0019-linux-reports-findings-hint-problem-fields.md).

**Journey test.** A run of a candidate RosmarOps as a person would, through a terminal with planned answers, on a test computer: R1 the first time, R2 an update from the last published version, R3 a computer with 60 projects. They run before a release is published. Not the same R2 as the factory scores' reference computer (`TestR2FactoryScores`). See [the how-to](how-to/journeys.md).

**Integrity check.** The comparison, before every command but `init`, `baseline`, `--help` and `--version`, of the program and its configuration with the references in the state. A change is accepted only by the person in a console, by the administrator's list or by a Microsoft Store update; otherwise the command stops with a red report. See [the decision](decisions/0006-self-integrity-check.md).

**Verdict.** The first line of the report's block 1: one plain sentence for the level of the score block 1 shows (exposed, open gaps or contained). With no score there is none. See [the decision](decisions/0035-verdict-start-here-and-what-it-means.md).

**Folded problem.** A problem of the report's detail past the first six: one line with its number, family box, title, count and level, opened by a click or by a link to it. See [the decision](decisions/0038-a-detail-you-can-manage.md).

**Problem of their own.** A problem about an agent itself: it can rewrite its own instructions, change its own settings, or act without you. When three agents or more have the same one at the same level, the report shows one row with a folded line per agent. See [the decision](decisions/0039-titles-that-say-whom-they-are-about.md).

**Project not finished.** A project whose check went over the minute RosmarOps gives each one. What it would have found in it is not in the report. The report names it in «What we could not read». See [the decision](decisions/0040-what-the-report-could-not-check-in-words.md).

**Problem family.** What a problem of the report's table is about: Key, Between agents, Agent, MCP, Orchestrator, Subagent, Project or Desktop. It is shown as an icon box coloured by the problem's level, with the family's name above the title. A rule that fits no family has neither. See [the decision](decisions/0037-problem-families-and-single-row.md).

**Start here.** The card under the score in block 1 with one recommended action, the first case that holds: a template that fixes urgent findings, when none is in use; a key saved in git whose service is known; or problem 1. No problems, no card. See [the decision](decisions/0035-verdict-start-here-and-what-it-means.md).

**What it means.** The sentence a finding shows in the HTML report under its title, where the others show their impact. The eleven rules whose title was their only sentence have one now, with variants for the cases the main sentence would overstate. See [the decision](decisions/0035-verdict-start-here-and-what-it-means.md).

## Problems and statistics

**First review.** The installation's first review: no review ran before, so the state has no `last_run` and the reports folder has no earlier report. It shows only the usage statistics question and, if something failed, the problem list ([ADR 0034](decisions/0034-one-question-after-the-first-review.md)).

**Problem report.** The list of problems RosmarOps ran into during reviews, kept on the computer as a closed list of codes. It is sent only when the person presses «Send», or after they chose to send the next ones without asking. See [the decision](decisions/0013-problem-reports-with-permission.md).

**Usage statistics.** A short anonymous summary of each review (`usage.v1`) that RosmarOps sends only while the person has turned it on. It is asked after the report until the person answers: on Windows in a window, elsewhere in the console with y or n (Enter alone leaves it for later). In the installation's first review it is the only question besides the problem list, and in later reviews it comes first while unanswered ([ADR 0034](decisions/0034-one-question-after-the-first-review.md)). It is turned off or on with `rosmarops usage off` or `rosmarops usage on`. Only a published version (exactly `X.Y.Z`) asks or sends; a development or test build never does ([ADR 0032](decisions/0032-only-published-versions-send-usage.md)). See [what is sent](usage-statistics.md) and [the decision](decisions/0014-usage-statistics-asked-once.md). The team's own installations are marked with `rosmarops usage team on`, so their summaries carry the channel `internal` and are counted apart ([how](how-to/mark-a-team-installation.md), [ADR 0024](decisions/0024-team-installations-counted-apart.md)).

## Company mode

**Company configuration.** `company.json`, which an administrator puts in a folder only administrators can change. It can only turn the company summary on or off and say where to look for projects. RosmarOps reads it only after checking who can change it. See [docs/company.md](company.md).

**Company summary.** `rosmarops-summary.json` in the reports data folder: counts and codes of one review that the company's own agent reads. Never paths, keys or names.

**Administrator fingerprint list.** `binaries.sha256`, next to the company configuration: the programs an administrator approved, by SHA-256 and version. A listed newer version is accepted without asking the person.
