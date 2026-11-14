package rules

import (
	"strings"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
)

// Risk kinds of the v1 report (BV-235, R5; CTO-48 §2.2 riskKindCell): five
// per evaluated agent, computed from today's rows, the credential findings and
// the MCP catalog. They never replace the rows.
const (
	KindMoney        = "money"
	KindSendsOut     = "sends_out"
	KindKeys         = "keys"
	KindRunsCommands = "runs_commands"
	KindChangesFiles = "changes_files"
)

// KindOrder is the order of the five kinds in agents_overview[].kinds.
var KindOrder = []string{KindMoney, KindSendsOut, KindKeys, KindRunsCommands, KindChangesFiles}

// Risk kind states.
const (
	StateBlocked   = "blocked"
	StateYou       = "you"
	StateBounded   = "bounded"
	StateModel     = "model"
	StateNobody    = "nobody"
	StateNoneKnown = "none_known"
	StateUnknown   = "unknown"
)

// Risk kind scopes: the row scopes plus listed_commands, a closed list of
// commands that run without asking (CTO-48 §3.7).
const (
	KindScopeMachine        = "machine"
	KindScopeSameProject    = "same_project"
	KindScopeSandbox        = "sandbox"
	KindScopeListedCommands = "listed_commands"
	KindScopeAnyDestination = "any_destination"
	KindScopeNone           = "none"
)

// Risk kind sources.
const (
	KindSourceRow         = "overview_row"
	KindSourceCredentials = "credential_findings"
	KindSourceCatalog     = "mcp_catalog"
)

// AllKindStates and AllKindScopes are the enums of riskKindCell, checked
// against the schema.
var (
	AllKindStates  = []string{StateBlocked, StateYou, StateBounded, StateModel, StateNobody, StateNoneKnown, StateUnknown}
	AllKindScopes  = []string{KindScopeMachine, KindScopeSameProject, KindScopeSandbox, KindScopeListedCommands, KindScopeAnyDestination, KindScopeNone}
	AllKindSources = []string{KindSourceRow, KindSourceCredentials, KindSourceCatalog}
)

// RiskKindCell is one of the five risk kinds of an agent: who decides, how
// far, whether it rests on a premise not readable at rest, and where it comes
// from.
type RiskKindCell struct {
	Kind    string `json:"kind"`
	State   string `json:"state"`
	Scope   string `json:"scope"`
	Assumed bool   `json:"assumed"`
	Source  string `json:"source"`
}

// mcpDispositionRead are the products whose MCP «who decides» RosmarOps reads
// (mcpDisposition); for the others «money» is unknown.
var mcpDispositionRead = map[string]bool{
	knowledge.ClaudeCodeProduct: true, knowledge.CursorProduct: true,
	"codex": true, "grok-build": true, "hermes": true,
}

// stateRank orders the states from the best control to the worst: when two
// sources meet (changes_files, several credentials or payment services) the
// worst one wins. unknown and none_known rank below every measured state.
func stateRank(s string) int {
	switch s {
	case StateBlocked:
		return 2
	case StateYou:
		return 3
	case StateModel:
		return 4
	case StateBounded:
		return 5
	case StateNobody:
		return 6
	case StateNoneKnown:
		return 1
	}
	return 0 // unknown
}

func worse(a, b RiskKindCell) RiskKindCell {
	if stateRank(b.State) > stateRank(a.State) {
		return b
	}
	return a
}

// rowKind is a row cell as a risk kind: its decider as a state (nobody in a
// project or a sandbox is bounded) and its scope.
func rowKind(kind string, c OverviewCell) RiskKindCell {
	k := RiskKindCell{Kind: kind, Scope: KindScopeNone, Assumed: c.Assumed, Source: KindSourceRow}
	switch c.Scope {
	case ScopeMachine, ScopeConfig, ScopeInstructions:
		k.Scope = KindScopeMachine
	case ScopeSameProject:
		k.Scope = KindScopeSameProject
	case ScopeSandbox:
		k.Scope = KindScopeSandbox
	case ScopeAnyDestination, ScopeListedDestinations:
		k.Scope = KindScopeAnyDestination
	}
	switch c.Decider {
	case DeciderBlocked:
		k.State = StateBlocked
	case DeciderYou:
		k.State = StateYou
	case DeciderModel:
		k.State = StateModel
	case DeciderNobody:
		k.State = StateNobody
		// «Acotado» (R5): without asking, but only in the project or a
		// sandbox. Sending out is never bounded in the v1.
		if kind != KindSendsOut && (c.Scope == ScopeSameProject || c.Scope == ScopeSandbox) {
			k.State = StateBounded
		}
	default:
		k.State, k.Scope = StateUnknown, KindScopeNone
	}
	return k
}

func rowOf(a OverviewAgent, row string) (OverviewCell, bool) {
	for _, c := range a.Rows {
		if c.Row == row {
			return c, true
		}
	}
	return OverviewCell{}, false
}

// hiddenAssumption reports an assumption not readable at rest among as.
func hiddenAssumption(as []string) bool {
	for _, a := range as {
		if graph.AssumptionHiddenAtRest(a) {
			return true
		}
	}
	return false
}

// modelDecides reports a way through another model (a classifier, a reviewer
// or an auxiliary model).
func modelDecides(as []string) bool {
	for _, a := range as {
		if a == graph.AssumeAutoClassifierPermits || a == graph.AssumeReviewerModelPermits || a == AssumeAuxModelPermits {
			return true
		}
	}
	return false
}

