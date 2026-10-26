package rules

import (
	"sort"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// «How your agents work» (decision 2026-10-25, cómo trabajan tus agentes,
// cálculo, T1–T13): for each agent, the same questions answered with the
// ways the findings stand on. Read and write ways come from the same
// inference probed with synthetic targets (graph.Probe, annex 2); running
// and sending out from egressWays and cursorEgress; changing other agents
// from the writes_config_of edges. The section carries codes only.

// Rows (T1), in their order.
const (
	RowReadsFiles         = "reads_files"
	RowWritesFiles        = "writes_files"
	RowRunsCommands       = "runs_commands"
	RowSendsOut           = "sends_out"
	RowChangesOtherAgents = "changes_other_agents"
	RowModelProvider      = "model_provider"
)

// AllOverviewRows lists the rows in their order.
var AllOverviewRows = []string{RowReadsFiles, RowWritesFiles, RowRunsCommands, RowSendsOut, RowChangesOtherAgents, RowModelProvider}

// Deciders (T2, T12).
const (
	DeciderNobody        = "nobody"
	DeciderModel         = "model"
	DeciderYou           = "you"
	DeciderBlocked       = "blocked"
	DeciderNotEvaluated  = "not_evaluated"
	DeciderNotApplicable = "not_applicable"
)

// Scopes (T4): each row has its own values.
const (
	ScopeMachine            = "machine"
	ScopeSameProject        = "same_project"
	ScopeNone               = "none"
	ScopeSandbox            = "sandbox"
	ScopeAnyDestination     = "any_destination"
	ScopeListedDestinations = "listed_destinations"
	ScopeConfig             = "config"
	ScopeInstructions       = "instructions"
)

// RowScopes are the scopes each row may use (T4).
var RowScopes = map[string][]string{
	RowReadsFiles:         {ScopeMachine, ScopeSameProject, ScopeNone},
	RowWritesFiles:        {ScopeMachine, ScopeSameProject, ScopeNone},
	RowRunsCommands:       {ScopeMachine, ScopeSandbox, ScopeNone},
	RowSendsOut:           {ScopeAnyDestination, ScopeListedDestinations, ScopeNone},
	RowChangesOtherAgents: {ScopeConfig, ScopeInstructions, ScopeNone},
	RowModelProvider:      {ScopeNone},
}

// Origins (T5).
const (
	OriginProductDefault           = "product_default"
	OriginConfigured               = "configured"
	OriginMixed                    = "mixed"
	OriginNone                     = "none"
	OriginProductDefaultNotChecked = "product_default_not_checked_here" // reserved (§5)
)

// Reasons a cell has no finding behind it (§4), and why an agent is not
// evaluated (§5).
const (
	NoFindingNoTargets          = "no_targets"
	NoFindingNotAFinding        = "not_a_finding"
	NoFindingFeedsOtherFindings = "feeds_other_findings"

	NotEvaluatedNoInference  = "no_inference"
	NotEvaluatedCoverage     = "coverage"
	NotEvaluatedGraphMissing = "graph_missing"

	PendingModelProviderResearch = "model_provider_research" // reserved (T1)
)

// Codes of what a way documentedly does not touch (§2.4, data.excluded).
const (
	ExcludedProtectedPaths              = "protected_paths"
	ExcludedSandboxProtectedPaths       = "sandbox_protected_paths"
	ExcludedDefaultIgnoreFileTools      = "default_ignore_file_tools"
	ExcludedDocumentedNetworkExceptions = "documented_network_exceptions"
)

// Model provider recipients (T12).
const (
	RecipientAnthropicAPI     = "anthropic_api"
	RecipientCustomGateway    = "custom_gateway"
	RecipientAWSBedrock       = "aws_bedrock"
	RecipientGoogleVertex     = "google_vertex"
	RecipientMicrosoftFoundry = "microsoft_foundry"
	RecipientAnthropicAWS     = "anthropic_aws"
	RecipientVendorBackend    = "vendor_backend"
	RecipientNotReadable      = "not_readable"
)

// AssumeSessionEnvNotReadable: a variable set only in a terminal session
// is not readable at rest (T12).
const AssumeSessionEnvNotReadable = "session_env_not_readable"

// StaleDays is the age at which an agent's knowledge base is stale (§6).
const StaleDays = 90

// OverviewAgent is one detected agent of agents_overview (§6).
type OverviewAgent struct {
	Agent              string            `json:"agent"`
	AgentID            string            `json:"agent_id"`
	Product            string            `json:"-"`
	Evaluated          bool              `json:"evaluated"`
	NotEvaluatedReason *string           `json:"not_evaluated_reason"`
	Knowledge          OverviewKnowledge `json:"knowledge"`
	Rows               []OverviewCell    `json:"rows"`
}

// OverviewKnowledge is the date of the agent's knowledge base and whether it
// is StaleDays old or more (§6).
type OverviewKnowledge struct {
	Date  *string `json:"date"`
	Stale bool    `json:"stale"`
}

// OverviewCell is one cell (§2): who decides, how far, from where.
type OverviewCell struct {
	Row             string        `json:"row"`
	Decider         string        `json:"decider"`
	Scope           string        `json:"scope"`
	Origin          string        `json:"origin"`
	Assumed         bool          `json:"assumed"`
	Also            *OverviewAlso `json:"also"`
	Data            OverviewData  `json:"data"`
	NoFindingReason *string       `json:"no_finding_reason"`
	Pending         *string       `json:"pending"`

	// winCode and winCap are the winning way's code and capability, for
	// the coherence tests of §4 (annex 2, point 2).
	winCode, winCap string
}

// OverviewAlso is the second way shown (T2): less permissive, wider.
type OverviewAlso struct {
	Decider string `json:"decider"`
	Scope   string `json:"scope"`
}

// OverviewData are the ids, numbers and codes a cell's text may cite (T6,
// T12); every field appears only where it makes sense.
type OverviewData struct {
	Projects                 *int     `json:"projects,omitempty"`
	CredentialsReachable     *int     `json:"credentials_reachable,omitempty"`
	CredentialsReachableHigh *int     `json:"credentials_reachable_high,omitempty"`
	PreapprovedRules         *int     `json:"preapproved_rules,omitempty"`
	LauncherRules            *int     `json:"launcher_rules,omitempty"`
	EditAllowRules           *int     `json:"edit_allow_rules,omitempty"`
	TargetAgents             []string `json:"target_agents,omitempty"`
	Excluded                 []string `json:"excluded,omitempty"`
	Ways                     []string `json:"ways,omitempty"`
	Assumptions              []string `json:"assumptions,omitempty"`
	FindingIDs               []string `json:"finding_ids,omitempty"`
	Recipient                string   `json:"recipient,omitempty"`
	ByoKeyStillRouted        *bool    `json:"byo_key_still_routed,omitempty"`
}

// candidate is one way a cell can stand on: a way of the inference, or the
// ask or deny that covers a target with no way (annex 2, point 4).
type candidate struct {
	decider, scope string
	code, cap      string
	basis          []string
	assumptions    []string
}

var deciderRank = map[string]int{DeciderNobody: 4, DeciderModel: 3, DeciderYou: 2, DeciderBlocked: 1}

var scopeRank = map[string]int{
	ScopeMachine: 3, ScopeSameProject: 2, ScopeSandbox: 2, ScopeConfig: 3, ScopeInstructions: 2,
	ScopeAnyDestination: 3, ScopeListedDestinations: 2, ScopeNone: 1,
}

// overviewInputs is what overview reads beyond the views and the findings.
type overviewInputs struct {
	in       Input
	nodes    map[string]graph.Node
	view     func(string) *agentView
	findings []Finding
	probe    map[string]graph.AgentProbe
	reaches  map[string]map[string]bool // agent → credentials it reaches
	scopes   map[string]string          // credential → observed scope
	foreign  map[string][]graph.Edge    // agent → writes_config_of to another agent's resources
}

// overview derives agents_overview for the evaluated agents (those with a
// knowledge base) and the detected ones without one (no_inference), one
// element per product: an ai_client and an extension of one product are one
// agent (decision 2026-10-26, agentes y MCP nuevos, A3). Coverage is the
// index's: the report turns an uncovered agent into not_evaluated coverage.
func overview(oi overviewInputs) []OverviewAgent {
	var out []OverviewAgent
	seen := map[string]int{} // product → index in out
	ids := make([]string, 0, len(oi.nodes))
	for id := range oi.nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := oi.nodes[id]
		if n.Kind != graph.KindAgent || (n.Type != graph.AgentAIClient && n.Type != graph.AgentExtension) {
			continue
		}
		base := oi.in.baseFor(n.Platform)
		if i, dup := seen[n.Platform]; dup {
			if n.Type == graph.AgentAIClient && !strings.HasPrefix(out[i].AgentID, "agent_client_") {
				out[i].AgentID = n.ID // the client's id over the extension's
			}
			continue
		}
		seen[n.Platform] = len(out)
		a := OverviewAgent{Agent: n.Name, AgentID: n.ID, Product: n.Platform, Rows: []OverviewCell{}}
		if base.Product == "" || n.Type != graph.AgentAIClient {
			r := NotEvaluatedNoInference
			a.NotEvaluatedReason = &r
			a.Rows = notEvaluatedRows()
			out = append(out, a)
			continue
		}
		a.Evaluated = true
		date := base.Version
		a.Knowledge = OverviewKnowledge{Date: &date, Stale: stale(date, oi.in.Now)}
		a.Rows = oi.cells(oi.view(n.ID))
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Agent < out[j].Agent })
	if out == nil {
		out = []OverviewAgent{}
	}
	return out
}

