# 0039. Titles that say whom they are about

Date: 2026-11-24

## Context

With the manageable detail (ADR 0038), the report could be read problem by problem. Five titles still did not say whom or what they were about, or said too much:

- **Desktop, every app.** «Can use * on your desktop without asking»: no agent, and «*» is the raw value for «every app».
- **Desktop, many apps.** Fifteen app ids in one title, which took seventeen lines at 390 px.
- **Acting without you.** «Can act without you: …» named no agent.
- **The same problem of their own.** Six nearly identical lines, one per agent: «{agent} can rewrite its own instructions».
- **Keys in git, in Spanish.** The Spanish title joined the key's name to a feminine past participle, so «Un secreto» did not agree.

## Decision

Signed by Albert with blocks 153 and 161 (24-11), with the advisor's answers to the reading. The texts are the signed ones, in the catalogs and in `docs/copy`.

**Desktop findings say the agent and sum up the list.**
- With «*», alone or with a terminal: «{agent} can use any app on your desktop…».
- With one to three named apps: the apps by name.
- With four or more: «{agent} can use {n} apps…». This holds for urgent findings and for findings to review alike.

**The «where» line of a desktop finding names every app, with no «and n more».**
- It writes «*» as «all apps».
- The technical details keep the raw «*».
- The HTML report and the Markdown show this line.
- The export for your AI keeps every place of a finding in its data block, as before.
- Known limit: past its 48 KB cut, the export for your AI gives each finding only its title, as for every other finding, so a desktop finding there says how many apps and not which.
- The findings window and the space view have no «where» line, so they show the title with the number.

**«Acts without you» says the agent.**

**One row per problem of their own shared by three agents or more.**
- It applies to rewriting their own instructions, changing their own settings, and acting without you.
- Each is one row, «{n} of your agents can …: {agents}», with one folded line per agent, as for the agents that change others (ADR 0038). The lines have no «can change» line.
- Findings of different levels are in different rows.
- Changing a program the agent runs keeps its own title and stays apart.
- With one or two agents, each agent keeps its own problem.

**Keys in git, in Spanish.** The title becomes «{cred_name}: en git» and «{cred_name}: en tu historial de git». The English title does not change.

## Consequences

**What does not change.**
- The findings, their ids, their count, the score, the rules and every schema stay the same.
- «Doesn't affect me», «I accept it» and «open since» are kept per finding id, and a title is not part of one.

**What leaves the computer.** The titles travel already written inside the encrypted results package for the space. Only the text of fields that already travel changes. No field, limit or format changes, and nothing is added. The problem list, usage statistics and the company summary carry no titles.

**Problem numbers.** A single row adds up its agents' findings, so it can move up the table. The problems after it then change number and anchor, as with the «between agents» row.

**Joining by title.** Two agents' «acts without you» findings, or two agents' «every app» findings, used to share a title. The detail of ADR 0038 joined them into one problem. Now each title names its agent, so they are one problem per agent.
