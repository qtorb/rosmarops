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

	// Services are the known external MCP servers the agent is configured to
	// use (BV-121): every catalog server it loads, read-only and denied
	// included, with who approves its use and the finding it gave, if any.
	// Only scored agents carry it; omitted when none.
	Services []OverviewService `json:"services,omitempty"`
}

// OverviewService is one catalog MCP server an agent is configured to use
// (BV-121, agents_overview.services[]).
type OverviewService struct {
	Service   string  `json:"service"`    // the catalog name
	Category  string  `json:"category"`   // money, infrastructure, …
	Access    string  `json:"access"`     // writes | read_only
	Approval  string  `json:"approval"`   // person | another_model | nobody | deny
	Scope     string  `json:"scope"`      // user | project
	FindingID *string `json:"finding_id"` // the finding it gave, or null
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
	reaches  map[string]map[string]bool   // agent → credentials it reaches
	scopes   map[string]string            // credential → observed scope
	foreign  map[string][]graph.Edge      // agent → writes_config_of to another agent's resources
	services map[string][]OverviewService // agent → catalog MCP servers it uses (BV-121)
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
		a.Services = oi.services[n.ID] // BV-121: catalog MCP servers it uses
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
	if v.node.Platform == knowledge.HermesProduct {
		return oi.hermesCells(v)
	}
	if v.node.Platform == knowledge.CodexProduct {
		return oi.codexCells(v)
	}
	if v.node.Platform == knowledge.GrokBuildProduct {
		return oi.grokCells(v)
	}
	return []OverviewCell{
		oi.readsCell(v), oi.writesCell(v), oi.runsCell(v), oi.sendsCell(v), oi.changesCell(v), oi.providerCell(v),
	}
}

// Codex sandbox modes, by permissiveness (CTO-16 §4).
const (
	codexDanger    = "danger-full-access"
	codexWorkspace = "workspace-write"
	codexReadOnly  = "read-only"
)

// codexSandbox is Codex's effective sandbox from the widest write-allow in
// force, and whether a configured layer set it: any_path is danger-full-access,
// working_directory is workspace-write, else read-only (the default is
// workspace-write, a product default).
func codexSandbox(v *agentView) (mode string, configured bool) {
	mode = codexReadOnly
	rank := map[string]int{codexDanger: 3, codexWorkspace: 2, codexReadOnly: 1}
	for _, c := range v.inForce {
		if c.Type != graph.CapWriteFiles || c.Approval != graph.ApprovalAllow {
			continue
		}
		var m string
		switch c.TargetScope {
		case graph.CodexScopeAnyPath:
			m = codexDanger
		case graph.CodexScopeWorkingDir:
			m = codexWorkspace
		default:
			continue
		}
		if rank[m] > rank[mode] {
			mode, configured = m, c.Basis == graph.BasisConfigured
		}
	}
	return mode, configured
}

// codexPolicy is Codex's approval policy and whether its reviewer is the
// auto_review model (CTO-16 §4), from the permission_mode capability in force;
// the default is on-request (a product default).
func codexPolicy(v *agentView) (policy string, autoReview bool) {
	policy = "on-request"
	for _, c := range v.inForce {
		if c.Type != graph.CapPermissionMode {
			continue
		}
		policy = c.TargetScope
		if strings.Contains(c.Rule, "auto_review") {
			autoReview = true
		}
	}
	return policy, autoReview
}

// codexEscalationDecider is who decides an action that needs escalation beyond
// the sandbox — writing outside the workspace or using the network (CTO-16 §4):
// nobody with danger-full-access (nothing escalates), blocked with a blanket
// never, the reviewer model with auto_review, otherwise the person.
func codexEscalationDecider(v *agentView) string {
	if mode, _ := codexSandbox(v); mode == codexDanger {
		return DeciderNobody
	}
	policy, autoReview := codexPolicy(v)
	switch {
	case policy == "never":
		return DeciderBlocked
	case autoReview:
		return DeciderModel
	default:
		return DeciderYou
	}
}