// stale reports a knowledge date StaleDays or more before now.
func stale(date string, now time.Time) bool {
	d, err := time.Parse("2006-01-02", date)
	if err != nil || now.IsZero() {
		return false
	}
	return !now.UTC().Before(d.AddDate(0, 0, StaleDays))
}

// NotEvaluatedRows are an agent's rows when it is not evaluated (§5): every
// cell not_evaluated, scope none, origin none.
func notEvaluatedRows() []OverviewCell {
	out := make([]OverviewCell, 0, len(AllOverviewRows))
	for _, r := range AllOverviewRows {
		out = append(out, OverviewCell{Row: r, Decider: DeciderNotEvaluated, Scope: ScopeNone, Origin: OriginNone})
	}
	return out
}

// NotEvaluatedRows are an agent's rows when it is not evaluated (§5).
func NotEvaluatedRows() []OverviewCell { return notEvaluatedRows() }

func (oi overviewInputs) cells(v *agentView) []OverviewCell {
	return []OverviewCell{
		oi.readsCell(v), oi.writesCell(v), oi.runsCell(v), oi.sendsCell(v), oi.changesCell(v), oi.providerCell(v),
	}
}

// dontAsk reports Claude Code's effective mode dontAsk, in which every
// question becomes a refusal (annex 2, point 4).
func (v *agentView) dontAsk() bool { return v.mode == "dontAsk" }

