// Package exposure computes the exposure potential of serie of each scored
// agent: the six axes of CTO-26 §1 and the class (rule B, §4), from the engine's
// own data. Autonomy (1), reach (2) and opacity (5) come from the reference
// computer R2 with one agent; contagion (3) and delegation (4) come from
// R2-todos (CTO-26 §2). Complexity (6) comes from the minimal agents registry
// (§3). The class is a weighted sum (rule B) and is provisional while any axis
// has no data. No user-facing prose: only ids, enums and numbers.
package exposure

import (
	"sort"

	"github.com/qtorb/rosmarops/internal/agents"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/rules"
)

// Axis names (CTO-26 §7.8, English).
const (
	AxisAutonomy   = "autonomy"
	AxisReach      = "reach"
	AxisContagion  = "contagion"
	AxisDelegation = "delegation"
	AxisOpacity    = "opacity"
	AxisComplexity = "complexity"
)

// AxisOrder is the fixed order axes are emitted in.
var AxisOrder = []string{AxisAutonomy, AxisReach, AxisContagion, AxisDelegation, AxisOpacity, AxisComplexity}

// Weights are rule B's axis weights (CTO-26 §4, signed). Max per axis is 3, so
// the maximum possible score is 2*3 + 2*3 + 2*3 + 1*3 + 1*3 + 1*3 = 27.
var Weights = map[string]int{
	AxisAutonomy: 2, AxisReach: 2, AxisContagion: 2,
	AxisDelegation: 1, AxisOpacity: 1, AxisComplexity: 1,
}

// no_data reasons (CTO-26 §6, closed enum).
const (
	NoDataContagionComputerMissing = "contagion_computer_missing"
	NoDataLoadsEdgeMissing         = "loads_edge_missing"
	NoDataRegistryMissing          = "registry_missing"
	NoDataNotEvaluated             = "not_evaluated"
)

// Axis is one axis of an agent: a 0–3 level with the input codes that justify
// it, or no level with a no_data reason. Level and NoData are mutually
// exclusive.
type Axis struct {
	Level  *int
	Inputs []string
	NoData string
}

func lvl(n int, inputs ...string) Axis {
	if inputs == nil {
		inputs = []string{}
	}
	return Axis{Level: &n, Inputs: inputs}
}

func noData(reason string) Axis { return Axis{NoData: reason, Inputs: []string{}} }

// Agent is one agent's exposure potential.
type Agent struct {
	Product     string
	Evaluated   bool
	Axes        map[string]Axis
	Class       string // "", "A".."D"
	ClassStatus string // "provisional" | "final" | "" (not evaluated)
	MissingAxes []string
	FactoryBand string
}

// Inputs are what Compute needs for one agent.
type Inputs struct {
	Product   string
	Evaluated bool

	// Single is the agent's agents_overview row on R2 (one agent): autonomy (1)
	// and reach (2). SingleAssumptions are the hidden-at-rest codes its findings
	// carry on R2: opacity (5).
	Single            rules.OverviewAgent
	SingleAssumptions []string

	// All is the agent's agents_overview row on R2-todos: delegation (4). AllGraph
	// and ClientID give the contagion graph signals (3): C1 shared instructions,
	// C2 reads_config_of.
	All      rules.OverviewAgent
	AllGraph graph.Graph
	ClientID string

	FactoryBand string
}

// Compute builds one agent's six axes and class.
func Compute(in Inputs) Agent {
	a := Agent{Product: in.Product, Evaluated: in.Evaluated, FactoryBand: in.FactoryBand, Axes: map[string]Axis{}}
	if !in.Evaluated {
		// Detected-only agent (CTO-26 §5, §6): opacity 3, the rest no data, no class.
		a.Axes[AxisOpacity] = lvl(3, "not_evaluated")
		for _, ax := range AxisOrder {
			if ax != AxisOpacity {
				a.Axes[ax] = noData(NoDataNotEvaluated)
			}
		}
		a.MissingAxes = missing(a.Axes)
		return a
	}
	a.Axes[AxisAutonomy] = autonomy(in.Single)
	a.Axes[AxisReach] = reach(in.Single)
	a.Axes[AxisContagion] = contagion(in.All, in.AllGraph, in.ClientID)
	a.Axes[AxisDelegation] = delegation(in.All)
	a.Axes[AxisOpacity] = opacity(in.SingleAssumptions)
	a.Axes[AxisComplexity] = complexity(in.Product)
	a.Class, a.ClassStatus, a.MissingAxes = classify(a.Axes)
	return a
}

// --- axes 1 and 2: from R2 with one agent ---

func cell(a rules.OverviewAgent, row string) (rules.OverviewCell, bool) {
	for _, c := range a.Rows {
		if c.Row == row {
			return c, true
		}
	}
	return rules.OverviewCell{}, false
}

type way struct{ decider, scope string }

