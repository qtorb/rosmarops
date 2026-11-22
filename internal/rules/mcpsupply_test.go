package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
)

func supplyEval(t *testing.T, g graph.Graph, settings *platform.ClaudeSettingsScan) rules.Result {
	t.Helper()
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	return rules.Evaluate(rules.Input{
		Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: []string{home + "/repo"}, Home: home, Now: now, ClaudeSettings: settings,
	})
}

func byRule(res rules.Result, rule string) []rules.Finding {
	var out []rules.Finding
	for _, f := range res.Findings {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

// BV-274: an unpinned launch is a yellow finding per (agent, server); a user
// server reaches every project; a rejected project server gives none.
func TestMCPUnpinnedLaunch(t *testing.T) {
	seen := now.UTC().Format("2006-01-02T15:04:05Z07:00")
	client := graph.Node{ID: mcpClient, Kind: graph.KindAgent, Type: graph.AgentAIClient, Name: "Claude Code", Platform: knowledge.ClaudeCodeProduct, ConfigPath: home + "/.claude", DiscoveredAt: seen}
	srv := func(id, name string, u *graph.UnpinnedLaunch) graph.Node {
		return graph.Node{ID: id, Kind: graph.KindAgent, Type: graph.AgentMCPServer, Name: name, Platform: knowledge.ClaudeCodeProduct, ConfigPath: home + "/.claude.json", DiscoveredAt: seen, Unpinned: u}
	}
	load := func(to, scope, approval string) graph.Edge {
		return graph.Edge{ID: "loads_" + to, Type: graph.EdgeLoads, From: mcpClient, To: to, Source: home + "/.claude.json", Certainty: graph.CertaintyObserved, AppliesIn: scope, ApprovalState: approval}
	}
	g := graph.Graph{
		Nodes: []graph.Node{
			client,
			srv("mcp_gh", "github", &graph.UnpinnedLaunch{Launcher: "npx", Package: "@modelcontextprotocol/server-github"}),
			srv("mcp_fetch", "fetch", &graph.UnpinnedLaunch{Launcher: "docker run", Package: "mcp/fetch"}),
			srv("mcp_off", "off", &graph.UnpinnedLaunch{Launcher: "uvx", Package: "tool"}),
			srv("mcp_pinned", "pinned", nil),
		},
		Edges: []graph.Edge{
			load("mcp_gh", "user", ""), load("mcp_fetch", "project:"+home+"/repo", graph.ApprovalApproved),
			load("mcp_off", "project:"+home+"/repo", graph.ApprovalRejected), load("mcp_pinned", "user", ""),
		},
	}
	fs := byRule(supplyEval(t, g, nil), rules.RuleMCPUnpinnedLaunch)
	got := map[string]rules.Finding{}
	for _, f := range fs {
		got[f.Params.Server] = f
	}
	if len(fs) != 2 {
		t.Fatalf("findings %+v", fs)
	}
	if f := got["github"]; f.Severity != rules.SeverityYellow || f.Radius != rules.RadiusAnyProject || f.Params.Launcher != "npx" ||
		f.Params.Package != "@modelcontextprotocol/server-github" || f.Params.Agent != "Claude Code" {
		t.Errorf("github %+v", f)
	}
	if f := got["fetch"]; f.Radius != rules.RadiusSameProject || f.Params.Launcher != "docker run" || f.Params.Package != "mcp/fetch" {
		t.Errorf("fetch %+v", f)
	}
}

// BV-274: enableAllProjectMcpServers: true in the user's settings and in a
// trusted project's gives a finding each, with the file as its {scope}.
func TestMCPProjectAutoApprove(t *testing.T) {
	seen := now.UTC().Format("2006-01-02T15:04:05Z07:00")
	g := graph.Graph{Nodes: []graph.Node{{ID: mcpClient, Kind: graph.KindAgent, Type: graph.AgentAIClient, Name: "Claude Code", Platform: knowledge.ClaudeCodeProduct, ConfigPath: home + "/.claude", DiscoveredAt: seen}}}
	yes := true
	settings := &platform.ClaudeSettingsScan{
		Trust: []platform.ProjectTrust{{Path: home + "/repo", Accepted: true}},
		Files: []platform.ClaudeSettingsFile{
			{Scope: platform.MCPScopeUser, ConfigPath: home + "/.claude/settings.json", Status: platform.MCPSourceRead, EnableAllProjectMcpServers: &yes},
			{Scope: platform.MCPScopeProject, ConfigPath: home + "/repo/.claude/settings.json", ScopePath: home + "/repo", Status: platform.MCPSourceRead, EnableAllProjectMcpServers: &yes},
		},
	}
	fs := byRule(supplyEval(t, g, settings), rules.RuleMCPProjectAutoApprove)
	if len(fs) != 2 {
		t.Fatalf("findings %+v", fs)
	}
	scopes := map[string]rules.Finding{}
	for _, f := range fs {
		scopes[f.Params.Scope] = f
		if f.Severity != rules.SeverityYellow || f.Evidence.Nodes[0] != mcpClient || f.Evidence.Locations[0] != f.Params.Scope {
			t.Errorf("%+v", f)
		}
	}
	if scopes[home+"/.claude/settings.json"].Radius != rules.RadiusAnyProject || scopes[home+"/repo/.claude/settings.json"].Radius != rules.RadiusSameProject {
		t.Errorf("radius %+v", scopes)
	}
	if fs := byRule(supplyEval(t, g, nil), rules.RuleMCPProjectAutoApprove); len(fs) != 0 {
		t.Errorf("no settings read: %+v", fs)
	}
}