// codexScanFlags reports, for the single Codex client of this run, whether any
// of its layers carries rules\*.rules (exec_rules_not_evaluated) and whether a
// [windows] sandbox is set anywhere (else windows_sandbox_unknown).
func codexScanFlags(scan platform.CodexScan) (rulesFiles, windowsSandboxSet bool) {
	for _, l := range scan.Layers {
		rulesFiles = rulesFiles || l.HasRulesFiles
		windowsSandboxSet = windowsSandboxSet || l.HasWindowsSandbox
	}
	return rulesFiles, windowsSandboxSet
}

// codexExplicitUntrusted reports a project the user config marks
// trust_level = "untrusted": Codex then asks before commands there, except
// those a rules file allows (CTO-16 §4, exec_rules_not_evaluated).
func codexExplicitUntrusted(scan platform.CodexScan) bool {
	for _, l := range scan.Layers {
		if l.Kind == platform.CodexLayerProject && l.TrustLevel == "untrusted" {
			return true
		}
	}
	return false
}

// codexInForceCap is the in-force capability of type typ with an allow
// approval, widest scope first (for the cell's finding lookup).
func (v *agentView) codexInForceCap(typ string) string {
	for _, c := range v.inForce {
		if c.Type == typ && c.Approval == graph.ApprovalAllow {
			return c.ID
		}
	}
	return ""
}

// codexCells are OpenAI Codex's six rows (BV-140, CTO-16 §4). Codex reads every
// file the user can read without asking (machine/nobody). Its write and shell
// rows follow the sandbox: workspace-write keeps writes to the project
// (same_project) and commands to its sandbox; danger-full-access reaches the
// whole machine. Sending out and writing outside the workspace escalate, decided
// by the approval policy (you), the reviewer model (auto_review) or refused
// (never). Every Codex cell rests on launch_flags_not_readable.
func (oi overviewInputs) codexCells(v *agentView) []OverviewCell {
	mode, configured := codexSandbox(v)
	rulesFiles, windowsSandboxSet := codexScanFlags(oi.in.Codex)
	untrusted := codexExplicitUntrusted(oi.in.Codex)
	return []OverviewCell{
		oi.codexReadsCell(v),
		oi.codexWritesCell(v, mode, configured),
		oi.codexRunsCell(v, mode, configured, rulesFiles, windowsSandboxSet, untrusted),
		oi.codexSendsCell(v),
		oi.codexChangesCell(v),
		oi.providerCell(v),
	}
}

// codexBaseAssumptions are the assumptions every Codex cell carries.
func codexBaseAssumptions(extra ...string) []string {
	return append([]string{AssumeLaunchFlagsNotReadable}, extra...)
}

// grokCompatSurfaces is the count of compat cells Grok has (3 vendors × 6
// surfaces, CTO-16 Grok §2).
const grokCompatCells = 18

// grokCompatIncomplete reports that some [compat] cell has no value in the file,
// so a remote setting could decide it (compat_cell_remote_unknown, CTO-16 Grok
// §2). With no scan, assume incomplete (the safe default).
func grokCompatIncomplete(scan platform.GrokScan) bool {
	set := map[string]bool{}
	for _, l := range scan.Layers {
		for k := range l.Compat {
			set[k] = true
		}
	}
	return len(set) < grokCompatCells
}

// grokBaseAssumptions are the assumptions every Grok Build cell carries: launch
// flags are not readable at rest (CTO-16 Grok §4).
func grokBaseAssumptions(extra ...string) []string {
	return append([]string{AssumeLaunchFlagsNotReadable}, extra...)
}

