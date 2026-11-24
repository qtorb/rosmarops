# Usage statistics

RosmarOps can send a short summary of each check to app.rosmarops.com, so we
can see which systems, agents and findings matter most. It is off until you
say yes, and you can turn it off at any time.

## What is sent

After each check, while usage statistics are on, one summary in the format
`usage.v1`:

- `schema`: always `usage.v1`;
- `install_id`: a random id made when you turn the statistics on. It is not
  derived from your computer, and turning them off and on again makes a new
  one;
- `day`: the date of the check (UTC), never the time;
- `version`: the RosmarOps version;
- `os` and `arch`: `windows`, `darwin` or `linux`, and `amd64` or `arm64`;
- `channel`: how you installed it: `store` (Microsoft Store), `homebrew`, or
  `other`; or `internal` on an installation the RosmarOps team marked as its
  own, so its checks are counted apart from people's;
- `agents`: the AI agents found, evaluated or only detected, as product ids
  from a fixed list (for example `claude-code`, `cursor`, `claude-desktop`);
- `score` and `class`: the computer's score (0-100) and class (A-D), or
  nothing when there is none;
- `status`: `green`, `yellow` or `red`;
- `findings`: how many findings of each kind, by code (for example
  `cred_readable: 2`);
- `space_connected`: whether this computer is connected to your space;
- `plugin_installed`: whether the RosmarOps plugin is turned on in Claude
  Code.

## What is never sent

Paths, file, project or computer names, user names, keys, the content of your
settings, the report, or anything you typed. Every field is a fixed value, a
number or an id from a fixed list: there is no free text. If a value does not
fit its list, nothing is sent.

## When it asks

After the report of a check you run yourself: a double click, the Microsoft
Store icon, or `rosmarops run` from a terminal.

- In the first check on a computer, it is the only question, after the
  problem list if something failed. The offer to connect your space and the
  «No me afecta» window start from the next check.
- In the following checks, while you have not answered, it is the first
  question after the report.

On Windows it is a window. It says, briefly, what is sent and what never is,
and has two buttons:

- **Yes, help** turns usage statistics on and sends this check's summary. The
  window then says thank you and closes with OK, or by itself after a minute.
- **No, thanks** turns them off. You are not asked again.

Closing the window with the X or Esc, or leaving it unanswered for ten
minutes, records nothing: you are asked again at the next check. Enter does
nothing: the window has no default button.

On macOS and Linux, or if the window cannot be shown, it asks in the
terminal, with the same texts and then:

> Send usage statistics? Type y for yes or n for no, and press Enter. Enter alone leaves it for later:

`y` (or `s`) is yes; `n` is no. Enter alone records nothing: usage statistics
stay off and you are asked again at the next check. Anything else asks once
more and then counts as Enter.

When it asks at the end of a check, that check's report has no usage
statistics line, since you are answering right then
([ADR 0033](decisions/0033-enter-is-not-yes-for-usage.md)). A scheduled weekly
check, the Claude Code plugin and any run without a terminal never ask and,
until you answer, never send.

If the question's text changes, it asks again. It changed in 0.20, so if you
answered before, you are asked once more, with the window
([ADR 0034](decisions/0034-one-question-after-the-first-review.md)).

Only a published version of RosmarOps (a version that is exactly three
numbers, such as `0.19.0`) asks or sends. A copy built from the source code
without a version, which says `0.1.0-dev`, and the test builds never ask and
never send, even if usage statistics are on: test runs are never counted as
someone's checks ([ADR 0032](decisions/0032-only-published-versions-send-usage.md)).

## How to turn it off or on

```
rosmarops usage off
rosmarops usage on
rosmarops usage
```

The last one prints whether usage statistics are on. The report also says it,
in one line at the end.

## Where to see what was sent

The last summary sent (or tried) is kept as it was sent in
`rosmarops-usage-last.json`, in the data subfolder of your reports folder (on
Windows, `%USERPROFILE%\.rosmarops\reports\data\rosmarops-usage-last.json`).
Every send is listed in `rosmarops-sends.log`, next to it, as
`rosmarops-usage.v1`. A send that fails is not retried; the report and the
check are the same with or without it.

## What the server keeps

The summaries are kept for 400 days and are not linked to any account or
space.

## Where the code is

- `internal/usage`: `Build`, the only function that decides what is sent, and
  its check against the `usage.v1` schema (`schemas/usage.v1.schema.json`, the
  same file as the server's);
- `internal/spacenet`, `SendUsage`: the one send, to app.rosmarops.com, only
  when the state records that you turned usage statistics on;
- `cmd/rosmarops/cmd_usage.go`: the question, the `rosmarops usage` command
  and the files above.