func waysOf(c rules.OverviewCell) []way {
	out := []way{{c.Decider, c.Scope}}
	if c.Also != nil {
		out = append(out, way{c.Also.Decider, c.Also.Scope})
	}
	return out
}

func noAsk(decider string) bool {
	return decider == rules.DeciderNobody || decider == rules.DeciderModel
}

// noAskScope reports whether the row has a no-ask way whose scope satisfies ok.
func noAskScope(a rules.OverviewAgent, row string, ok func(scope string) bool) bool {
	c, found := cell(a, row)
	if !found {
		return false
	}
	for _, w := range waysOf(c) {
		if noAsk(w.decider) && ok(w.scope) {
			return true
		}
	}
	return false
}

func anyScope(scope string) bool  { return scope != rules.ScopeNone }
func isMachine(scope string) bool { return scope == rules.ScopeMachine }
func anyDest(scope string) bool   { return scope == rules.ScopeAnyDestination }
func sendsScope(scope string) bool {
	return scope == rules.ScopeAnyDestination || scope == rules.ScopeListedDestinations
}

// autonomy (axis 1): how many of four sensitive actions the agent does without
// asking, of serie.
func autonomy(a rules.OverviewAgent) Axis {
	var inputs []string
	a1 := noAskScope(a, rules.RowReadsFiles, isMachine)
	a2 := noAskScope(a, rules.RowWritesFiles, anyScope)
	a3 := noAskScope(a, rules.RowRunsCommands, anyScope) // sandbox counts
	a4 := noAskScope(a, rules.RowSendsOut, anyScope)
	n := 0
	for _, p := range []struct {
		ok   bool
		code string
	}{{a1, "reads_files:machine"}, {a2, "writes_files"}, {a3, "runs_commands"}, {a4, "sends_out"}} {
		if p.ok {
			n++
			inputs = append(inputs, p.code)
		}
	}
	level := n
	if level > 3 {
		level = 3
	}
	return lvl(level, inputs...)
}

// reach (axis 2): how far what it does without asking goes.
func reach(a rules.OverviewAgent) Axis {
	machine := noAskScope(a, rules.RowReadsFiles, isMachine) ||
		noAskScope(a, rules.RowWritesFiles, isMachine) ||
		noAskScope(a, rules.RowRunsCommands, isMachine)
	sendsOut := noAskScope(a, rules.RowSendsOut, sendsScope)
	sendsAny := noAskScope(a, rules.RowSendsOut, anyDest)
	anything := machine || sendsOut ||
		noAskScope(a, rules.RowReadsFiles, anyScope) ||
		noAskScope(a, rules.RowWritesFiles, anyScope) ||
		noAskScope(a, rules.RowRunsCommands, anyScope)

	var inputs []string
	if machine {
		inputs = append(inputs, "machine")
	}
	if sendsAny {
		inputs = append(inputs, "sends_out:any_destination")
	} else if sendsOut {
		inputs = append(inputs, "sends_out:listed_destinations")
	}
	switch {
	case !anything:
		return lvl(0)
	case machine && sendsAny:
		return lvl(3, inputs...)
	case machine || sendsOut:
		return lvl(2, inputs...)
	default:
		return lvl(1, "same_project_or_sandbox")
	}
}

// --- axis 4: delegation, from R2-todos ---

func delegation(a rules.OverviewAgent) Axis {
	modelIn := func(row string) bool {
		c, ok := cell(a, row)
		if !ok {
			return false
		}
		for _, w := range waysOf(c) {
			if w.decider == rules.DeciderModel {
				return true
			}
		}
		return false
	}
	level := 0
	var inputs []string
	add := func(row string, n int) {
		if modelIn(row) {
			if n > level {
				level = n
			}
			inputs = append(inputs, row+":model")
		}
	}
	add(rules.RowReadsFiles, 1)
	add(rules.RowWritesFiles, 2)
	add(rules.RowChangesOtherAgents, 2)
	add(rules.RowRunsCommands, 3)
	add(rules.RowSendsOut, 3)
	sort.Strings(inputs)
	return lvl(level, inputs...)
}

// --- axis 5: opacity, from R2 with one agent ---

func opacity(assumptions []string) Axis {
	seen := map[string]bool{}
	var hidden []string
	for _, code := range assumptions {
		if graph.AssumptionHiddenAtRest(code) && !seen[code] {
			seen[code] = true
			hidden = append(hidden, code)
		}
	}
	sort.Strings(hidden)
	level := len(hidden)
	if level > 2 {
		level = 2
	}
	return lvl(level, hidden...)
}

// --- axis 3: contagion, from R2-todos ---

