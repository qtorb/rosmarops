package trust_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-202 point 4: one machine with every agent the engine knows installed at
// once — with config and, separately, detected without config — plus a project
// whose AGENTS.md and GEMINI.md are present. The graph is assembled exactly as
// cmd/rosmarops writeGraph does (FromScansWithGrok → AddInstructions →
// Merge{Copilot,Antigravity,Gemini,Cline} → Build → MarkCodexUntrusted →
// MarkGeminiUntrusted → Infer) and then encoded and validated against
// schemas/v2/graph.v2.schema.json. The 0.10.1 bug (5 nodes failing the node
// oneOf on Albert's machine) would have been caught by this combination.

// allAgentScans are every agent's platform scan. configured=true gives each a
// read config layer; configured=false leaves each detected but unconfigured.
type allAgentScans struct {
	settings    platform.ClaudeSettingsScan
	cursor      platform.CursorScan
	codex       platform.CodexScan
	grok        platform.GrokScan
	hermes      platform.HermesScan
	copilot     platform.CopilotScan
	antigravity platform.AntigravityScan
	gemini      platform.GeminiScan
	cline       platform.ClineScan
	instr       platform.InstructionScan
}

func allAgents(configured bool) allAgentScans {
	st := platform.MCPSourceRead
	hd := fcHome + "/.hermes"

	s := allAgentScans{
		settings: platform.ClaudeSettingsScan{
			Files: []platform.ClaudeSettingsFile{{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: st}},
			Trust: []platform.ProjectTrust{{Path: fcRepo, Accepted: true}},
		},
		cursor:      platform.CursorScan{ConfigDir: fcHome + "/.cursor", Exists: true},
		codex:       platform.CodexScan{ConfigDir: fcCodexDir, Exists: true},
		grok:        platform.GrokScan{ConfigDir: fcGrokDir, Exists: true},
		copilot:     platform.CopilotScan{ConfigDir: fcCopilotDir, Present: true},
		antigravity: platform.AntigravityScan{ConfigDir: fcGemini, Present: true},
		gemini:      platform.GeminiScan{ConfigDir: fcGemini, Present: true, FolderTrustEnabled: true},
		cline:       platform.ClineScan{ConfigDir: fcHome + "/.cline/data", Present: true, GlobalStateStatus: st, HooksEnabled: true, MigrationVersion: 3},
	}
	if configured {
		s.codex.Layers = []platform.CodexLayer{
			{Kind: platform.CodexLayerUser, Path: fcCodexCfg, Status: st},
			func() platform.CodexLayer {
				l := platform.CodexLayer{Kind: platform.CodexLayerProject, Path: fcCodexProj, Root: fcRepo, Status: st}
				l.Trusted = true
				return l
			}(),
		}
		s.grok.Layers = []platform.GrokLayer{{Kind: platform.GrokLayerUser, Path: fcGrokCfg, Status: st}}
		s.copilot.Layers = []platform.CopilotLayer{{Kind: platform.CopilotLayerUser, Path: fcCopilotDir + "/settings.json", Status: st}}
		s.gemini.Layers = []platform.GeminiLayer{{Kind: platform.GeminiLayerUser, Path: fcGemini + "/settings.json", Status: st}}
		s.antigravity.Settings = platform.AntigravitySettings{Path: fcGemini + "/antigravity-cli/settings.json", Status: st}
		// Cline auto-approval (BV-202 C5): puts the five capabilities at
		// basis=configured, which must each carry a rule or the encode fails.
		s.cline.HasAutoApproval = true
		s.cline.ReadFiles, s.cline.EditFiles, s.cline.ExecuteSafeCommands, s.cline.UseBrowser, s.cline.UseMcp = true, false, true, true, false
		cfg := platform.HermesConfig{Path: hd + "/config.yaml", ConfigDir: hd, Status: st, ApprovalMode: "smart", TerminalBackend: "local"}
		s.hermes = platform.HermesScan{
			Configs: []platform.HermesConfig{cfg},
			Sources: []platform.MCPSource{{Platform: "hermes", ConfigPath: cfg.Path, ConfigDir: cfg.ConfigDir, Status: st, LoadsMCP: true}},
		}
	}

	// The project's AGENTS.md (every agent that loads it) and GEMINI.md (Gemini).
	agentsMD := fcRepo + "/AGENTS.md"
	geminiMD := fcRepo + "/GEMINI.md"
	for _, p := range []struct{ product, dir string }{
		{"claude-code", fcClaudeDir},
		{"cursor", fcHome + "/.cursor"},
		{"codex", fcCodexDir},
		{"grok-build", fcGrokDir},
		{"cline", fcHome + "/.cline/data"},
		{"antigravity-cli", fcGemini},
		{"github-copilot", fcCopilotDir},
		{"hermes", hd},
	} {
		s.instr.Files = append(s.instr.Files, platform.InstructionFile{Product: p.product, ConfigDir: p.dir, Path: agentsMD, Shared: true})
	}
	s.instr.Files = append(s.instr.Files, platform.InstructionFile{Product: "gemini-cli", ConfigDir: fcGemini, Path: geminiMD, Shared: true})
	return s
}

