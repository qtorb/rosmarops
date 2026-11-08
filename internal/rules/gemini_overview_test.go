package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-193 (CTO-22 Gemini §7): Google Gemini CLI evaluated. It reads the
// workspace without asking; writing and running ask by default (you), unless
// auto_edit writes without asking or tools.allowed run_shell_command runs any
// command. A project's settings, MCP and mode load only when its folder is
// trusted (trustedFolders.json).

const (
	geGemini   = home + "/.gemini"
	geSettings = geGemini + "/settings.json"
)

// geminiEvaluate runs the Gemini CLI pipeline (FromScans → FromGeminiCLI →
// MarkGeminiUntrusted → Infer → Evaluate) and returns the graph, the result and
// the Gemini ai_client id.
func geminiEvaluate(t *testing.T, scan platform.GeminiScan, claude []platform.ClaudeSettingsFile, creds []entry) (graph.Graph, rules.Result, string) {
	t.Helper()
	var cf platform.CredentialFileScan
	for _, e := range creds {
		cf.Entries = append(cf.Entries, platform.CredentialEntry{Path: e.path, Line: e.line, Key: e.key, Value: secrets.NewSecretValue(e.value)})
	}
	roots := []string{repoWeb}
	if claude == nil {
		claude = []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{})}
	}
	settings := platform.ClaudeSettingsScan{Files: claude, Trust: []platform.ProjectTrust{{Path: repoWeb, Accepted: true}}}
	servers := graph.GeminiMCPServers(scan)
	f, _, err := graph.FromScans(platform.MCPScan{Servers: servers}, settings, cf, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	f = graph.Merge(f, graph.FromGeminiCLI(scan, now))
	g := graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(salt)}, now)
	graph.MarkGeminiUntrusted(&g, scan, roots)
	g, err = graph.Infer(g, roots, home)
	if err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	res := rules.Evaluate(rules.Input{Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases, RepoRoots: roots, Home: home, Now: now, ClaudeSettings: &settings})
	var client string
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient && n.Platform == knowledge.GeminiCLIProduct {
			client = n.ID
		}
	}
	if client == "" {
		t.Fatal("no Gemini CLI ai_client in graph")
	}
	return g, res, client
}

func geCell(t *testing.T, res rules.Result, client, row string) rules.OverviewCell {
	t.Helper()
	for _, a := range res.AgentsOverview {
		if a.AgentID != client {
			continue
		}
		if !a.Evaluated {
			t.Fatalf("Gemini CLI not evaluated: %+v", a)
		}
		for _, c := range a.Rows {
			if c.Row == row {
				return c
			}
		}
	}
	t.Fatalf("no %s row for Gemini CLI", row)
	return rules.OverviewCell{}
}

func geRules(res rules.Result, client string) map[string]bool {
	out := map[string]bool{}
	for _, f := range res.Findings {
		if len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == client {
			out[f.Rule] = true
		}
	}
	return out
}

func geScan(layers ...platform.GeminiLayer) platform.GeminiScan {
	return platform.GeminiScan{ConfigDir: geGemini, Present: true, FolderTrustEnabled: true, Layers: layers}
}

func geUser(mode string, allowed ...string) platform.GeminiLayer {
	return platform.GeminiLayer{Kind: platform.GeminiLayerUser, Path: geSettings, Status: platform.MCPSourceRead, DefaultApprovalMode: mode, ToolsAllowed: allowed}
}

// G1. Default (CTO-22 Gemini §7 G1): empty user settings; a credential in the
// workspace. Reads nobody, writes and runs you. BV-198 M3: google_web_search is
// allowed without asking, so it is internet egress — the sends row decides
// nobody and the workspace credential is read-and-send (cred_read_and_send),
// not merely readable.
func TestGemini_CaseDefault(t *testing.T) {
	scan := geScan(geUser(""))
	_, res, client := geminiEvaluate(t, scan, nil, []entry{{repoWeb + "/.env", 1, "SUPABASE_SECRET_KEY", sbSecret}})

	if c := geCell(t, res, client, rules.RowReadsFiles); c.Decider != rules.DeciderNobody {
		t.Errorf("default reads: want nobody, got %+v", c)
	}
	if c := geCell(t, res, client, rules.RowWritesFiles); c.Decider != rules.DeciderYou {
		t.Errorf("default writes: want you, got %+v", c)
	}
	if c := geCell(t, res, client, rules.RowRunsCommands); c.Decider != rules.DeciderYou {
		t.Errorf("default runs: want you, got %+v", c)
	}
	if c := geCell(t, res, client, rules.RowSendsOut); c.Decider != rules.DeciderNobody {
		t.Errorf("default sends: web_search is egress, want nobody, got %+v", c)
	}
	rs := geRules(res, client)
	if !rs[rules.RuleCredReadAndSend] {
		t.Errorf("default: web_search egress makes the workspace credential read-and-send (have %v)", rs)
	}
	if rs[rules.RuleCredReadable] {
		t.Errorf("default: with egress the credential is cred_read_and_send, not cred_readable (have %v)", rs)
	}
}

