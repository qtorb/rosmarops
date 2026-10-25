# RosmarOps

**See what your AI coding agents can reach on your computer — and how one agent can change another.**

RosmarOps reads the configuration of the AI agents on your Windows computer (today Claude Code and Cursor; Cline is detected but not scored yet), your repositories and your local credentials. It tells you, with evidence:

- which keys each agent can read without asking you;
- whether it can send them out, and who decides that: you, another AI model, or nobody;
- which agents can rewrite another agent's configuration or instructions;
- which keys are saved in git.

You get a visual report with one thing to do today, the exact setting to change, and a score per agent. It reads only. It never changes anything and never connects to the network.

---

## Install and run (Windows)

1. Download `rosmarops-windows-amd64.zip` from the [latest release](https://github.com/qtorb/rosmarops/releases/latest).
2. Optional, recommended: check it (see [Check the download](#check-the-download)).
3. Extract the whole zip, then double-click `rosmarops-run.bat` (or `rosmarops-ejecutar.bat` for the report in Spanish).
4. The report opens in your browser when it finishes (up to 5 minutes).

Run it again after you change something: the report remembers the previous run and tells you what is new, what is still there and what is no longer observed.

## Check the download

Each release has a `SHA256SUMS.txt` file and a GitHub artifact attestation:

```
gh attestation verify rosmarops-windows-amd64.zip --repo qtorb/rosmarops
```

The program is not signed with a code-signing certificate yet, so Windows SmartScreen may warn you. The attestation and the checksum are how you check it is the build from this repository.

## Build it yourself

Requires Go 1.22 or later.

```
git clone https://github.com/qtorb/rosmarops.git
cd rosmarops
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o rosmarops.exe ./cmd/rosmarops
```

To reproduce the released `rosmarops.exe` exactly, build the tagged commit from a clean clone with Go 1.24.7: its SHA-256 matches the one in `SHA256SUMS.txt`.

---

## What it reads

| Agent or source | What RosmarOps reads |
|---|---|
| Claude Code | Its settings at every level (managed, user, project, local), permission rules and modes, hooks, MCP servers, `CLAUDE.md` and rules |
| Cursor | `~/.cursor` and each project's `.cursor`: permissions, sandbox, MCP servers, hooks, rules, and Claude Code hooks that Cursor also loads |
| Cline | Its MCP servers (scored in a later version) |
| Credentials | `.env` files in your repositories, MCP `env` blocks, agent settings and common credential files (npm, AWS, GitHub CLI, git, Railway, Supabase) |
| Repositories | Keys in tracked files and in git history, hooks, workflows |

## What it does not see

- What an agent does while it runs. RosmarOps reads configuration at rest.
- Settings that live only inside an app's database or on a vendor's servers (for example, the mode you pick in Cursor's settings), and anything you set only in one terminal session. The report states what it assumes in those cases.
- Whether a key in git was actually pushed to a public place.

Whatever it could not read is listed in the report and never counted as clean.

---

## Trust

RosmarOps reads broad parts of your computer, which is the same shape malware has. So every claim is something you can check:

- no networking code in the program;
- writes only inside `~/.rosmarops/` and its reports folder;
- secrets never shown in full;
- no telemetry, ever: there is no switch because there is no code that could send anything.

Each claim, and how to check it: [`docs/trust-model.md`](docs/trust-model.md).

---

## License

[AGPL-3.0](LICENSE). If you modify RosmarOps and run it as a service, you must publish your changes under the same license.

## Security issues

See [`SECURITY.md`](SECURITY.md).
