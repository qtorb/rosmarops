# 0033. Enter is not «yes» to the usage statistics question

Date: 2026-11-23

## Context

ADR 0014 made Enter mean yes to the usage statistics question (`[Y/n]`). A
default of yes to sending data is the opposite of what RosmarOps promises
(«it never sends anything you haven't approved»). It also turned usage
statistics on by accident in a trust test that pressed Enter past the other
questions (ADR 0032).

There was also a mismatch in the report. When the question is about to be
asked at the end of the same review, the report, written just before, still
said «off until you answer. RosmarOps will ask at the end of the next scan
from a terminal».

## Decision

Signed by Albert on 23-11 at 15:30.

- **Enter alone is no answer.** `askUsage` answers:
  - `s`, `S`, `y` or `Y` turns usage statistics on;
  - `n` or `N` turns them off;
  - Enter alone records nothing: usage statistics stay off and undecided, and the question comes back at the next review with someone at a terminal;
  - anything else repeats the question once and then counts as Enter;
  - an input that ends counts as Enter.

  After Enter the console prints the existing undecided line (`usage.state.undecided`). No state field is added: «undecided» is the state having no answer, as before.
- **The prompt says so.** The text of `usage.ask.prompt` is signed:
  - English: «Send usage statistics? Type y for yes or n for no, and press Enter. Enter alone leaves it for later:»
  - Spanish: «¿Enviar estadísticas de uso? Escribe s para sí o n para no, y pulsa Intro. Intro solo lo deja para más tarde:»

  No other text changes.
- **No «next scan» when it asks now.** When this review's after-report step will ask, the report has no usage line, as the problem list does with `ProblemsWillAsk`. It asks when it is a published version, there is no answer to the current text, the review is not scheduled, and someone is at a console (`usageWillAsk`).
- **The line survives a rebuilt report.** When the review's report is rebuilt after the person's answers about settings RosmarOps cannot read, the usage line is carried over like the problem-list fields.

## Consequences

- This replaces «Pressing Enter means yes» in ADR 0014. The rest of ADR 0014 stands.
- `state.v2`, `usage.v1` and `usage.TextVersion` do not change. Answers already recorded stay as they are and are not asked again.
- A person who presses Enter is asked again at every review from a terminal, until they type an answer. A scheduled review never asks, and its report keeps the «off until you answer» line.
