package trust_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-198 point 8 (CTO-23 §1, §5): the reference computer «R2» as a repository
// fixture, and a test that fixes the nine factory scores. R2 is a Windows-shaped
// machine with ONE scored agent installed, a git project (src/app) whose
// AGENTS.md is present and whose .env (OPENAI key, a medium credential) is
// ignored by git and so unversioned, and a user-level AWS key (high) in
// ~/.aws/credentials. The project folder is trusted, as a product asks on first
// open. Each agent is scored alone with the real engine (graph → rules → index →
// BuildV2); a score that moves without a code or knowledge-base change fails the
// test (CTO-23 §5, reproducibility).
//
// PROVISIONAL FOOTPRINT: until the «measure-clean-installs» workflow (point 7)
// runs on a real Windows machine, each agent's at-rest files are taken from the
// reference-computer spec (§1) and RosmarOps's own knowledge of each product's
// configuration folder, not from a measured clean install. What a product writes
// only on first use is "sin verificar" and is NOT assumed here. When point 7's
// artifacts land, this fixture and the pinned scores are reconciled with them.

const (
	r2AWS  = fcHome + "/.aws/credentials" // user-level AWS key (high)
	r2Env  = fcRepo + "/.env"             // project .env, git-ignored (OPENAI, medium)
	r2Toml = fcRepo + "/AGENTS.md"
)

// r2Creds is the reference computer's two credentials: a high AWS key outside
// the project and a medium OpenAI key in the project's unversioned .env.
func r2Creds() []platform.CredentialEntry {
	return []platform.CredentialEntry{
		{Path: r2AWS, Line: 1, Key: "AWS_ACCESS_KEY_ID", Value: secrets.NewSecretValue("AKIAABCDEFGHIJ123456")},
		{Path: r2Env, Line: 1, Key: "OPENAI_API_KEY", Value: secrets.NewSecretValue("sk-proj-" + strings.Repeat("O", 48))},
	}
}

func r2Settings(files ...platform.ClaudeSettingsFile) platform.ClaudeSettingsScan {
	return platform.ClaudeSettingsScan{Files: files, Trust: []platform.ProjectTrust{{Path: fcRepo, Accepted: true}}}
}

func r2BuildV2(t *testing.T, g *graph.Graph, settings *platform.ClaudeSettingsScan, extra func(*report.V2Input)) report.V2 {
	t.Helper()
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	in := report.V2Input{
		Graph: g, GraphSHA256: strings0(64), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: []string{fcRepo}, V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: fcNow,
		ClaudeSettings: settings, Home: fcHome,
	}
	if extra != nil {
		extra(&in)
	}
	v, _ := report.BuildV2(in)
	return v
}

// r2Score returns the score of the one evaluated scored agent in v (R2 installs
// exactly one), and whether it has a value.
func r2Score(t *testing.T, v report.V2, product string) (int, string) {
	t.Helper()
	for _, a := range v.Index.Agents {
		if a.AgentID == "" {
			continue
		}
		// Match by the product behind the agent id via the overview.
		for _, o := range v.AgentsOverview {
			if o.AgentID == a.AgentID && o.Product == product {
				if a.Value == nil {
					t.Fatalf("%s has no score: %+v", product, a)
				}
				return *a.Value, a.Band
			}
		}
	}
	t.Fatalf("no evaluated %s in R2 report", product)
	return 0, ""
}

func r2Instr(product, configDir string) []platform.InstructionFile {
	return []platform.InstructionFile{{Product: product, ConfigDir: configDir, Path: r2Toml, Shared: true}}
}

// --- per-agent R2 reports ---

func r2ClaudeCode(t *testing.T) report.V2 {
	// Claude Code installed alone, default (auto) mode, Bash available. It loads
	// the repo's AGENTS.md (no CLAUDE.md present).
	settings := r2Settings(platform.ClaudeSettingsFile{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead})
	frag, _, err := graph.FromScans(platform.MCPScan{}, settings, platform.CredentialFileScan{Entries: r2Creds()}, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: r2Instr("claude-code", fcClaudeDir)}, fcNow)
	g, err := graph.Infer(graph.Build(frag, r2Prov(), fcNow), []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, nil)
}

