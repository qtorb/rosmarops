// Package index is the Agent Posture Score (decision 2026-10-24, índice de
// postura agéntica, I1–I14 with adjustments 1 and 2): a score out of 100
// per evaluated agent, higher is better, inside the band of the agent's
// worst v2 finding. It reads the rules' findings; it adds no judgement of
// its own beyond the published weights.
package index

import (
	"math"
	"sort"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/rules"
)

// Version is index_version: it goes up when the weights, K, the bands or
// the coverage conditions change (I7). Version 2 adds cred_in_git
// (decision 2026-10-24, memoria, M8); version 3 counts the agent extensions
// as agents present, not evaluated (decision 2026-10-26, agentes y MCP
// nuevos, A3), with the weights of version 2. Version 4 adds
// mcp_external_action (AP-10, BV-117/BV-121). The weights of versions 1 to 3
// are kept for the double calculation of I7.
const Version = 4

// K is the constant of the formula (I3).
const K = 80.0

// Bands (I1, I12): the band is the agent's traffic light.
const (
	BandExposed   = "exposed"   // a red finding: 0–49
	BandOpenGaps  = "open_gaps" // a yellow finding, no red: 50–79
	BandContained = "contained" // neither: 80–100
)

// AllBands lists the bands.
var AllBands = []string{BandExposed, BandOpenGaps, BandContained}

// Reasons a score is not available (I5).
const (
	ReasonCoverage           = "coverage"             // the graph or a source of the agent could not be read
	ReasonNotApplicable      = "not_applicable"       // no AI agent on the machine
	ReasonAgentsNotEvaluated = "agents_not_evaluated" // machine score: an agent present has no inference
)

// Comparison statuses (I7).
const (
	CompareNone               = "none"
	CompareDirect             = "direct"
	CompareNotComparableRules = "not_comparable_rules"
	CompareNotComparableScope = "not_comparable_scope"
)

// Index is the index object of report.v2.json (I8).
type Index struct {
	IndexVersion     int          `json:"index_version"`
	RulesVersion     string       `json:"rules_version"`
	KnowledgeVersion string       `json:"knowledge_version"`
	Agents           []AgentIndex `json:"agents"`
	Machine          Score        `json:"machine"`
	Scope            Scope        `json:"scope"`

	// HistoryReset: index-history.json did not validate and was set aside
	// with the suffix .invalid; a new one starts (I9).
	HistoryReset bool `json:"history_reset"`
}

// Score is a value, or the code of why there is none.
type Score struct {
	Value       *int   `json:"value"`
	Unavailable string `json:"unavailable,omitempty"`
	Band        string `json:"band,omitempty"`
}

// AgentIndex is the score of one evaluated agent.
type AgentIndex struct {
	Agent   string `json:"agent"` // product name
	AgentID string `json:"agent_id"`
	Score
	Red    int `json:"red"`
	Yellow int `json:"yellow"`

	// Weights are, per finding, its weight and the score without it (I4).
	Weights []Weight `json:"weights"`

	// Projection is the score once today's action is applied, from its
	// declared effect (I6): full removes the findings it cuts completely,
	// radius_only takes them to same_project, partial changes nothing.
	Projection *Projection `json:"projection,omitempty"`

	Comparison Comparison `json:"comparison"`
}

// Weight is one finding's weight and the score without it.
type Weight struct {
	FindingID string  `json:"finding_id"`
	Weight    float64 `json:"weight"`
	Without   int     `json:"without"`
}

// Projection is the score after today's action.
type Projection struct {
	ActionID string `json:"action_id"`
	Value    int    `json:"value"`
}

// Comparison is how this score compares with the last run's (I7).
type Comparison struct {
	Status    string `json:"status"`
	Previous  *int   `json:"previous,omitempty"`
	PrevRepos int    `json:"prev_repos,omitempty"`
	NowRepos  int    `json:"now_repos,omitempty"`

	// MethodDelta, when index_version changed since the previous run, is
	// how much of the change comes from the method (I7): this score minus
	// this graph's score with the previous version's weights.
	MethodDelta      *int `json:"method_delta,omitempty"`
	PrevIndexVersion int  `json:"prev_index_version,omitempty"`
}

