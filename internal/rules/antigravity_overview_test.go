package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-140 (CTO-16 Antigravity §7): the Antigravity CLI evaluated. It reads and
// writes inside the workspace without asking (the whole machine with
// allowNonWorkspaceAccess); commands, web and MCP ask by default, nothing asks
// under always-proceed, and agentMode auto hands each escalation to a reviewer
// model.

const (
	agGemini   = home + "/.gemini"
	agSettings = agGemini + "/antigravity-cli/settings.json"
)

func agServer(path, root string) platform.MCPServer {
	s := platform.MCPServer{Client: knowledge.AntigravityCLIProduct, Name: "x", Command: "node", ConfigPath: path, ConfigDir: agGemini, LoadsMCP: true, Pointer: "/mcpServers/x", Scope: platform.MCPScopeUser}
	if root != "" {
		s.Scope, s.ScopePath = platform.MCPScopeProject, root
	}
	return s
}

func antigravityEvaluate(t *testing.T, scan platform.AntigravityScan, servers []platform.MCPServer, claude []platform.ClaudeSettingsFile, instr []platform.InstructionFile, creds []entry) (graph.Graph, rules.Result, string) {
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
	f, _, err := graph.FromScans(platform.MCPScan{Servers: servers}, settings, cf, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	f = graph.Merge(f, graph.FromAntigravityCLI(scan, now))
	if len(instr) > 0 {
		f = graph.AddInstructions(f, platform.InstructionScan{Files: instr}, now)
	}
	g := graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(salt)}, now)
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
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient && n.Platform == knowledge.AntigravityCLIProduct {
			client = n.ID
		}
	}
	if client == "" {
		t.Fatal("no Antigravity CLI ai_client in graph")
	}
	return g, res, client
}

func agCell(t *testing.T, res rules.Result, client, row string) rules.OverviewCell {
	t.Helper()
	for _, a := range res.AgentsOverview {
		if a.AgentID != client {
			continue
		}
		if !a.Evaluated {
			t.Fatalf("Antigravity CLI not evaluated: %+v", a)
		}
		for _, c := range a.Rows {
			if c.Row == row {
				return c
			}
		}
	}
	t.Fatalf("no %s row for Antigravity CLI", row)
	return rules.OverviewCell{}
}

func agRules(res rules.Result, client string) map[string]bool {
	out := map[string]bool{}
	for _, f := range res.Findings {
		if len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == client {
			out[f.Rule] = true
		}
	}
	return out
}

// A. CLI in approve-all (CTO-16 Antigravity §7 A): always-proceed +
// allowNonWorkspaceAccess. Writes decide nobody machine-wide; own config, a
// foreign global config and cred_read_and_send fire.
func TestAntigravity_CaseApproveAll(t *testing.T) {
	scan := platform.AntigravityScan{ConfigDir: agGemini, Present: true, Settings: platform.AntigravitySettings{
		Path: agSettings, Status: platform.MCPSourceRead, ToolPermission: "always-proceed", AllowNonWorkspaceAccess: true,
	}}
	claude := []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{})}
	_, res, client := antigravityEvaluate(t, scan, nil, claude, nil, []entry{{repoWeb + "/.env", 1, "SUPABASE_SERVICE_ROLE_KEY", serviceRole}})

	if c := agCell(t, res, client, rules.RowWritesFiles); c.Decider != rules.DeciderNobody || c.Scope != rules.ScopeMachine {
		t.Errorf("approve-all writes: want machine/nobody, got %+v", c)
	}
	rs := agRules(res, client)
	for _, want := range []string{rules.RuleOwnConfigWritable, rules.RuleForeignAgentConfigWritable, rules.RuleCredReadAndSend} {
		if !rs[want] {
			t.Errorf("approve-all: missing %q (have %v)", want, rs)
		}
	}
}

// B. CLI default (CTO-16 Antigravity §7 B): writes the project without asking
// (foreign project config, own .agents/mcp_config.json, AGENTS.md shared);
// sending decides you.
func TestAntigravity_CaseDefault(t *testing.T) {
	scan := platform.AntigravityScan{ConfigDir: agGemini, Present: true, Settings: platform.AntigravitySettings{Path: agSettings, Status: platform.MCPSourceRead}}
	claude := []platform.ClaudeSettingsFile{
		userSettings(platform.ClaudePermissions{}),
		projectSettings(repoWeb, platform.ClaudePermissions{}),
	}
	servers := []platform.MCPServer{agServer(repoWeb+"/.agents/mcp_config.json", repoWeb)}
	instr := []platform.InstructionFile{{Product: "claude-code", ConfigDir: claudeDir, Path: repoWeb + "/AGENTS.md"}}
	_, res, client := antigravityEvaluate(t, scan, servers, claude, instr, nil)

	if c := agCell(t, res, client, rules.RowWritesFiles); c.Decider != rules.DeciderNobody || c.Scope != rules.ScopeSameProject {
		t.Errorf("default writes: want same_project/nobody, got %+v", c)
	}
	if c := agCell(t, res, client, rules.RowSendsOut); c.Decider != rules.DeciderYou {
		t.Errorf("default sends: want you, got %+v", c)
	}
	rs := agRules(res, client)
	for _, want := range []string{rules.RuleOwnConfigWritable, rules.RuleForeignAgentConfigWritable, rules.RuleForeignAgentInstructionsWritable} {
		if !rs[want] {
			t.Errorf("default: missing %q (have %v)", want, rs)
		}
	}
}

// D. Hook that approves (CTO-16 Antigravity §7 D): .agents/hooks.json is an
// agent_executable of the CLI that any agent with write access in the repo can
// rewrite.
func TestAntigravity_CaseHook(t *testing.T) {
	scan := platform.AntigravityScan{
		ConfigDir: agGemini, Present: true,
		Settings: platform.AntigravitySettings{Path: agSettings, Status: platform.MCPSourceRead},
		Hooks:    []platform.AntigravityHook{{Path: repoWeb + "/.agents/hooks.json", Root: repoWeb}},
	}
	// Claude Code with Write allowed in the repo rewrites the hook.
	claude := []platform.ClaudeSettingsFile{
		userSettings(platform.ClaudePermissions{}),
		projectSettings(repoWeb, platform.ClaudePermissions{Allow: []string{"Write", "Edit"}}),
	}
	g, res, client := antigravityEvaluate(t, scan, nil, claude, nil, nil)

	var hook bool
	for _, n := range g.Nodes {
		if n.Kind == graph.KindResource && n.Type == graph.ResourceAgentExecutable && n.Configures == client {
			hook = true
		}
	}
	if !hook {
		t.Errorf("hook: no agent_executable for the Antigravity CLI")
	}
	// Claude Code rewrites the hook (foreign_agent_config_writable).
	var claudeClient string
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient && n.Platform == knowledge.ClaudeCodeProduct {
			claudeClient = n.ID
		}
	}
	var claudeForeign bool
	for _, f := range res.Findings {
		if f.Rule == rules.RuleForeignAgentConfigWritable && len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == claudeClient {
			claudeForeign = true
		}
	}
	if !claudeForeign {
		t.Errorf("hook: Claude Code should rewrite the Antigravity hook (foreign_agent_config_writable)")
	}
}
