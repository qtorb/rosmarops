# RosmarOps on a computer your company manages

A company can install RosmarOps on its computers with a device manager such as Fleet, and read a short summary of each review. This page says what the company configures, what it can read, and what it cannot do. It is written for the administrator and for the person who uses the computer.

Company mode works on Windows and Linux. macOS has no company mode.

## What the company sets up

An administrator installs two files in a folder only administrators can change:

| | Windows | Linux |
|---|---|---|
| Company configuration | `<ProgramData>\RosmarOps\company.json` | `/etc/rosmarops/company.json` |
| Administrator fingerprint list | `<ProgramData>\RosmarOps\binaries.sha256` | `/etc/rosmarops/binaries.sha256` |
| The program | `%ProgramFiles%\RosmarOps\rosmarops.exe` | `/usr/local/bin/rosmarops` |

On Windows, RosmarOps finds ProgramData through the Windows API, never through the `%ProgramData%` variable, which anyone can change. On Linux the folder is fixed.

`company.json` follows [schemas/company.v1.json](../schemas/company.v1.json). For example:

```json
{"schema_version":1,"summary":{"enabled":true},"first_run":{"repo_roots":["~/source","~/projects"]}}
```

`binaries.sha256` holds one accepted program per line: its SHA-256 in lowercase hex, two spaces and its version. Lines that start with `#` are ignored. A single malformed line makes RosmarOps ignore the whole list.

## What the company configuration can and cannot do

It can only:

- turn the company summary on or off (`summary.enabled`). Without a company configuration it is off;
- say where to look for projects (`first_run.repo_roots`), only while the person has no RosmarOps configuration of their own. Each folder is relative to the person's own folder (`~/…`) and cannot leave it.

It cannot:

- hide, dismiss or accept findings;
- add secret patterns, change limits or the language;
- move the report: with a company configuration the report is always in the usual folder, and the report says so if the person had chosen another;
- connect the computer to a space, send anything, or touch the person's key.

A company configuration with any other setting is not accepted: RosmarOps ignores it, the review goes on with the person's own configuration, and the report says so.

When the person has their own configuration (`~/.rosmarops/config.json`), it stays the configuration in use, and only `summary.enabled` is taken from the company file. When the person has none, the review uses the company configuration in memory and never creates a `config.json`.

## Who can change these files

RosmarOps reads `company.json` and `binaries.sha256` only after it checks, before opening them, that they and their folder can be changed by administrators alone:

- **Windows:** owned by Administrators, SYSTEM or TrustedInstaller, and no entry gives any other identity (CREATOR OWNER included) the right to write, delete or change permissions. Neither is a link.
- **Linux:** the file and every folder up to `/` belong to root, and none is writable by group or others. The file is not a link.

A file that fails the check does not exist for RosmarOps. The report says the company configuration was ignored, and the review is never green.

RosmarOps only reads these files. It never writes, creates or changes the permissions of them or of their folder.

## The administrator fingerprint list

When the program changes, RosmarOps normally asks the person to confirm the change, and a scheduled review stops with a red report. On a managed computer the list is a third way to accept it: the new program is accepted without asking when its SHA-256 is in the list with its own version. Neither the list nor the Microsoft Store ever accepts an older version, or the same version with another fingerprint.

The first review on a computer with the company configuration, and no state yet, is accepted as a first run only if the program passes the list or the Store check. Deleting RosmarOps's state is never enough to accept any program.

`rosmarops session-check` and `rosmarops statusline` never accept a change through the list, because they never write. They say RosmarOps changed, and the next review accepts it.

## What the company reads

With `summary.enabled` on, each review writes `rosmarops-summary.json` in the `data` folder of the reports folder (`~/.rosmarops/reports/data/` on Windows and macOS, `~/RosmarOps/data/` on Linux), one JSON line that follows [schemas/summary.v1.json](../schemas/summary.v1.json). The company's agent reads that file. It holds:

- the program's version and the rules' version;
- whether the review finished, stopped for integrity or failed;
- the status, and the number of red and yellow findings;
- the computer's score and band;
- the findings by rule, as counts;
- the agents used, grouped by type and product, with their counts and worst band;
- the checks that do not apply on the computer;
- which configuration was used and how the program was accepted.

It never holds paths, file contents, keys or their previews, names of projects, repositories, MCP servers, extensions or packages, the user's or the computer's name, or any identifier derived from a path. The «Your company» section of the person's report shows the summary exactly as the company reads it.

With the summary off, or without a trusted company configuration, the review writes no summary and removes a previous one, so the company never reads an old result.

Writing the summary uses no network. RosmarOps never sends it anywhere: the company's own agent reads the file.

## Installing with Fleet

The install and uninstall scripts are in [docs/company/](company/README.md). They are drafts until the pilot proves them on real computers. Set the version in the script to the version you install, because it goes into `binaries.sha256`.

The scripts run as SYSTEM or root and leave the program, `company.json` with the summary on, `binaries.sha256` with the installed program's fingerprint, and a weekly review for every user: a scheduled task for the Users group on Windows, a global systemd user timer on Linux. RosmarOps recognises either as the computer's weekly review.

Do not run `rosmarops run` directly from a Fleet script: it would review the SYSTEM or root account and leave the report there. Start the person's own review instead:

- Windows: `Start-ScheduledTask -TaskName 'RosmarOps weekly review (company)'` (it runs only while the person is signed in).
- Linux, for each person with RosmarOps set up:

```sh
#!/bin/sh
for cfg in /home/*/.rosmarops/config.json; do
  [ -f "$cfg" ] || continue
  u=$(stat -c %U "$cfg")
  runuser -l "$u" -c 'setsid rosmarops run --unattended >/dev/null 2>&1 < /dev/null &'
done
echo started
```

## Fleet policies

These policies use the `parse_json` table of fleetd. A policy passes when the query returns a row. They are untested until the pilot. On Linux, replace the path with `/home/%/RosmarOps/data/rosmarops-summary.json`.

No red findings, a finished review, and a summary less than 8 days old:

```sql
SELECT 1 WHERE
  EXISTS (SELECT 1 FROM file WHERE path LIKE 'C:\Users\%\.rosmarops\reports\data\rosmarops-summary.json')
  AND NOT EXISTS (
    SELECT 1 FROM file f JOIN parse_json p ON p.path = f.path
    WHERE f.path LIKE 'C:\Users\%\.rosmarops\reports\data\rosmarops-summary.json'
      AND (   (p.key = 'red_total' AND p.value <> '0')
           OR (p.key = 'run' AND p.value <> 'complete')
           OR (p.key = 'generated_unix' AND CAST(p.value AS INTEGER) < (SELECT unix_time FROM time) - 8*86400)));
```

- **Fewer than 5 yellow findings:** replace the `red_total` line with `(p.key = 'yellow_total' AND CAST(p.value AS INTEGER) >= 5)`.
- **A computer score of 70 or more:** replace it with `(p.key = 'machine_index' AND (p.value = '' OR p.value = 'null' OR CAST(p.value AS INTEGER) < 70))`. A `null` score fails.
- **Several conditions:** put the lines in the same `NOT EXISTS (… AND (… OR … OR …))`.

To see what `parse_json` returns on one computer:

```sql
SELECT p.key, p.value FROM file f JOIN parse_json p ON p.path = f.path
WHERE f.path LIKE 'C:\Users\%\.rosmarops\reports\data\rosmarops-summary.json';
```

What a company counts as compliant is its own decision. The summary has no «compliant» field.
