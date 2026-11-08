package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-140 (CTO-16 Copilot §7): GitHub Copilot in VS Code evaluated. Built-in
// tools read and write only inside the workspace; the permission level (Manual
// by default, Allow all / global auto-approve, Assisted) drives the deciders.

const copilotDir = home + "/AppData/Roaming/Code/User"

func boolp(b bool) *bool { return &b }

func copilotUserLayer(mut func(*platform.CopilotLayer)) platform.CopilotLayer {
	l := platform.CopilotLayer{Kind: platform.CopilotLayerUser, Path: copilotDir + "/settings.json", Status: platform.MCPSourceRead}
	if mut != nil {
		mut(&l)
	}
	return l
}

func copilotEvaluate(t *testing.T, scan platform.CopilotScan, extPresent bool, claude []platform.ClaudeSettingsFile, instr []platform.InstructionFile, creds []entry) (graph.Graph, rules.Result, string) {
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
	mcp := platform.MCPScan{Servers: graph.CopilotMCPServers(scan)}
	f, _, err := graph.FromScans(mcp, settings, cf, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	f = graph.Merge(f, graph.FromCopilot(scan, extPresent, now))
	if len(instr) > 0 {
		f = graph.AddInstructions(f, platform.InstructionScan{Files: instr}, now)
	}
	g := graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(salt)}, now)
	g, err = graph.Infer(g, roots, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.Encode(g); err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	res := rules.Evaluate(rules.Input{
		Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases, RepoRoots: roots,
		Home: home, Now: now, ClaudeSettings: &settings,
	})
	var client string
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient && n.Platform == knowledge.GitHubCopilotProduct {
			client = n.ID
		}
	}
	if client == "" {
		t.Fatal("no GitHub Copilot ai_client in graph")
	}
	return g, res, client
}

func copilotCell(t *testing.T, res rules.Result, client, row string) rules.OverviewCell {
	t.Helper()
	for _, a := range res.AgentsOverview {
		if a.AgentID != client {
			continue
		}
		if !a.Evaluated {
			t.Fatalf("Copilot not evaluated: %+v", a)
		}
		for _, c := range a.Rows {
			if c.Row == row {
				return c
			}
		}
	}
	t.Fatalf("no %s row for Copilot", row)
	return rules.OverviewCell{}
}

func copilotRules(res rules.Result, client string) map[string]bool {
	out := map[string]bool{}
	for _, f := range res.Findings {
		if len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == client {
			out[f.Rule] = true
		}
	}
	return out
}

// 1. Default (CTO-16 Copilot §7 case 1): no chat.* keys, no workspace config
// targets. Manual permissions — the user approves commands (the product's
// read-only list runs automatically), edits apply in the workspace without
// asking, URLs ask. No configuration finding.
func TestCopilot_CaseDefault(t *testing.T) {
	scan := platform.CopilotScan{ConfigDir: copilotDir, Present: true, Layers: []platform.CopilotLayer{copilotUserLayer(nil)}}
	_, res, client := copilotEvaluate(t, scan, true, nil, nil, nil)

	if c := copilotCell(t, res, client, rules.RowRunsCommands); c.Decider != rules.DeciderYou {
		t.Errorf("default runs: want you, got %+v", c)
	}
	if c := copilotCell(t, res, client, rules.RowWritesFiles); c.Decider != rules.DeciderNobody || c.Scope != rules.ScopeSameProject {
		t.Errorf("default writes: want same_project/nobody, got %+v", c)
	}
	if c := copilotCell(t, res, client, rules.RowSendsOut); c.Decider != rules.DeciderYou {
		t.Errorf("default sends: want you, got %+v", c)
	}
	rs := copilotRules(res, client)
	if rs[rules.RuleForeignAgentConfigWritable] || rs[rules.RuleOwnConfigWritable] {
		t.Errorf("default: no configuration finding expected: %v", rs)
	}
}

// M1 (BV-198, CTO-23 §0): Copilot reaches a credential whose .env is inside the
// workspace. Before the fix it filtered on the empty cred.Path (copilot.go) and
// reached nothing, so a Copilot-only computer scored 100 even over a project .env.
func TestCopilot_M1_ReachesWorkspaceCred(t *testing.T) {
	scan := platform.CopilotScan{ConfigDir: copilotDir, Present: true, Layers: []platform.CopilotLayer{copilotUserLayer(nil)}}
	g, _, client := copilotEvaluate(t, scan, true, nil, nil, []entry{{repoWeb + "/.env", 1, "OPENAI_API_KEY", openAIKey}})
	if !reachesACredential(g, client) {
		t.Errorf("M1: Copilot does not reach the workspace .env credential")
	}
}

// 2. YOLO global (CTO-16 Copilot §7 case 2): chat.tools.global.autoApprove true
// in user settings — every row decides nobody.
func TestCopilot_CaseYoloGlobal(t *testing.T) {
	scan := platform.CopilotScan{ConfigDir: copilotDir, Present: true, Layers: []platform.CopilotLayer{copilotUserLayer(func(l *platform.CopilotLayer) {
		l.GlobalAutoApprove = boolp(true)
	})}}
	_, res, client := copilotEvaluate(t, scan, true, nil, nil, nil)

	for _, row := range []string{rules.RowWritesFiles, rules.RowRunsCommands, rules.RowSendsOut} {
		if c := copilotCell(t, res, client, row); c.Decider != rules.DeciderNobody {
			t.Errorf("yolo %s: want nobody, got %+v", row, c)
		}
	}
}