// grokWriteDecider is who decides a without-asking write for Grok's mode (CTO-16
// Grok §4): a wildcard write allow or an approve-everything mode or acceptEdits
// edits without asking (nobody); auto is the classifier (model); dontAsk refuses
// what is not pre-approved (blocked); the default mode asks (you).
func grokWriteDecider(v *agentView, mode string) (decider, scope string, assume []string) {
	for _, c := range v.inForce {
		if c.Type == graph.CapWriteFiles && c.Approval == graph.ApprovalAllow && c.Basis == graph.BasisConfigured && c.TargetScope == scopeAll {
			return DeciderNobody, ScopeMachine, nil
		}
	}
	switch {
	case grokBypassMode(mode), mode == "acceptEdits":
		return DeciderNobody, ScopeMachine, nil
	case mode == "auto":
		return DeciderModel, ScopeMachine, []string{graph.AssumeAutoClassifierPermits}
	case mode == "dontAsk":
		return DeciderBlocked, ScopeNone, nil
	default:
		return DeciderYou, ScopeNone, nil
	}
}

// grokEscalationDecider is who decides sending out or running the general shell
// for Grok's mode: an approve-everything mode (nobody), auto (model), dontAsk
// (blocked); otherwise the person asks (acceptEdits and the default mode).
func grokEscalationDecider(mode string) (decider string, assume []string) {
	switch {
	case grokBypassMode(mode):
		return DeciderNobody, nil
	case mode == "auto":
		return DeciderModel, []string{graph.AssumeAutoClassifierPermits}
	case mode == "dontAsk":
		return DeciderBlocked, nil
	default:
		return DeciderYou, nil
	}
}

// grokCells are xAI Grok Build's six rows (BV-140, CTO-16 Grok §4). Grok reads
// every file the user can read without asking (machine/nobody). Writing, sending
// out and the general shell follow its permission mode — its own or the one it
// inherits from Claude Code: an approve-everything mode or a wildcard allow acts
// without asking (nobody), auto hands the step to a classifier (model), the
// default mode asks (you). Every cell rests on launch_flags_not_readable; the
// changes row notes compat_cell_remote_unknown when a compat cell is unset.
func (oi overviewInputs) grokCells(v *agentView) []OverviewCell {
	mode, _ := grokMode(v)
	incomplete := grokCompatIncomplete(oi.in.Grok)
	return []OverviewCell{
		oi.grokReadsCell(v),
		oi.grokWritesCell(v, mode),
		oi.grokRunsCell(v, mode),
		oi.grokSendsCell(v, mode),
		oi.grokChangesCell(v, incomplete),
		oi.providerCell(v),
	}
}

func (oi overviewInputs) grokReadsCell(v *agentView) OverviewCell {
	cell := OverviewCell{Row: RowReadsFiles, Decider: DeciderNobody, Scope: ScopeMachine, Origin: OriginProductDefault}
	creds, high := 0, 0
	for cred := range oi.reaches[v.node.ID] {
		creds++
		if classOf(oi.nodes[cred], oi.scopes[cred]) == ClassHigh {
			high++
		}
	}
	cell.Data.Projects = intp(len(oi.in.RepoRoots))
	cell.Data.CredentialsReachable, cell.Data.CredentialsReachableHigh = intp(creds), intp(high)
	cell.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleCredReadAndSend, RuleCredReadable}, "", v.codexInForceCap(graph.CapReadFiles))
	cell.Data.Assumptions = grokBaseAssumptions()
	if len(cell.Data.FindingIDs) == 0 {
		r := NoFindingNoTargets
		cell.NoFindingReason = &r
	}
	return cell
}

func (oi overviewInputs) grokWritesCell(v *agentView, mode string) OverviewCell {
	decider, scope, assume := grokWriteDecider(v, mode)
	cell := OverviewCell{Row: RowWritesFiles, Decider: decider, Scope: scope, Origin: OriginConfigured}
	cell.Data.Projects = intp(len(oi.in.RepoRoots))
	edit := 0
	for _, c := range v.inForce {
		if c.Type == graph.CapWriteFiles && c.Approval == graph.ApprovalAllow && c.Basis == graph.BasisConfigured {
			edit++
		}
	}
	cell.Data.EditAllowRules = intp(edit)
	cell.Data.Assumptions = sortedUnique(grokBaseAssumptions(assume...))
	cell.Assumed = contains(assume, graph.AssumeAutoClassifierPermits)
	if decider == DeciderNobody || decider == DeciderModel {
		r := NoFindingNotAFinding
		cell.NoFindingReason = &r
		cell.Data.Excluded = []string{ExcludedProtectedPaths}
	}
	return cell
}