// kinds are the five risk kinds of the evaluated agent a (CTO-48 §3.5–§3.7).
func (oi overviewInputs) kinds(a OverviewAgent, v *agentView) []RiskKindCell {
	out := make([]RiskKindCell, 0, len(KindOrder))
	out = append(out, oi.moneyKind(a))
	if c, ok := rowOf(a, RowSendsOut); ok {
		out = append(out, rowKind(KindSendsOut, c))
	} else {
		out = append(out, RiskKindCell{Kind: KindSendsOut, State: StateUnknown, Scope: KindScopeNone, Source: KindSourceRow})
	}
	out = append(out, oi.keysKind(a))
	out = append(out, oi.runsKind(a, v))
	ch := RiskKindCell{Kind: KindChangesFiles, State: StateUnknown, Scope: KindScopeNone, Source: KindSourceRow}
	for _, row := range []string{RowWritesFiles, RowChangesOtherAgents} {
		if c, ok := rowOf(a, row); ok {
			ch = worse(ch, rowKind(KindChangesFiles, c))
		}
	}
	return append(out, ch)
}

// moneyKind (§3.6): the «who decides» of the payment services of the catalog
// the agent loads, the worst one; none_known without one; unknown for an agent
// whose MCP «who decides» is not read.
func (oi overviewInputs) moneyKind(a OverviewAgent) RiskKindCell {
	k := RiskKindCell{Kind: KindMoney, State: StateUnknown, Scope: KindScopeNone, Source: KindSourceCatalog}
	if !mcpDispositionRead[a.Product] {
		return k
	}
	k.State = StateNoneKnown
	first := true
	for _, s := range a.Services {
		if s.Category != "money" {
			continue
		}
		c := RiskKindCell{Kind: KindMoney, Scope: KindScopeMachine, Source: KindSourceCatalog}
		if s.Scope == "project" {
			c.Scope = KindScopeSameProject
		}
		switch s.Approval {
		case MCPApprovalDeny:
			c.State = StateBlocked
		case MCPApprovalPerson:
			c.State = StateYou
		case MCPApprovalAnotherModel:
			c.State = StateModel
		default:
			c.State = StateNobody
		}
		if s.FindingID != nil {
			for _, f := range oi.findings {
				if f.ID == *s.FindingID {
					c.Assumed = hiddenAssumption(f.Evidence.Assumptions)
				}
			}
		}
		if first || stateRank(c.State) > stateRank(k.State) {
			k, first = c, false
		}
	}
	return k
}

// keysKind (§3.5): from the agent's cred_read_and_send and cred_readable
// findings: who decides on the winning way, and the radius as the scope
// (same_project is bounded, any_project the machine); without one, the
// reads_files row.
func (oi overviewInputs) keysKind(a OverviewAgent) RiskKindCell {
	var k RiskKindCell
	found := false
	for _, f := range oi.findings {
		if (f.Rule != RuleCredReadAndSend && f.Rule != RuleCredReadable) || len(f.Evidence.Nodes) == 0 || f.Evidence.Nodes[0] != a.AgentID {
			continue
		}
		c := RiskKindCell{
			Kind: KindKeys, State: StateNobody, Scope: KindScopeMachine, Source: KindSourceCredentials,
			Assumed: hiddenAssumption(f.Evidence.Assumptions),
		}
		if modelDecides(f.Evidence.Assumptions) {
			c.State = StateModel
		}
		if f.Radius == RadiusSameProject {
			c.Scope = KindScopeSameProject
			if c.State == StateNobody {
				c.State = StateBounded
			}
		}
		if !found || stateRank(c.State) > stateRank(k.State) || (c.State == k.State && c.Scope == KindScopeMachine) {
			k, found = c, true
		}
	}
	if found {
		return k
	}
	if c, ok := rowOf(a, RowReadsFiles); ok {
		return rowKind(KindKeys, c)
	}
	return RiskKindCell{Kind: KindKeys, State: StateUnknown, Scope: KindScopeNone, Source: KindSourceRow}
}

// runsKind (§3.7): the runs_commands row, bounded with listed_commands when
// the agent asks for commands but a closed list runs without asking: Claude
// Code's and Grok Build's read-only commands, Copilot's read-only terminal
// list, and Cursor's terminalAllowlist entries that neither reach the network
// nor launch code.
func (oi overviewInputs) runsKind(a OverviewAgent, v *agentView) RiskKindCell {
	c, ok := rowOf(a, RowRunsCommands)
	if !ok {
		return RiskKindCell{Kind: KindRunsCommands, State: StateUnknown, Scope: KindScopeNone, Source: KindSourceRow}
	}
	k := rowKind(KindRunsCommands, c)
	listed := false
	switch a.Product {
	case knowledge.ClaudeCodeProduct, "grok-build":
		listed = k.State == StateYou
	case "github-copilot":
		listed = k.State == StateYou && c.Origin == OriginProductDefault
	case knowledge.CursorProduct:
		if v != nil && (k.State == StateYou || k.State == StateModel) {
			base := oi.in.baseFor(a.Product)
			for _, cp := range v.inForce {
				if cp.Basis == graph.BasisConfigured && cp.Approval == graph.ApprovalAllow && cp.Type == graph.CapExecuteShell &&
					strings.HasPrefix(cp.Rule, "terminalAllowlist:") {
					if _, network, launcher := graph.CursorAllowlistClass(cp.TargetScope, base); !network && !launcher {
						listed = true
					}
				}
			}
		}
	}
	if listed {
		k.State, k.Scope = StateBounded, KindScopeListedCommands
	}
	return k
}
