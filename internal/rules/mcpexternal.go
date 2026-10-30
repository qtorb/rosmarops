package rules

import (
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
)

// mcp_external_action (BV-117/BV-121, AP-10 Anexo 1): an evaluated agent is set
// up to use a known MCP server that acts outside the machine (move money,
// change infrastructure or a database, send messages, write documents…). The
// server was recognised by the build-time catalog pass (graph.ExternalAction);
// here the rule turns each (agent, server) into a finding or, when the agent
// asks a person first (except money) or the server is read-only or denied, into
// no finding but still a line of agents_overview.services.

// RuleMCPExternalAction is the rule code.
const RuleMCPExternalAction = "mcp_external_action"

// Catalog categories used by the rule and the index weight (aliases of the
// knowledge catalog's, so callers of the rules layer need not import it).
const (
	CategoryMoney          = knowledge.CategoryMoney
	CategoryInfrastructure = knowledge.CategoryInfrastructure
	CategoryDatabase       = knowledge.CategoryDatabase
)

// MCP dispositions (who stands between the agent and a write tool of the
// server): the values of agents_overview.services[].approval.
const (
	MCPApprovalNobody       = "nobody"        // used without asking
	MCPApprovalAnotherModel = "another_model" // another model decides (Hermes smart)
	MCPApprovalPerson       = "person"        // asks a person first
	MCPApprovalDeny         = "deny"          // the agent's permission denies it
)

// mcpExternal builds the mcp_external_action findings and the per-agent
// services list of agents_overview. It evaluates only the agents RosmarOps
// scores (Claude Code, Cursor, Hermes Agent); a catalog server loaded by a
// detected-only agent still shows in the report's MCP destinations, but not
// here (its disposition is not read). One finding per (agent, server); the
// weight is the index's.
func mcpExternal(in Input, nodes map[string]graph.Node, view func(string) *agentView) ([]Finding, map[string][]OverviewService) {
	g := in.Graph
	services := map[string][]OverviewService{}
	var findings []Finding

	// loads edges to each catalog mcp_server, sorted for determinism.
	type load struct {
		edge   graph.Edge
		server graph.Node
	}
	var loads []load
	for _, e := range g.Edges {
		if e.Type != graph.EdgeLoads {
			continue
		}
		srv, ok := nodes[e.To]
		if !ok || srv.ExternalAction == nil {
			continue
		}
		loads = append(loads, load{e, srv})
	}
	sort.Slice(loads, func(i, j int) bool { return loads[i].edge.ID < loads[j].edge.ID })

	for _, l := range loads {
		client, ok := nodes[l.edge.From]
		if !ok || client.Kind != graph.KindAgent || client.Type != graph.AgentAIClient {
			continue
		}
		disp := mcpDisposition(view(client.ID), l.server.Name)
		if disp == "" {
			continue // not a scored product: no finding, no services line
		}
		ea := l.server.ExternalAction
		scope := mcpScope(l.edge.AppliesIn)
		var findingID *string
		if f, ok := mcpFinding(in, client, l.server, l.edge, disp, scope); ok {
			findings = append(findings, f)
			id := f.ID
			findingID = &id
		}
		access := "writes"
		if ea.ReadOnly {
			access = "read_only"
		}
		services[client.ID] = append(services[client.ID], OverviewService{
			Service: ea.Service, Category: ea.Category, Access: access,
			Approval: disp, Scope: scope, FindingID: findingID,
		})
	}
	for id := range services {
		s := services[id]
		sort.SliceStable(s, func(i, j int) bool {
			if s[i].Service != s[j].Service {
				return s[i].Service < s[j].Service
			}
			return s[i].Scope < s[j].Scope
		})
		services[id] = s
	}
	return findings, services
}

// mcpScope maps a loads edge's applies_in to the service scope (user | project)
// and the finding radius: user-level servers reach every project.
func mcpScope(appliesIn string) string {
	if appliesIn == "user" {
		return "user"
	}
	return "project"
}

// mcpFinding builds the finding for one (agent, server), or reports ok=false
// when the configuration gives none: a read-only or denied server, or an agent
// that asks a person first for anything but money.
func mcpFinding(in Input, client, server graph.Node, edge graph.Edge, disp, scope string) (Finding, bool) {
	ea := server.ExternalAction
	if ea.ReadOnly || disp == MCPApprovalDeny {
		return Finding{}, false
	}
	asking := disp == MCPApprovalPerson
	sev := mcpSeverity(ea.Category, asking)
	if sev == "" {
		return Finding{}, false // non-money category, asking a person: no finding
	}
	radius := RadiusSameProject
	if scope == "user" {
		radius = RadiusAnyProject
	}
	confidence := ConfidenceHigh
	assumptions := []string{}
	if disp == MCPApprovalAnotherModel {
		confidence = ConfidenceMedium
		assumptions = []string{AssumeAuxModelPermits}
	}
	locations := mcpLocations(edge, server)
	f := Finding{
		ID:          findingID(RuleMCPExternalAction, client.ID, server.ID),
		Rule:        RuleMCPExternalAction,
		RuleVersion: RuleVersion,
		Severity:    sev,
		Confidence:  confidence,
		Radius:      radius,
		Params: Params{
			Agent:       client.Name,
			Service:     ea.Service,
			Category:    ea.Category,
			Disposition: disp,
			MCPID:       ea.ID,
			VendorNote:  mcpVendorNotes[ea.ID],
			Count:       1,
		},
		Evidence: Evidence{
			Nodes:       []string{client.ID, server.ID},
			Edges:       []string{edge.ID},
			Locations:   locations,
			Assumptions: assumptions,
		},
		Group:   Group{Count: 1, Members: []MemberGroup{}},
		Actions: []Action{},
	}
	return f, true
}