// buildAllAgentsGraph assembles the graph in the exact order of writeGraph.
func buildAllAgentsGraph(t *testing.T, s allAgentScans) graph.Graph {
	t.Helper()
	var servers []platform.MCPServer
	servers = append(servers, graph.CodexMCPServers(s.codex)...)
	servers = append(servers, graph.GrokMCPServers(s.grok)...)
	servers = append(servers, graph.CopilotMCPServers(s.copilot)...)
	servers = append(servers, graph.GeminiMCPServers(s.gemini)...)
	mcp := platform.MCPScan{Servers: servers, Sources: s.hermes.Sources}
	creds := platform.CredentialFileScan{Entries: []platform.CredentialEntry{
		{Path: fcHome + "/.aws/credentials", Line: 1, Key: "AWS_ACCESS_KEY_ID", Value: secrets.NewSecretValue("AKIAABCDEFGHIJ123456")},
	}}

	frag, _, err := graph.FromScansWithGrok(mcp, s.settings, s.cursor, creds, graph.Repos{}, s.hermes, s.codex, s.grok, fcSalt, fcNow)
	if err != nil {
		t.Fatalf("FromScansWithGrok: %v", err)
	}
	frag = graph.AddInstructions(frag, s.instr, fcNow)
	frag = graph.Merge(frag, graph.FromCopilot(s.copilot, true, fcNow))
	frag = graph.Merge(frag, graph.FromAntigravityCLI(s.antigravity, fcNow))
	frag = graph.Merge(frag, graph.FromGeminiCLI(s.gemini, fcNow))
	frag = graph.Merge(frag, graph.FromCline(s.cline, true, fcNow))
	g := graph.Build(frag, r2Prov(), fcNow)
	graph.MarkCodexUntrusted(&g, s.codex)
	graph.MarkGeminiUntrusted(&g, s.gemini, []string{fcRepo})
	g, err = graph.Infer(g, []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}
	return g
}

func TestAllAgentsGraphValidates(t *testing.T) {
	for _, configured := range []bool{true, false} {
		name := "detected_no_config"
		if configured {
			name = "with_config"
		}
		t.Run(name, func(t *testing.T) {
			g := buildAllAgentsGraph(t, allAgents(configured))
			// The combination must have produced a non-trivial graph.
			if len(g.Nodes) < 8 {
				t.Fatalf("only %d nodes — the all-agents fixture is not exercising the engine", len(g.Nodes))
			}
			var clients int
			for _, n := range g.Nodes {
				if n.Type == graph.AgentAIClient {
					clients++
				}
			}
			if clients < 8 {
				t.Errorf("only %d ai_client nodes for an all-agents machine", clients)
			}
			// The contract: the encoded graph validates against the v2 schema.
			if _, err := graph.Encode(g); err != nil {
				t.Fatalf("all-agents graph fails graph.v2 schema validation (%s): %v", name, err)
			}
		})
	}
}
