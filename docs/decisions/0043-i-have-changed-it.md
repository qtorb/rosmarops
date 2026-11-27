# 0043. «I have changed it»: RosmarOps records a key changed from the findings window

Date: 2026-11-27

## Context

The report's step for a key saved in git told the person to record the change themselves. The person had two ways to do it:
- copy instructions for their agent;
- add `{"date": …, "id": "cred_…", "note": ""}` by hand to `allow_list.rotated_credentials` in `config.json`.

Owners with a real report did not know what to do with that entry (reading BV-313, vigil `docs/research/2026-11-27-bv-313-lectura.md`). The reading found three more problems:
- The Markdown report showed the rotation's step in English in a Spanish report: `action.rotate_credential.how` and `.label` did not exist in `es.json`.
- The entry's date was the day of the review that wrote the report, not the day of the change.
- Invariant 2 let RosmarOps rewrite `config.json` only to change which findings are dismissed or accepted, so RosmarOps could not write the entry itself.

## Decision

Signed by Albert on 27-11 at 10:18, the change of invariant 2 included (block 223, BV-314). The texts are literal, from the advisor's specification of 27-11 (vigil `docs/research/2026-11-27-clave-en-git-ya-la-he-cambiado.md`).

1. **Invariant 2.** Its third point now reads: «`~/.rosmarops/config.json`: created once at install if it does not exist, and rewritten only when the person changes which findings are dismissed or accepted, or marks a key saved in git as changed.» Everything else stays the same:
   - the same file;
   - the same window and console;
   - the same refusal to write any configuration outside `~/.rosmarops/config.json`;
   - the same atomic write;
   - the same re-pointing of the configuration's integrity reference.
2. **A third choice in the findings window and the console.**
   - **Which findings.** A finding of a key saved in git (`cred_in_git`) offers «I have changed it», then «I accept it», then «Dismiss». The three exclude each other. The new button is the same control as the other two.
   - **Saving.** On save, RosmarOps writes `{"date": <the day it is marked>, "id": <cred_…>, "note": ""}` into `allow_list.rotated_credentials`. It takes that finding out of `dismissed` and `accepted`, keeps every other key of the file, and keeps the entries of keys the window does not show, with their date and note, whoever wrote them.
   - **The next review** does not ask about the change, and the key gives no `cred_in_git`. The findings of agents that can read the same key (`cred_readable`, `cred_read_and_send`) keep showing.
   - **The console.** «3c» marks or unmarks the key of finding 3. On any other finding, «3c» changes nothing and the console says so.
   - **The tally** adds the keys changed after the same middle dot, when the window has a key saved in git.
3. **The report's step points to the window.**
   - Step 2 says to run `rosmarops findings` and mark the finding.
   - The copy button for the agent and «I'd rather do it by hand» move into a folded «Other ways to record it», with a note that the next review then asks.
   - The table's link says that it leads to the steps.
   - The agent's text, used by the HTML, the export for your AI and the space, now asks for today's date.
4. **The Markdown report** has the rotation's label and step in Spanish.

What does not change:
- when the findings window opens by itself;
- the rules that decide which findings there are;
- no trust test is weakened;
- the HTML still has no external URL.

## Not done: the group «Keys you have changed»

The specification asks the window to show a key already recorded as changed in its own group, `findings.group.rotated`, so that unmarking it brings the finding back. This needs data that the report does not carry today:
- a credential in `allow_list.rotated_credentials` gives no `cred_in_git` (`internal/rules/credingit.go`);
- `rules.Silenced.Rotated` is not written to `report.v2.json` (`json:"-"`);
- so the window, which reads the last review's `report.v2.json`, has no title, service or project for that key.

Showing it would need either the rules to build those findings into a list, or a new field in `report.v2.json` and its schema. The block says to stop and ask before either. So:
- the window and the console draw the group from cards in it, and both choices can be undone before saving;
- nothing puts a card in that group yet.

## Consequences

- RosmarOps now writes a third list into `~/.rosmarops/config.json`, only from `rosmarops findings` and the window after a review. `tests/trust/no_writes_out_of_scope_test.go` passes unchanged.
- Marking a key as changed is a statement: RosmarOps cannot check it. The window, the report and the release note say so.
- Recording a key on another day needs `rosmarops findings` in a terminal: the window opens by itself only after a review with new findings.
