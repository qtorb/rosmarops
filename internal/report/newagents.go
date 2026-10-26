package report

import (
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/memory"
)

// seenAgents are the agents of the graph by their comparison key (decision
// 2026-10-26, agentes y MCP nuevos, A1): a client or an extension by its
// node id; an MCP server by client, name and scope (the applies_in of its
// loads edge), never by its node id, which holds the file's path. Moving a
// server to another file of the same scope does not make it new; moving it
// from user to project does.
func seenAgents(g graph.Graph) []memory.SeenAgent {
	scope := map[string]string{}
	for _, e := range g.Edges {
		if e.Type == graph.EdgeLoads {
			scope[e.To] = e.AppliesIn
		}
	}
	var out []memory.SeenAgent
	for _, n := range g.Nodes {
		if n.Kind != graph.KindAgent {
			continue
		}
		switch n.Type {
		case graph.AgentAIClient, graph.AgentExtension:
			out = append(out, memory.SeenAgent{Key: n.ID, Name: n.Name})
		case graph.AgentMCPServer:
			out = append(out, memory.SeenAgent{Key: "mcp\x00" + n.Platform + "\x00" + n.Name + "\x00" + scope[n.ID], MCP: true, Name: n.Name})
		}
	}
	return out
}