// Scope is what the score covers.
type Scope struct {
	Evaluated    []string `json:"evaluated"`
	NotEvaluated []string `json:"not_evaluated"`
	Repos        int      `json:"repos"`
}

// Agent is one AI agent of the graph (an ai_client).
type Agent struct {
	ID, Name, Product, ConfigDir string
}

// Input is what the score is computed from.
type Input struct {
	GraphBuilt bool
	Agents     []Agent

	// Inference are the products the knowledge base has inference for.
	Inference map[string]bool

	Findings []rules.Finding
	Today    rules.Today

	// MediumReadable counts, per agent id, the medium credentials read with
	// no way out (posture).
	MediumReadable map[string]int

	SourcesFailed []graph.SourceFailed
	Repos         int

	RulesVersion, KnowledgeVersion string

	// Previous is the last history entry, nil for a first run.
	Previous *Entry
}

// weightOf is a finding's weight (I2): by rule and credential class, ×1.5
// for radius any_project on the credential rules. Confidence and group
// size do not change it.
func weightOf(f rules.Finding) float64 { return weightIn(Version, f) }

// weightIn is a finding's weight in an index version: version 1 has no
// cred_in_git (weight 0); versions 2 and 3 weigh it 20 when red, ×1.5 with
// a remote in some member, and 8 when yellow. Version 3 changes no weight.
// Version 4 adds mcp_external_action, weight 0 before it (AP-10, «Gravedad y
// peso»): money 20 red / 6 asking; infrastructure and database 15; every
// other class 8; ×1.5 when it reaches every project (radius any_project).
func weightIn(version int, f rules.Finding) float64 {
	if f.Rule == rules.RuleMCPExternalAction {
		if version < 4 {
			return 0
		}
		var w float64
		switch f.Params.Category {
		case rules.CategoryMoney:
			if f.Severity == rules.SeverityRed {
				w = 20
			} else {
				w = 6
			}
		case rules.CategoryInfrastructure, rules.CategoryDatabase:
			w = 15
		default:
			w = 8
		}
		if f.Radius == rules.RadiusAnyProject {
			w *= 1.5
		}
		return w
	}
	if f.Rule == rules.RuleCredInGit {
		if version < 2 {
			return 0
		}
		if f.Severity != rules.SeverityRed {
			return 8
		}
		if f.Params.HasRemote != nil && *f.Params.HasRemote {
			return 30
		}
		return 20
	}
	var w float64
	switch f.Rule {
	case rules.RuleCredReadAndSend:
		w = 8 // medium (and unclassified, which counts as medium)
		if f.Params.Class == rules.ClassHigh {
			w = 20
		}
	case rules.RuleCredReadable:
		w = 8
	case rules.RuleForeignAgentConfigWritable, rules.RuleOwnConfigWritable:
		w = 15
	case rules.RuleForeignAgentInstructionsWritable:
		w = 6
	case rules.RuleOwnInstructionsWritable:
		w = 4
	}
	if (f.Rule == rules.RuleCredReadAndSend || f.Rule == rules.RuleCredReadable) && f.Radius == rules.RadiusAnyProject {
		w *= 1.5
	}
	return w
}

// postureWeight is the weight of one medium credential readable with no
// way out (I2). Low credentials and templates weigh 0.
const postureWeight = 2

// item is one weighed element: a finding's severity and weight.
type item struct {
	id       string
	severity string
	weight   float64
}

// score is the formula (I3): the band of the worst finding, and inside it
// ⌊span × K / (K + P)⌋ above the band's floor.
func score(items []item, posture float64) (int, string) {
	p, red, yellow := posture, false, false
	for _, it := range items {
		p += it.weight
		red = red || it.severity == rules.SeverityRed
		yellow = yellow || it.severity == rules.SeverityYellow
	}
	f := K / (K + p)
	switch {
	case red:
		return int(math.Floor(49 * f)), BandExposed
	case yellow:
		return 50 + int(math.Floor(29*f)), BandOpenGaps
	}
	return 80 + int(math.Floor(20*f)), BandContained
}

