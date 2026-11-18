# Glossary

The words RosmarOps uses, as the code uses them. The map of the code is in [ARCHITECTURE.md](../ARCHITECTURE.md).

## The graph

**Agent.** A program that can act on the computer: an AI client such as Claude Code or Cursor (`ai_client`), an MCP server it loads (`mcp_server`), an editor extension (`extension`) or a chat bot (`bot`). A node of kind `agent` in the graph.

**Capability.** Something an agent can do: run shell commands, read files, write files, use the network, call an MCP tool, run in a permission mode, or operate an app. Each capability has an approval (`allow`, `ask`, `deny` or `unknown`) and a basis: set in a file (`configured`), the product's default (`product_default`), or answered by the person (`declared`).

**Credential.** A secret an agent can reach, such as an API key, a token or a private key. The graph keeps only its metadata and a salted fingerprint, never the value.

**Resource.** Something an agent can reach or change: a file, a database, an external service, another agent's configuration, instructions or executable, a git repository, an inbound channel, a schedule, or an agent's memory.

**Edge certainty.** How an edge of the graph is known. `observed`: read in a file. `inferred`: worked out from the product's documented behaviour (see knowledge file).

## Findings

**Finding.** Something the report tells you to look at, with its evidence and severity. There are two kinds in the code:

- a **v1 finding** (`finding.Finding`), from the older scanners: a new VS Code extension, a new scheduled task, a changed shell profile and similar;
- a **v2 finding** (`rules.Finding`), from a rule over the graph. This is the product's main kind.

**Rule.** The code that turns a pattern in the graph into v2 findings, such as `cred_read_and_send` or `cred_in_git`. Rules live in `internal/rules`.

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

**Declared answer.** The person's answer to a question about a setting RosmarOps cannot read on disk, for Cursor and Grok Bot. Until there is an answer, the least controlled case is assumed. See [the decision](decisions/0017-declared-answers.md).

**Knowledge file.** One of the `internal/knowledge/*.json` files: the documented behaviour of each supported product, each entry with its source and date. It says what a product does by default, not whether that is dangerous.

## Space and kit

**Space.** The person's area on `app.rosmarops.com`, where the results of all their computers can be seen together. A computer sends to it only after the person connects it.

**Kit.** The person's key, which locks their results so only they can open them. It is created on one computer and imported on the others. It is protected with a secret phrase and a recovery code.

**Results package.** One review's results, sealed with the kit key before they leave the computer. The server stores it and cannot read it.

## Inside Claude Code

**Session check.** A short check that a Claude Code hook runs when a session starts in a project. It tells what changed in that project's Claude Code setup since the last snapshot: new allow rules, a mode, hooks, MCP servers, skills or agents.

**Status line.** The line Claude Code shows below its prompt. RosmarOps can provide it with the last review's score, counts and date, and never a path or a name.

## Problems and statistics

**Problem report.** The list of problems RosmarOps ran into during reviews, kept on the computer as a closed list of codes. It is sent only when the person presses «Send», or after they chose to send the next ones without asking. See [the decision](decisions/0013-problem-reports-with-permission.md).

**Usage statistics.** A short anonymous summary of each review (`usage.v1`) that RosmarOps sends only while the person has turned it on. It is asked once, at the end of a review from a console, and turned off or on with `rosmarops usage off` or `rosmarops usage on`. See [what is sent](usage-statistics.md) and [the decision](decisions/0014-usage-statistics-asked-once.md).

## Company mode

**Company configuration.** `company.json`, which an administrator puts in a folder only administrators can change. It can only turn the company summary on or off and say where to look for projects. RosmarOps reads it only after checking who can change it. See [docs/company.md](company.md).

**Company summary.** `rosmarops-summary.json` in the reports data folder: counts and codes of one review that the company's own agent reads. Never paths, keys or names.

**Administrator fingerprint list.** `binaries.sha256`, next to the company configuration: the programs an administrator approved, by SHA-256 and version. A listed newer version is accepted without asking the person.