// G2. Editing without asking (CTO-22 Gemini §7 G2): auto_edit, the repo trusted,
// the repo holds another agent's config. Writes nobody; foreign_agent_config_writable.
func TestGemini_CaseAutoEdit(t *testing.T) {
	scan := geScan(geUser("auto_edit"))
	scan.TrustedFolders = map[string]string{repoWeb: "TRUST_FOLDER"}
	claude := []platform.ClaudeSettingsFile{
		userSettings(platform.ClaudePermissions{}),
		projectSettings(repoWeb, platform.ClaudePermissions{}),
	}
	_, res, client := geminiEvaluate(t, scan, claude, nil)

	if c := geCell(t, res, client, rules.RowWritesFiles); c.Decider != rules.DeciderNobody || c.Scope != rules.ScopeSameProject {
		t.Errorf("auto_edit writes: want same_project/nobody, got %+v", c)
	}
	if !geRules(res, client)[rules.RuleForeignAgentConfigWritable] {
		t.Errorf("auto_edit: missing foreign_agent_config_writable (have %v)", geRules(res, client))
	}
}

// G3. YOLO in the file (CTO-22 Gemini §7 G3): defaultApprovalMode yolo is
// ignored, so the posture is G1's; launch_flags_not_readable is shown.
func TestGemini_CaseYoloIgnored(t *testing.T) {
	scan := geScan(geUser("yolo"))
	_, res, client := geminiEvaluate(t, scan, nil, nil)

	c := geCell(t, res, client, rules.RowWritesFiles)
	if c.Decider != rules.DeciderYou {
		t.Errorf("yolo writes: want you (yolo ignored), got %+v", c)
	}
	if !contains(c.Data.Assumptions, rules.AssumeLaunchFlagsNotReadable) {
		t.Errorf("yolo: missing launch_flags_not_readable: %+v", c.Data.Assumptions)
	}
}

// G4. Untrusted project with a trap (CTO-22 Gemini §7 G4): the repo's
// .gemini/settings.json has a trusted MCP server and auto_edit, but the repo is
// not a trusted folder. Its mode and MCP capabilities carry workspace_untrusted;
// trusting the folder puts them in force and the MCP server loads.
func TestGemini_CaseUntrustedProject(t *testing.T) {
	proj := platform.GeminiLayer{
		Kind: platform.GeminiLayerProject, Path: repoWeb + "/.gemini/settings.json", Root: repoWeb, Status: platform.MCPSourceRead,
		DefaultApprovalMode: "auto_edit",
		MCP:                 []platform.GeminiMCPServer{{ID: "x", Command: "evil.exe", Trust: true}},
	}

	// Untrusted: the project's configured capabilities are not in force.
	untrusted := geScan(geUser(""), proj)
	g, _, client := geminiEvaluate(t, untrusted, nil, nil)
	var sawUntrusted bool
	for _, n := range g.Nodes {
		if n.Kind == graph.KindCapability && n.Basis == graph.BasisConfigured && !n.InForce && n.NotInForceReason == graph.ReasonWorkspaceUntrusted {
			sawUntrusted = true
		}
	}
	if !sawUntrusted {
		t.Errorf("untrusted project: want a configured capability not in force with workspace_untrusted")
	}
	for _, e := range g.Edges {
		if e.Type == graph.EdgeLoads && e.From == client {
			t.Errorf("untrusted project: the MCP server should not load")
		}
	}

	// Trusted: the MCP server loads (a loads edge from Gemini).
	trusted := geScan(geUser(""), proj)
	trusted.TrustedFolders = map[string]string{repoWeb: "TRUST_FOLDER"}
	g2, _, client2 := geminiEvaluate(t, trusted, nil, nil)
	var loads bool
	for _, e := range g2.Edges {
		if e.Type == graph.EdgeLoads && e.From == client2 {
			loads = true
		}
	}
	if !loads {
		t.Errorf("trusted project: the MCP server should load")
	}
}

// G5. System wins (CTO-22 Gemini §7 G5): the system settings allow
// run_shell_command; runs nobody, and the shell capability applies in the
// system scope.
func TestGemini_CaseSystemWins(t *testing.T) {
	sys := platform.GeminiLayer{Kind: platform.GeminiLayerSystem, Path: "/ProgramData/gemini-cli/settings.json", Status: platform.MCPSourceRead, ToolsAllowed: []string{"run_shell_command"}}
	scan := geScan(geUser(""), sys)
	g, res, client := geminiEvaluate(t, scan, nil, nil)

	if c := geCell(t, res, client, rules.RowRunsCommands); c.Decider != rules.DeciderNobody {
		t.Errorf("system runs: want nobody, got %+v", c)
	}
	var sawSystem bool
	for _, n := range g.Nodes {
		if n.Kind == graph.KindCapability && n.Type == graph.CapExecuteShell && n.Approval == graph.ApprovalAllow && n.AppliesIn == "system" {
			sawSystem = true
		}
	}
	if !sawSystem {
		t.Errorf("system runs: want an execute_shell allow capability applies_in=system")
	}
	_ = client
}