// wayDecider is model for a way through a classifier (it carries
// auto_classifier_permits), nobody otherwise (§2.1).
func wayDecider(assumptions []string) string {
	for _, a := range assumptions {
		if a == graph.AssumeAutoClassifierPermits {
			return DeciderModel
		}
	}
	return DeciderNobody
}

func (oi overviewInputs) basisOf(capIDs ...string) []string {
	var out []string
	for _, id := range capIDs {
		if c, ok := oi.nodes[id]; ok {
			out = append(out, c.Basis)
		}
	}
	return out
}

// targetCandidates turns probe hits into candidates: each edge through the
// same edgeWay as the findings; a target with no way, you when an ask covers
// it and no deny, blocked otherwise (dontAsk: blocked).
func (oi overviewInputs) targetCandidates(v *agentView, hits []graph.ProbeHit, noOutside bool, scopeOf func(w way, t graph.ProbeTarget) string) []candidate {
	var out []candidate
	for _, h := range hits {
		if len(h.Edges) == 0 {
			scope := ScopeSameProject
			if h.Target.Outside || noOutside {
				scope = ScopeMachine
			}
			d, by := DeciderBlocked, h.DenyBy
			if len(h.AskBy) > 0 && len(h.DenyBy) == 0 && !v.dontAsk() {
				d, by = DeciderYou, h.AskBy
			}
			out = append(out, candidate{decider: d, scope: scope, basis: oi.basisOf(by...)})
			continue
		}
		for _, e := range h.Edges {
			w := v.edgeWay(e, oi.nodes, v.base)
			scope := scopeOf(w, h.Target)
			if noOutside {
				scope = ScopeMachine
			}
			out = append(out, candidate{decider: wayDecider(w.assumptions), scope: scope, code: w.code, cap: w.cap, basis: oi.basisOf(w.cap), assumptions: w.assumptions})
		}
	}
	return out
}

