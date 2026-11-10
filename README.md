# RosmarOps

**See what your AI agents can do on your computer without asking you, and how one agent can change another.**

Free and open source (AGPL-3.0). It runs on your computer and only reads. Nothing leaves your computer unless you approve it first. Each agent it evaluates gets an Agent Posture Score out of 100.

[![The RosmarOps report: a score per agent, the first thing to change and how much the score improves if you do](https://rosmarops.com/assets/sample/sample-desktop.en.png)](https://rosmarops.com/sample-report/)

*The [sample report](https://rosmarops.com/sample-report/), made with example data.*

RosmarOps reads the configuration of the AI agents on your Windows, Mac or Linux computer, your repositories and your local credentials. It scores nine agents today: Claude Code, Cursor, Cline, GitHub Copilot, Codex, Grok Build, Antigravity CLI, Gemini CLI and Hermes Agent. It also detects Antigravity (the app), GitHub Copilot for JetBrains, Cline CLI, Claude Desktop and n8n, without scoring them yet.

It tells you, with evidence:

- which keys each agent can read without asking you;
- whether it can send them out, and who decides that: you, another AI model, or nobody;
- which agents can rewrite another agent's configuration or instructions;
- which keys are saved in git.

You get a visual report with a score per agent, the first thing to do with the exact setting to change, and a table of how each agent works: who decides what it does (nobody, another AI model, or you) and how far it reaches. It reads only and never changes anything.

How each agent behaves out of the box, version by version, is tracked in the [RosmarOps Observatory](https://rosmarops.com/observatory/). How findings and scores are worked out: [Methodology](https://rosmarops.com/methodology/).

---

## Install and run (Windows)

1. Download `rosmarops.exe` from the [latest release](https://github.com/qtorb/rosmarops/releases/latest).
2. Optional, recommended: check it (see [Check the download](#check-the-download)).
3. Double-click `rosmarops.exe`. It finds your projects on its own; if it finds none, it asks you to choose the folder where you keep them.
4. The report opens in your browser when it finishes (usually within 5 minutes; with many projects, up to 20), in your Windows language, with links to the other languages.

Run it again after you change something: the report remembers the previous run and tells you which findings are new, still there or no longer observed, which agents or MCP servers are new, and which MCP servers now point somewhere else.

To run it every week without thinking about it, the report gives you the exact command to schedule a review every Friday at 09:00 (or your agent can run it for you). RosmarOps does not create the task itself. When the scheduled review finishes, Windows shows a notification; if notifications are turned off, the report opens in your browser.

## Install and run (macOS, preview)

1. Download `rosmarops-macos-apple-silicon` (Mac with Apple silicon: M1, M2, M3, M4) or `rosmarops-macos-intel` (Mac with Intel) from the [latest release](https://github.com/qtorb/rosmarops/releases/latest). Not sure? Apple menu > About This Mac: «Chip» means Apple silicon, «Processor» means Intel.
2. Check it (see [Check the download](#check-the-download)).
3. In Terminal, in the folder where you saved it:

       chmod +x rosmarops-macos-apple-silicon
       xattr -d com.apple.quarantine rosmarops-macos-apple-silicon
       ./rosmarops-macos-apple-silicon

4. The report opens in your browser when it finishes.

The macOS version is a preview. The report lists what it does not check yet on macOS; none of that counts as clean. It is not signed or notarised by Apple: the `xattr` command removes the warning macOS shows for downloaded programs.

## Install and run (Linux)

1. Download `rosmarops-linux-amd64` (x86-64, also for WSL) or `rosmarops-linux-arm64` (ARM, for example a Raspberry Pi) from the [latest release](https://github.com/qtorb/rosmarops/releases/latest).
2. Check it (see [Check the download](#check-the-download)).
3. In a terminal, in the folder where you saved it:

       chmod +x rosmarops-linux-amd64
       ./rosmarops-linux-amd64

4. The report opens in your browser when it finishes.

Claude Desktop is not looked for on Linux, because it has no Linux version.

## Check the download

Each release has a `SHA256SUMS.txt` file and a GitHub artifact attestation:

```
gh attestation verify rosmarops.exe --repo qtorb/rosmarops
```

```
# macOS and Linux
shasum -a 256 -c --ignore-missing SHA256SUMS.txt
gh attestation verify rosmarops-macos-apple-silicon --repo qtorb/rosmarops
```

On Linux, `sha256sum -c --ignore-missing SHA256SUMS.txt` works too.

The program is not signed with a code-signing certificate yet, so Windows SmartScreen may warn you the first time you open it. Check the attestation and the checksum first: they are how you know it is the build from this repository. Then choose «More info» and «Run anyway».

## Build it yourself

Requires Go 1.22 or later.

```
git clone https://github.com/qtorb/rosmarops.git
cd rosmarops
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o rosmarops.exe ./cmd/rosmarops
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -o rosmarops-macos-apple-silicon ./cmd/rosmarops
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -o rosmarops-macos-intel ./cmd/rosmarops
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o rosmarops-linux-amd64 ./cmd/rosmarops
```

To reproduce the released `rosmarops.exe` exactly, check out the release tag in a clean git clone and, with Go 1.24.7, first write the Windows icon and version, then build, passing the version without the «v» (for v0.7.3: `GOTOOLCHAIN=go1.24.7 go run ./tools/winres -version 0.7.3`, then `GOTOOLCHAIN=go1.24.7 CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-X main.version=0.7.3" -o rosmarops.exe ./cmd/rosmarops`). Releases before v0.7.3 need only the second command. Its SHA-256 matches the one in `SHA256SUMS.txt`.

---

## What it reads

`~` is your home folder; `<repo>` is each of your projects.

| Agent or source | What RosmarOps reads |
|---|---|
| Claude Code | `~/.claude/settings.json`, `~/.claude.json`, each project's `.claude/settings.json`, `.claude/settings.local.json` and `.mcp.json`, the managed settings, `CLAUDE.md`, `CLAUDE.local.md` and `.claude/rules`, and its skills |
| Cursor | `~/.cursor` and each project's `.cursor`: permissions, sandbox, CLI settings, hooks, MCP servers, rules and skills |
| Cline | Its global state and MCP settings (in `~/.cline` and in VS Code), each project's `.clinerules` and `.cline` hooks and skills, and the keys in its `secrets.json` |
| GitHub Copilot | VS Code's user and project `settings.json` and `mcp.json`, Copilot's own `config.json` and `mcp-config.json`, and its skills (`~/.copilot/skills`, `<repo>/.github/skills`) |
| Codex | `config.toml` in `~/.codex` (or `CODEX_HOME`) and in each project's `.codex`, its profiles, the managed `requirements.toml` on Windows, `AGENTS.md`, and the keys in its `auth.json` |
| Grok Build | `config.toml`, `managed_config.toml`, `requirements.toml` and `trusted_folders.toml` in `~/.grok` (or `GROK_HOME`), each project's `.grok/config.toml`, and the keys in its `auth.json` and `mcp_credentials.json` |
| Antigravity CLI | Its settings in `~/.gemini/antigravity-cli`, its MCP servers and hooks (`~/.gemini/config` and each project's `.agents`), `GEMINI.md`, `.agents/rules` and its skills |
| Gemini CLI | `~/.gemini/settings.json`, `system-defaults.json`, `trustedFolders.json`, each project's `.gemini/settings.json`, `GEMINI.md`, its skills and the keys in its `oauth_creds.json` |
| Hermes Agent | `config.yaml` and its profiles in `~/.hermes` (on Windows, `%LOCALAPPDATA%\hermes`), MCP servers, the keys in its `.env` and `auth.json`, its skills, and its instruction files (including `SOUL.md`) |
| Detected agents | Antigravity (the app), GitHub Copilot for JetBrains, Cline CLI, Claude Desktop and n8n: only that they are installed, and for Claude Desktop its MCP servers |
| Shared files | `AGENTS.md` in each project, and the skills in `~/.agents/skills` and `<repo>/.agents/skills`, which several agents load |
| Credentials | `.env` files in your repositories, MCP `env` and `headers` blocks, agent settings and common credential files (npm, AWS, GitHub CLI, git, Railway, Supabase) |
| Repositories | Keys in tracked files and in git history, hooks, workflows |
| Windows | The list of scheduled tasks and Windows Defender's exclusions, to tell you when a new one appears, and whether Windows allows notifications |

## What it sends, only with your permission

- **Your results, if you connect this computer to your space.** It sends only your results, encrypted, only to app.rosmarops.com, after each check. You can disconnect at any time.
- **A problem list, if you press «Send».** When RosmarOps runs into a problem, it asks you whether to send it. The list carries error codes, the version and the system, never your files, paths, names or keys. You can tick «Send the next ones without asking», and untick it with `rosmarops problems ask`.

Every send is listed in rosmarops-sends.log, in the data subfolder of your reports folder (on Windows, %USERPROFILE%\.rosmarops\reports\data\rosmarops-sends.log).

## What it does not see

- What an agent does while it runs. RosmarOps reads configuration at rest.
- Settings that live only inside an app's database or on a vendor's servers (for example, the mode you pick in Cursor's settings), and anything you set only in one terminal session. The report states what it assumes in those cases.
- Whether a key in git was actually pushed to a public place.
- Whether a key you declared rotated was really revoked at the provider. RosmarOps records your statement and says so.

Whatever it could not read is listed in the report and never counted as clean.

---

## Trust

RosmarOps reads broad parts of your computer, which is the same shape malware has. So every claim is something you can check:

- network only with your permission, and only to app.rosmarops.com: your encrypted results, if you connect your space, and the problem list, if you press «Send»;
- writes only inside `~/.rosmarops/` and its reports folder;
- secrets never shown in full;
- it never sends anything you haven't approved, and there is no option that sends by default.

Each claim, and how to check it: [`docs/trust-model.md`](docs/trust-model.md).

---

## License

[AGPL-3.0](LICENSE). If you modify RosmarOps and run it as a service, you must publish your changes under the same license.

## Security issues

See [`SECURITY.md`](SECURITY.md).
