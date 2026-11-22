package rules

import (
	"sort"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
)

// BV-274 (Albert 21-11): two MCP rules.
//   - mcp_unpinned_launch: a local MCP server started by npx, bunx, pnpm dlx,
//     uvx, pipx run or docker run with no exact version (graph.UnpinnedLaunch,
//     in memory only). Its code can change every time it starts. Yellow,
//     weight 6, ×1.5 when the server loads in every project (user scope).
//   - mcp_project_autoapprove: Claude Code with enableAllProjectMcpServers:
//     true in a layer where it takes effect (graph.ProjectAutoApprovals).
//     Yellow, weight 8.
const (
	RuleMCPUnpinnedLaunch     = "mcp_unpinned_launch"
	RuleMCPProjectAutoApprove = "mcp_project_autoapprove"
)

// mcpUnpinnedFindings is one finding per (agent, server) the agent loads
// whose launch is unpinned. Its params carry only the server's name, the
// launcher and the package or image name.
func mcpUnpinnedFindings(in Input, nodes map[string]graph.Node) []Finding {
	var out []Finding
	for _, e := range in.Graph.Edges {
		if !LoadsInForce(e) {
			continue
		}
		server, ok := nodes[e.To]
		if !ok || server.Unpinned == nil {
			continue
		}
		client, ok := nodes[e.From]
		if !ok || client.Kind != graph.KindAgent || client.Type != graph.AgentAIClient {
			continue
		}
		radius := RadiusSameProject
		if mcpScope(e.AppliesIn) == "user" {
			radius = RadiusAnyProject
		}
		locs := mcpLocations(e, server)
		out = append(out, Finding{
			ID: findingID(RuleMCPUnpinnedLaunch, client.ID, server.ID), Rule: RuleMCPUnpinnedLaunch, RuleVersion: RuleVersion,
			Severity: SeverityYellow, Confidence: ConfidenceHigh, Radius: radius,
			Params: Params{
				Agent: client.Name, Server: server.Name, Package: server.Unpinned.Package,
				Launcher: server.Unpinned.Launcher, Count: 1,
			},
			Evidence: Evidence{Nodes: []string{client.ID, server.ID}, Edges: []string{e.ID}, Locations: locs, Assumptions: []string{}},
			Group:    group(in, locs), Actions: []Action{},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// mcpAutoApproveFindings is one finding per settings file whose
// enableAllProjectMcpServers: true takes effect, on the Claude Code agent of
// the user's configuration folder.
func mcpAutoApproveFindings(in Input) []Finding {
	if in.ClaudeSettings == nil {
		return nil
	}
	var client graph.Node
	for _, n := range in.Graph.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient && n.Platform == knowledge.ClaudeCodeProduct {
			client = n
			break
		}
	}
	if client.ID == "" {
		return nil
	}
	var out []Finding
	for _, a := range graph.ProjectAutoApprovals(*in.ClaudeSettings) {
		locs := []string{a.ConfigPath}
		radius := RadiusSameProject
		if a.Scope == "user" || a.Scope == "managed" {
			radius = RadiusAnyProject
		}
		out = append(out, Finding{
			ID: findingID(RuleMCPProjectAutoApprove, client.ID, a.ConfigPath), Rule: RuleMCPProjectAutoApprove, RuleVersion: RuleVersion,
			Severity: SeverityYellow, Confidence: ConfidenceHigh, Radius: radius,
			Params:   Params{Agent: client.Name, Scope: a.ConfigPath, Count: 1},
			Evidence: Evidence{Nodes: []string{client.ID}, Edges: []string{}, Locations: locs, Assumptions: []string{}},
			Group:    group(in, locs), Actions: []Action{},
		})
	}
	return out
}