// pick fills a cell from its candidates (T2, T5): the most permissive wins,
// then the widest; ties by way code and capability. also is the widest less
// permissive candidate wider than the winner, the most permissive on a tie.
// origin from the basis of the candidates tied with the winner in decider and
// scope; assumed when the winner carries a premise assumption.
func pick(row string, cs []candidate) OverviewCell {
	cell := OverviewCell{Row: row, Scope: ScopeNone, Origin: OriginNone}
	if len(cs) == 0 {
		return cell
	}
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if deciderRank[a.decider] != deciderRank[b.decider] {
			return deciderRank[a.decider] > deciderRank[b.decider]
		}
		if scopeRank[a.scope] != scopeRank[b.scope] {
			return scopeRank[a.scope] > scopeRank[b.scope]
		}
		if a.code != b.code {
			return a.code < b.code
		}
		return a.cap < b.cap
	})
	win := cs[0]
	cell.Decider, cell.Scope = win.decider, win.scope
	cell.winCode, cell.winCap = win.code, win.cap
	var basis, ways, assumptions []string
	for _, c := range cs {
		if c.code != "" {
			ways = append(ways, c.code)
		}
		if c.decider == win.decider && c.scope == win.scope {
			basis = append(basis, c.basis...)
		}
	}
	assumptions = append(assumptions, win.assumptions...)
	cell.Origin = originOf(basis)
	for _, a := range win.assumptions {
		cell.Assumed = cell.Assumed || overviewPremise[a]
	}
	var also *candidate
	for i := range cs {
		c := cs[i]
		// A blocked target is no way: also is a way that acts (T2; the
		// copy has no html.ov.also.blocked).
		if c.decider == DeciderBlocked || deciderRank[c.decider] >= deciderRank[win.decider] || scopeRank[c.scope] <= scopeRank[win.scope] {
			continue
		}
		if also == nil || scopeRank[c.scope] > scopeRank[also.scope] ||
			scopeRank[c.scope] == scopeRank[also.scope] && deciderRank[c.decider] > deciderRank[also.decider] {
			also = &cs[i]
		}
	}
	if also != nil {
		cell.Also = &OverviewAlso{Decider: also.decider, Scope: also.scope}
	}
	cell.Data.Ways, cell.Data.Assumptions = sortedUniqueOrNil(ways), sortedUniqueOrNil(assumptions)
	return cell
}

// overviewPremise are the premise assumptions that make a cell assumed
// (§2.3): the mode RosmarOps cannot confirm at rest.
var overviewPremise = map[string]bool{
	graph.AssumeIDERunModeNotReadable: true,
	graph.AssumeUIModeChoiceUnknown:   true,
	graph.AssumeAutoModeAvailable:     true,
}

func originOf(basis []string) string {
	pd, cf := false, false
	for _, b := range basis {
		pd = pd || b == graph.BasisProductDefault
		cf = cf || b == graph.BasisConfigured
	}
	switch {
	case pd && cf:
		return OriginMixed
	case cf:
		return OriginConfigured
	case pd:
		return OriginProductDefault
	}
	return OriginNone
}

