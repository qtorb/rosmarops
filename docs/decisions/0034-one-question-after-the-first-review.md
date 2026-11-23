# 0034. One question after the first review, in a window

Date: 2026-11-23

## Context

After a double click, the report opened in the browser, and then came, one after the other: the window to connect the space, the «No me afecta» window, the problem list if something had failed, and last, in the console, the usage statistics question. The person was already reading the report, so the question that gives us data came where nobody was looking (BV-286, signed by Albert on 2026-11-23 at 15:55).

## Decision

**When the question comes** (Windows, macOS and Linux, with someone present: a double click, the Microsoft Store icon, or `rosmarops run` from a terminal):

- **The installation's first review** is the one with no earlier review: no `last_run` in the state and no earlier report in the reports folder (`PriorRuns`). A score history set aside because it did not validate does not make a review the first. The program reads this at the start of the review, before it writes either, in `firstReviewThisRun`. There is no new field in the state.
- **In the first review**, after the report, the problem list comes first (only if something failed), and then the usage statistics question. The connect offer and the «No me afecta» window do not appear; `run` does not print its `rosmarops findings` line.
  - If the first review finds usage statistics already answered (a state kept from before), it shows only the problem list, if there is one.
- **In the following reviews**, while there is no answer, the usage statistics question comes first, and then what came before: the connect offer, the «No me afecta» window and the problem list.
- **Once answered**, it does not come back. It is changed with `rosmarops usage on` and `rosmarops usage off`, as before.

**On Windows the question is a window**, `usageWindowScript` in `cmd/rosmarops/windowscripts.go`. It is a new, short script built from the connect window's pieces: the same form, colours, fonts, scale and flat buttons. The connect window's script is not changed.

- **The answers:**
  - «Yes, help» turns usage statistics on and sends this review's summary. The window then shows a thank-you that closes with OK, the X, or by itself after 60 seconds (a `Timer` in the script). The window then fits the thank-you: 28 px above the text, OK 16 px under it and 28 px below, with the same width and the same top-left corner (corrected on 23-11).
  - «No, thanks» turns them off. They are not asked again.
  - Closing with the X or Esc, or giving no answer in 10 minutes, records nothing. The question comes back at the next review with someone present.
- **The keys:**
  - The window has no default button and no cancel button (`AcceptButton` and `CancelButton` empty).
  - No button has the focus when it opens, and the window sees the keys first (`KeyPreview`).
  - Enter and the space bar do nothing until the person moves the focus to a button with Tab. Esc closes the window like the X.
- **When the window cannot be shown**, for example when PowerShell cannot load WinForms, the question is asked in the console.

**On macOS and Linux, and on Windows without the window**, the question stays in the console, with the same texts in this order: title, why, body, never, check, the command that turns it off, and then the question (ADR 0033).

**The texts** are the signed ones, placed literally in English and Spanish: `usage.ask.why` and `usage.ask.check` are new, `usage.ask.body` and `usage.ask.never` change, and the window's `usage.window.yes`, `usage.window.no`, `usage.window.thanks` and `usage.window.ok` are new.

**The question's text version (`usage.TextVersion`) goes from 1 to 2** (Albert's decision). Someone who answered the earlier text is asked once more, with the window.

**What does not change:**
- what is sent, `usage.v1`, the state's schema and the network invariant;
- the question is never asked in the scheduled review, without a terminal, in `session-check`, in `statusline`, or in a build that is not a published version (ADR 0032);
- no test sends anything to app.rosmarops.com.

**Tests:**
- **On every OS:** each answer, the order in the first and following reviews, and the «never» cases, through the window's seam `usageWindowHook`.
- **On a real Windows desktop:** the window's layout, keys and answers, through its self-test `ROSMAROPS_USAGE_WINDOW_SELFTEST` (`diagnostic-windows.yml`, job `usage-window`), with screenshots in Spanish and English.
- **In the journeys:** a pull request's journeys use a `-journeys` build, which never asks, so they never see the window. The release journeys of a published version do, and the self-test hook makes the window answer itself «later». The first review on Windows expects only the usage window, and the second expects it before the connect window.

## Consequences

- The first review of a new installation shows one question.
- Connecting the space and «No me afecta» start from the second review.
- Everyone who already answered is asked once more, because the text changed.
