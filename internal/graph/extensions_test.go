package graph_test

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Decision 2026-10-26 (agentes y MCP nuevos, A3): an extension of
// knowledge.AgentExtensions is an extension agent whose id is stable
// between runs and across versions; any other extension gives no node. The
// graph validates.
func TestAddExtensions(t *testing.T) {
	at := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	build := func(exts ...platform.VSCodeExtension) graph.Graph {
		f := graph.AddExtensions(graph.Fragment{}, exts, "/h/.vscode/extensions", at)
		g := graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: "k", HostID: "host_x"}, at)
		if _, err := graph.Encode(g); err != nil {
			t.Fatal(err)
		}
		return g
	}
	g1 := build(platform.VSCodeExtension{ID: "GitHub.Copilot", Version: "1.300.0"}, platform.VSCodeExtension{ID: "ms-python.python", Version: "2026.4.0"})
	g2 := build(platform.VSCodeExtension{ID: "github.copilot", Version: "1.301.0"})
	if len(g1.Nodes) != 1 || len(g2.Nodes) != 1 {
		t.Fatalf("nodes %+v / %+v", g1.Nodes, g2.Nodes)
	}
	n := g1.Nodes[0]
	if n.ID != g2.Nodes[0].ID || !strings.HasPrefix(n.ID, "agent_ext_") || n.ID != graph.ExtensionID("vscode", "github.copilot") {
		t.Errorf("id %s vs %s", n.ID, g2.Nodes[0].ID)
	}
	if n.Kind != graph.KindAgent || n.Type != graph.AgentExtension || n.Name != "GitHub Copilot" || n.Platform != "github-copilot" || n.ConfigPath != "/h/.vscode/extensions" {
		t.Errorf("node %+v", n)
	}
}