func sortedUniqueOrNil(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	return sortedUnique(ss)
}

func intp(n int) *int { return &n }

// findingsOf are the ids of the agent's findings of rules that hold a way
// with code and capability (§4, compared by code and capability: a probed
// edge has another target than the findings').
func (oi overviewInputs) findingsOf(agent string, rs []string, code, capID string) []string {
	var out []string
	for _, f := range oi.findings {
		if f.Rule == "" || len(f.Evidence.Nodes) == 0 || f.Evidence.Nodes[0] != agent || !contains(rs, f.Rule) {
			continue
		}
		for _, leg := range f.legs {
			for _, w := range leg {
				if (code == "" || w.code == code) && (capID == "" || w.cap == capID) {
					out = append(out, f.ID)
				}
			}
		}
	}
	return sortedUniqueOrNil(out)
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// readsCell (§3, annex 2): the probed read ways, their scope the radius of
// edgeWay; credentials_reachable from the agent's reaches edges.
func (oi overviewInputs) readsCell(v *agentView) OverviewCell {
	ap := oi.probe[v.node.ID]
	cs := oi.targetCandidates(v, ap.Read, ap.NoOutside, func(w way, _ graph.ProbeTarget) string {
		if w.radius == RadiusAnyProject {
			return ScopeMachine
		}
		return ScopeSameProject
	})
	cell := pick(RowReadsFiles, cs)
	creds, high := 0, 0
	for cred := range oi.reaches[v.node.ID] {
		creds++
		if classOf(oi.nodes[cred], oi.scopes[cred]) == ClassHigh {
			high++
		}
	}
	cell.Data.Projects = intp(len(oi.in.RepoRoots))
	cell.Data.CredentialsReachable, cell.Data.CredentialsReachableHigh = intp(creds), intp(high)
	if contains(cell.Data.Ways, "file_tools") && v.node.Platform == knowledge.CursorProduct {
		cell.Data.Excluded = []string{ExcludedDefaultIgnoreFileTools}
	}
	if cell.Decider == DeciderNobody || cell.Decider == DeciderModel {
		win := cs[0]
		cell.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleCredReadAndSend, RuleCredReadable}, win.code, win.cap)
		// No key within the winning way's reach (§4): none at all, or only
		// through another way (Cursor's file tools do not see .env files).
		if len(cell.Data.FindingIDs) == 0 {
			r := NoFindingNoTargets
			cell.NoFindingReason = &r
		}
	}
	return cell
}

// writesCell (§3, annex 2): the probed write ways; machine when one reaches a
// target outside the roots.
func (oi overviewInputs) writesCell(v *agentView) OverviewCell {
	ap := oi.probe[v.node.ID]
	cs := oi.targetCandidates(v, ap.Write, ap.NoOutside, func(_ way, t graph.ProbeTarget) string {
		if t.Outside {
			return ScopeMachine
		}
		return ScopeSameProject
	})
	cell := pick(RowWritesFiles, cs)
	edit := 0
	for _, c := range v.inForce {
		if c.Type == graph.CapWriteFiles && c.Approval == graph.ApprovalAllow && c.Basis == graph.BasisConfigured {
			edit++
		}
	}
	cell.Data.Projects, cell.Data.EditAllowRules = intp(len(oi.in.RepoRoots)), intp(edit)
	if cell.Decider == DeciderNobody || cell.Decider == DeciderModel {
		r := NoFindingNotAFinding
		cell.NoFindingReason = &r
		if v.node.Platform == knowledge.CursorProduct {
			cell.Data.Excluded = []string{ExcludedSandboxProtectedPaths}
		} else {
			cell.Data.Excluded = []string{ExcludedProtectedPaths}
		}
	}
	return cell
}

