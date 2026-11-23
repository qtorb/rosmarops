package rules

import (
	"github.com/qtorb/rosmarops/internal/graph"
)

// Claude Desktop's six rows (BV-277). Outside Cowork, which is not read and
// not scored, the app reads, writes, runs and sends only through MCP tools:
// the servers it loads and its desktop extensions (assumed active: their
// settings files are never opened). Its «Always allow» approvals live in the
// app's databases, so the least controlled case is assumed: with at least one
// such route nobody decides, on the whole computer (what a tool does inside
// is out of scope), unless the managed policy turns persistent approvals off
// and the person decides; with none, it does nothing of the kind. Every cell
// rests on ui_mode_choice_unknown.

func (oi overviewInputs) claudeDesktopCells(v *agentView) []OverviewCell {
	routes := oi.claudeDesktopRoutes(v)
	decider, origin := DeciderYou, OriginNone
	if routes {
		decider, origin = openCodeDecider(claudeDesktopMCPCap(v))
	}
	assume := sortedUnique([]string{graph.AssumeUIModeChoiceUnknown})
	row := func(r, scope string) OverviewCell {
		if !routes {
			scope = ScopeNone
		}
		return OverviewCell{Row: r, Decider: decider, Scope: scope, Origin: origin, Assumed: routes, Data: OverviewData{Assumptions: assume}}
	}
	reads := row(RowReadsFiles, ScopeMachine)
	if routes {
		creds, high := 0, 0
		for cred := range oi.reaches[v.node.ID] {
			creds++
			if classOf(oi.nodes[cred], oi.scopes[cred]) == ClassHigh {
				high++
			}
		}
		reads.Data.CredentialsReachable, reads.Data.CredentialsReachableHigh = intp(creds), intp(high)
		reads.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleCredReadAndSend, RuleCredReadable}, "", "")
	}
	sends := row(RowSendsOut, ScopeAnyDestination)
	sends.Data.Excluded = []string{ExcludedDocumentedNetworkExceptions}
	return []OverviewCell{
		reads,
		row(RowWritesFiles, ScopeMachine),
		row(RowRunsCommands, ScopeMachine),
		sends,
		oi.geminiChangesCell(v),
		oi.providerCell(v),
	}
}

// claudeDesktopMCPCap is the app's in-force capability to call MCP tools.
func claudeDesktopMCPCap(v *agentView) *graph.Node {
	for i := range v.inForce {
		if c := &v.inForce[i]; c.Type == graph.CapInvokeMCPTool {
			return c
		}
	}
	return nil
}

// claudeDesktopRoutes reports whether the app has a way to act on the
// computer: an MCP server it loads, or a desktop extension the managed
// policy does not turn off.
func (oi overviewInputs) claudeDesktopRoutes(v *agentView) bool {
	for _, e := range oi.in.Graph.Edges {
		if e.Type == graph.EdgeLoads && e.From == v.node.ID && e.ApprovalState != graph.ApprovalRejected {
			return true
		}
	}
	s := oi.in.ClaudeDesktop
	off := s.Managed.Extensions != nil && !*s.Managed.Extensions
	return len(s.Extensions) > 0 && !off
}