func r2Cursor(t *testing.T) report.V2 {
	settings := r2Settings()
	cursor := platform.CursorScan{ConfigDir: fcHome + "/.cursor", Exists: true}
	frag, _, err := graph.FromScansWithCursor(platform.MCPScan{}, settings, cursor, platform.CredentialFileScan{Entries: r2Creds()}, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: r2Instr("cursor", fcHome+"/.cursor")}, fcNow)
	g, err := graph.Infer(graph.Build(frag, r2Prov(), fcNow), []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, nil)
}

func r2Codex(t *testing.T) report.V2 {
	scan := platform.CodexScan{
		ConfigDir: fcCodexDir, Exists: true,
		Layers: []platform.CodexLayer{
			{Kind: platform.CodexLayerUser, Path: fcCodexCfg, Status: platform.MCPSourceRead},
			func() platform.CodexLayer {
				l := platform.CodexLayer{Kind: platform.CodexLayerProject, Path: fcCodexProj, Root: fcRepo, Status: platform.MCPSourceRead}
				l.Trusted = true
				return l
			}(),
		},
	}
	settings := r2Settings()
	frag, _, err := graph.FromScansWithCodex(platform.MCPScan{Servers: graph.CodexMCPServers(scan)}, settings, platform.CursorScan{}, platform.CredentialFileScan{Entries: r2Creds()}, graph.Repos{}, platform.HermesScan{}, scan, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: r2Instr("codex", fcCodexDir)}, fcNow)
	g := graph.Build(frag, r2Prov(), fcNow)
	graph.MarkCodexUntrusted(&g, scan)
	g, err = graph.Infer(g, []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, func(in *report.V2Input) { in.Codex = scan })
}

func r2Copilot(t *testing.T) report.V2 {
	scan := platform.CopilotScan{ConfigDir: fcCopilotDir, Present: true, Layers: []platform.CopilotLayer{{Kind: platform.CopilotLayerUser, Path: fcCopilotDir + "/settings.json", Status: platform.MCPSourceRead}}}
	settings := r2Settings()
	frag, _, err := graph.FromScans(platform.MCPScan{Servers: graph.CopilotMCPServers(scan)}, settings, platform.CredentialFileScan{Entries: r2Creds()}, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.Merge(frag, graph.FromCopilot(scan, true, fcNow))
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: r2Instr("github-copilot", fcCopilotDir)}, fcNow)
	g, err := graph.Infer(graph.Build(frag, r2Prov(), fcNow), []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, nil)
}

func r2Gemini(t *testing.T) report.V2 {
	// Gemini loads GEMINI.md, not AGENTS.md: the reference repo has AGENTS.md
	// only, so Gemini loads no project instruction here.
	scan := platform.GeminiScan{ConfigDir: fcGemini, Present: true, FolderTrustEnabled: true, Layers: []platform.GeminiLayer{{Kind: platform.GeminiLayerUser, Path: fcGemini + "/settings.json", Status: platform.MCPSourceRead}}}
	settings := r2Settings()
	frag, _, err := graph.FromScans(platform.MCPScan{Servers: graph.GeminiMCPServers(scan)}, settings, platform.CredentialFileScan{Entries: r2Creds()}, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.Merge(frag, graph.FromGeminiCLI(scan, fcNow))
	g := graph.Build(frag, r2Prov(), fcNow)
	graph.MarkGeminiUntrusted(&g, scan, []string{fcRepo})
	g, err = graph.Infer(g, []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, nil)
}

func r2Cline(t *testing.T) report.V2 {
	scan := platform.ClineScan{ConfigDir: fcHome + "/.cline/data", Present: true, GlobalStateStatus: platform.MCPSourceRead, HooksEnabled: true, MigrationVersion: 3}
	settings := r2Settings()
	frag, _, err := graph.FromScans(platform.MCPScan{}, settings, platform.CredentialFileScan{Entries: r2Creds()}, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.Merge(frag, graph.FromCline(scan, true, fcNow))
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: r2Instr("cline", fcHome+"/.cline/data")}, fcNow)
	g, err := graph.Infer(graph.Build(frag, r2Prov(), fcNow), []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, func(in *report.V2Input) { in.Cline = scan })
}