// runsCell (annex 2, point 4): the capabilities egressWays and cursorEgress
// list, without egress_mcp_allowlist nor a network entry that is no
// launcher. Claude Code runs with the user's rights (machine); Cursor's
// Auto-review in its sandbox, unless insecure_none is in force (the user's
// or a root's); a terminalAllowlist launcher writes any file, as the
// inference says (machine). Run Everything is a reserved way that does not
// come out today (point 5): the cell rests on ide_run_mode_not_readable.
func (oi overviewInputs) runsCell(v *agentView) OverviewCell {
	insecure := false
	for _, c := range v.inForce {
		insecure = insecure || c.Basis == graph.BasisConfigured && c.Rule == graph.CursorInsecureNoneRule
	}
	var cs []candidate
	launchers := 0
	for _, w := range v.egress {
		launcher := contains(w.assumptions, AssumeCodeLauncherRule)
		switch {
		case w.code == "egress_mcp_allowlist", w.code == "egress_allowlist" && !launcher:
			continue
		}
		if launcher {
			launchers++
		}
		scope := ScopeMachine
		if w.code == "egress_auto_review" && !insecure {
			scope = ScopeSandbox
		}
		cs = append(cs, candidate{decider: wayDecider(w.assumptions), scope: scope, code: w.code, cap: w.cap, basis: oi.basisOf(w.cap), assumptions: w.assumptions})
	}
	pre := 0
	for _, c := range v.inForce {
		if c.Type == graph.CapExecuteShell && c.Approval == graph.ApprovalAllow && c.Basis == graph.BasisConfigured {
			pre++
		}
	}
	var cell OverviewCell
	if len(cs) == 0 {
		cell = oi.questionCell(v, RowRunsCommands, graph.CapExecuteShell)
	} else {
		cell = pick(RowRunsCommands, cs)
		r := NoFindingFeedsOtherFindings
		cell.NoFindingReason = &r
	}
	cell.Data.PreapprovedRules, cell.Data.LauncherRules = intp(pre), intp(launchers)
	return cell
}

// questionCell is a cell with no way (runs_commands, sends_out): you when an
// ask without specifier of typ is in force and no deny of it, blocked
// otherwise; dontAsk makes it blocked. Scope none.
func (oi overviewInputs) questionCell(v *agentView, row, typ string) OverviewCell {
	var ask, deny []string
	for _, c := range v.inForce {
		if c.Type != typ || c.TargetScope != scopeAll {
			continue
		}
		switch c.Approval {
		case graph.ApprovalAsk:
			ask = append(ask, c.ID)
		case graph.ApprovalDeny:
			deny = append(deny, c.ID)
		}
	}
	d, by := DeciderBlocked, deny
	if len(ask) > 0 && len(deny) == 0 && !v.dontAsk() {
		d, by = DeciderYou, ask
	}
	return OverviewCell{Row: row, Decider: d, Scope: ScopeNone, Origin: originOf(oi.basisOf(by...))}
}

// sendsCell (§3): exactly egressWays / cursorEgress; with none, a question.
func (oi overviewInputs) sendsCell(v *agentView) OverviewCell {
	var cs []candidate
	for _, w := range v.egress {
		cs = append(cs, candidate{decider: wayDecider(w.assumptions), scope: ScopeAnyDestination, code: w.code, cap: w.cap, basis: oi.basisOf(w.cap), assumptions: w.assumptions})
	}
	var cell OverviewCell
	if len(cs) == 0 {
		cell = oi.questionCell(v, RowSendsOut, graph.CapNetwork)
	} else {
		cell = pick(RowSendsOut, cs)
		cell.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleCredReadAndSend}, cs[0].code, cs[0].cap)
		if len(oi.reaches[v.node.ID]) == 0 {
			r := NoFindingNoTargets
			cell.NoFindingReason = &r
		}
	}
	cell.Data.Excluded = []string{ExcludedDocumentedNetworkExceptions}
	return cell
}

