package rules_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-140 (CTO-16 Grok §7): xAI Grok Build evaluated. It inherits Claude Code's
// and Cursor's configuration; the findings come from the shared rules through
// the grok inference and the MCP pipeline.

const (
	grokDir    = home + "/.grok"
	grokConfig = grokDir + "/config.toml"
	grokAuth   = grokDir + "/auth.json"
)

// grokUserLayer is a user config.toml layer read, with the given fields.
func grokUserLayer(mut func(*platform.GrokLayer)) platform.GrokLayer {
	l := platform.GrokLayer{Kind: platform.GrokLayerUser, Path: grokConfig, Status: platform.MCPSourceRead}
	if mut != nil {
		mut(&l)
	}
	return l
}

func grokEvaluate(t *testing.T, scan platform.GrokScan, claude []platform.ClaudeSettingsFile, creds []entry) (graph.Graph, rules.Result, string) {
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
	mcp := platform.MCPScan{Servers: graph.GrokMCPServers(scan)}
	f, _, err := graph.FromScansWithGrok(mcp, settings, platform.CursorScan{}, cf, graph.Repos{}, platform.HermesScan{}, platform.CodexScan{}, scan, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	g, err := graph.Infer(graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(salt)}, now), roots, home)
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
		Home: home, Now: now, ClaudeSettings: &settings, Grok: scan,
	})
	var client string
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient && n.Platform == knowledge.GrokBuildProduct {
			client = n.ID
		}
	}
	if client == "" {
		t.Fatal("no Grok Build ai_client in graph")
	}
	return g, res, client
}

func grokRules(res rules.Result, client string) map[string]bool {
	out := map[string]bool{}
	for _, f := range res.Findings {
		if len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == client {
			out[f.Rule] = true
		}
	}
	return out
}

func grokCell(t *testing.T, res rules.Result, client, row string) rules.OverviewCell {
	t.Helper()
	for _, a := range res.AgentsOverview {
		if a.AgentID != client {
			continue
		}
		if !a.Evaluated {
			t.Fatalf("Grok not evaluated: %+v", a)
		}
		for _, c := range a.Rows {
			if c.Row == row {
				return c
			}
		}
	}
	t.Fatalf("no %s row for Grok", row)
	return rules.OverviewCell{}
}

// T1 — inherits «approve everything» from Claude Code (CTO-16 Grok §7 T1).
func TestGrok_T1_InheritsClaudeBypass(t *testing.T) {
	scan := platform.GrokScan{ConfigDir: grokDir, Exists: true, Layers: []platform.GrokLayer{grokUserLayer(nil)}}
	claude := []platform.ClaudeSettingsFile{{
		Scope: platform.MCPScopeUser, ConfigPath: userJSON, ConfigDir: claudeDir, Status: platform.MCPSourceRead,
		Permissions: platform.ClaudePermissions{DefaultMode: "bypassPermissions"},
	}}
	_, res, client := grokEvaluate(t, scan, claude, []entry{{dashEnv, 4, "SUPABASE_SERVICE_ROLE_KEY", serviceRole}})

	if c := grokCell(t, res, client, rules.RowReadsFiles); c.Decider != rules.DeciderNobody || c.Scope != rules.ScopeMachine {
		t.Errorf("reads: %+v", c)
	}
	if c := grokCell(t, res, client, rules.RowWritesFiles); c.Decider != rules.DeciderNobody {
		t.Errorf("writes under inherited bypass should be nobody: %+v", c)
	}
	rs := grokRules(res, client)
	for _, want := range []string{rules.RuleOwnConfigWritable, rules.RuleForeignAgentConfigWritable} {
		if !rs[want] {
			t.Errorf("T1: missing finding %q (have %v)", want, rs)
		}
	}
	// The permission mode is configured, from Claude Code's settings file.
	c := grokCell(t, res, client, rules.RowWritesFiles)
	if c.Origin != rules.OriginConfigured {
		t.Errorf("T1 writes origin: %+v", c)
	}
}

// T2 — compat off, mode ask (CTO-16 Grok §7 T2): no loads to Claude/Cursor MCP;
// reads any path (a high-privilege credential readable); write and shell the
// user decides.
func TestGrok_T2_CompatOffAsk(t *testing.T) {
	scan := platform.GrokScan{ConfigDir: grokDir, Exists: true, Layers: []platform.GrokLayer{grokUserLayer(func(l *platform.GrokLayer) {
		l.PermissionMode, l.HasPermissionMode = "ask", true
		l.ClaudeImported = true
		l.Compat = map[string]bool{"cursor.mcps": false, "cursor.hooks": false, "cursor.rules": false, "cursor.skills": false, "cursor.agents": false}
	})}}
	_, res, client := grokEvaluate(t, scan, nil, []entry{{home + "/.aws/credentials", 1, "SUPABASE_SERVICE_ROLE_KEY", serviceRole}})

	if c := grokCell(t, res, client, rules.RowReadsFiles); c.Decider != rules.DeciderNobody || c.Scope != rules.ScopeMachine {
		t.Errorf("reads: %+v", c)
	}
	if c := grokCell(t, res, client, rules.RowWritesFiles); c.Decider != rules.DeciderYou {
		t.Errorf("writes under ask should be you: %+v", c)
	}
	if c := grokCell(t, res, client, rules.RowSendsOut); c.Decider != rules.DeciderYou {
		t.Errorf("sends under ask should be you: %+v", c)
	}
	rs := grokRules(res, client)
	if !rs[rules.RuleCredReadable] {
		t.Errorf("T2: the high-privilege credential should be readable: %v", rs)
	}
	if rs[rules.RuleCredReadAndSend] {
		t.Errorf("T2: ask mode must not send out, so no cred_read_and_send: %v", rs)
	}
}

