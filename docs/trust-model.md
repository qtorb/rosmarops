# RosmarOps — Trust Model

## Why this document exists

RosmarOps reads the configuration of the AI agents on your computer, your repositories and parts of your operating system. From a permissions standpoint, that is the same shape malware has.

The only reason to trust RosmarOps is that **you can check what it does**. This document lists every claim we make and how you can check it yourself.

---

## What RosmarOps never does

If you ever see RosmarOps doing any of these things, treat it as a critical bug.

- **It never connects anywhere without your permission.** Without it, the program opens no connection at all: not to send data, not to check for updates, not for anything. With it, see «What it sends, only with your permission» below.
- **It never sends telemetry.** Not anonymous, not aggregated, not opt-in. There is no switch: the only thing the program can ever send is your encrypted results, to your space, with your permission.
- **It never changes your files, settings, repositories, keys or agents.** It reads. The report tells you what to change; you, or your own agent with your approval, make the change.
- **It never prints a secret in full.** Not in the report, not in its logs, not in its state.
- **It never writes outside its own folders** (see claim 3).
- **It never creates scheduled tasks.** The report gives you the exact command for a weekly review; you, or your agent with your approval, create the task.

## What it sends, only with your permission

Nothing leaves your devices unless you approve it first.

- RosmarOps sends nothing unless you approve it. If you connect this computer to your space, it sends only your results, encrypted, only to app.rosmarops.com, after each check. You can disconnect at any time. Every send is listed in rosmarops-sends.log in your reports folder.
- It connects only to app.rosmarops.com, over HTTPS on port 443. It never sends your keys, files, settings or the report in clear, and never checks for updates.

---

## Claims and how to check them

| # | Claim | How to check it |
|---|-------|-----------------|
| 1 | The source code is public, under AGPL-3.0 | The GitHub repository and its `LICENSE` file. The full commit history is visible. |
| 2 | Only one part of the program can use the network, and only with your permission | Run `go list -deps ./cmd/rosmarops` with `GOOS=windows`, with `GOOS=darwin` and with `GOOS=linux`: only `internal/spacenet` brings in `net`, a package starting with `net/`, or `crypto/tls`. `tests/trust/no_network_test.go` checks this for each system and checks the compiled binary. It also runs the program on Linux while tracing its network system calls: without permission, a single connection attempt fails the test. With permission and a local proxy that only records, the only connection the program may ask for is `app.rosmarops.com:443`, and a program built to send anywhere else fails. |
| 3 | It writes only inside `~/.rosmarops/`, where it keeps its state and the kit key file (`~/.rosmarops/kit.dpapi` on Windows, `~/.rosmarops/kit.key` on macOS and Linux), and the reports folder you configure (by default `~/.rosmarops/reports/`) | Every file write in the source goes to one of those two places. `tests/trust/no_writes_out_of_scope_test.go` runs the program in a sandbox and fails if anything is written elsewhere. |
| 4 | Secrets are never shown in full | A detected secret is shown as its first 4 characters, ` ... `, and its last 4. The graph of what each agent can reach stores only a salted fingerprint of each key, never the value. `tests/trust/no_full_secret_leak_test.go` and `no_secret_in_graph_test.go` feed known fake secrets and fail if the full value appears in any output, log or state. |
| 5 | The visual report loads nothing from outside your computer | The HTML report has its styles and script inside the file, and no external fonts, images or links. `tests/trust/no_external_in_html_test.go` checks it. |
| 6 | The export for your AI never includes secrets | The «Copy for your AI» text contains no secret values, previews or fingerprints, and replaces your user folder with `~`. If you paste it into an AI in the cloud, it leaves your computer because you paste it, and the report says so next to the button. Checked by `no_full_secret_leak_test.go`. |
| 7 | The share card identifies nothing | The card contains only the score, the band, the counts, the date and the method version. `tests/trust/no_identity_in_card_test.go` checks it. |
| 8 | Its configuration is plain data | `config.json` is JSON with no executable content, validated against `schemas/config.v1.json` every time it is read. |
| 9 | It checks its own integrity | The first time you set it up, RosmarOps records a fingerprint of its own program and of its configuration. On every later run it compares them. If the configuration changed, it shows you what changed and asks whether you made the change. If the program changed, it shows the old and new version and fingerprint and asks whether you installed it; compare the new fingerprint with the one published for that release. If you say no, nobody answers, or it is running as a scheduled review, it stops with a red report. If its state is lost, it treats that as a change, never as a fresh start. **Limit:** this catches unintended changes to the program and its configuration. It does not protect against someone who can already write with your user account: a replaced program does not check itself. For that, check the attestation and the published checksum (see «At release»). |
| 10 | The weekly review notification names nothing | When a scheduled review finishes, Windows shows a notification with an agent's name, its score and the counts: no repositories, services, paths or secrets in its text. RosmarOps registers nothing in Windows: the notification appears under Windows PowerShell's app identity, and Windows keeps it in its notification center with the path to the report, so that clicking it opens the report. If Windows does not allow notifications, RosmarOps opens the report in your default browser instead. `tests/trust/no_identity_in_notification_test.go` checks the text. |

Space key on each computer. RosmarOps keeps your space key and this computer's sending credential on each connected computer, so it can lock and send your results after each check without asking you. On Windows they are protected with DPAPI for the current user (~/.rosmarops/kit.dpapi). On macOS they are in a file only your user can read (~/.rosmarops/kit.key, 0600, in a 0700 folder), and protection on disk comes from FileVault. This protects them from other users of the computer and from copies of the disk or backups. It does not protect them from a malicious program running as you: such a program can use them the same way RosmarOps does.

---

## At release

Every release on GitHub includes:

- the Windows program, `rosmarops.exe`, and the macOS preview, `rosmarops-macos-apple-silicon` and `rosmarops-macos-intel`, with a `SHA256SUMS.txt` file;
- a **GitHub artifact attestation** that ties the program to the exact commit and the GitHub workflow that built it. Check it with `gh attestation verify rosmarops.exe --repo qtorb/rosmarops` (on macOS, `gh attestation verify rosmarops-macos-apple-silicon --repo qtorb/rosmarops`);
- a **reproducible build**: building the tagged commit from a clean git clone with Go 1.24.7 and the documented command (`GOTOOLCHAIN=go1.24.7 CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-X main.version=<tag without v>"`) produces a `rosmarops.exe` with the same SHA-256 as the one in the release; for macOS, the same with `GOOS=darwin GOARCH=arm64` or `GOOS=darwin GOARCH=amd64`.

---

## What we do not claim yet

- **The program is not signed with a code-signing certificate.** Windows SmartScreen may warn you the first time you run it. Use the attestation and the checksum above to check it instead.
- **No independent security review has been published.**
- **RosmarOps does not see everything.** It reads configuration at rest. It does not see what an agent does while it runs, settings that exist only inside an app's database or on a vendor's servers, or anything you set only in one terminal session. The report lists what it assumes and what it could not read, and never counts what it could not read as clean.

---

## Reporting a trust violation

If you see behaviour that contradicts any claim above:

1. Stop running the program.
2. Report it as described in `SECURITY.md`.

A trust violation is the most serious kind of bug in this product.