func contagion(a rules.OverviewAgent, g graph.Graph, clientID string) Axis {
	c1 := loadsSharedInstructions(g, clientID)
	c2 := readsConfigOfAnother(g, clientID)
	c3inst, c3cfg := changesOtherAgents(a)

	var inputs []string
	if c1 {
		inputs = append(inputs, "C1:shared_instructions")
	}
	if c2 {
		inputs = append(inputs, "C2:reads_config_of")
	}
	if c3inst {
		inputs = append(inputs, "C3:instructions")
	}
	if c3cfg {
		inputs = append(inputs, "C3:config")
	}
	switch {
	case c3cfg || (c3inst && c2):
		return lvl(3, inputs...)
	case (c1 && c2) || c3inst:
		return lvl(2, inputs...)
	case c1:
		return lvl(1, inputs...)
	default:
		return lvl(0)
	}
}

// loadsSharedInstructions (C1): the agent is configured by an agent_instructions
// resource whose file also configures another agent — a shared instruction file
// (AGENTS.md, GEMINI.md, ~/.agents/skills) that others write.
func loadsSharedInstructions(g graph.Graph, clientID string) bool {
	byPath := map[string]map[string]bool{} // instruction path → set of client ids it configures
	for _, n := range g.Nodes {
		if n.Kind == graph.KindResource && n.Type == graph.ResourceAgentInstructions && n.Configures != "" {
			if byPath[n.Path] == nil {
				byPath[n.Path] = map[string]bool{}
			}
			byPath[n.Path][n.Configures] = true
		}
	}
	for _, clients := range byPath {
		if clients[clientID] && len(clients) >= 2 {
			return true
		}
	}
	return false
}

// readsConfigOfAnother (C2): the agent has a reads_config_of edge to another
// agent (BV-208 pt2).
func readsConfigOfAnother(g graph.Graph, clientID string) bool {
	for _, e := range g.Edges {
		if e.Type == graph.EdgeReadsConfigOf && e.From == clientID {
			return true
		}
	}
	return false
}

// changesOtherAgents (C3): the changes_other_agents row with a decider other
// than «you» (here, a no-ask way), split by whether it reaches another agent's
// instructions or its configuration.
func changesOtherAgents(a rules.OverviewAgent) (toInstructions, toConfig bool) {
	c, ok := cell(a, rules.RowChangesOtherAgents)
	if !ok {
		return false, false
	}
	for _, w := range waysOf(c) {
		if !noAsk(w.decider) {
			continue
		}
		switch w.scope {
		case rules.ScopeInstructions:
			toInstructions = true
		case rules.ScopeConfig:
			toConfig = true
		}
	}
	return toInstructions, toConfig
}

// --- axis 6: complexity, from the registry ---

func complexity(product string) Axis {
	e, ok := agents.EntryOf(product)
	if !ok {
		return noData(NoDataRegistryMissing)
	}
	_, level := e.Complexity()
	var inputs []string
	for _, s := range e.AffectsSources() {
		inputs = append(inputs, string(s.Layer)+":"+string(s.Format))
	}
	sort.Strings(inputs)
	return lvl(level, inputs...)
}

// --- the class (rule B, §4) ---

func missing(axes map[string]Axis) []string {
	var out []string
	for _, ax := range AxisOrder {
		if axes[ax].Level == nil {
			out = append(out, ax)
		}
	}
	return out
}

// classify is rule B: F = Σ weight·level / Σ weight·3 over the axes with data;
// cuts <0.25 A, <0.50 B, <0.75 C, ≥0.75 D. Provisional while any axis has no
// data.
func classify(axes map[string]Axis) (class, status string, missingAxes []string) {
	points, maxPossible := 0, 0
	for _, ax := range AxisOrder {
		a := axes[ax]
		if a.Level == nil {
			continue
		}
		points += Weights[ax] * *a.Level
		maxPossible += Weights[ax] * 3
	}
	missingAxes = missing(axes)
	status = "final"
	if len(missingAxes) > 0 {
		status = "provisional"
	}
	if maxPossible == 0 {
		return "", status, missingAxes
	}
	f := float64(points) / float64(maxPossible)
	switch {
	case f < 0.25:
		class = "A"
	case f < 0.50:
		class = "B"
	case f < 0.75:
		class = "C"
	default:
		class = "D"
	}
	return class, status, missingAxes
}

// AllowedClasses returns the classes coherent with a factory band (CTO-26 §4):
// contained → A,B; open_gaps → B,C; exposed → C,D.
func AllowedClasses(band string) []string {
	switch band {
	case "contained":
		return []string{"A", "B"}
	case "open_gaps":
		return []string{"B", "C"}
	case "exposed":
		return []string{"C", "D"}
	}
	return nil
}

// CoherentWithBand reports whether an agent's class is coherent with its factory
// band. A not-evaluated agent (no class) is vacuously coherent.
func (a Agent) CoherentWithBand() bool {
	if a.Class == "" {
		return true
	}
	for _, c := range AllowedClasses(a.FactoryBand) {
		if a.Class == c {
			return true
		}
	}
	return false
}