// changesCell (§3): the agent's writes_config_of edges to another agent's
// resources, through edgeWay; config when one is an agent_config or
// agent_executable, instructions otherwise. With none: you, scope none.
func (oi overviewInputs) changesCell(v *agentView) OverviewCell {
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
		cs = append(cs, candidate{decider: wayDecider(w.assumptions), scope: scope, code: w.code, cap: w.cap, basis: oi.basisOf(w.cap), assumptions: w.assumptions})
	}
	if len(cs) == 0 {
		return OverviewCell{Row: RowChangesOtherAgents, Decider: DeciderYou, Scope: ScopeNone, Origin: OriginNone}
	}
	cell := pick(RowChangesOtherAgents, cs)
	cell.Data.TargetAgents = sortedUnique(targets)
	cell.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleForeignAgentConfigWritable, RuleForeignAgentInstructionsWritable}, "", "")
	return cell
}

// providerCell (T12): who receives what the agent reads. Claude Code: the
// first settings layer, by precedence, that sets one of the provider
// variables (managed, local, project, user, then VS Code's
// claudeCode.environmentVariables), configured; none, Anthropic's API as
// the product default, assuming no session variable changes it. Cursor:
// its servers, also with the user's own key.
func (oi overviewInputs) providerCell(v *agentView) OverviewCell {
	cell := OverviewCell{Row: RowModelProvider, Decider: DeciderNotApplicable, Scope: ScopeNone}
	switch v.node.Platform {
	case knowledge.CursorProduct:
		t := true
		cell.Origin, cell.Data.Recipient, cell.Data.ByoKeyStillRouted = OriginProductDefault, RecipientVendorBackend, &t
	case knowledge.ClaudeCodeProduct:
		if r, ok := claudeRecipient(oi.in.ClaudeSettings); ok {
			cell.Origin, cell.Data.Recipient = OriginConfigured, r
		} else {
			cell.Origin, cell.Data.Recipient, cell.Assumed = OriginProductDefault, RecipientAnthropicAPI, true
			cell.Data.Assumptions = []string{AssumeSessionEnvNotReadable}
		}
	default:
		cell.Origin, cell.Data.Recipient = OriginNone, RecipientNotReadable
	}
	return cell
}

// providerVars are Claude Code's provider variables by recipient, in the
// order a layer is read: a provider switch wins over a base URL (T12).
var providerVars = []struct{ key, recipient string }{
	{"CLAUDE_CODE_USE_BEDROCK", RecipientAWSBedrock},
	{"CLAUDE_CODE_USE_VERTEX", RecipientGoogleVertex},
	{"CLAUDE_CODE_USE_FOUNDRY", RecipientMicrosoftFoundry},
	{"CLAUDE_CODE_USE_ANTHROPIC_AWS", RecipientAnthropicAWS},
	{"ANTHROPIC_BASE_URL", RecipientCustomGateway},
}

// claudeRecipient is the recipient the settings layers set, by precedence
// (CTO-3): managed sources, local, project, user, then VS Code. A switch
// counts when set to anything but empty, "0" or "false"; the base URL when
// set. Values are compared, never kept.
func claudeRecipient(scan *platform.ClaudeSettingsScan) (string, bool) {
	if scan == nil {
		return "", false
	}
	var layers [][]platform.EnvVar
	for _, m := range scan.Managed {
		for _, f := range m.Files {
			layers = append(layers, f.Env)
		}
	}
	for _, scope := range []string{platform.MCPScopeLocal, platform.MCPScopeProject, platform.MCPScopeUser} {
		for _, f := range scan.Files {
			if f.Scope == scope && f.Status == platform.MCPSourceRead {
				layers = append(layers, f.Env)
			}
		}
	}
	if scan.VSCode != nil {
		layers = append(layers, scan.VSCode.Env)
	}
	for _, env := range layers {
		for _, pv := range providerVars {
			for _, ev := range env {
				if ev.Key != pv.key {
					continue
				}
				val := strings.ToLower(strings.TrimSpace(ev.Value.Reveal()))
				if pv.recipient == RecipientCustomGateway && val != "" ||
					pv.recipient != RecipientCustomGateway && val != "" && val != "0" && val != "false" {
					return pv.recipient, true
				}
			}
		}
	}
	return "", false
}
