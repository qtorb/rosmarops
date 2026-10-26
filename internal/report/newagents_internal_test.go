package report

import (
	"reflect"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/memory"
)

// Decision 2026-10-26 (agentes y MCP nuevos, A1): an MCP server is compared
// by client, name and scope, not by its node id: moved to another file of
// the same scope it is not new; moved from user to project it is.
func TestSeenAgents_MCPScope(t *testing.T) {
	salt := []byte("0123456789abcdef0123456789abcdef")
	g := func(serverID, appliesIn string) graph.Graph {
		return graph.Graph{
			Nodes: []graph.Node{
				{ID: "agent_client_cu", Kind: graph.KindAgent, Type: graph.AgentAIClient, Name: "Cursor", Platform: "cursor"},
				{ID: serverID, Kind: graph.KindAgent, Type: graph.AgentMCPServer, Name: "browser", Platform: "cursor"},
			},
			Edges: []graph.Edge{{ID: "edge_loads_" + serverID, Type: graph.EdgeLoads, From: "agent_client_cu", To: serverID, AppliesIn: appliesIn}},
		}
	}
	run := func(day string, gr graph.Graph) memory.Run {
		return memory.Run{Date: day, Report: "r", RulesVersion: "1", KnowledgeVersion: "k", Agents: []string{"Cursor"}, Repos: 1, Complete: true, Seen: seenAgents(gr)}
	}
	m, _, _ := memory.Update(memory.File{}, run("2026-10-01", g("agent_mcp_a", "user")), nil, salt)
	m, _, root := memory.Update(m, run("2026-10-02", g("agent_mcp_b", "user")), nil, salt)
	if root.NewMCP != nil || root.NewAgents != nil {
		t.Errorf("moved within user scope: %v %v", root.NewMCP, root.NewAgents)
	}
	_, _, root = memory.Update(m, run("2026-10-03", g("agent_mcp_c", "project:/src/a")), nil, salt)
	if !reflect.DeepEqual(root.NewMCP, []string{"browser"}) {
		t.Errorf("user → project: %v", root.NewMCP)
	}
}
