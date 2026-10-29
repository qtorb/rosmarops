# RosmarOps

**See what your AI coding agents can reach on your computer — and how one agent can change another.**

RosmarOps reads the configuration of the AI agents on your Windows computer (today Claude Code and Cursor; Cline, GitHub Copilot, Continue, Codex, Antigravity, Hermes Agent and Claude Desktop are detected but not scored yet), your repositories and your local credentials. It tells you, with evidence:

- which keys each agent can read without asking you;
- whether it can send them out, and who decides that: you, another AI model, or nobody;
- which agents can rewrite another agent's configuration or instructions;
- which keys are saved in git.

You get a visual report with a score per agent, the first thing to do with the exact setting to change, and a table of how each agent works: who decides what it does (nobody, another AI model, or you) and how far it reaches. It reads only. It never changes anything and never connects to the network.

How each agent behaves out of the box, version by version, is tracked in the [RosmarOps Observatory](https://rosmarops.com/observatory/). How findings and scores are worked out: [Methodology](https://rosmarops.com/methodology/).

---

## Install and run (Windows)

1. Download `rosmarops.exe` from the [latest release](https://github.com/qtorb/rosmarops/releases/latest).
2. Optional, recommended: check it (see [Check the download](#check-the-download)).
3. Double-click `rosmarops.exe`. It finds your projects on its own; if it finds none, it asks you to choose the folder where you keep them.
4. The report opens in your browser when it finishes (up to 5 minutes), in your Windows language, with a link to the other language.

Run it again after you change something: the report remembers the previous run and tells you which findings are new, still there or no longer observed, which agents or MCP servers are new, and which MCP servers now point somewhere else.

To run it every week without thinking about it, the report gives you the exact command to schedule a review every Friday at 09:00 (or your agent can run it for you). RosmarOps does not create the task itself. When the scheduled review finishes, Windows shows a notification; if notifications are turned off, the report opens in your browser.

## Install and run (macOS, preview)

1. Download `rosmarops-darwin-arm64` (Apple silicon) or `rosmarops-darwin-amd64` (Intel) from the [latest release](https://github.com/qtorb/rosmarops/releases/latest).
2. Check it (see [Check the download](#check-the-download)).
3. In Terminal, in the folder where you saved it:

       chmod +x rosmarops-darwin-arm64
       xattr -d com.apple.quarantine rosmarops-darwin-arm64
       ./rosmarops-darwin-arm64

4. The report opens in your browser when it finishes.

The macOS version is a preview. The report lists what it does not check yet on macOS; none of that counts as clean. It is not signed or notarised by Apple: the `xattr` command removes the warning macOS shows for downloaded programs.

## Check the download

Each release has a `SHA256SUMS.txt` file and a GitHub artifact attestation:

```
gh attestation verify rosmarops.exe --repo qtorb/rosmarops
```

```
# macOS
shasum -a 256 -c --ignore-missing SHA256SUMS.txt
gh attestation verify rosmarops-darwin-arm64 --repo qtorb/rosmarops
```

The program is not signed with a code-signing certificate yet, so Windows SmartScreen may warn you the first time you open it. Check the attestation and the checksum first: they are how you know it is the build from this repository. Then choose «More info» and «Run anyway».

## Build it yourself

Requires Go 1.22 or later.

```
git clone https://github.com/qtorb/rosmarops.git
cd rosmarops
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o rosmarops.exe ./cmd/rosmarops
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -o rosmarops-darwin-arm64 ./cmd/rosmarops
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -o rosmarops-darwin-amd64 ./cmd/rosmarops
```

To reproduce the released `rosmarops.exe` exactly, check out the release tag in a clean git clone and build it with Go 1.24.7, passing the version without the «v» (for v0.1.0: `GOTOOLCHAIN=go1.24.7 CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-X main.version=0.1.0" -o rosmarops.exe ./cmd/rosmarops`). Its SHA-256 matches the one in `SHA256SUMS.txt`.

---

## What it reads

| Agent or source | What RosmarOps reads |
|---|---|
| Claude Code | Its settings at every level (managed, user, project, local), permission rules and modes, hooks, MCP servers, `CLAUDE.md` and rules, and the environment settings that choose where it sends your requests |
| Cursor | `~/.cursor` and each project's `.cursor`: permissions, sandbox, MCP servers, hooks, rules, and Claude Code hooks that Cursor also loads |
| Cline | Its MCP servers (scored in a later version) |
| VS Code extensions | Which AI agent extensions are installed (Cline, GitHub Copilot, Continue), to list them as detected; any other new extension is listed in the technical report |
| Codex | Detected, not scored yet: the keys in its `auth.json`, and `AGENTS.md` as its instructions |
| Antigravity | Detected, not scored yet: its MCP servers (`~/.gemini/config/mcp_config.json` and each project's `.agents/mcp_config.json`) and its instruction files (`GEMINI.md`, `AGENTS.md`, `.agents/rules`) |
| Hermes Agent | Detected, not scored yet: the keys in its `.env` files, and its instruction files |
| Claude Desktop | Detected, not scored yet: its MCP servers in `claude_desktop_config.json` |
| Credentials | `.env` files in your repositories, MCP `env` and `headers` blocks, agent settings and common credential files (npm, AWS, GitHub CLI, git, Railway, Supabase) |
| Repositories | Keys in tracked files and in git history, hooks, workflows |
| Windows | The list of scheduled tasks and Windows Defender's exclusions, to tell you when a new one appears, and whether Windows allows notifications |

## What it does not see

- What an agent does while it runs. RosmarOps reads configuration at rest.
- Settings that live only inside an app's database or on a vendor's servers (for example, the mode you pick in Cursor's settings), and anything you set only in one terminal session. The report states what it assumes in those cases.
- Whether a key in git was actually pushed to a public place.
- Whether a key you declared rotated was really revoked at the provider. RosmarOps records your statement and says so.

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
