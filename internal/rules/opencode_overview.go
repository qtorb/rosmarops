package rules

import (
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

// The OpenCode family's six rows (BV-276): OpenCode and Kilo Code. Each row
// is decided by the user scope's capability of its category (a project's
// own files change it only there): allow is nobody, ask is you, deny is
// blocked. By default OpenCode reads, edits, runs commands, reaches the
// network and calls MCP tools without asking, and asks before reading
// outside the project; Kilo Code asks before commands. Every cell rests on
// launch_flags_not_readable and ui_mode_choice_unknown: an option given at
// start, an environment variable or a choice made in the session is not on
// disk.

// openCodeCap is the agent's in-force user-scope capability of a type and
// target scope; nil when there is none.
func openCodeCap(v *agentView, typ, scope string) *graph.Node {
	for i := range v.inForce {
		c := &v.inForce[i]
		if c.Type == typ && c.TargetScope == scope && c.AppliesIn == platform.MCPScopeUser {
			return c
		}
	}
	return nil
}

// openCodeDecider is who decides for a capability, and where that came from.
func openCodeDecider(c *graph.Node) (decider, origin string) {
	if c == nil {
		return DeciderYou, OriginProductDefault
	}
	origin = OriginProductDefault
	if c.Basis == graph.BasisConfigured {
		origin = OriginConfigured
	}
	switch c.Approval {
	case graph.ApprovalAllow:
		return DeciderNobody, origin
	case graph.ApprovalDeny:
		return DeciderBlocked, origin
	}
	return DeciderYou, origin
}

func (oi overviewInputs) openCodeCells(v *agentView) []OverviewCell {
	return []OverviewCell{
		oi.openCodeReadsCell(v),
		oi.openCodeWritesCell(v),
		oi.openCodeRunsCell(v),
		oi.openCodeSendsCell(v),
		oi.geminiChangesCell(v), // the same: what it writes of other agents
		oi.providerCell(v),
	}
}

func (oi overviewInputs) openCodeReadsCell(v *agentView) OverviewCell {
	decider, origin := openCodeDecider(openCodeCap(v, graph.CapReadFiles, "workspace"))
	scope := ScopeSameProject
	if outside := openCodeCap(v, graph.CapReadFiles, "*"); outside != nil && outside.Approval == graph.ApprovalAllow {
		decider, origin = openCodeDecider(outside)
		scope = ScopeMachine
	}
	cell := OverviewCell{Row: RowReadsFiles, Decider: decider, Scope: scope, Origin: origin}
	creds, high := 0, 0
	for cred := range oi.reaches[v.node.ID] {
		creds++
		if classOf(oi.nodes[cred], oi.scopes[cred]) == ClassHigh {
			high++
		}
	}
	cell.Data.Projects = intp(len(oi.in.RepoRoots))
	cell.Data.CredentialsReachable, cell.Data.CredentialsReachableHigh = intp(creds), intp(high)
	cell.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleCredReadAndSend, RuleCredReadable}, "", "")
	cell.Data.Assumptions = geminiBaseAssumptions()
	if also, ok := terminalReadAlso(v, DeciderNobody); ok {
		cell.Also = also
	}
	if decider == DeciderNobody && len(cell.Data.FindingIDs) == 0 {
		r := NoFindingNoTargets
		cell.NoFindingReason = &r
	}
	return cell
}

func (oi overviewInputs) openCodeWritesCell(v *agentView) OverviewCell {
	decider, origin := openCodeDecider(openCodeCap(v, graph.CapWriteFiles, "workspace"))
	cell := OverviewCell{Row: RowWritesFiles, Decider: decider, Scope: ScopeSameProject, Origin: origin}
	cell.Data.Projects = intp(len(oi.in.RepoRoots))
	cell.Data.Assumptions = geminiBaseAssumptions()
	if decider == DeciderNobody {
		r := NoFindingNotAFinding
		cell.NoFindingReason = &r
	}
	return cell
}

func (oi overviewInputs) openCodeRunsCell(v *agentView) OverviewCell {
	decider, origin := openCodeDecider(openCodeCap(v, graph.CapExecuteShell, "*"))
	scope := ScopeNone
	if decider == DeciderNobody {
		scope = ScopeMachine
	}
	cell := OverviewCell{Row: RowRunsCommands, Decider: decider, Scope: scope, Origin: origin}
	cell.Data.Assumptions = geminiBaseAssumptions()
	if decider == DeciderNobody {
		r := NoFindingFeedsOtherFindings
		cell.NoFindingReason = &r
	}
	return cell
}

func (oi overviewInputs) openCodeSendsCell(v *agentView) OverviewCell {
	decider, origin := openCodeDecider(openCodeCap(v, graph.CapNetwork, "any_destination"))
	cell := OverviewCell{Row: RowSendsOut, Decider: decider, Scope: ScopeAnyDestination, Origin: origin}
	if decider == DeciderNobody {
		cell.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleCredReadAndSend}, "", "")
		if len(cell.Data.FindingIDs) == 0 {
			r := NoFindingNoTargets
			cell.NoFindingReason = &r
		}
	}
	cell.Data.Assumptions = geminiBaseAssumptions()
	cell.Data.Excluded = []string{ExcludedDocumentedNetworkExceptions}
	return cell
}
