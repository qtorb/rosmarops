package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-175: an AI agent set up with an n8n MCP server, and an n8n folder that
// keeps its credential key beside the database, both yield «to review»
// findings that never change the agent's score.

func n8nEval(t *testing.T, withBridge, withKey bool) rules.Result {
	t.Helper()
	seen := now.UTC().Format("2006-01-02T15:04:05Z07:00")
	nodes := []graph.Node{{
		ID: mcpClient, Kind: graph.KindAgent, Type: graph.AgentAIClient,
		Name: "Claude Code", Platform: knowledge.ClaudeCodeProduct, ConfigPath: home + "/.claude", DiscoveredAt: seen,
	}}
	var edges []graph.Edge
	if withBridge {
		sid := "mcp_n8n"
		nodes = append(nodes, graph.Node{
			ID: sid, Kind: graph.KindAgent, Type: graph.AgentMCPServer, Name: "n8n",
			Platform: knowledge.ClaudeCodeProduct, ConfigPath: home + "/.claude.json", DiscoveredAt: seen,
			N8N: &graph.N8NMatch{Signal: "port"},
		})
		edges = append(edges, graph.Edge{
			ID: "loads_n8n", Type: graph.EdgeLoads, From: mcpClient, To: sid,
			Source: home + "/.claude.json", Certainty: graph.CertaintyObserved, AppliesIn: "user",
		})
	}
	if withKey {
		nodes = append(nodes, graph.Node{
			ID: "res_n8n_folder", Kind: graph.KindResource, Type: graph.ResourceFile,
			Path: home + "/.n8n", DiscoveredAt: seen, N8NKey: true,
		})
	}
	g := graph.Graph{Nodes: nodes, Edges: edges}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	return rules.Evaluate(rules.Input{
		Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: []string{home + "/repo"}, Home: home, Now: now,
	})
}

func findOf(res rules.Result, rule string) (rules.Finding, bool) {
	for _, f := range res.Findings {
		if f.Rule == rule {
			return f, true
		}
	}
	return rules.Finding{}, false
}

func TestN8NBridgeFinding(t *testing.T) {
	res := n8nEval(t, true, false)
	f, ok := findOf(res, rules.RuleN8NMCPBridge)
	if !ok {
		t.Fatal("no n8n_mcp_bridge finding")
	}
	if f.Severity != rules.SeverityYellow {
		t.Errorf("severity: want yellow, got %s", f.Severity)
	}
	if f.Params.Agent != "Claude Code" || f.Params.Server != "n8n" {
		t.Errorf("params: agent=%q server=%q", f.Params.Agent, f.Params.Server)
	}
	// First evidence node is the n8n server, not the evaluated client: the
	// finding stays outside the client's score.
	if f.Evidence.Nodes[0] != "mcp_n8n" {
		t.Errorf("first evidence node: want mcp_n8n, got %s", f.Evidence.Nodes[0])
	}
}

func TestN8NKeyFinding(t *testing.T) {
	res := n8nEval(t, false, true)
	f, ok := findOf(res, rules.RuleN8NKeyWithData)
	if !ok {
		t.Fatal("no n8n_key_with_data finding")
	}
	if f.Severity != rules.SeverityYellow {
		t.Errorf("severity: want yellow, got %s", f.Severity)
	}
	if len(f.Evidence.Locations) == 0 || f.Evidence.Locations[0] != home+"/.n8n" {
		t.Errorf("locations: %v", f.Evidence.Locations)
	}
	if f.Evidence.Nodes[0] != "res_n8n_folder" {
		t.Errorf("first evidence node: want res_n8n_folder, got %s", f.Evidence.Nodes[0])
	}
}

// TestN8NDoesNotChangeScore proves the brief's «sin cambiar la nota»: an agent
// whose only finding is the n8n bridge keeps a perfect, contained score,
// because the finding is attributed to the n8n server, not the agent, and
// weighs 0.
func TestN8NDoesNotChangeScore(t *testing.T) {
	res := n8nEval(t, true, true)
	if _, ok := findOf(res, rules.RuleN8NMCPBridge); !ok {
		t.Fatal("setup: no bridge finding")
	}
	idx := index.Compute(index.Input{
		GraphBuilt: true,
		Agents:     []index.Agent{{ID: mcpClient, Name: "Claude Code", Product: knowledge.ClaudeCodeProduct, ConfigDir: home + "/.claude"}},
		Inference:  map[string]bool{knowledge.ClaudeCodeProduct: true},
		Findings:   res.Findings, Today: res.Today,
		RulesVersion: "t", KnowledgeVersion: "t",
	})
	if len(idx.Agents) != 1 {
		t.Fatalf("want 1 scored agent, got %d", len(idx.Agents))
	}
	a := idx.Agents[0]
	if a.Value == nil || *a.Value != 100 || a.Band != index.BandContained {
		t.Errorf("n8n findings changed the score: value=%v band=%s (want 100/contained)", a.Value, a.Band)
	}
}
