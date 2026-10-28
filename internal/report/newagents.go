package report

import (
	"strings"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/memory"
)

// mcpDestText is a server's destination for a report line (BV-91, D4): "sends
// to {host}" for a remote server, "runs {command} on this computer" for a local
// one, empty when unknown. The same fragment serves the HTML and the Markdown.
func mcpDestText(cat catalog.Catalog, s MCPServerLine) string {
	switch {
	case s.Remote != "":
		return cat.Text("html.mcp.dest.remote", map[string]string{"host": s.Remote})
	case s.Local != "":
		return cat.Text("html.mcp.dest.local", map[string]string{"command": s.Local})
	default:
		return ""
	}
}

// mcpParams are the {name}, {agent} and {destination} of a new or changed MCP
// server line.
func mcpParams(cat catalog.Catalog, s MCPServerLine) map[string]string {
	return map[string]string{"name": s.Name, "agent": s.Agent, "destination": mcpDestText(cat, s)}
}

// MCPServerLine is a new or changed MCP server for the «since last time»
// column (BV-91, D4): its name, the client that loads it, and where it sends
// data — a remote host, or the local command it runs. Remote and Local are
// exclusive; both empty when the destination is unknown.
type MCPServerLine struct {
	Name   string
	Agent  string
	Remote string
	Local  string
}

// mcpServerLines pairs each memory MCP reference (by comparison key) with its
// server node in the graph, and reads the client that loads it and its
// destination (the sends_to origin's host, or the local command with the user
// folder shown as ~). A reference with no matching node keeps only its name.
func mcpServerLines(g graph.Graph, refs []memory.MCPRef, home string) []MCPServerLine {
	if len(refs) == 0 {
		return nil
	}
	byID := map[string]graph.Node{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	scope := map[string]string{}
	loadsFrom := map[string]string{}
	origin := map[string]string{}
	for _, e := range g.Edges {
		switch e.Type {
		case graph.EdgeLoads:
			scope[e.To] = e.AppliesIn
			loadsFrom[e.To] = e.From
		case graph.EdgeSendsTo:
			origin[e.From] = byID[e.To].Path
		}
	}
	byKey := map[string]graph.Node{}
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentMCPServer {
			byKey["mcp\x00"+n.Platform+"\x00"+n.Name+"\x00"+scope[n.ID]] = n
		}
	}
	out := make([]MCPServerLine, 0, len(refs))
	for _, r := range refs {
		n, ok := byKey[r.Key]
		if !ok {
			out = append(out, MCPServerLine{Name: r.Name})
			continue
		}
		line := MCPServerLine{Name: n.Name}
		if cid, ok := loadsFrom[n.ID]; ok {
			line.Agent = byID[cid].Name
		}
		if o, ok := origin[n.ID]; ok {
			line.Remote = originHost(o)
		} else if n.Command != "" {
			line.Local = userPath(n.Command, home, "~")
		}
		out = append(out, line)
	}
	return out
}

// originHost is the host (and non-default port) of a scheme://host[:port]
// origin, without the scheme, so the HTML never carries "https://" (D4, and so
// no_external_in_html_test keeps passing).
func originHost(origin string) string {
	if i := strings.Index(origin, "://"); i >= 0 {
		return origin[i+3:]
	}
	return origin
}

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
			out = append(out, memory.SeenAgent{
				Key: "mcp\x00" + n.Platform + "\x00" + n.Name + "\x00" + scope[n.ID],
				MCP: true, Name: n.Name, Dest: n.DestinationRef, // BV-91, D3
			})
		}
	}
	return out
}
