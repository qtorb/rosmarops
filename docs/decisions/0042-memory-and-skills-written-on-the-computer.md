# 0042. Memory and skills written on the computer

Date: 2026-11-26

## Context

The score left out two writes an agent makes on the computer without asking:

- **Hermes Agent in a container.** With an isolated terminal backend (`terminal.backend` set to `docker`, `ssh`, `modal`, `daytona` or `vercel_sandbox`), RosmarOps dropped every write edge of Hermes. The report also offered «Run Hermes Agent in a container» as if it covered every write. But Hermes's `memory` and `skill_manage` tools do not go through the backend:
  - the memory folder is `get_hermes_home() / "memories"` (`tools/memory_tool.py:38-40`);
  - `skill_manage` writes with `mkdir_under_hermes_home` and `atomic_write_text` on the computer's paths (`tools/skill_manager_tool.py:421-423`, in `_guarded_write`);
  - neither file reads `terminal.backend`.

  These lines are from NousResearch/hermes-agent at commit `02afb7489e1099a8f21cd307b96b9a648dfb9243`. A Hermes with memory and a skill with a script scored 100 «contained», a score it did not have.
- **Claude Code in Manual mode.** The permissions page says Manual mode (`default`) asks before editing files, «except files such as Claude Code's own plan and auto memory files» (code.claude.com/docs/en/permissions, permission system table, read on 26-11-2026). RosmarOps gave no edge for it. It also read `autoMemoryDirectory` only in the user settings, and did not read `autoMemoryEnabled`.

## Evidence

- **The lab test, Hermes H1.** Run 45812326416 of «Hermes Agent in a container, memory and skills (H1)» in qtorb/rosmarops-lab, 26-11-2026, with Hermes at `02afb74` on Python 3.14.10, no model and no key.
  - With `terminal.backend: docker`, a `write_file` to `/tmp` stayed in the container. That is the control.
  - `memory add` wrote `memories/*.md` on the computer.
  - `skill_manage create` wrote the skill's `SKILL.md` on the computer.
  - `skill_manage write_file` wrote its `scripts/run.py` on the computer.
  - Result: yes.
- **The other isolated backends.** Only `docker` was run. The other four are included because the two tools never consult the backend; the lines are cited above.
- **`singularity` and `sprites`.** RosmarOps does not list them as isolated backends. With either of them, Hermes is already treated as running on the computer («unknown backend»), with every write counted, so this decision does not change them.

## Decision

Signed by Albert with block 200 (BV-310), as specified in sections H and C of the advisor's proposal of 26-11 (vigil `docs/research/2026-11-26-nota-que-cuenta-de-menos-propuesta.md`).

**H2 and H3. Hermes keeps the writes of its memory and skills.** With an isolated backend, Hermes gets a `writes_config_of` edge to each of the following, through its `write_files` capability scoped `isolated_backend`:
- its own `agent_memory`;
- each `SKILL.md` (`agent_instructions`) and each script (`agent_executable`) of a skill in its own `skills` folder.

`graph.HermesWritesOnHost` says which resources these are. Every other write and every read stays in the container, with no edge, as before.

**H4. The action says what the container does not cover.** The English `how` of `hermes_isolated_backend`, in the console and in the HTML, now ends «Its memory and its skills are still written on your computer.» The five Spanish texts of the action and of `unknown.hermes_backend_unknown` were missing; they are now in `es.json`, literally as the signed proposal gives them.

**H5. The action promises only what the container cuts.**
- The container action is no longer offered on the writes `graph.HermesWritesOnHost` names. A finding made only of those writes does not offer it; a finding with other ways too keeps it for those ways only.
- So the «If you fix this: {now} → {then}» line of the action keeps the memory and skill writes, and never promises more than Hermes scores in the container.

**C1. Claude Code in Manual mode writes its auto memory.** When a session runs in `default`, Claude Code gets a `writes_config_of` edge to its own `agent_memory`:
- the edge goes through its product-default Edit capability, with the mode's assumptions;
- there is no edge if an Edit rule covers the memory;
- it shows as one more location of «{agent} can rewrite its own instructions» (`own_instructions_writable`), with no new text.

The other modes keep what they had: the documentation does not say it for them.

**C2. RosmarOps reads both auto memory keys in every settings scope it reads.**
- `autoMemoryEnabled` and `autoMemoryDirectory` are read in user, project, local and managed settings.
- The memory folder of a project, under `projects/`, is matched to the project by its name: the project's path with every character that is not a letter or a digit turned into «-».
- Which value of `autoMemoryEnabled` applies:
  - a managed value first, then the project's local settings, then its project settings, then the user's;
  - a memory of no known project is off only when the managed or user settings turn it off and no project turns it on.
- With `autoMemoryEnabled` false there is no C1 edge.
- The value is kept in memory only (`AutoMemoryOff`), never in `graph.v2.json`.

## Measured

The fixtures are R2, the reference computer, with one agent. «Before» is the BV-309 measurement; «after» is pinned by `tests/trust/bv310_memory_skills_test.go`.

| Fixture | Before | After |
|---|---|---|
| Hermes, docker, memory and a skill with a script | 100 contained, B | 39 exposed, B (`own_config_writable` 15, `own_instructions_writable` 4) |
| Hermes, docker, memory only | 100 contained, B | 77 open_gaps, B (`own_instructions_writable` 4) |
| Claude Code, `default`, with memory | 74 open_gaps, B | 73 open_gaps, B (`own_instructions_writable` 4 added) |
| Claude Code, `default`, with memory, `autoMemoryEnabled: false` | 74 open_gaps, B | 74 open_gaps, B |

- The same Hermes scores hold with `ssh`, `modal`, `daytona` and `vercel_sandbox`.
- The factory scores do not change: Claude Code 31 and Hermes 32, pinned by `TestR2FactoryScores`.
- The sample computer (`testdata/sample-home`) has no memory or skill folder of Claude Code or Hermes, so its report does not change.

## Release note

- EN: "Hermes Agent in a container: RosmarOps now counts that its memory and its skills are still written on your computer. If you use Hermes this way, your score can go down without you having changed anything."
- ES: «Hermes Agent en un contenedor: RosmarOps cuenta ahora que su memoria y sus skills se siguen escribiendo en tu ordenador. Si usas Hermes así, tu nota puede bajar sin que hayas cambiado nada.»

## Consequences

- No weight, level, limit or schema changes. The new in-memory field is not serialized. `ListAlwaysOn` now takes the Claude Code settings already read.
- A person who runs Hermes in a container with memory or skills sees their score go down without changing anything. The release note says so.
- Not covered, for 0.22:
  - Codex's background memory, Cursor's Memories, and Claude Code's subagent memory and enterprise skills;
  - Hermes's `memory.write_approval` and `skills.write_approval`;
  - Claude Code's `--restricted` and `CLAUDE_CODE_DISABLE_AUTO_MEMORY`, which are not readable at rest.