func (oi overviewInputs) grokRunsCell(v *agentView, mode string) OverviewCell {
	decider, assume := grokEscalationDecider(mode)
	// A wildcard [permission] allow "*" is «approve everything» and covers the
	// shell too (CTO-16 Grok §4), so commands run without asking.
	for _, c := range v.inForce {
		if c.Type == graph.CapExecuteShell && c.Approval == graph.ApprovalAllow && c.Basis == graph.BasisConfigured && c.TargetScope == scopeAll {
			decider, assume = DeciderNobody, nil
		}
	}
	scope := ScopeMachine
	if decider == DeciderBlocked {
		scope = ScopeNone
	}
	cell := OverviewCell{Row: RowRunsCommands, Decider: decider, Scope: scope, Origin: OriginConfigured}
	pre := 0
	for _, c := range v.inForce {
		if c.Type == graph.CapExecuteShell && c.Approval == graph.ApprovalAllow && c.Basis == graph.BasisConfigured {
			pre++
		}
	}
	cell.Data.PreapprovedRules, cell.Data.LauncherRules = intp(pre), intp(0)
	cell.Data.Assumptions = sortedUnique(grokBaseAssumptions(assume...))
	cell.Assumed = contains(assume, graph.AssumeAutoClassifierPermits)
	r := NoFindingFeedsOtherFindings
	cell.NoFindingReason = &r
	return cell
}

func (oi overviewInputs) grokSendsCell(v *agentView, mode string) OverviewCell {
	cell := OverviewCell{Row: RowSendsOut, Origin: OriginConfigured}
	assume := grokBaseAssumptions()
	if len(v.egress) > 0 {
		cell.Decider, cell.Scope = DeciderNobody, ScopeAnyDestination
		for _, w := range v.egress {
			if contains(w.assumptions, graph.AssumeAutoClassifierPermits) {
				cell.Decider, cell.Assumed = DeciderModel, true
				assume = append(assume, graph.AssumeAutoClassifierPermits)
			}
		}
		cell.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleCredReadAndSend}, "", v.codexInForceCap(graph.CapNetwork))
		if len(oi.reaches[v.node.ID]) == 0 {
			r := NoFindingNoTargets
			cell.NoFindingReason = &r
		}
	} else {
		d, a := grokEscalationDecider(mode)
		cell.Decider, assume = d, grokBaseAssumptions(a...)
		cell.Scope = ScopeAnyDestination
		if d == DeciderBlocked {
			cell.Scope = ScopeNone
		}
		cell.Assumed = contains(a, graph.AssumeAutoClassifierPermits)
	}
	cell.Data.Assumptions = sortedUnique(assume)
	cell.Data.Excluded = []string{ExcludedDocumentedNetworkExceptions}
	return cell
}

// grokChangesCell is Grok's writes to another agent's resources (CTO-16 Grok
// §5). It rests on compat_cell_remote_unknown when a compat cell is unset,
// because the cells decide which of Claude Code's and Cursor's files Grok loads
// and can change.
func (oi overviewInputs) grokChangesCell(v *agentView, compatIncomplete bool) OverviewCell {
	base := grokBaseAssumptions()
	if compatIncomplete {
		base = append(base, AssumeCompatCellRemoteUnknown)
	}
	var cs []candidate
	var targets []string
	for _, e := range oi.foreign[v.node.ID] {
		r := oi.nodes[e.To]
		scope := ScopeInstructions
		if r.Type == graph.ResourceAgentConfig || r.Type == graph.ResourceAgentExecutable {
			scope = ScopeConfig
		}
		targets = append(targets, r.Configures)
		cs = append(cs, candidate{decider: DeciderNobody, scope: scope, basis: []string{graph.BasisConfigured}, assumptions: base})
	}
	if len(cs) == 0 {
		return OverviewCell{Row: RowChangesOtherAgents, Decider: DeciderYou, Scope: ScopeNone, Origin: OriginNone, Data: OverviewData{Assumptions: sortedUnique(base)}}
	}
	cell := pick(RowChangesOtherAgents, cs)
	cell.Data.TargetAgents = sortedUnique(targets)
	cell.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleForeignAgentConfigWritable, RuleForeignAgentInstructionsWritable}, "", "")
	cell.Data.Assumptions = sortedUnique(base)
	return cell
}

