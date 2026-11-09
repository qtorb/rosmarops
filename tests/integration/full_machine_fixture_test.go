package integration_test

import (
	"fmt"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-203 point 2 (CTO-24 E8): a fixed full-machine fixture — a Windows home
// with every agent the engine scores, driven through the real single assembly
// (pipeline.Assemble) and the real report/space/package pipeline, with every
// output validated against its schema (full_machine_test.go).
//
// The fixture is built in Go (not read from disk) so TestFullMachinePipeline
// runs identically on Linux, Windows and macOS (point 11, ci.yml). It models a
// Windows machine with:
//   - a non-ASCII user name, C:\Users\… paths;
//   - 48 repositories, several carrying AGENTS.md, GEMINI.md and .env;
//   - the nine scored agents (claude-code, cursor, codex, github-copilot,
//     gemini-cli, cline, grok-build, hermes, antigravity-cli), with config and,
//     in the detected-only variant, without;
//   - Codex with 15 Computer Use apps allowed;
//   - Hermes under %LOCALAPPDATA%;
//   - credentials located by ruta:línea and by ruta#/puntero.
//
// A new scored agent does not enter the engine without its part here: the test
// asserts that every scored product produced a note.

const (
	fmUser    = "José-Añá" // non-ASCII Windows user name
	fmHome    = `C:\Users\` + fmUser
	fmClaude  = fmHome + `\.claude`
	fmCodex   = fmHome + `\.codex`
	fmGrok    = fmHome + `\.grok`
	fmGemini  = fmHome + `\.gemini`
	fmCopilot = fmHome + `\AppData\Roaming\Code\User`
	fmClineD  = fmHome + `\.cline\data`
	fmHermesD = fmHome + `\AppData\Local\hermes` // %LOCALAPPDATA%
)

var (
	fmSalt = []byte("0123456789abcdef0123456789abcdef")
	fmNow  = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
)

// fmScoredProducts are the nine agents the engine scores. A note must appear
// for each when the machine has it.
var fmScoredProducts = []string{
	"claude-code", "cursor", "codex", "github-copilot",
	"gemini-cli", "cline", "grok-build", "hermes", "antigravity-cli",
}

// fmRepos is the 48 repository roots under the user's src folder.
func fmRepos() []string {
	roots := make([]string, 0, 48)
	for i := 1; i <= 48; i++ {
		roots = append(roots, fmt.Sprintf(`%s\src\repo%02d`, fmHome, i))
	}
	return roots
}

// fmAgentsMDRepos are the three repos whose AGENTS.md every loading agent reads.
func fmAgentsMDRepos() []string { return fmRepos()[0:3] }

// fmGeminiMDRepos are the two repos whose GEMINI.md Gemini reads.
func fmGeminiMDRepos() []string { return fmRepos()[3:5] }

// fmEnvRepos are the four repos whose .env holds a (fake) OpenAI key.
func fmEnvRepos() []string { return fmRepos()[5:9] }

// fmComputerUseApps is the fifteen Computer Use app ids Codex is told to allow
// without asking (CTO-22 Computer Use).
func fmComputerUseApps() []string {
	apps := make([]string, 0, 15)
	for i := 1; i <= 15; i++ {
		apps = append(apps, fmt.Sprintf("com.example.app%02d", i))
	}
	return apps
}

// fmProductsLoadingAgentsMD are the agents that load a shared AGENTS.md.
var fmProductsLoadingAgentsMD = []struct{ product, dir string }{
	{"claude-code", fmClaude},
	{"cursor", fmHome + `\.cursor`},
	{"codex", fmCodex},
	{"grok-build", fmGrok},
	{"cline", fmClineD},
	{"antigravity-cli", fmGemini},
	{"github-copilot", fmCopilot},
	{"hermes", fmHermesD},
}

// fullMachineInput builds the pipeline input for the full machine. configured
// gives each agent a read config layer; detected-only leaves each present but
// unconfigured. The Codex, Grok, Cline, Hermes and Gemini scans are returned
// too, because report.BuildV2 takes them for the overview cells.
func fullMachineInput(configured bool) (pipeline.Input, fullMachineScans) {
	st := platform.MCPSourceRead
	repos := fmRepos()

	var trust []platform.ProjectTrust
	for _, r := range repos {
		trust = append(trust, platform.ProjectTrust{Path: r, Accepted: true})
	}

	codex := platform.CodexScan{ConfigDir: fmCodex, Exists: true}
	grok := platform.GrokScan{ConfigDir: fmGrok, Exists: true}
	copilot := platform.CopilotScan{ConfigDir: fmCopilot, Present: true}
	antigravity := platform.AntigravityScan{ConfigDir: fmGemini, Present: true}
	gemini := platform.GeminiScan{ConfigDir: fmGemini, Present: true, FolderTrustEnabled: true}
	cline := platform.ClineScan{ConfigDir: fmClineD, Present: true, GlobalStateStatus: st, HooksEnabled: true, MigrationVersion: 3}
	var hermes platform.HermesScan
	settings := platform.ClaudeSettingsScan{
		Files: []platform.ClaudeSettingsFile{{Scope: platform.MCPScopeUser, ConfigPath: fmClaude + `\settings.json`, ConfigDir: fmClaude, Status: st}},
		Trust: trust,
	}

	if configured {
		// Codex: user + a trusted project layer, and 15 Computer Use apps.
		user := platform.CodexLayer{Kind: platform.CodexLayerUser, Path: fmCodex + `\config.toml`, Status: st}
		user.ComputerUse = &platform.CodexComputerUse{DefaultAppAccess: "allow", Allowed: fmComputerUseApps()}
		proj := platform.CodexLayer{Kind: platform.CodexLayerProject, Path: repos[0] + `\.codex\config.toml`, Root: repos[0], Status: st}
		proj.Trusted = true
		codex.Layers = []platform.CodexLayer{user, proj}
		grok.Layers = []platform.GrokLayer{{Kind: platform.GrokLayerUser, Path: fmGrok + `\config.toml`, Status: st}}
		copilot.Layers = []platform.CopilotLayer{{Kind: platform.CopilotLayerUser, Path: fmCopilot + `\settings.json`, Status: st}}
		gemini.Layers = []platform.GeminiLayer{{Kind: platform.GeminiLayerUser, Path: fmGemini + `\settings.json`, Status: st}}
		antigravity.Settings = platform.AntigravitySettings{Path: fmGemini + `\antigravity-cli\settings.json`, Status: st}
		cline.HasAutoApproval = true
		cline.ReadFiles, cline.EditFiles, cline.ExecuteSafeCommands, cline.UseBrowser, cline.UseMcp = true, false, true, true, false
		cfg := platform.HermesConfig{Path: fmHermesD + `\config.yaml`, ConfigDir: fmHermesD, Status: st, ApprovalMode: "smart", TerminalBackend: "local"}
		hermes = platform.HermesScan{
			Configs: []platform.HermesConfig{cfg},
			Sources: []platform.MCPSource{{Platform: "hermes", ConfigPath: cfg.Path, ConfigDir: cfg.ConfigDir, Status: st, LoadsMCP: true}},
		}
	}

	// Shared instruction files: AGENTS.md in three repos for every loading
	// agent, GEMINI.md in two repos for Gemini.
	var instr platform.InstructionScan
	for _, r := range fmAgentsMDRepos() {
		for _, p := range fmProductsLoadingAgentsMD {
			instr.Files = append(instr.Files, platform.InstructionFile{Product: p.product, ConfigDir: p.dir, Path: r + `\AGENTS.md`, Shared: true})
		}
	}
	for _, r := range fmGeminiMDRepos() {
		instr.Files = append(instr.Files, platform.InstructionFile{Product: "gemini-cli", ConfigDir: fmGemini, Path: r + `\GEMINI.md`, Shared: true})
	}

	// Credentials spread across the machine: a user AWS key by ruta:línea, a
	// user settings token by ruta#/puntero, and an OpenAI key in four repos'
	// .env (ruta:línea), unversioned.
	creds := platform.CredentialFileScan{Entries: []platform.CredentialEntry{
		{Path: fmHome + `\.aws\credentials`, Line: 1, Key: "AWS_ACCESS_KEY_ID", Value: secrets.NewSecretValue("AKIAABCDEFGHIJ123456")},
		{Path: fmClaude + `\settings.json`, Pointer: "/mcpServers/github/env/GITHUB_TOKEN", Key: "GITHUB_TOKEN", Value: secrets.NewSecretValue("ghp_" + repeat("A", 36))},
	}}
	for _, r := range fmEnvRepos() {
		creds.Entries = append(creds.Entries, platform.CredentialEntry{Path: r + `\.env`, Line: 1, Key: "OPENAI_API_KEY", Value: secrets.NewSecretValue("sk-proj-" + repeat("O", 48))})
	}

	// The VS Code extensions a configured Copilot/Cline machine lists.
	exts := []platform.VSCodeExtension{{ID: "github.copilot"}, {ID: "saoudrizwan.claude-dev"}}

	var servers []platform.MCPServer
	servers = append(servers, graph.CodexMCPServers(codex)...)
	servers = append(servers, graph.GrokMCPServers(grok)...)
	servers = append(servers, graph.CopilotMCPServers(copilot)...)
	servers = append(servers, graph.GeminiMCPServers(gemini)...)

	in := pipeline.Input{
		MCP:          platform.MCPScan{Servers: servers, Sources: hermes.Sources},
		Settings:     settings,
		Cursor:       platform.CursorScan{ConfigDir: fmHome + `\.cursor`, Exists: true},
		Hermes:       hermes,
		Codex:        codex,
		Grok:         grok,
		Copilot:      copilot,
		Antigravity:  antigravity,
		Gemini:       gemini,
		Cline:        cline,
		Instructions: instr,
		CredFiles:    creds,
		Extensions:   exts,
		Repos:        graph.Repos{},
		Provenance: graph.Provenance{
			EngineVersion:    "0.10.3-test",
			RulesVersion:     graph.RulesVersion,
			KnowledgeVersion: graph.KnowledgeVersion,
			HostID:           graph.HostID(fmSalt),
		},
		Salt:      fmSalt,
		RepoRoots: repos,
		Home:      fmHome,
		Now:       fmNow,
	}
	return in, fullMachineScans{Codex: codex, Grok: grok, Cline: cline, Hermes: hermes, Gemini: gemini, Settings: settings}
}

// fullMachineScans carries the per-agent scans report.BuildV2 needs beyond the
// graph itself.
type fullMachineScans struct {
	Codex    platform.CodexScan
	Grok     platform.GrokScan
	Cline    platform.ClineScan
	Hermes   platform.HermesScan
	Gemini   platform.GeminiScan
	Settings platform.ClaudeSettingsScan
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
