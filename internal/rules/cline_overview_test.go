package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-193 (CTO-22 Cline §7): the Cline VS Code extension evaluated. By default it
// reads, edits, browses and calls MCP without asking and asks before commands;
// its auto-approval settings turn these on or off; a pre-migration version let
// the agent's own model judge command safety.

const clineData = home + "/.cline/data"

// clineEvaluate runs the Cline pipeline (FromScans → FromCline → Infer →
// Evaluate) with the scan threaded into the overview input (for the migration
// marker), and returns the graph, the result and the Cline ai_client id.
func clineEvaluate(t *testing.T, scan platform.ClineScan, servers []platform.MCPServer, claude []platform.ClaudeSettingsFile, creds []entry) (graph.Graph, rules.Result, string) {
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
	f = graph.Merge(f, graph.FromCline(scan, true, now))
	g := graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(salt)}, now)
	g, err = graph.Infer(g, roots, home)
	if err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	res := rules.Evaluate(rules.Input{Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases, RepoRoots: roots, Home: home, Now: now, ClaudeSettings: &settings, Cline: scan})
	var client string
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient && n.Platform == knowledge.ClineProduct {
			client = n.ID
		}
	}
	if client == "" {
		t.Fatal("no Cline ai_client in graph")
	}
	return g, res, client
}

func clCell(t *testing.T, res rules.Result, client, row string) rules.OverviewCell {
	t.Helper()
	for _, a := range res.AgentsOverview {
		if a.AgentID != client {
			continue
		}
		if !a.Evaluated {
			t.Fatalf("Cline not evaluated: %+v", a)
		}
		for _, c := range a.Rows {
			if c.Row == row {
				return c
			}
		}
	}
	t.Fatalf("no %s row for Cline", row)
	return rules.OverviewCell{}
}

func clRules(res rules.Result, client string) map[string]bool {
	out := map[string]bool{}
	for _, f := range res.Findings {
		if len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == client {
			out[f.Rule] = true
		}
	}
	return out
}

func clBase() platform.ClineScan {
	return platform.ClineScan{ConfigDir: clineData, Present: true, GlobalStateStatus: platform.MCPSourceRead, HooksEnabled: true, MigrationVersion: 3}
}

// C1. Fresh install (CTO-22 Cline §7 C1): no autoApprovalSettings, migration 3,
// a credential on the machine. Read, edit and MCP nobody; commands you;
// cred_read_and_send (it edits and can leave by the browser or MCP).
func TestCline_CaseFreshInstall(t *testing.T) {
	scan := clBase()
	_, res, client := clineEvaluate(t, scan, nil, nil, []entry{{home + "/.aws/credentials", 1, "SUPABASE_SECRET_KEY", sbSecret}})

	if c := clCell(t, res, client, rules.RowReadsFiles); c.Decider != rules.DeciderNobody {
		t.Errorf("fresh reads: want nobody, got %+v", c)
	}
	if c := clCell(t, res, client, rules.RowWritesFiles); c.Decider != rules.DeciderNobody {
		t.Errorf("fresh writes: want nobody, got %+v", c)
	}
	if c := clCell(t, res, client, rules.RowRunsCommands); c.Decider != rules.DeciderYou {
		t.Errorf("fresh runs: want you, got %+v", c)
	}
	// Cline reaches the credential and can leave by the browser or MCP; the
	// engine's send path is shell/mode-based, so with commands still asking it
	// records cred_readable (same red band). Either is the exposed credential.
	rs := clRules(res, client)
	if !rs[rules.RuleCredReadAndSend] && !rs[rules.RuleCredReadable] {
		t.Errorf("fresh: missing a reachable-credential finding (have %v)", rs)
	}
}

// C2. As Albert's machine (CTO-22 Cline §7 C2): readFiles, executeSafeCommands
// and useMcp true, migration 3, a repo with another agent's config. Commands
// nobody; foreign_agent_config_writable.
func TestCline_CaseExecuteSafeCommands(t *testing.T) {
	scan := clBase()
	scan.HasAutoApproval = true
	scan.ReadFiles, scan.ExecuteSafeCommands, scan.UseMcp = true, true, true
	claude := []platform.ClaudeSettingsFile{
		userSettings(platform.ClaudePermissions{}),
		projectSettings(repoWeb, platform.ClaudePermissions{}),
	}
	_, res, client := clineEvaluate(t, scan, nil, claude, nil)

	if c := clCell(t, res, client, rules.RowRunsCommands); c.Decider != rules.DeciderNobody {
		t.Errorf("exec-safe runs: want nobody, got %+v", c)
	}
	if !clRules(res, client)[rules.RuleForeignAgentConfigWritable] {
		t.Errorf("exec-safe: missing foreign_agent_config_writable (have %v)", clRules(res, client))
	}
}

// C3. Old version (CTO-22 Cline §7 C3): the same as C2 without the migration
// marker. Commands run, but the agent's own model judged them (model,
// agent_model_judges_command_safety).
func TestCline_CaseOldVersion(t *testing.T) {
	scan := clBase()
	scan.MigrationVersion = 0
	scan.HasAutoApproval = true
	scan.ReadFiles, scan.ExecuteSafeCommands, scan.UseMcp = true, true, true
	_, res, client := clineEvaluate(t, scan, nil, nil, nil)

	c := clCell(t, res, client, rules.RowRunsCommands)
	if c.Decider != rules.DeciderModel || !c.Assumed {
		t.Errorf("old-version runs: want model/assumed, got %+v", c)
	}
	if !contains(c.Data.Assumptions, rules.AssumeAgentModelJudgesCommandSafety) {
		t.Errorf("old-version runs: missing agent_model_judges_command_safety: %+v", c.Data.Assumptions)
	}
}

// C5. Repo hook (CTO-22 Cline §7 C5): a .clinerules/hooks/PreToolUse.ps1 is a
// project_hook_runs_repo_script; with hooks disabled, it is not a finding.
func TestCline_CaseRepoHook(t *testing.T) {
	hook := repoWeb + "/.clinerules/hooks/PreToolUse.ps1"
	scan := clBase()
	scan.Hooks = []platform.ClineHook{{Path: hook, Root: repoWeb, Size: 120}}
	_, res, _ := clineEvaluate(t, scan, nil, nil, nil)
	if !hasHookFinding(res, hook) {
		t.Errorf("hook: missing project_hook_runs_repo_script for %s", hook)
	}

	off := clBase()
	off.HooksEnabled = false
	off.Hooks = []platform.ClineHook{{Path: hook, Root: repoWeb, Size: 120}}
	_, res2, _ := clineEvaluate(t, off, nil, nil, nil)
	if hasHookFinding(res2, hook) {
		t.Errorf("hook disabled: project_hook_runs_repo_script should not fire")
	}
}

// hasHookFinding reports a project_hook_runs_repo_script finding whose
// locations name the given hook file.
func hasHookFinding(res rules.Result, hook string) bool {
	for _, f := range res.Findings {
		if f.Rule != rules.RuleProjectHookRunsRepoScript {
			continue
		}
		for _, loc := range f.Evidence.Locations {
			if loc == hook {
				return true
			}
		}
	}
	return false
}