func (oi overviewInputs) codexReadsCell(v *agentView) OverviewCell {
	origin := OriginProductDefault
	for _, c := range v.inForce {
		if c.Type == graph.CapReadFiles && c.Approval == graph.ApprovalAllow && c.Basis == graph.BasisConfigured {
			origin = OriginMixed
		}
	}
	cell := OverviewCell{Row: RowReadsFiles, Decider: DeciderNobody, Scope: ScopeMachine, Origin: origin}
	creds, high := 0, 0
	for cred := range oi.reaches[v.node.ID] {
		creds++
		if classOf(oi.nodes[cred], oi.scopes[cred]) == ClassHigh {
			high++
		}
	}
	cell.Data.Projects = intp(len(oi.in.RepoRoots))
	cell.Data.CredentialsReachable, cell.Data.CredentialsReachableHigh = intp(creds), intp(high)
	cell.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleCredReadAndSend, RuleCredReadable}, "", v.codexInForceCap(graph.CapReadFiles))
	cell.Data.Assumptions = codexBaseAssumptions()
	if len(cell.Data.FindingIDs) == 0 {
		r := NoFindingNoTargets
		cell.NoFindingReason = &r
	}
	return cell
}

func (oi overviewInputs) codexWritesCell(v *agentView, mode string, configured bool) OverviewCell {
	scope := ScopeSameProject
	if mode == codexDanger {
		scope = ScopeMachine
	}
	origin := OriginProductDefault
	if configured {
		origin = OriginConfigured
	}
	cell := OverviewCell{Row: RowWritesFiles, Decider: DeciderNobody, Scope: scope, Origin: origin}
	cell.Data.Projects = intp(len(oi.in.RepoRoots))
	edit := 0
	for _, c := range v.inForce {
		if c.Type == graph.CapWriteFiles && c.Approval == graph.ApprovalAllow && c.Basis == graph.BasisConfigured {
			edit++
		}
	}
	cell.Data.EditAllowRules = intp(edit)
	assume := codexBaseAssumptions()
	// Writing outside the workspace is an escalation (CTO-16 §4): with
	// auto_review the reviewer model decides it (scope machine), so the cell
	// shows nobody inside the project and, as its second way, the model outside.
	if _, autoReview := codexPolicy(v); autoReview && mode != codexDanger {
		cell.Also = &OverviewAlso{Decider: DeciderModel, Scope: ScopeMachine}
		assume = append(assume, AssumeReviewerModelPermits)
		cell.Assumed = true
	}
	cell.Data.Assumptions = sortedUnique(assume)
	cell.Data.Excluded = []string{ExcludedProtectedPaths}
	r := NoFindingNotAFinding
	cell.NoFindingReason = &r
	return cell
}

