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
  `other`;
- `agents`: the AI agents found, as product ids from a fixed list (for
  example `claude-code`, `cursor`);
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

At the end of a check run from a terminal, or at the end of the window when
you double-click RosmarOps, after the problem-report step. It asks once:

> Send usage statistics? [Y/n]

Enter or `y` is yes; `n` is no. A scheduled weekly check, the Claude Code
plugin and any run without a terminal never ask and, until you answer, never
send. If the question's text changes, it asks again.

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
