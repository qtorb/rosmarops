# 0040. What the report could not check, in words

Date: 2026-11-26

## Context

Albert's own 0.20.0 report (Windows, 9 agents, 64 projects) showed three things a person could not act on:
- The findings line said «at least 126 findings · 3 projects not finished», but the page said nowhere which projects those were, why they did not finish, or what to do.
- «What we could not read» showed internal codes, such as `…\.agents\skills (source_unsupported)`.
- A problem with a single row repeated its title on its only row.

The problem list also promised «we can see where it stopped» for a review that did not finish, but what is sent says only that it happened.

## Decision

Signed by Albert with block 196 (BV-307), as specified in R1, R2, R3 and R5 of the advisor's proposal of 26-11. The texts are the signed ones, in the catalogs.

**The projects that did not finish are named.**
- «What we could not read» starts with a paragraph that says how many projects went over the minute RosmarOps gives each one.
- Then it lists their paths, and ends with a line that says when this happens and what to do.
- The «{k} projects not finished» of the findings line is a link to that paragraph.
- This shows whether or not the computer is connected to a space.

**Each source not read says why in words.**
- It reads `path — why`, with one text for each of the five reason codes, in the report and in the space view.
- Unsupported agents, the scopes of review errors and the graph file name are unchanged.

**A problem whose only row has the problem's title does not repeat it.**
- The row's content is drawn open under the header.
- The row's verdict, such as «open since», moves to the header, on the right.

**The window about a review that did not finish** now promises only what is sent: that it happened, and on which version and system.

## Consequences

- No finding, limit, schema or grouping rule changes.
- The space view's coverage lines change only in their text: no field changes.
- A finding's anchor (its id) stays on its row's content, so links to it still work.