func (oi overviewInputs) codexRunsCell(v *agentView, mode string, configured, rulesFiles, windowsSandboxSet, untrusted bool) OverviewCell {
	scope, decider := ScopeSandbox, DeciderNobody
	if mode == codexDanger {
		scope = ScopeMachine
	}
	origin := OriginProductDefault
	if configured {
		origin = OriginConfigured
	}
	// An explicitly-untrusted project (and no user-level danger-full-access)
	// makes Codex ask before commands there, except those a rules file allows
	// (CTO-16 §4): the person decides, with exec_rules_not_evaluated.
	untrustedAsks := untrusted && mode != codexDanger
	if untrustedAsks {
		scope, decider = ScopeNone, DeciderYou
	}
	cell := OverviewCell{Row: RowRunsCommands, Decider: decider, Scope: scope, Origin: origin}
	pre := 0
	for _, c := range v.inForce {
		if c.Type == graph.CapExecuteShell && c.Approval == graph.ApprovalAllow && c.Basis == graph.BasisConfigured {
			pre++
		}
	}
	cell.Data.PreapprovedRules, cell.Data.LauncherRules = intp(pre), intp(0)
	assume := codexBaseAssumptions()
	if rulesFiles || untrustedAsks {
		// Unparsed rules\*.rules could let commands run without asking (CTO-16 §4).
		assume = append(assume, AssumeExecRulesNotEvaluated)
	}
	if !untrustedAsks && mode != codexDanger && !windowsSandboxSet {
		// The command runs in a Windows sandbox whose implementation is chosen
		// at launch; assume the least isolating one (CTO-16 §4, point 9).
		assume = append(assume, AssumeWindowsSandboxUnknown)
	}
	cell.Data.Assumptions = sortedUnique(assume)
	r := NoFindingFeedsOtherFindings
	cell.NoFindingReason = &r
	return cell
}

func (oi overviewInputs) codexSendsCell(v *agentView) OverviewCell {
	cell := OverviewCell{Row: RowSendsOut, Origin: OriginNone}
	assume := codexBaseAssumptions()
	if len(v.egress) > 0 {
		// A network or machine-shell allow sends out without asking.
		cell.Decider, cell.Scope, cell.Origin = DeciderNobody, ScopeAnyDestination, OriginConfigured
		cell.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleCredReadAndSend}, "", v.codexInForceCap(graph.CapNetwork))
		if len(oi.reaches[v.node.ID]) == 0 {
			r := NoFindingNoTargets
			cell.NoFindingReason = &r
		}
	} else {
		// Network off by default: using it escalates, decided by the policy.
		cell.Decider = codexEscalationDecider(v)
		cell.Origin = OriginProductDefault
		switch cell.Decider {
		case DeciderBlocked:
			cell.Scope = ScopeNone
		case DeciderModel:
			cell.Scope = ScopeAnyDestination
			assume = append(assume, AssumeReviewerModelPermits)
			cell.Assumed = true
		default:
			cell.Scope = ScopeAnyDestination
		}
	}
	cell.Data.Assumptions = sortedUnique(assume)
	cell.Data.Excluded = []string{ExcludedDocumentedNetworkExceptions}
	return cell
}

// codexChangesCell is Codex's writes to another agent's resources (CTO-16 §5):
// each writes_config_of edge is a write without asking (workspace-write inside
// the project, danger-full-access anywhere), so decider nobody.
func (oi overviewInputs) codexChangesCell(v *agentView) OverviewCell {
	var cs []candidate
	var targets []string
	for _, e := range oi.foreign[v.node.ID] {
		r := oi.nodes[e.To]
		scope := ScopeInstructions
		if r.Type == graph.ResourceAgentConfig || r.Type == graph.ResourceAgentExecutable {
			scope = ScopeConfig
		}
		targets = append(targets, r.Configures)
		cs = append(cs, candidate{decider: DeciderNobody, scope: scope, basis: []string{graph.BasisProductDefault}, assumptions: codexBaseAssumptions()})
	}
	if len(cs) == 0 {
		return OverviewCell{Row: RowChangesOtherAgents, Decider: DeciderYou, Scope: ScopeNone, Origin: OriginNone, Data: OverviewData{Assumptions: codexBaseAssumptions()}}
	}
	cell := pick(RowChangesOtherAgents, cs)
	cell.Data.TargetAgents = sortedUnique(targets)
	cell.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleForeignAgentConfigWritable, RuleForeignAgentInstructionsWritable}, "", "")
	cell.Data.Assumptions = codexBaseAssumptions()
	return cell
}