// mcpSeverity is the finding severity for a category and whether the agent
// asks a person first (AP-10 Anexo 1, «Gravedad y peso»): money is red without
// a person and yellow asking; infrastructure and database are red without a
// person and no finding asking; every other class is yellow without a person
// and no finding asking. "" means no finding.
func mcpSeverity(category string, asking bool) string {
	switch category {
	case knowledge.CategoryMoney:
		if asking {
			return SeverityYellow
		}
		return SeverityRed
	}
	if asking {
		return ""
	}
	switch category {
	case knowledge.CategoryInfrastructure, knowledge.CategoryDatabase:
		return SeverityRed
	default:
		return SeverityYellow
	}
}

// mcpLocations are the finding's evidence locations: the server's config file.
func mcpLocations(edge graph.Edge, server graph.Node) []string {
	if edge.Source != "" {
		return []string{edge.Source}
	}
	if server.ConfigPath != "" {
		return []string{server.ConfigPath}
	}
	return []string{}
}

// mcpVendorNotes marks the catalog ids whose finding adds the provider's own
// confirmation note (AP-10 Anexo 1). The note text is a catalog message keyed
// by the id (finding.mcp_external_action.vendor.<id>).
var mcpVendorNotes = map[string]bool{
	"stripe":           true,
	"shopify_checkout": true,
	"hubspot":          true,
	"supabase":         true,
}

// MCPVendorNoteIDs are the catalog ids whose finding carries a provider note,
// sorted; the report registers and renders a note text per id.
func MCPVendorNoteIDs() []string {
	ids := make([]string, 0, len(mcpVendorNotes))
	for id := range mcpVendorNotes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// mcpDisposition is who stands between the agent and a write tool of the named
// server, for the agents RosmarOps scores; "" for any other product.
func mcpDisposition(v *agentView, serverName string) string {
	if v == nil {
		return ""
	}
	switch v.node.Platform {
	case knowledge.ClaudeCodeProduct:
		return claudeMCPDisposition(v, serverName)
	case knowledge.CursorProduct:
		return cursorMCPDisposition(v, serverName)
	case knowledge.HermesProduct:
		return hermesMCPDisposition(v)
	}
	return ""
}

// claudeMCPDisposition reads Claude Code's invoke_mcp_tool rules
// (permissions.go): a server-wide deny (mcp__<server> or mcp__<server>__*) wins,
// then a server-wide allow; otherwise the effective mode decides
// (bypassPermissions never asks, dontAsk blocks, anything else asks).
func claudeMCPDisposition(v *agentView, serverName string) string {
	allow, deny := false, false
	for _, c := range v.inForce {
		if c.Type != graph.CapInvokeMCPTool || !c.InForce {
			continue
		}
		if !mcpRuleCoversServer(c.TargetScope, serverName) {
			continue
		}
		switch c.Approval {
		case graph.ApprovalDeny:
			deny = true
		case graph.ApprovalAllow:
			allow = true
		}
	}
	switch {
	case deny:
		return MCPApprovalDeny
	case allow:
		return MCPApprovalNobody
	case v.mode == modeBypass:
		return MCPApprovalNobody
	case v.dontAsk():
		return MCPApprovalDeny
	default:
		return MCPApprovalPerson
	}
}

// mcpRuleCoversServer reports whether a Claude Code rule covers every tool of
// the server: mcp__<server> or mcp__<server>__* (a single-tool rule such as
// mcp__<server>__pay is not server-wide: it does not tell whether the write
// tools ask).
func mcpRuleCoversServer(rule, server string) bool {
	rule = strings.TrimSpace(rule)
	if !strings.EqualFold(rule, "mcp__"+server) && !strings.EqualFold(rule, "mcp__"+server+"__*") {
		return false
	}
	return true
}

// cursorMCPDisposition reads Cursor's mcpAllowlist: an entry that covers the
// whole server (server:* , the bare server name, or *:*) means it is used
// without asking; otherwise Cursor asks.
func cursorMCPDisposition(v *agentView, serverName string) string {
	for _, c := range v.inForce {
		if c.Type != graph.CapInvokeMCPTool || c.Approval != graph.ApprovalAllow || !c.InForce {
			continue
		}
		if cursorAllowlistCoversServer(c.TargetScope, serverName) {
			return MCPApprovalNobody
		}
	}
	return MCPApprovalPerson
}

// cursorAllowlistCoversServer reports whether a Cursor mcpAllowlist entry
// (server[:tool], with * wildcards) covers every tool of the server.
func cursorAllowlistCoversServer(entry, server string) bool {
	entry = strings.TrimSpace(entry)
	left, right, hasColon := strings.Cut(entry, ":")
	if !(strings.EqualFold(left, server) || left == "*") {
		return false
	}
	return !hasColon || right == "" || right == "*"
}

// hermesMCPDisposition maps Hermes Agent's approvals.mode (smart | manual |
// off; smart when absent) to the disposition: smart lets the auxiliary model
// decide, manual asks you, off never asks.
func hermesMCPDisposition(v *agentView) string {
	mode := "smart"
	for _, c := range v.inForce {
		if c.Type == graph.CapPermissionMode {
			mode = c.TargetScope
		}
	}
	switch mode {
	case "off":
		return MCPApprovalNobody
	case "manual":
		return MCPApprovalPerson
	default:
		return MCPApprovalAnotherModel
	}
}
