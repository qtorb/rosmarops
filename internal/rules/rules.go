// Package rules is the rules layer of the v2 report (decision 2026-10-24,
// informe legible, E1–E13 with A1 and A2): it reads the built capability
// graph and turns it into findings, each with its actions, and picks the one
// thing to do today. It is judgement, not knowledge: what a product does is
// in the graph and the knowledge base; what counts as dangerous is here.
//
// It is a pure function over its input: no I/O. Findings carry ids, enums,
// codes and technical detail only; the text shown to people comes from the
// message catalog by code.
package rules

import (
	"log/slog"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Version is the rules version recorded in the report's provenance, and
// RuleVersion the version of each rule. Neither enters a finding id.
const (
	Version     = "3"
	RuleVersion = "2"
)

// Rule codes (§1).
const (
	RuleCredReadAndSend                  = "cred_read_and_send"
	RuleCredReadable                     = "cred_readable"
	RuleForeignAgentConfigWritable       = "foreign_agent_config_writable"
	RuleForeignAgentInstructionsWritable = "foreign_agent_instructions_writable"
	RuleOwnInstructionsWritable          = "own_instructions_writable"
	RuleOwnConfigWritable                = "own_config_writable"
	RulePackageKnownMalicious            = "package_known_malicious"
)

// AllRules lists every rule code.
var AllRules = []string{
	RuleCredReadAndSend, RuleCredReadable, RuleCredInGit,
	RuleForeignAgentConfigWritable, RuleForeignAgentInstructionsWritable,
	RuleOwnInstructionsWritable, RuleOwnConfigWritable, RulePackageKnownMalicious,
}

// AssumeCodeLauncherRule marks a way that rests on the code launcher table
// (D4): an execute_shell allow rule counted as an unrestricted shell.
const AssumeCodeLauncherRule = graph.AssumeCodeLauncherRule

// AllAssumptions lists every assumption a finding can carry: the graph's and
// code_launcher_rule.
var AllAssumptions = []string{
	graph.AssumeProcessRunsAsUser, graph.AssumeVersionAsDocumented, graph.AssumeBashToolAvailable,
	graph.AssumeNoPreToolUseHookBlocks, graph.AssumeAutoClassifierPermits, graph.AssumeReadDenyPartial,
	graph.AssumeShellWriteUnchecked, graph.AssumeEditRulePartial, graph.AssumeAutoModeAvailable,
	graph.AssumeUIModeChoiceUnknown, AssumeCodeLauncherRule,
	graph.AssumeIDERunModeNotReadable, graph.AssumeThirdPartyImportsNotReadable, graph.AssumeSandboxPlatformUnknown,
	graph.AssumeConfigFileProtectionUnknown, graph.AssumeTeamPolicyUnknown, AssumeRemoteVisibilityUnknown,
}

// premiseAssumptions say the way depends on a model or a mode RosmarOps
// cannot confirm at rest: one is enough for confidence medium (§1, E4). The
// others (base and partial-way) leave confidence high.
var premiseAssumptions = map[string]bool{
	graph.AssumeAutoClassifierPermits: true,
	graph.AssumeAutoModeAvailable:     true,
	graph.AssumeUIModeChoiceUnknown:   true,
	graph.AssumeIDERunModeNotReadable: true,
}

// Severities, confidences and radii.
const (
	SeverityRed    = "red"
	SeverityYellow = "yellow"

	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"

	RadiusAnyProject  = "any_project"
	RadiusSameProject = "same_project"
)

// Credential classes (§1, the class table of the rules layer).
const (
	ClassHigh         = "high"
	ClassMedium       = "medium"
	ClassLow          = "low"
	ClassUnclassified = "unclassified"
)

// AllClasses lists the classes in posture order.
var AllClasses = []string{ClassHigh, ClassMedium, ClassLow, ClassUnclassified}

// Posture codes (§6, A2).
const (
	PostureReadableWithoutPrompt = "posture.credentials_readable_without_prompt"
	PostureInTemplatesOnly       = "posture.credentials_in_templates_only"
)

// AllPostures lists every posture code.
var AllPostures = []string{PostureReadableWithoutPrompt, PostureInTemplatesOnly, PostureEnvFilesTracked}

// TodayNothingUrgent is the today code when there is no finding.
const TodayNothingUrgent = "today.nothing_urgent"

// classOf is the class table (§1): type + service + observed_scope.
//   - high: Supabase service_role or secret, Stripe secret keys, db
//     credentials, AWS;
//   - medium: model API keys (OpenAI, OpenRouter, Anthropic);
//   - low: Supabase publishable and anon;
//   - everything else is unclassified (medium until classified).
//
// A private key (type private_key, CTO-7 change 4) is high.
func classOf(cred graph.Node, scope string) string {
	switch {
	case cred.Type == graph.CredentialPrivateKey:
		return ClassHigh
	case cred.Service == "supabase" && (scope == "service_role" || scope == "secret"):
		return ClassHigh
	case cred.Service == "supabase" && (scope == "publishable" || scope == "anon"):
		return ClassLow
	case cred.Type == graph.CredentialDBCredential:
		return ClassHigh
	case cred.Type == graph.CredentialAPIKey && (cred.Service == "stripe" || cred.Service == "aws"):
		return ClassHigh
	case cred.Type == graph.CredentialAPIKey && (cred.Service == "openai" || cred.Service == "openrouter" || cred.Service == "anthropic"):
		return ClassMedium
	}
	return ClassUnclassified
}

// isTemplate reports a template file (A2): .env.example, .env.sample,
// .env.template, or a name ending in .example.
func isTemplate(file string) bool {
	base := strings.ToLower(path.Base(strings.ReplaceAll(file, `\`, "/")))
	switch base {
	case ".env.example", ".env.sample", ".env.template":
		return true
	}
	return strings.HasSuffix(base, ".example")
}

// Input is what the rules read.
type Input struct {
	Graph graph.Graph
	Base  knowledge.Base

	// Bases are the knowledge bases by product (decision 2026-10-25, Cursor):
	// each agent is evaluated with its product's. A product missing here
	// uses Base.
	Bases map[string]knowledge.Base

	// RepoRoots are the configured repositories, and Families maps each
	// canonical root (graph.CanonicalPath) to the root of its family's first
	// repository (BV-33). A root missing from Families is its own family.
	RepoRoots []string
	Families  map[string]string

	// The secrets committed in the repositories, as the scan found them
	// (decision 2026-10-24, memoria, §2.2): every occurrence, where the
	// graph keeps one committed_in edge per credential and repository.
	// Without them, the edges stand for the occurrences.
	Committed []finding.CommittedSecret
	// Rotated and Fixtures are allow_list.rotated_credentials and
	// test_fixtures (M9, M10); ConfigFile the configuration file the
	// rotate_credential action edits, as shown; Now the run's time.
	Rotated    []config.AllowEntryRotated
	Fixtures   []config.AllowEntryTestFixture
	ConfigFile string
	Now        time.Time

	// ClaudeSettings is the Claude Code settings scan, for the model
	// provider's variables (T12); Home the user's home folder, for the
	// probe's target outside the roots (annex 2). Both optional.
	ClaudeSettings *platform.ClaudeSettingsScan
	Home           string
}

// baseFor is the knowledge base of a product; empty for a product with no
// base, which is never evaluated with another product's.
func (in Input) baseFor(product string) knowledge.Base {
	if b, ok := in.Bases[product]; ok {
		return b
	}
	if in.Bases == nil && product == in.Base.Product {
		return in.Base
	}
	return knowledge.Base{}
}

// Result is what the rules produce.
type Result struct {
	Findings []Finding
	Today    Today
	Posture  []Posture

	// Launchers are the execute_shell allow rules counted as an
	// unrestricted shell through the launcher table, for the run log.
	Launchers []LauncherRule

	// MediumReadable are, per agent id, the medium or unclassified
	// credentials the agent reads without a way out (posture, not a
	// finding), for the posture score.
	MediumReadable map[string]map[string]bool

	// Silenced is the test data the user silenced (M10–M11), TestData the
	// keys treated as test data by default (decision 2026-10-26).
	Silenced Silenced
	TestData TestData

	// AgentsOverview is the report's agents_overview section (decision
	// 2026-10-25, cómo trabajan tus agentes, cálculo).
	AgentsOverview []OverviewAgent
}

// LauncherRule is one shell rule counted through the launcher table.
type LauncherRule struct {
	Capability string
	Rule       string
	Form       string
}

// Finding is one finding of the v2 report (§6).
type Finding struct {
	ID          string   `json:"id"`
	Rule        string   `json:"rule"`
	RuleVersion string   `json:"rule_version"`
	Severity    string   `json:"severity"`
	Confidence  string   `json:"confidence"`
	Radius      string   `json:"radius,omitempty"`
	Params      Params   `json:"params"`
	Evidence    Evidence `json:"evidence"`
	Group       Group    `json:"group"`
	Actions     []Action `json:"actions"`

	// Memory is the finding's verdict against the last checked run (memoria,
	// M3), set by the report from findings-memory.json.
	Memory *FindingMemory `json:"memory,omitempty"`

	class    string
	credType string // the credential's type, for its label
	credID   string // the credential the label comes from
	observed bool   // the label credential's authenticates_to is observed
	others   int    // the other credentials merged into the finding (M12)
	// legs are the ways the finding stands on: for cred_read_and_send the
	// reaches edges and the egress paths, for the others one leg.
	legs [][]way
}

// CredType is the type of a credential finding's credential (empty for the
// other rules); not written to the report, which keeps it in the graph.
func (f Finding) CredType() string { return f.credType }

// FindingMemory is a finding's memory (M1, M3): its verdict, the start of
// its current streak, when it was first seen, and how many times it came
// back.
type FindingMemory struct {
	Verdict    string `json:"verdict"`
	OpenSince  string `json:"open_since"`
	FirstSeen  string `json:"first_seen"`
	Reappeared int    `json:"reappeared"`
}

// Params are the typed values the catalog texts take. Never a secret value.
type Params struct {
	Agent        string `json:"agent"`
	Service      string `json:"service,omitempty"`
	Scope        string `json:"scope,omitempty"`
	Class        string `json:"class,omitempty"`
	TargetAgent  string `json:"target_agent,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
	Count        int    `json:"count"`

	// cred_in_git
	CredID    string `json:"cred_id,omitempty"`
	CredType  string `json:"cred_type,omitempty"`
	HasRemote *bool  `json:"has_remote,omitempty"`

	// package_known_malicious (BV-100)
	Ecosystem string `json:"ecosystem,omitempty"`
	Package   string `json:"package,omitempty"`
	Version   string `json:"version,omitempty"`
	IntelID   string `json:"intel_id,omitempty"`
}

// Evidence is what the finding rests on.
type Evidence struct {
	Nodes       []string `json:"nodes"`
	Edges       []string `json:"edges"`
	Locations   []string `json:"locations"`
	Assumptions []string `json:"assumptions"`
}

// Group is the finding's members, grouped by repository family.
type Group struct {
	Count   int           `json:"count"`
	Members []MemberGroup `json:"members"`
}

// MemberGroup is the members of one family: Family is the root of the
// family's first repository, empty for members outside every repository.
type MemberGroup struct {
	Family string   `json:"family"`
	Count  int      `json:"count"`
	Items  []string `json:"items"`
}

// Posture is one count of the posture block.
type Posture struct {
	Code    string         `json:"code"`
	Count   int            `json:"count"`
	ByClass map[string]int `json:"by_class"`
	creds   []string       // the credentials counted, sorted
}

// Creds are the ids of the credentials the posture line counts, sorted.
func (p Posture) Creds() []string { return p.creds }

// way is one element a finding stands on: an inferred edge, or an egress
// path (a capability: no edge exists for "can send it out").
type way struct {
	id          string // edge id, or capability id for egress
	code        string // file_tools, bash_read_only, mode_auto, ..., egress_*
	cap         string // the capability it goes through
	target      string // the edge's credential or resource; empty for egress
	radius      string // reaches only
	assumptions []string
}

// agentView is one Claude Code client's capabilities as the rules need them.
type agentView struct {
	node       graph.Node
	inForce    []graph.Node
	mode       string
	modeCap    *graph.Node
	blockReads bool
	egress     []way
	base       knowledge.Base
}

// Evaluate applies the rules to the graph.
func Evaluate(in Input) Result {
	g := in.Graph
	nodes := make(map[string]graph.Node, len(g.Nodes))
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	caps := map[string][]graph.Node{}
	scopes := map[string]string{} // credential → observed_scope
	observed := map[string]bool{} // credential → authenticates_to observed
	for _, e := range g.Edges {
		switch e.Type {
		case graph.EdgeHas:
			if c, ok := nodes[e.To]; ok && c.InForce {
				caps[e.From] = append(caps[e.From], c)
			}
		case graph.EdgeAuthenticatesTo:
			if e.ObservedScope != "" && scopes[e.From] == "" {
				scopes[e.From] = e.ObservedScope
			}
			if e.Certainty == graph.CertaintyObserved {
				observed[e.From] = true
			}
		}
	}
	var res Result
	agents := map[string]*agentView{}
	view := func(id string) *agentView {
		if v, ok := agents[id]; ok {
			return v
		}
		v := newAgentView(nodes[id], caps[id], in.baseFor(nodes[id].Platform), in.RepoRoots, &res.Launchers)
		agents[id] = v
		return v
	}

	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient {
			view(n.ID)
		}
	}

	// Credentials: reaches edges per agent and credential.
	type pair struct{ agent, cred string }
	reaches := map[pair][]graph.Edge{}
	writes := map[string][]graph.Edge{} // finding key → edges
	writeRule := map[string]string{}
	for _, e := range g.Edges {
		switch e.Type {
		case graph.EdgeReaches:
			if nodes[e.To].Kind == graph.KindCredential {
				reaches[pair{e.From, e.To}] = append(reaches[pair{e.From, e.To}], e)
			}
		case graph.EdgeWritesConfigOf:
			r := nodes[e.To]
			rule, key := writeRuleOf(e.From, r)
			if rule == "" {
				continue
			}
			writes[key] = append(writes[key], e)
			writeRule[key] = rule
		}
	}

	posture := map[string]map[string]string{} // code → credential → class
	addPosture := func(code, cred, class string) {
		if posture[code] == nil {
			posture[code] = map[string]string{}
		}
		posture[code][cred] = class
	}
	// Test data (M10, and by default since decision 2026-10-26): a
	// silenced key gives no finding at all, in git or read by an agent; it
	// is counted and shown instead.
	git, silenced, testData, silencedCreds := gitFindings(in, nodes, scopes, observed, addPosture)
	var perCred []Finding
	for p, edges := range reaches {
		if silencedCreds[p.cred] {
			continue
		}
		a, cred := view(p.agent), nodes[p.cred]
		class := classOf(cred, scopes[cred.ID])
		if allTemplates(cred) {
			addPosture(PostureInTemplatesOnly, cred.ID, class)
			continue
		}
		var rule, sev string
		switch {
		case len(a.egress) > 0 && class == ClassHigh:
			rule, sev = RuleCredReadAndSend, SeverityRed
		case len(a.egress) > 0 && (class == ClassMedium || class == ClassUnclassified):
			rule, sev = RuleCredReadAndSend, SeverityYellow
		case len(a.egress) == 0 && class == ClassHigh:
			rule, sev = RuleCredReadable, SeverityYellow
		default:
			addPosture(PostureReadableWithoutPrompt, cred.ID, class)
			if class == ClassMedium || class == ClassUnclassified {
				if res.MediumReadable == nil {
					res.MediumReadable = map[string]map[string]bool{}
				}
				if res.MediumReadable[p.agent] == nil {
					res.MediumReadable[p.agent] = map[string]bool{}
				}
				res.MediumReadable[p.agent][cred.ID] = true
			}
			continue
		}
		f := credFinding(in, nodes, a, cred, scopes[cred.ID], class, rule, sev, edges)
		f.observed = observed[cred.ID]
		perCred = append(perCred, f)
	}
	res.Findings = append(res.Findings, mergeSameFiles(in, perCred)...)
	for key, edges := range writes {
		res.Findings = append(res.Findings, writeFinding(in, view(edges[0].From), nodes, writeRule[key], edges))
	}

	for i := range res.Findings {
		f := &res.Findings[i]
		f.Actions = actionsFor(in, view(agentOf(*f)), *f, nodes)
	}
	res.Findings = append(res.Findings, git...)
	// Threat-intel matches (BV-100, F0a): one red finding per node the cross
	// marked. The match rides on the node in memory; nothing is read here that
	// is not already in the graph.
	for _, n := range g.Nodes {
		if n.Malicious != nil {
			res.Findings = append(res.Findings, maliciousPackageFinding(in, n))
		}
	}
	res.Silenced, res.TestData = silenced, testData
	if res.Findings == nil {
		res.Findings = []Finding{} // a report with no finding still lists none
	}
	sortFindings(res.Findings)
	res.Today = pickToday(res.Findings)
	for _, code := range AllPostures {
		p := Posture{Code: code, ByClass: map[string]int{}}
		for _, c := range AllClasses {
			p.ByClass[c] = 0
		}
		for cred, class := range posture[code] {
			p.Count++
			p.ByClass[class]++
			p.creds = append(p.creds, cred)
		}
		sort.Strings(p.creds)
		res.Posture = append(res.Posture, p)
	}
	sort.Slice(res.Launchers, func(i, j int) bool { return res.Launchers[i].Capability < res.Launchers[j].Capability })

	// agents_overview (decision 2026-10-25, cómo trabajan tus agentes,
	// cálculo): from the same views and findings, and the probe's targets.
	oi := overviewInputs{
		in: in, nodes: nodes, view: view, findings: res.Findings, scopes: scopes,
		reaches: map[string]map[string]bool{}, foreign: map[string][]graph.Edge{},
	}
	for p := range reaches {
		if oi.reaches[p.agent] == nil {
			oi.reaches[p.agent] = map[string]bool{}
		}
		oi.reaches[p.agent][p.cred] = true
	}
	for _, e := range g.Edges {
		if e.Type != graph.EdgeWritesConfigOf {
			continue
		}
		if to := nodes[nodes[e.To].Configures]; to.ID != "" && to.Platform != nodes[e.From].Platform {
			oi.foreign[e.From] = append(oi.foreign[e.From], e)
		}
	}
	probe, err := graph.Probe(g, in.RepoRoots, in.Home)
	if err != nil {
		slog.Warn("rules: agents overview probe", "err", err)
	}
	oi.probe = probe
	res.AgentsOverview = overview(oi)
	return res
}

// agentOf is the agent node of a finding (the first evidence node).
func agentOf(f Finding) string { return f.Evidence.Nodes[0] }

// allTemplates reports a credential whose every location is a template file.
func allTemplates(cred graph.Node) bool {
	files := graph.CredentialFiles(cred)
	for _, f := range files {
		if !isTemplate(f) {
			return false
		}
	}
	return len(files) > 0
}

// maliciousPackageFinding is the red finding for a node the threat-intel cross
// marked (BV-100, F0a): confidence high, radius any_project. Its params carry
// the ecosystem, the normalized package name, the exact version and the intel
// entry id — never a value read from the machine.
func maliciousPackageFinding(in Input, n graph.Node) Finding {
	m := n.Malicious
	members := []string{}
	if n.Path != "" {
		members = []string{n.Path}
	}
	return Finding{
		ID:          findingID(RulePackageKnownMalicious, n.ID, m.IntelID),
		Rule:        RulePackageKnownMalicious,
		RuleVersion: RuleVersion,
		Severity:    SeverityRed,
		Confidence:  ConfidenceHigh,
		Radius:      RadiusAnyProject,
		Params: Params{
			Ecosystem: m.Ecosystem, Package: m.Package, Version: m.Version,
			IntelID: m.IntelID, Count: 1,
		},
		Evidence: Evidence{Nodes: []string{n.ID}, Edges: []string{}, Locations: members, Assumptions: []string{}},
		Group:    group(in, members),
		// The fix is rendered from finding.package_known_malicious.action, not
		// from the generic Actions machinery; the list is empty but present so
		// the report validates against report.v2.
		Actions: []Action{},
	}
}

// writeRuleOf is the rule and finding key of a writes_config_of edge from
// agent to resource r (§1 c, d): another agent's config or executable, its
// instructions, or the agent's own.
func writeRuleOf(agent string, r graph.Node) (rule, key string) {
	config := r.Type == graph.ResourceAgentConfig || r.Type == graph.ResourceAgentExecutable
	switch {
	case r.Configures != agent && config:
		rule = RuleForeignAgentConfigWritable
	case r.Configures != agent && r.Type == graph.ResourceAgentInstructions:
		rule = RuleForeignAgentInstructionsWritable
	case config:
		rule = RuleOwnConfigWritable
	case r.Type == graph.ResourceAgentInstructions:
		rule = RuleOwnInstructionsWritable
	default:
		return "", ""
	}
	if r.Configures != agent {
		return rule, findingID(rule, agent, r.Configures, r.Type)
	}
	return rule, findingID(rule, agent, r.Type)
}

// findingID is "f_" + sha16(rule, key): the rule version does not enter it.
func findingID(rule string, key ...string) string {
	return "f_" + graph.KeyHash(append([]string{rule}, key...)...)
}

func newAgentView(n graph.Node, caps []graph.Node, base knowledge.Base, repoRoots []string, launchers *[]LauncherRule) *agentView {
	v := &agentView{node: n, inForce: caps, base: base}
	sort.Slice(v.inForce, func(i, j int) bool { return v.inForce[i].ID < v.inForce[j].ID })
	v.mode, v.modeCap = graph.EffectiveMode(v.inForce)
	boundary := map[string]bool{}
	for _, c := range v.inForce {
		if c.Basis == graph.BasisConfigured && c.Type == graph.CapReadFiles && c.Approval == graph.ApprovalDeny && c.TargetScope == outsideWorkingDirectories {
			v.blockReads = true
			boundary[c.AppliesIn] = true
		}
	}
	if n.Platform == knowledge.CursorProduct {
		// readBoundary workspace narrows the radius when the user's
		// sandbox.json sets it, or every repository's (decision 2026-10-25,
		// Cursor, annex 2 point 8).
		v.blockReads = boundary["user"]
		if !v.blockReads && len(repoRoots) > 0 {
			all := true
			for _, r := range repoRoots {
				all = all && boundary["project:"+graph.CanonicalPath(r)]
			}
			v.blockReads = all
		}
		v.egress = v.cursorEgress(base)
		return v
	}
	v.egress = v.egressWays(base, launchers)
	return v
}

// cursorEgress are Cursor's ways to send data out without a person
// (decision 2026-10-25, Cursor, §2.4, annex 2 point 2): Auto-review, through
// the classifier; a terminalAllowlist entry that sends to the network or
// runs code; an mcpAllowlist "*:*". The documented network exceptions and
// the sandbox's default domains are no egress.
func (v *agentView) cursorEgress(base knowledge.Base) []way {
	baseA := []string{graph.AssumeProcessRunsAsUser, graph.AssumeVersionAsDocumented, graph.AssumeTeamPolicyUnknown}
	var out []way
	if v.modeCap != nil && v.mode == modeAutoReview {
		out = append(out, way{
			id: v.modeCap.ID, code: "egress_auto_review", cap: v.modeCap.ID,
			assumptions: append(append([]string{}, baseA...), graph.AssumeIDERunModeNotReadable, graph.AssumeAutoClassifierPermits),
		})
	}
	for _, c := range v.inForce {
		if c.Basis != graph.BasisConfigured || c.Approval != graph.ApprovalAllow {
			continue
		}
		switch {
		case c.Type == graph.CapExecuteShell && strings.HasPrefix(c.Rule, "terminalAllowlist:"):
			_, network, launcher := graph.CursorAllowlistClass(c.TargetScope, base)
			switch {
			case network:
				out = append(out, way{id: c.ID, code: "egress_allowlist", cap: c.ID, assumptions: append([]string{}, baseA...)})
			case launcher:
				out = append(out, way{id: c.ID, code: "egress_allowlist", cap: c.ID, assumptions: append(append([]string{}, baseA...), AssumeCodeLauncherRule)})
			}
		case c.Type == graph.CapInvokeMCPTool && c.TargetScope == "*:*":
			out = append(out, way{id: c.ID, code: "egress_mcp_allowlist", cap: c.ID, assumptions: append([]string{}, baseA...)})
		}
	}
	return out
}

const (
	outsideWorkingDirectories = "outside_working_directories"
	modeAuto                  = "auto"
	modeBypass                = "bypassPermissions"
	modeAcceptEdits           = "acceptEdits"
	modeAutoReview            = "auto_review"
	scopeAll                  = "*"
	shellReadOnlyEntry        = "shell_readonly_bash_commands"
	readInWorkDirsEntry       = "read_in_working_dirs"
)

// modeAssumptions are the assumptions of every way through the effective mode,
// as the inference adds them.
func (v *agentView) modeAssumptions() []string {
	a := []string{graph.AssumeUIModeChoiceUnknown}
	if v.modeCap != nil && v.modeCap.Basis == graph.BasisProductDefault && v.mode == modeAuto {
		a = append(a, graph.AssumeAutoModeAvailable)
	}
	return a
}

// egressWays are the agent's ways to send data out without a person (§0):
// bypassPermissions; auto, through the classifier; an execute_shell allow in
// force with target "*" or through a code launcher (D3), unless a deny or
// ask without specifier for its tool overrides it. WebFetch's preapproved
// domains and ask rules are no egress.
func (v *agentView) egressWays(base knowledge.Base, launchers *[]LauncherRule) []way {
	baseA := []string{graph.AssumeProcessRunsAsUser, graph.AssumeVersionAsDocumented}
	var out []way
	if v.modeCap != nil {
		switch v.mode {
		case modeBypass:
			out = append(out, way{
				id: v.modeCap.ID, code: "egress_bypass", cap: v.modeCap.ID,
				assumptions: append(append([]string{}, baseA...), v.modeAssumptions()...),
			})
		case modeAuto:
			out = append(out, way{
				id: v.modeCap.ID, code: "egress_auto", cap: v.modeCap.ID,
				assumptions: append(append(append([]string{}, baseA...), graph.AssumeAutoClassifierPermits), v.modeAssumptions()...),
			})
		}
	}
	for _, c := range v.inForce {
		if c.Type != graph.CapExecuteShell || c.Approval != graph.ApprovalAllow || c.Basis != graph.BasisConfigured || v.shellDenied(shellTool(c.Rule)) {
			continue
		}
		a := append(append([]string{}, baseA...), graph.AssumeNoPreToolUseHookBlocks)
		if c.TargetScope == scopeAll {
			out = append(out, way{id: c.ID, code: "egress_shell", cap: c.ID, assumptions: a})
			continue
		}
		if l, ok := knowledge.LauncherOf(c.TargetScope, base.CodeLaunchers); ok {
			*launchers = append(*launchers, LauncherRule{Capability: c.ID, Rule: c.Rule, Form: l.Form})
			out = append(out, way{id: c.ID, code: "egress_launcher", cap: c.ID, assumptions: append(a, AssumeCodeLauncherRule)})
		}
	}
	return out
}

// shellDenied reports a configured deny or ask in force for tool without a
// specifier, which overrides every allow of that tool.
func (v *agentView) shellDenied(tool string) bool {
	for _, c := range v.inForce {
		if c.Basis == graph.BasisConfigured && c.Type == graph.CapExecuteShell &&
			(c.Approval == graph.ApprovalDeny || c.Approval == graph.ApprovalAsk) &&
			c.TargetScope == scopeAll && (c.Rule == tool || strings.HasPrefix(c.Rule, tool+"(")) {
			return true
		}
	}
	return false
}

// shellTool is the tool of an execute_shell rule: Bash or PowerShell.
func shellTool(rule string) string {
	if rule == "PowerShell" || strings.HasPrefix(rule, "PowerShell(") {
		return "PowerShell"
	}
	return "Bash"
}

// wayCode is the way an inferred edge goes through, from its capability.
func (v *agentView) wayCode(c graph.Node, base knowledge.Base) string {
	pd := func(entry string) bool {
		e, ok := base.Entry(entry)
		return ok && c.Basis == graph.BasisProductDefault && c.Type == e.Type && c.TargetScope == e.TargetScope
	}
	switch {
	case pd(shellReadOnlyEntry):
		return "bash_read_only"
	case pd(readInWorkDirsEntry):
		return "file_tools"
	case v.node.Platform == knowledge.CursorProduct:
		return v.cursorWayCode(c, base)
	case c.Type == graph.CapPermissionMode && c.TargetScope == modeAuto:
		return "mode_auto"
	case c.Type == graph.CapPermissionMode && c.TargetScope == modeBypass:
		return "mode_bypass"
	case c.Type == graph.CapPermissionMode && c.TargetScope == modeAcceptEdits:
		return "mode_accept_edits"
	case c.Type == graph.CapWriteFiles:
		return "edit_allow"
	case c.Type == graph.CapExecuteShell:
		return "shell_allow"
	}
	return "other"
}

// cursorWayCode is the way of a Cursor edge: Auto-review's shell, a
// terminalAllowlist entry, or the file tools.
func (v *agentView) cursorWayCode(c graph.Node, base knowledge.Base) string {
	pd := func(entry string) bool {
		e, ok := base.Entry(entry)
		return ok && c.Basis == graph.BasisProductDefault && c.Type == e.Type && c.TargetScope == e.TargetScope
	}
	switch {
	case c.Type == graph.CapPermissionMode && c.TargetScope == modeAutoReview:
		return "mode_auto_review"
	case pd("file_tools_read"):
		return "file_tools"
	case pd("file_tools_write_workspace"):
		return "file_tools_write"
	case c.Type == graph.CapExecuteShell:
		return "allowlist_shell"
	}
	return "other"
}

// edgeWay builds the way of an inferred edge. A reaches edge has radius
// any_project when its way does not depend on the working directory: the
// Bash read-only commands or bypassPermissions, without
// blockReadsOutsideWorkingDirectories in force (§1 b).
func (v *agentView) edgeWay(e graph.Edge, nodes map[string]graph.Node, base knowledge.Base) way {
	capID := e.Via
	if capID == "" && len(e.DerivedFrom) > 1 {
		capID = e.DerivedFrom[1] // writes_config_of: client, capability, resource
	}
	w := way{id: e.ID, cap: capID, target: e.To, code: v.wayCode(nodes[capID], base), assumptions: e.Assumptions}
	if e.Type == graph.EdgeReaches {
		w.radius = RadiusSameProject
		if (w.code == "bash_read_only" || w.code == "mode_bypass" || w.code == "mode_auto_review" || w.code == "allowlist_shell") && !v.blockReads {
			w.radius = RadiusAnyProject
		}
	}
	return w
}

// credFinding builds a cred_read_and_send or cred_readable finding: key agent
// + credential (its fingerprint), one finding for every location.
func credFinding(in Input, nodes map[string]graph.Node, a *agentView, cred graph.Node, scope, class, rule, sev string, edges []graph.Edge) Finding {
	var reach []way
	radius := RadiusSameProject
	for _, e := range edges {
		w := a.edgeWay(e, nodes, a.base)
		reach = append(reach, w)
		if w.radius == RadiusAnyProject {
			radius = RadiusAnyProject
		}
	}
	legs := [][]way{reach}
	if rule == RuleCredReadAndSend {
		legs = append(legs, a.egress)
	}
	f := Finding{
		ID: findingID(rule, a.node.ID, cred.Fingerprint), Rule: rule, RuleVersion: RuleVersion,
		Severity: sev, Radius: radius, class: class, credType: cred.Type, credID: cred.ID, legs: legs,
		Params: Params{Agent: a.node.Name, Service: cred.Service, Scope: scope, Class: class, Count: len(cred.Locations)},
	}
	f.Evidence = evidence(a.node.ID, []string{cred.ID}, legs, cred.Locations)
	f.Confidence = confidence(f.Evidence.Assumptions)
	f.Group = group(in, cred.Locations)
	return f
}

// OthersCount is how many more credentials a merged credential finding
// holds besides the one that gives its title (M12); not in the report's
// JSON, which lists them all in evidence.
func (f Finding) OthersCount() int { return f.others }

// privilegeLevel is the level of a credential within its class (M12, a table
// of the rules layer): A bypasses all authorisation or reaches the data
// (db_credential, Supabase service_role and secret, Stripe secret keys); B
// controls infrastructure or code (AWS, GitHub, Railway, Slack); C the rest.
// Lower is stronger.
func privilegeLevel(f Finding) int {
	switch {
	case f.credType == graph.CredentialDBCredential,
		f.Params.Service == "supabase" && (f.Params.Scope == "service_role" || f.Params.Scope == "secret"),
		f.Params.Service == "stripe" && f.credType == graph.CredentialAPIKey:
		return 0
	case f.Params.Service == "aws" || f.Params.Service == "github" || f.Params.Service == "railway" || f.Params.Service == "slack":
		return 1
	}
	return 2
}

// titleBefore is the total order of the credential that titles a merged
// finding (M12, §4): class; privilege level; authenticates_to observed over
// inferred; service; observed_scope; credential id.
func titleBefore(a, b Finding) bool {
	switch {
	case classRank[a.class] != classRank[b.class]:
		return classRank[a.class] > classRank[b.class]
	case privilegeLevel(a) != privilegeLevel(b):
		return privilegeLevel(a) < privilegeLevel(b)
	case a.observed != b.observed:
		return a.observed
	case a.Params.Service != b.Params.Service:
		return a.Params.Service < b.Params.Service
	case a.Params.Scope != b.Params.Scope:
		return a.Params.Scope < b.Params.Scope
	}
	return a.credID < b.credID
}

// classRank orders classes for a merge: the highest wins.
var classRank = map[string]int{ClassHigh: 3, ClassMedium: 2, ClassUnclassified: 1, ClassLow: 0}

// mergeSameFiles joins the credential findings of one agent, one service and
// one set of files (each location's part before ":<line>" or "#") into one
// finding (BV-39): severity and class the highest, confidence and radius the
// least favourable (medium, any_project), locations, evidence and ways
// joined; the label (scope and type) is the highest-class credential's, the
// lowest credential id on a tie. Key: agent + service + the sorted files.
func mergeSameFiles(in Input, fs []Finding) []Finding {
	type key struct{ agent, service, files string }
	groups := map[key][]Finding{}
	var order []key
	for _, f := range fs {
		files := sortedUnique(graph.CredentialFiles(graph.Node{Locations: f.Evidence.Locations}))
		k := key{agentOf(f), f.Params.Service, strings.Join(files, "\x00")}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], f)
	}
	out := make([]Finding, 0, len(order))
	for _, k := range order {
		members := groups[k]
		sort.Slice(members, func(i, j int) bool { return titleBefore(members[i], members[j]) })
		m := members[0] // the label's credential
		m.others = len(members) - 1
		var nodes, edges, locations, assumptions []string
		var reach []way
		for _, f := range members {
			nodes = append(nodes, f.Evidence.Nodes...)
			edges = append(edges, f.Evidence.Edges...)
			locations = append(locations, f.Evidence.Locations...)
			assumptions = append(assumptions, f.Evidence.Assumptions...)
			reach = append(reach, f.legs[0]...)
			if f.Rule == RuleCredReadAndSend {
				m.Rule = RuleCredReadAndSend
			}
			if f.Severity == SeverityRed {
				m.Severity = SeverityRed
			}
			if f.Confidence == ConfidenceMedium {
				m.Confidence = ConfidenceMedium
			}
			if f.Radius == RadiusAnyProject {
				m.Radius = RadiusAnyProject
			}
		}
		m.legs = append([][]way{reach}, m.legs[1:]...)
		m.Evidence = Evidence{
			Nodes:       agentFirst(sortedUnique(nodes), k.agent),
			Edges:       sortedUnique(edges),
			Locations:   sortedUnique(locations),
			Assumptions: sortedUnique(assumptions),
		}
		files := strings.Split(k.files, "\x00")
		m.ID = findingID(m.Rule, append([]string{k.agent, k.service}, files...)...)
		m.Params.Count = len(m.Evidence.Locations)
		m.Group = group(in, m.Evidence.Locations)
		out = append(out, m)
	}
	return out
}

// writeFinding builds a finding of rule (c) or (d) from its writes_config_of
// edges: one finding for every resource of the same agent and type.
func writeFinding(in Input, a *agentView, nodes map[string]graph.Node, rule string, edges []graph.Edge) Finding {
	var leg []way
	var resources, paths []string
	seen := map[string]bool{}
	for _, e := range edges {
		leg = append(leg, a.edgeWay(e, nodes, a.base))
		if !seen[e.To] {
			seen[e.To] = true
			resources = append(resources, e.To)
			paths = append(paths, nodes[e.To].Path)
		}
	}
	r := nodes[edges[0].To]
	_, key := writeRuleOf(a.node.ID, r)
	f := Finding{
		ID: key, Rule: rule, RuleVersion: RuleVersion, legs: [][]way{leg},
		Params: Params{Agent: a.node.Name, ResourceType: r.Type, Count: len(resources)},
	}
	switch rule {
	case RuleForeignAgentConfigWritable, RuleOwnConfigWritable:
		f.Severity = SeverityRed
	default:
		f.Severity = SeverityYellow
	}
	if r.Configures != a.node.ID {
		f.Params.TargetAgent = nodes[r.Configures].Name
	}
	f.Evidence = evidence(a.node.ID, resources, f.legs, paths)
	if r.Configures != a.node.ID {
		f.Evidence.Nodes = sortedUnique(append(f.Evidence.Nodes, r.Configures))
		f.Evidence.Nodes = agentFirst(f.Evidence.Nodes, a.node.ID)
	}
	f.Confidence = confidence(f.Evidence.Assumptions)
	f.Group = group(in, paths)
	return f
}

// evidence collects the nodes (agent first), edges, locations and the union
// of the assumptions of every way the finding uses.
func evidence(agent string, targets []string, legs [][]way, locations []string) Evidence {
	nodes := append([]string{}, targets...)
	var edges, assumptions []string
	for _, leg := range legs {
		for _, w := range leg {
			nodes = append(nodes, w.cap)
			if !strings.HasPrefix(w.code, "egress_") {
				edges = append(edges, w.id)
			}
			assumptions = append(assumptions, w.assumptions...)
		}
	}
	return Evidence{
		Nodes:       agentFirst(sortedUnique(append(nodes, agent)), agent),
		Edges:       sortedUnique(edges),
		Locations:   sortedUnique(locations),
		Assumptions: sortedUnique(assumptions),
	}
}

// confidence is medium when a premise assumption is among the finding's,
// high otherwise (E4: assumptions lower the confidence, never the severity).
func confidence(assumptions []string) string {
	for _, a := range assumptions {
		if premiseAssumptions[a] {
			return ConfidenceMedium
		}
	}
	return ConfidenceHigh
}

// group gathers the members (locations or resource paths) by repository
// family; members outside every repository go in the family "".
func group(in Input, members []string) Group {
	byFamily := map[string][]string{}
	for _, m := range sortedUnique(members) {
		fam := familyOf(in, m)
		byFamily[fam] = append(byFamily[fam], m)
	}
	g := Group{Count: len(sortedUnique(members)), Members: []MemberGroup{}}
	for fam, items := range byFamily {
		g.Members = append(g.Members, MemberGroup{Family: fam, Count: len(items), Items: items})
	}
	sort.Slice(g.Members, func(i, j int) bool {
		a, b := g.Members[i], g.Members[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Family < b.Family
	})
	return g
}

// familyOf is the family of the deepest configured repository holding the
// member's file, or "".
func familyOf(in Input, member string) string {
	file := memberFile(member)
	best := ""
	for _, r := range in.RepoRoots {
		if graph.Within(r, file) && len(r) > len(best) {
			best = r
		}
	}
	if best == "" {
		return ""
	}
	if fam, ok := in.Families[graph.CanonicalPath(best)]; ok {
		return fam
	}
	return best
}

// memberFile is the file of a location: before "#", without a final ":line".
func memberFile(loc string) string {
	if f, ok := graph.FileOfResource(loc); ok && strings.Contains(loc, "#") {
		return f
	}
	files := graph.CredentialFiles(graph.Node{Locations: []string{loc}})
	if len(files) == 1 {
		return files[0]
	}
	return loc
}

func sortedUnique(ss []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range ss {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// agentFirst moves the agent id to the front of a sorted id list.
func agentFirst(ids []string, agent string) []string {
	out := []string{agent}
	for _, id := range ids {
		if id != agent {
			out = append(out, id)
		}
	}
	return out
}