// hermesCells are Hermes Agent's six rows (BV-111). Its read, write, shell and
// network tools run without asking (decider nobody); the permission mode
// (smart / manual / off) decides only the ask-gated write of another agent's
// protected instructions, which lands in the changes row. A local or absent
// terminal backend reaches the whole computer (scope machine); an isolated
// backend keeps the file and shell tools inside it (read/write scope none,
// shell scope sandbox, basis configured), and an unrecognized value is treated
// as local, with the reason noted on the findings.
func (oi overviewInputs) hermesCells(v *agentView) []OverviewCell {
	isolated := false
	for _, c := range v.inForce {
		if c.Basis == graph.BasisConfigured && c.TargetScope == graph.HermesScopeIsolated {
			isolated = true
		}
	}
	return []OverviewCell{
		oi.hermesReadsCell(v, isolated), oi.hermesWritesCell(v, isolated),
		oi.hermesRunsCell(v, isolated), oi.hermesSendsCell(v),
		oi.hermesChangesCell(v, hermesMode(v)), oi.providerCell(v),
	}
}

// hermesUnknownBackends maps each Hermes Agent node whose terminal.backend is
// a value RosmarOps does not recognize (treated as local) to that raw value
// (BV-111). Agents with a local, absent or isolated backend are not listed.
func hermesUnknownBackends(nodes map[string]graph.Node, scan platform.HermesScan) map[string]string {
	out := map[string]string{}
	for _, n := range nodes {
		if n.Kind != graph.KindAgent || n.Type != graph.AgentAIClient || n.Platform != knowledge.HermesProduct {
			continue
		}
		for _, c := range scan.Configs {
			if graph.CanonicalPath(c.ConfigDir) != graph.CanonicalPath(n.ConfigPath) {
				continue
			}
			if b := c.TerminalBackend; b != "" && b != "local" && !graph.IsolatedBackend(b) {
				out[n.ID] = b
			}
		}
	}
	return out
}

// hermesMode is Hermes Agent's permission mode from its permission_mode
// capability (approvals.mode): smart, manual or off; smart when absent.
func hermesMode(v *agentView) string {
	for _, c := range v.inForce {
		if c.Type == graph.CapPermissionMode {
			return c.TargetScope
		}
	}
	return "smart"
}

// hermesCap is the in-force capability of type typ with an allow approval.
func (v *agentView) hermesCap(typ string) *graph.Node {
	for i := range v.inForce {
		if v.inForce[i].Type == typ && v.inForce[i].Approval == graph.ApprovalAllow {
			return &v.inForce[i]
		}
	}
	return nil
}

func (oi overviewInputs) hermesReadsCell(v *agentView, isolated bool) OverviewCell {
	scope, origin := ScopeMachine, OriginProductDefault
	if isolated {
		scope, origin = ScopeNone, OriginConfigured
	}
	cell := OverviewCell{Row: RowReadsFiles, Decider: DeciderNobody, Scope: scope, Origin: origin}
	creds, high := 0, 0
	for cred := range oi.reaches[v.node.ID] {
		creds++
		if classOf(oi.nodes[cred], oi.scopes[cred]) == ClassHigh {
			high++
		}
	}
	cell.Data.Projects = intp(len(oi.in.RepoRoots))
	cell.Data.CredentialsReachable, cell.Data.CredentialsReachableHigh = intp(creds), intp(high)
	var capID string
	if c := v.hermesCap(graph.CapReadFiles); c != nil {
		capID = c.ID
	}
	cell.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleCredReadAndSend, RuleCredReadable}, "", capID)
	if len(cell.Data.FindingIDs) == 0 {
		r := NoFindingNoTargets
		cell.NoFindingReason = &r
	}
	return cell
}

func (oi overviewInputs) hermesWritesCell(v *agentView, isolated bool) OverviewCell {
	scope, origin := ScopeMachine, OriginProductDefault
	if isolated {
		scope, origin = ScopeNone, OriginConfigured
	}
	cell := OverviewCell{Row: RowWritesFiles, Decider: DeciderNobody, Scope: scope, Origin: origin}
	cell.Data.Projects = intp(len(oi.in.RepoRoots))
	cell.Data.EditAllowRules = intp(hermesConfiguredAllow(v, graph.CapWriteFiles))
	r := NoFindingNotAFinding
	cell.NoFindingReason = &r
	cell.Data.Excluded = []string{ExcludedProtectedPaths}
	return cell
}

