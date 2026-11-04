package rules

import "github.com/qtorb/rosmarops/internal/graph"

// n8n findings (BV-175), both in «To review», neither changing the posture
// score (nota) this version: their first evidence node is not an evaluated
// agent, so they weigh 0 and sit outside every agent's band, like
// package_known_malicious and project_hook_runs_repo_script. They still raise
// the run's overall status to at least yellow (fail-secure).

// Rule codes.
const (
	RuleN8NMCPBridge   = "n8n_mcp_bridge"
	RuleN8NKeyWithData = "n8n_key_with_data"
)

// n8nBridgeFinding is the finding for one AI agent set up to use an n8n
// instance as an MCP server (BV-175): one per (agent, n8n server). The first
// evidence node is the n8n mcp_server, so the finding never enters the agent's
// score. {agent} is the client's name; {server} the MCP server entry's name.
func n8nBridgeFinding(in Input, client, server graph.Node, edge graph.Edge) Finding {
	locations := []string{}
	switch {
	case edge.Source != "":
		locations = []string{edge.Source}
	case server.ConfigPath != "":
		locations = []string{server.ConfigPath}
	}
	// {server} shows the server's own name in the agent's configuration and its
	// host, never the full path, the Authorization header or any token (BV-175,
	// advisor 04-11). The host rides on the in-memory match; empty for a
	// package/stdio n8n, where only the name is shown.
	srvLabel := server.Name
	if server.N8N != nil && server.N8N.Host != "" {
		srvLabel = server.Name + " (" + server.N8N.Host + ")"
	}
	return Finding{
		ID:          findingID(RuleN8NMCPBridge, client.ID, server.ID),
		Rule:        RuleN8NMCPBridge,
		RuleVersion: RuleVersion,
		Severity:    SeverityYellow,
		Confidence:  ConfidenceHigh,
		Params:      Params{Agent: client.Name, Server: srvLabel, Count: 1},
		Evidence: Evidence{
			Nodes: []string{server.ID, client.ID}, Edges: []string{edge.ID},
			Locations: locations, Assumptions: []string{},
		},
		Group:   Group{Count: 1, Members: []MemberGroup{}},
		Actions: []Action{},
	}
}

// n8nKeyFinding is the finding for an n8n user folder that holds the database
// and the credential encryption key together (BV-175). Its first evidence node
// is the folder resource, so it never enters an agent's score. {path} is the
// folder, read from the finding's location; the key's value is never read.
func n8nKeyFinding(in Input, n graph.Node) Finding {
	members := []string{}
	if n.Path != "" {
		members = []string{n.Path}
	}
	return Finding{
		ID:          findingID(RuleN8NKeyWithData, n.ID),
		Rule:        RuleN8NKeyWithData,
		RuleVersion: RuleVersion,
		Severity:    SeverityYellow,
		Confidence:  ConfidenceHigh,
		Params:      Params{Count: 1},
		Evidence:    Evidence{Nodes: []string{n.ID}, Edges: []string{}, Locations: members, Assumptions: []string{}},
		Group:       Group{Count: 1, Members: []MemberGroup{}},
		Actions:     []Action{},
	}
}