// T3 — untrusted project, then trusted (CTO-16 Grok §7 T3).
func TestGrok_T3_ProjectTrust(t *testing.T) {
	proj := platform.GrokLayer{
		Kind: platform.GrokLayerProject, Path: repoWeb + "/.grok/config.toml", Root: repoWeb, Status: platform.MCPSourceRead,
		Allow: []string{"*"}, MCP: []platform.GrokMCPServer{{ID: "x", Command: "node", Args: []string{"x.js"}}},
	}
	// Untrusted: the project capabilities are not in force.
	scan := platform.GrokScan{ConfigDir: grokDir, Exists: true, Layers: []platform.GrokLayer{grokUserLayer(nil), proj}}
	g, _, _ := grokEvaluate(t, scan, nil, nil)
	var untrusted bool
	for _, n := range g.Nodes {
		if n.Kind == graph.KindCapability && n.NotInForceReason == graph.ReasonWorkspaceUntrusted {
			untrusted = true
		}
	}
	if !untrusted {
		t.Errorf("untrusted project: no workspace_untrusted capability")
	}

	// Trusted: the wildcard allow is in force; writes decide nobody.
	proj.Trusted = true
	scan.Trusted = []string{graph.CanonicalPath(repoWeb)}
	scan.Layers[1] = proj
	_, res, client := grokEvaluate(t, scan, nil, nil)
	if c := grokCell(t, res, client, rules.RowWritesFiles); c.Decider != rules.DeciderNobody {
		t.Errorf("trusted wildcard: writes should be nobody: %+v", c)
	}
}

// T4 — credentials in clear (CTO-16 Grok §7 T4): yolo sends them out.
func TestGrok_T4_CredsInClear(t *testing.T) {
	scan := platform.GrokScan{ConfigDir: grokDir, Exists: true, Layers: []platform.GrokLayer{grokUserLayer(func(l *platform.GrokLayer) {
		l.Yolo = true
		l.APIKeys = []platform.EnvVar{{Key: "mine", Value: secrets.NewSecretValue("xai-FAKEapikey0000WXYZ")}}
		l.MCP = []platform.GrokMCPServer{{
			ID: "github", Command: "npx", EnvKeys: []string{"GITHUB_PERSONAL_ACCESS_TOKEN"},
			Env: []platform.EnvVar{{Key: "GITHUB_PERSONAL_ACCESS_TOKEN", Value: secrets.NewSecretValue("ghp_FAKEFAKEFAKEFAKE1234")}},
		}}
		l.SessionCreds = []platform.EnvVar{{Key: "auth.json:tokens.access_token", Value: secrets.NewSecretValue("grok-sess-FAKEtoken5678")}}
	})}}
	g, res, client := grokEvaluate(t, scan, nil, nil)

	rs := grokRules(res, client)
	if !rs[rules.RuleCredReadAndSend] {
		t.Errorf("T4: yolo should give cred_read_and_send: %v", rs)
	}
	if c := grokCell(t, res, client, rules.RowSendsOut); c.Decider != rules.DeciderNobody {
		t.Errorf("sends under yolo should be nobody: %+v", c)
	}
	// Its own auth.json is reached but excused as its session file.
	byID := map[string]graph.Node{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	var reachesAuth bool
	for _, e := range g.Edges {
		if e.Type == graph.EdgeReaches && e.From == client {
			locs := byID[e.To].Locations
			if len(locs) > 0 && strings.Contains(locs[0], "auth.json") {
				reachesAuth = true
			}
		}
	}
	if !reachesAuth {
		t.Errorf("T4: Grok should reach its own auth.json")
	}
	// No secret in clear in the graph.
	enc, _ := graph.Encode(g)
	if strings.Contains(string(enc), "FAKEapikey") || strings.Contains(string(enc), "FAKEFAKE") || strings.Contains(string(enc), "FAKEtoken") {
		t.Errorf("a secret leaked into the graph")
	}
}

// T5 — auto mode (CTO-16 Grok §7 T5): a classifier decides, with
// auto_classifier_permits.
func TestGrok_T5_AutoMode(t *testing.T) {
	scan := platform.GrokScan{ConfigDir: grokDir, Exists: true, Layers: []platform.GrokLayer{grokUserLayer(func(l *platform.GrokLayer) {
		l.PermissionMode, l.HasPermissionMode = "auto", true
	})}}
	_, res, client := grokEvaluate(t, scan, nil, nil)
	c := grokCell(t, res, client, rules.RowWritesFiles)
	if c.Decider != rules.DeciderModel || !c.Assumed {
		t.Errorf("writes under auto should be model/assumed: %+v", c)
	}
	if !contains(c.Data.Assumptions, "auto_classifier_permits") {
		t.Errorf("auto writes cell missing auto_classifier_permits: %+v", c.Data.Assumptions)
	}
}