// 3. Terminal open in a repo (CTO-16 Copilot §7 case 3): a project
// .vscode/settings.json with a terminal catch-all runs commands without asking,
// scope same_project. Writing and sending stay Manual.
func TestCopilot_CaseTerminalOpen(t *testing.T) {
	proj := platform.CopilotLayer{
		Kind: platform.CopilotLayerProject, Path: repoWeb + "/.vscode/settings.json", Root: repoWeb, Status: platform.MCPSourceRead,
		TerminalAutoApproveAll: boolp(true), TerminalIgnoreDefaults: boolp(true),
	}
	scan := platform.CopilotScan{ConfigDir: copilotDir, Present: true, Layers: []platform.CopilotLayer{copilotUserLayer(nil), proj}}
	_, res, client := copilotEvaluate(t, scan, true, nil, nil, nil)

	if c := copilotCell(t, res, client, rules.RowRunsCommands); c.Decider != rules.DeciderNobody || c.Scope != rules.ScopeSameProject {
		t.Errorf("terminal-open runs: want same_project/nobody, got %+v", c)
	}
	if c := copilotCell(t, res, client, rules.RowSendsOut); c.Decider != rules.DeciderYou {
		t.Errorf("terminal-open sends: should stay Manual (you), got %+v", c)
	}
}

// 4. Policy wins (CTO-16 Copilot §7 case 4): HKLM ChatToolsAutoApprove = 0 and
// the user's global auto-approve set — the user capability is not in force and
// the level falls back to Manual (runs you).
func TestCopilot_CasePolicyWins(t *testing.T) {
	user := copilotUserLayer(func(l *platform.CopilotLayer) { l.GlobalAutoApprove = boolp(true) })
	managed := platform.CopilotLayer{
		Kind: platform.CopilotLayerManaged, Path: "registry:Software\\Policies\\Microsoft\\VSCode", Status: platform.MCPSourceRead,
		GlobalAutoApprove: boolp(false),
	}
	scan := platform.CopilotScan{ConfigDir: copilotDir, Present: true, Layers: []platform.CopilotLayer{user, managed}}
	g, res, client := copilotEvaluate(t, scan, true, nil, nil, nil)

	if c := copilotCell(t, res, client, rules.RowRunsCommands); c.Decider != rules.DeciderYou {
		t.Errorf("policy-wins runs: want you (manual), got %+v", c)
	}
	// The user's allow_all capability is present but not in force.
	var off bool
	for _, n := range g.Nodes {
		if n.Kind == graph.KindCapability && n.Type == graph.CapPermissionMode && n.TargetScope == "allow_all" && !n.InForce {
			off = true
			if n.NotInForceReason != graph.ReasonModeNotAllowedInScope {
				t.Errorf("policy-wins: not-in-force reason = %q", n.NotInForceReason)
			}
		}
	}
	if !off {
		t.Errorf("policy-wins: expected a not-in-force allow_all capability")
	}
}

// 5. Cross with Claude Code (CTO-16 Copilot §7 case 5): Claude Code with Write
// allowed in the repo rewrites Copilot's .vscode/settings.json
// (foreign_agent_config_writable); Copilot's workspace edits reach Claude
// Code's CLAUDE.md (foreign_agent_instructions_writable).
func TestCopilot_CaseCrossClaude(t *testing.T) {
	claude := []platform.ClaudeSettingsFile{
		userSettings(platform.ClaudePermissions{}),
		projectSettings(repoWeb, platform.ClaudePermissions{Allow: []string{"Write", "Edit"}}),
	}
	// Copilot's config lives in the repo: a project .vscode/settings.json.
	proj := platform.CopilotLayer{Kind: platform.CopilotLayerProject, Path: repoWeb + "/.vscode/settings.json", Root: repoWeb, Status: platform.MCPSourceRead}
	scan := platform.CopilotScan{ConfigDir: copilotDir, Present: true, Layers: []platform.CopilotLayer{copilotUserLayer(nil), proj}}
	instr := []platform.InstructionFile{{Product: "claude-code", ConfigDir: claudeDir, Path: repoWeb + "/CLAUDE.md"}}

	g, res, client := copilotEvaluate(t, scan, true, claude, instr, nil)

	// Copilot rewrites CLAUDE.md in the workspace (foreign instructions).
	rs := copilotRules(res, client)
	if !rs[rules.RuleForeignAgentInstructionsWritable] {
		t.Errorf("cross: Copilot should reach CLAUDE.md (foreign_agent_instructions_writable): %v", rs)
	}
	// Claude Code (Write allowed) rewrites Copilot's .vscode/settings.json.
	var claudeClient string
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient && n.Platform == knowledge.ClaudeCodeProduct {
			claudeClient = n.ID
		}
	}
	claudeRules := map[string]bool{}
	for _, f := range res.Findings {
		if len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == claudeClient {
			claudeRules[f.Rule] = true
		}
	}
	if !claudeRules[rules.RuleForeignAgentConfigWritable] {
		t.Errorf("cross: Claude Code should rewrite Copilot's .vscode/settings.json (foreign_agent_config_writable): %v", claudeRules)
	}
}