// Compute computes the index (I1–I8).
func Compute(in Input) Index {
	idx := Index{
		IndexVersion: Version, RulesVersion: in.RulesVersion, KnowledgeVersion: in.KnowledgeVersion,
		Agents: []AgentIndex{},
		Scope:  Scope{Evaluated: []string{}, NotEvaluated: []string{}, Repos: in.Repos},
	}
	byAgent, machine := groupFindings(in.Findings)
	agents := append([]Agent{}, in.Agents...)
	sort.SliceStable(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	// Detected, not evaluated: one per product code, an ai_client and an
	// extension of one product joined (A3).
	pending := map[string]bool{}
	for _, a := range agents {
		if !in.Inference[a.Product] {
			if !pending[a.Product] {
				pending[a.Product] = true
				idx.Scope.NotEvaluated = append(idx.Scope.NotEvaluated, a.Name)
			}
			continue
		}
		idx.Scope.Evaluated = append(idx.Scope.Evaluated, a.Name)
		idx.Agents = append(idx.Agents, agentIndex(in, a, append(append([]rules.Finding{}, byAgent[a.ID]...), machine...)))
	}
	idx.Scope.Evaluated, idx.Scope.NotEvaluated = uniq(idx.Scope.Evaluated), uniq(idx.Scope.NotEvaluated)

	// The machine score (I5): only when every agent present has a score;
	// the worst of them. No agent: not applicable, never 100.
	switch {
	case len(agents) == 0:
		idx.Machine.Unavailable = ReasonNotApplicable
	case len(idx.Scope.NotEvaluated) > 0:
		idx.Machine.Unavailable = ReasonAgentsNotEvaluated
	default:
		for _, a := range idx.Agents {
			if a.Value == nil {
				idx.Machine = Score{Unavailable: ReasonCoverage}
				break
			}
			if idx.Machine.Value == nil || *a.Value < *idx.Machine.Value {
				v := *a.Value
				idx.Machine = Score{Value: &v, Band: a.Band}
			}
		}
	}
	return idx
}

// covered reports the coverage conditions of §2 for agent a, per agent
// (BV-101, decision 3; LECTURA-103 §2): the graph was built and no source of
// THIS agent (a source inside its configuration folder) failed to be read. A
// source failing elsewhere — another agent's, or a global step — no longer voids
// this agent's score or note; it still raises the run to at least yellow (B6)
// via the status reducer and is still listed in «What we could not read», both
// independent of this function.
func covered(in Input, a Agent) bool {
	if !in.GraphBuilt {
		return false
	}
	if a.ConfigDir == "" {
		return true
	}
	for _, f := range in.SourcesFailed {
		if graph.Within(a.ConfigDir, f.Path) {
			return false
		}
	}
	return true
}

func agentIndex(in Input, a Agent, fs []rules.Finding) AgentIndex {
	ai := AgentIndex{Agent: a.Name, AgentID: a.ID, Weights: []Weight{}}
	for _, f := range fs {
		switch f.Severity {
		case rules.SeverityRed:
			ai.Red++
		case rules.SeverityYellow:
			ai.Yellow++
		}
	}
	if !covered(in, a) {
		ai.Unavailable = ReasonCoverage
		ai.Comparison = Comparison{Status: CompareNone}
		return ai
	}
	posture := float64(postureWeight * in.MediumReadable[a.ID])
	items := make([]item, len(fs))
	for i, f := range fs {
		items[i] = item{f.ID, f.Severity, weightOf(f)}
	}
	v, band := score(items, posture)
	ai.Value, ai.Band = &v, band
	for i, f := range fs {
		rest := append(append([]item{}, items[:i]...), items[i+1:]...)
		without, _ := score(rest, posture)
		ai.Weights = append(ai.Weights, Weight{FindingID: f.ID, Weight: items[i].weight, Without: without})
	}
	if p, ok := project(fs, in.Today, posture); ok {
		ai.Projection = &p
	}
	ai.Comparison = compare(in, ai)
	if prev := in.Previous; prev != nil && prev.IndexVersion != Version && ai.Comparison.Status == CompareDirect {
		// I7: the score of this graph with the previous method too; the
		// difference is the method's, not the machine's.
		old := make([]item, len(fs))
		for i, f := range fs {
			old[i] = item{f.ID, f.Severity, weightIn(prev.IndexVersion, f)}
		}
		then, _ := score(old, posture)
		d := v - then
		ai.Comparison.MethodDelta = &d
		ai.Comparison.PrevIndexVersion = prev.IndexVersion
	}
	return ai
}

// groupFindings are the findings by agent id and those with no agent, which
// weigh in every agent's score.
func groupFindings(fs []rules.Finding) (map[string][]rules.Finding, []rules.Finding) {
	byAgent := map[string][]rules.Finding{}
	var machine []rules.Finding
	for _, f := range fs {
		switch {
		case rules.IsMachineFinding(f):
			machine = append(machine, f)
		case len(f.Evidence.Nodes) > 0:
			byAgent[f.Evidence.Nodes[0]] = append(byAgent[f.Evidence.Nodes[0]], f)
		}
	}
	return byAgent, machine
}

// ActionProjections are, per evaluated agent with a score, the score after
// each radius_only action of its findings, computed as today's projection
// (I6, project): the line of each row whose action only narrows the reach
// (informe HTML v3 §15.3). Not part of the index.
func ActionProjections(in Input) map[string]map[string]int {
	out := map[string]map[string]int{}
	byAgent, machine := groupFindings(in.Findings)
	for _, a := range in.Agents {
		if !in.Inference[a.Product] || !covered(in, a) {
			continue
		}
		fs := append(append([]rules.Finding{}, byAgent[a.ID]...), machine...)
		posture := float64(postureWeight * in.MediumReadable[a.ID])
		for _, f := range fs {
			for _, act := range f.Actions {
				if act.Completeness != rules.CompletenessRadiusOnly {
					continue
				}
				if p, ok := project(fs, rules.Today{ActionID: act.ID}, posture); ok {
					if out[a.ID] == nil {
						out[a.ID] = map[string]int{}
					}
					out[a.ID][act.ID] = p.Value
				}
			}
		}
	}
	return out
}

// project applies today's action to the agent's findings (I6, §3): a
// finding where the action is full disappears; radius_only takes it to
// same_project; partial leaves it.
func project(fs []rules.Finding, today rules.Today, posture float64) (Projection, bool) {
	if today.ActionID == "" {
		return Projection{}, false
	}
	var items []item
	touched := false
	for _, f := range fs {
		g := f
		for _, act := range f.Actions {
			if act.ID != today.ActionID {
				continue
			}
			touched = true
			switch act.Completeness {
			case rules.CompletenessFull:
				g.Rule = "" // removed
			case rules.CompletenessRadiusOnly:
				g.Radius = rules.RadiusSameProject
			}
		}
		if g.Rule != "" {
			items = append(items, item{g.ID, g.Severity, weightOf(g)})
		}
	}
	if !touched {
		return Projection{}, false
	}
	v, _ := score(items, posture)
	return Projection{ActionID: today.ActionID, Value: v}, true
}

// compare is the comparison with the last run (I7): direct with the same
// versions and scope; not comparable when the rules or the knowledge base
// changed, or the scope changed (the evaluated agents, or more than 10%
// fewer repositories). A previous entry of an earlier index version is
// compared directly, with the method's share apart (MethodDelta); one of a
// later version is not comparable.
func compare(in Input, ai AgentIndex) Comparison {
	prev := in.Previous
	if prev == nil {
		return Comparison{Status: CompareNone}
	}
	var pa *EntryAgent
	for i := range prev.Agents {
		if prev.Agents[i].Agent == ai.Agent {
			pa = &prev.Agents[i]
		}
	}
	if pa == nil || pa.Value == nil || ai.Value == nil {
		return Comparison{Status: CompareNone}
	}
	switch {
	case prev.IndexVersion > Version || prev.RulesVersion != in.RulesVersion || prev.KnowledgeVersion != in.KnowledgeVersion:
		return Comparison{Status: CompareNotComparableRules, Previous: pa.Value}
	case !sameStrings(prev.Scope.Agents, evaluatedNames(in)) || float64(in.Repos) < 0.9*float64(prev.Scope.Repos):
		return Comparison{Status: CompareNotComparableScope, Previous: pa.Value, PrevRepos: prev.Scope.Repos, NowRepos: in.Repos}
	}
	return Comparison{Status: CompareDirect, Previous: pa.Value}
}

func evaluatedNames(in Input) []string {
	var out []string
	for _, a := range in.Agents {
		if in.Inference[a.Product] {
			out = append(out, a.Name)
		}
	}
	return uniq(out)
}

func sameStrings(a, b []string) bool {
	a, b = uniq(a), uniq(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func uniq(ss []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