func (oi overviewInputs) hermesRunsCell(v *agentView, isolated bool) OverviewCell {
	scope, origin := ScopeMachine, OriginProductDefault
	if isolated {
		scope, origin = ScopeSandbox, OriginConfigured
	}
	cell := OverviewCell{Row: RowRunsCommands, Decider: DeciderNobody, Scope: scope, Origin: origin}
	cell.Data.PreapprovedRules, cell.Data.LauncherRules = intp(hermesConfiguredAllow(v, graph.CapExecuteShell)), intp(0)
	r := NoFindingFeedsOtherFindings
	cell.NoFindingReason = &r
	return cell
}

func (oi overviewInputs) hermesSendsCell(v *agentView) OverviewCell {
	cell := OverviewCell{Row: RowSendsOut, Decider: DeciderNobody, Scope: ScopeAnyDestination, Origin: OriginProductDefault}
	var capID string
	if c := v.hermesCap(graph.CapNetwork); c != nil {
		capID = c.ID
	}
	cell.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleCredReadAndSend}, "egress_network", capID)
	if len(oi.reaches[v.node.ID]) == 0 {
		r := NoFindingNoTargets
		cell.NoFindingReason = &r
	}
	cell.Data.Excluded = []string{ExcludedDocumentedNetworkExceptions}
	return cell
}

// hermesChangesCell (BV-111): Hermes's writes to another agent's resources. A
// config or executable is written without asking (nobody); another agent's
// protected instructions are the one ask-gated write, decided by the mode
// (smart → the auxiliary model, manual → you, off or absent → nobody).
func (oi overviewInputs) hermesChangesCell(v *agentView, mode string) OverviewCell {
	var cs []candidate
	var targets []string
	for _, e := range oi.foreign[v.node.ID] {
		r := oi.nodes[e.To]
		targets = append(targets, r.Configures)
		if r.Type == graph.ResourceAgentInstructions {
			dec, assume := hermesAskDecider(mode)
			cs = append(cs, candidate{decider: dec, scope: ScopeInstructions, assumptions: assume, basis: []string{graph.BasisProductDefault}})
			continue
		}
		scope := ScopeInstructions
		if r.Type == graph.ResourceAgentConfig || r.Type == graph.ResourceAgentExecutable {
			scope = ScopeConfig
		}
		cs = append(cs, candidate{decider: DeciderNobody, scope: scope, basis: []string{graph.BasisProductDefault}})
	}
	if len(cs) == 0 {
		return OverviewCell{Row: RowChangesOtherAgents, Decider: DeciderYou, Scope: ScopeNone, Origin: OriginNone}
	}
	cell := pick(RowChangesOtherAgents, cs)
	cell.Data.TargetAgents = sortedUnique(targets)
	cell.Data.FindingIDs = oi.findingsOf(v.node.ID, []string{RuleForeignAgentConfigWritable, RuleForeignAgentInstructionsWritable}, "", "")
	return cell
}

// hermesAskDecider is who decides an ask-gated action under a Hermes mode
// (BV-111): the auxiliary model for smart (a premise, aux_model_permits), you
// for manual, nobody for off or any non-asking mode.
func hermesAskDecider(mode string) (string, []string) {
	switch mode {
	case "smart":
		return DeciderModel, []string{AssumeAuxModelPermits}
	case "manual":
		return DeciderYou, nil
	default:
		return DeciderNobody, nil
	}
}

// hermesConfiguredAllow counts the agent's configured allow capabilities of
// type typ (an isolated backend turns its file and shell tools configured).
func hermesConfiguredAllow(v *agentView, typ string) int {
	n := 0
	for _, c := range v.inForce {
		if c.Type == typ && c.Approval == graph.ApprovalAllow && c.Basis == graph.BasisConfigured {
			n++
		}
	}
	return n
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