func r2Grok(t *testing.T) report.V2 {
	scan := platform.GrokScan{ConfigDir: fcGrokDir, Exists: true, Layers: []platform.GrokLayer{{Kind: platform.GrokLayerUser, Path: fcGrokCfg, Status: platform.MCPSourceRead}}}
	settings := r2Settings()
	frag, _, err := graph.FromScansWithGrok(platform.MCPScan{Servers: graph.GrokMCPServers(scan)}, settings, platform.CursorScan{}, platform.CredentialFileScan{Entries: r2Creds()}, graph.Repos{}, platform.HermesScan{}, platform.CodexScan{}, scan, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: r2Instr("grok-build", fcGrokDir)}, fcNow)
	g, err := graph.Infer(graph.Build(frag, r2Prov(), fcNow), []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, func(in *report.V2Input) { in.Grok = scan })
}

func r2Hermes(t *testing.T) report.V2 {
	hd := fcHome + "/.hermes"
	cfg := platform.HermesConfig{Path: hd + "/config.yaml", ConfigDir: hd, Status: platform.MCPSourceRead, ApprovalMode: "smart", TerminalBackend: "local"}
	source := platform.MCPSource{Platform: "hermes", ConfigPath: cfg.Path, ConfigDir: cfg.ConfigDir, Status: platform.MCPSourceRead, LoadsMCP: true}
	hermes := platform.HermesScan{Configs: []platform.HermesConfig{cfg}, Sources: []platform.MCPSource{source}}
	settings := r2Settings()
	frag, _, err := graph.FromScansWithHermes(platform.MCPScan{Sources: hermes.Sources}, settings, platform.CursorScan{}, platform.CredentialFileScan{Entries: r2Creds()}, graph.Repos{}, hermes, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: r2Instr("hermes", hd)}, fcNow)
	g, err := graph.Infer(graph.Build(frag, r2Prov(), fcNow), []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, func(in *report.V2Input) { in.Hermes = hermes })
}

func r2Antigravity(t *testing.T) report.V2 {
	scan := platform.AntigravityScan{ConfigDir: fcGemini, Present: true, Settings: platform.AntigravitySettings{Path: fcGemini + "/antigravity-cli/settings.json", Status: platform.MCPSourceRead}}
	settings := r2Settings()
	frag, _, err := graph.FromScans(platform.MCPScan{}, settings, platform.CredentialFileScan{Entries: r2Creds()}, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.Merge(frag, graph.FromAntigravityCLI(scan, fcNow))
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: r2Instr("antigravity-cli", fcGemini)}, fcNow)
	g, err := graph.Infer(graph.Build(frag, r2Prov(), fcNow), []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, nil)
}

func r2Prov() graph.Provenance {
	return graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(fcSalt)}
}

// TestR2FactoryScores fixes the nine factory scores of the reference computer.
// A change here without a code or knowledge-base change is a regression (CTO-23
// §5); a deliberate change updates the pinned values in the same commit.
func TestR2FactoryScores(t *testing.T) {
	cases := []struct {
		product string
		build   func(*testing.T) report.V2
		score   int
		band    string
	}{
		{"claude-code", r2ClaudeCode, 31, "exposed"},
		{"cursor", r2Cursor, 31, "exposed"},
		{"codex", r2Codex, 73, "open_gaps"},
		{"github-copilot", r2Copilot, 76, "open_gaps"},
		{"gemini-cli", r2Gemini, 76, "open_gaps"},
		{"cline", r2Cline, 27, "exposed"},
		{"grok-build", r2Grok, 32, "exposed"},
		{"hermes", r2Hermes, 32, "exposed"},
		{"antigravity-cli", r2Antigravity, 76, "open_gaps"},
	}
	for _, c := range cases {
		t.Run(c.product, func(t *testing.T) {
			v := c.build(t)
			got, band := r2Score(t, v, c.product)
			t.Logf("R2 %s = %d (%s)", c.product, got, band)
			if got != c.score || band != c.band {
				t.Errorf("R2 %s = %d (%s), want %d (%s) — a score moved; if intended, update the pin", c.product, got, band, c.score, c.band)
			}
		})
	}
}
