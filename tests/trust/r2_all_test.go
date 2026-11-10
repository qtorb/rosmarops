package trust_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

// BV-208 (CTO-26 §2, decision D1): «R2-todos» is the reference computer R2 with
// all nine scored agents installed at once, each with the same default data R2
// uses for it alone — the same project (src/app), its AGENTS.md, its .env and
// the single user-level AWS key — assembled through the one graph assembly
// pipeline.Assemble that `rosmarops run` uses (CTO-24 E3), never by hand.
//
// Only the contagion (axis 3) and delegation (axis 4) axes of the exposure
// potential come from R2-todos; axes 1, 2 and 5 and the factory note keep coming
// from R2 with a single agent, so a shared-machine signal never leaks into them.
//
// D1 (provisional footprint): until CTO-25's Windows clean-install photos exist,
// R2-todos uses the same at-rest data R2 uses per agent (contagion_computer_source
// = "r2_fixtures" in exposure-potential.json). When the photos land, R2-todos
// moves to them in a separate task.

// r2AllInput is the single pipeline.Input with the nine agents' R2 scans merged:
// one project, one AGENTS.md loaded by every agent that reads it, one .env and
// one AWS key. It mirrors, field by field, the nine per-agent builders of
// r2_factory_scores_test.go.
func r2AllInput() pipeline.Input {
	codex := platform.CodexScan{
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
	copilot := platform.CopilotScan{ConfigDir: fcCopilotDir, Present: true, Layers: []platform.CopilotLayer{{Kind: platform.CopilotLayerUser, Path: fcCopilotDir + "/settings.json", Status: platform.MCPSourceRead}}}
	gemini := platform.GeminiScan{ConfigDir: fcGemini, Present: true, FolderTrustEnabled: true, Layers: []platform.GeminiLayer{{Kind: platform.GeminiLayerUser, Path: fcGemini + "/settings.json", Status: platform.MCPSourceRead}}}
	cline := platform.ClineScan{ConfigDir: fcHome + "/.cline/data", Present: true, GlobalStateStatus: platform.MCPSourceRead, HooksEnabled: true, MigrationVersion: 3}
	grok := platform.GrokScan{ConfigDir: fcGrokDir, Exists: true, Layers: []platform.GrokLayer{{Kind: platform.GrokLayerUser, Path: fcGrokCfg, Status: platform.MCPSourceRead}}}
	antigravity := platform.AntigravityScan{ConfigDir: fcGemini, Present: true, Settings: platform.AntigravitySettings{Path: fcGemini + "/antigravity-cli/settings.json", Status: platform.MCPSourceRead}}
	hd := fcHome + "/.hermes"
	hcfg := platform.HermesConfig{Path: hd + "/config.yaml", ConfigDir: hd, Status: platform.MCPSourceRead, ApprovalMode: "smart", TerminalBackend: "local"}
	hermes := platform.HermesScan{
		Configs: []platform.HermesConfig{hcfg},
		Sources: []platform.MCPSource{{Platform: "hermes", ConfigPath: hcfg.Path, ConfigDir: hcfg.ConfigDir, Status: platform.MCPSourceRead, LoadsMCP: true}},
	}

	var servers []platform.MCPServer
	servers = append(servers, graph.CodexMCPServers(codex)...)
	servers = append(servers, graph.CopilotMCPServers(copilot)...)
	servers = append(servers, graph.GeminiMCPServers(gemini)...)
	servers = append(servers, graph.GrokMCPServers(grok)...)

	// Every agent that loads the repo's AGENTS.md at its own config folder, as
	// each per-agent R2 builder does (Gemini loads GEMINI.md, not present in R2).
	instr := platform.InstructionScan{Files: []platform.InstructionFile{
		{Product: "claude-code", ConfigDir: fcClaudeDir, Path: r2Toml, Shared: true},
		{Product: "cursor", ConfigDir: fcHome + "/.cursor", Path: r2Toml, Shared: true},
		{Product: "codex", ConfigDir: fcCodexDir, Path: r2Toml, Shared: true},
		{Product: "github-copilot", ConfigDir: fcCopilotDir, Path: r2Toml, Shared: true},
		{Product: "cline", ConfigDir: fcHome + "/.cline/data", Path: r2Toml, Shared: true},
		{Product: "grok-build", ConfigDir: fcGrokDir, Path: r2Toml, Shared: true},
		{Product: "hermes", ConfigDir: hd, Path: r2Toml, Shared: true},
		{Product: "antigravity-cli", ConfigDir: fcGemini, Path: r2Toml, Shared: true},
	}}

	return pipeline.Input{
		MCP:          platform.MCPScan{Servers: servers, Sources: hermes.Sources},
		Settings:     r2Settings(platform.ClaudeSettingsFile{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead}),
		Cursor:       platform.CursorScan{ConfigDir: fcHome + "/.cursor", Exists: true},
		Hermes:       hermes,
		Codex:        codex,
		Grok:         grok,
		Copilot:      copilot,
		Antigravity:  antigravity,
		Gemini:       gemini,
		Cline:        cline,
		Instructions: instr,
		Extensions:   []platform.VSCodeExtension{{ID: "github.copilot"}, {ID: "saoudrizwan.claude-dev"}},
		CredFiles:    platform.CredentialFileScan{Entries: r2Creds()},
	}
}

// r2AllReport assembles R2-todos through pipeline.Assemble (E3) and builds its
// v2 report, with every per-agent scan set so agents_overview is complete.
func r2AllReport(t *testing.T) (report.V2, graph.Graph) {
	t.Helper()
	in := r2AllInput()
	g := r2Assemble(t, in)
	if _, err := graph.Encode(g); err != nil {
		t.Fatalf("R2-all graph fails graph.v2 schema validation: %v", err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	settings := r2Settings(platform.ClaudeSettingsFile{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead})
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings0(64), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: []string{fcRepo}, V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: fcNow,
		ClaudeSettings: &settings, Home: fcHome,
		Codex: in.Codex, Grok: in.Grok, Cline: in.Cline, Hermes: in.Hermes,
	})
	return v, g
}

// TestR2AllAssembles: R2-todos assembles through the single assembly and scores
// all nine agents at once (the machine axes 3 and 4 read it). A tenth agent or a
// missing one fails here.
func TestR2AllAssembles(t *testing.T) {
	v, _ := r2AllReport(t)
	want := map[string]bool{
		"claude-code": false, "cursor": false, "codex": false, "github-copilot": false,
		"gemini-cli": false, "cline": false, "grok-build": false, "hermes": false, "antigravity-cli": false,
	}
	for _, o := range v.AgentsOverview {
		if _, ok := want[o.Product]; ok && o.Evaluated {
			want[o.Product] = true
		}
	}
	for p, seen := range want {
		if !seen {
			t.Errorf("R2-all: %s is not an evaluated agent in agents_overview", p)
		}
	}
}

// TestR2AllC2ReadsConfigOf (BV-208 pt2, CTO-26 §1 eje 3 C2): in R2-todos the
// graph carries the new reads_config_of edge from Grok Build to Claude Code and
// to Cursor — the «loads another product's config» signal R2 (one agent) cannot
// show. No other product reads another's config at rest today.
func TestR2AllC2ReadsConfigOf(t *testing.T) {
	_, g := r2AllReport(t)
	prod := map[string]string{}
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient {
			prod[n.ID] = n.Platform
		}
	}
	got := map[string]bool{}
	for _, e := range g.Edges {
		if e.Type != graph.EdgeReadsConfigOf {
			continue
		}
		if prod[e.From] != "grok-build" {
			t.Errorf("reads_config_of from %q, want only grok-build", prod[e.From])
		}
		got[prod[e.From]+"->"+prod[e.To]] = true
	}
	for _, want := range []string{"grok-build->claude-code", "grok-build->cursor"} {
		if !got[want] {
			t.Errorf("missing reads_config_of %s", want)
		}
	}
}
