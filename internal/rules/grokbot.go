package rules

import (
	"github.com/qtorb/rosmarops/internal/declared"
	"github.com/qtorb/rosmarops/internal/graph"
)

// KindSourceDeclared is a risk kind of Grok Bot's cloud work, from the
// person's answers about the account (BV-248, CTO-56 §2.3; report.v2
// riskKindCell.source declared, Albert 16-11).
const KindSourceDeclared = "declared"

// grokBotLocal is Grok Bot's «Execution on this computer» from its declared
// shell capability: its approval and the capability.
func (v *agentView) grokBotLocal() (approval string, c *graph.Node) {
	for i := range v.inForce {
		if v.inForce[i].Type == graph.CapExecuteShell {
			return v.inForce[i].Approval, &v.inForce[i]
		}
	}
	return graph.ApprovalAsk, nil
}

// grokBotCells (CTO-56 §2.3, B1): on this computer, Grok Bot reads, writes,
// runs and sends only through a command. Always allow: no one, the whole
// computer, any destination; Ask every time: you; Never allow: blocked.
func (oi overviewInputs) grokBotCells(v *agentView) []OverviewCell {
	approval, c := v.grokBotLocal()
	decider, scope := DeciderYou, ScopeMachine
	switch approval {
	case graph.ApprovalAllow:
		decider = DeciderNobody
	case graph.ApprovalDeny:
		decider, scope = DeciderBlocked, ScopeNone
	}
	var basis, assumptions []string
	if c != nil {
		basis = []string{c.Basis}
		if d := graph.GrokBotAssumption(*c); d != "" {
			assumptions = []string{d}
		}
	}
	cell := func(row, sc string) OverviewCell {
		out := OverviewCell{Row: row, Decider: decider, Scope: sc, Origin: originOf(basis), Assumed: len(assumptions) > 0}
		out.Data.Assumptions = sortedUniqueOrNil(assumptions)
		return out
	}
	sendScope := ScopeAnyDestination
	if scope == ScopeNone {
		sendScope = ScopeNone
	}
	reads := cell(RowReadsFiles, scope)
	creds, high := 0, 0
	for cred := range oi.reaches[v.node.ID] {
		creds++
		if classOf(oi.nodes[cred], oi.scopes[cred]) == ClassHigh {
			high++
		}
	}
	reads.Data.Projects = intp(len(oi.in.RepoRoots))
	reads.Data.CredentialsReachable, reads.Data.CredentialsReachableHigh = intp(creds), intp(high)
	writes := cell(RowWritesFiles, scope)
	writes.Data.Projects, writes.Data.EditAllowRules = intp(len(oi.in.RepoRoots)), intp(0)
	runs := cell(RowRunsCommands, scope)
	runs.Data.PreapprovedRules, runs.Data.LauncherRules = intp(0), intp(0)
	sends := cell(RowSendsOut, sendScope)
	if decider == DeciderNobody {
		reads.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleCredReadAndSend, RuleCredReadable}, "", "")
		if len(reads.Data.FindingIDs) == 0 {
			r := NoFindingNoTargets
			reads.NoFindingReason = &r
		}
		r := NoFindingNotAFinding
		writes.NoFindingReason = &r
		f := NoFindingFeedsOtherFindings
		runs.NoFindingReason = &f
		sends.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleCredReadAndSend}, "", "")
		if len(oi.reaches[v.node.ID]) == 0 {
			n := NoFindingNoTargets
			sends.NoFindingReason = &n
		}
	}
	return []OverviewCell{reads, writes, runs, sends, oi.grokBotChangesCell(v), oi.providerCell(v)}
}

// grokBotChangesCell: Grok Bot's writes to another agent's resources, through
// its declared commands (Always allow); you, scope none, without one.
func (oi overviewInputs) grokBotChangesCell(v *agentView) OverviewCell {
	var cs []candidate
	var targets []string
	for _, e := range oi.foreign[v.node.ID] {
		w := v.edgeWay(e, oi.nodes, v.base)
		r := oi.nodes[e.To]
		scope := ScopeInstructions
		if r.Type == graph.ResourceAgentConfig || r.Type == graph.ResourceAgentExecutable {
			scope = ScopeConfig
		}
		targets = append(targets, r.Configures)
		cs = append(cs, candidate{decider: DeciderNobody, scope: scope, code: w.code, cap: w.cap, basis: oi.basisOf(w.cap), assumptions: w.assumptions})
	}
	if len(cs) == 0 {
		return OverviewCell{Row: RowChangesOtherAgents, Decider: DeciderYou, Scope: ScopeNone, Origin: OriginNone}
	}
	cell := pick(RowChangesOtherAgents, cs)
	cell.Data.TargetAgents = sortedUnique(targets)
	cell.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleForeignAgentConfigWritable, RuleForeignAgentInstructionsWritable}, "", "")
	return cell
}

// grokBotCloudKinds (CTO-56 §2.3, B2–B4): the money and sends_out kinds of
// Grok Bot's cloud work, the worse of them and of this computer's. With
// Auto-review on and «Ask first» rules for the four actions, another model
// decides (reviewer_model_permits); otherwise no one, except what Auto-review
// stops. Without tools that send or publish (B4 = no), sends_out from the
// cloud is none_known. Unanswered: the least controlled case (§4).
func (oi overviewInputs) grokBotCloudKinds(kinds []RiskKindCell) []RiskKindCell {
	st := declared.ResolveGrokBot(graph.GrokBotFacts(oi.in.GrokBot), declared.Clean(declared.GrokBot, oi.in.Declared[declared.GrokBot]))
	if !st.Present {
		return kinds
	}
	cloud := RiskKindCell{State: StateNobody, Scope: KindScopeAnyDestination, Assumed: true, Source: KindSourceDeclared}
	if st.Answers[declared.B2AutoReview].Code == declared.On && st.Answers[declared.B3AskFirst].Code == declared.AskFirstAll {
		cloud.State = StateModel
	}
	for i, k := range kinds {
		c := cloud
		c.Kind = k.Kind
		switch k.Kind {
		case KindMoney:
			kinds[i] = cloudWorse(k, c)
		case KindSendsOut:
			if st.Answers[declared.B4SendTools].Code == declared.No {
				c.State, c.Scope, c.Assumed = StateNoneKnown, KindScopeNone, true
			}
			kinds[i] = cloudWorse(k, c)
		}
	}
	return kinds
}

// cloudWorse is the worse of this computer's kind and the cloud's; an unknown
// kind on this computer gives way to the cloud's.
func cloudWorse(local, cloud RiskKindCell) RiskKindCell {
	if local.State == StateUnknown || local.State == StateNoneKnown {
		return cloud
	}
	return worse(local, cloud)
}
