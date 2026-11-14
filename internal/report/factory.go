package report

import (
	"github.com/qtorb/rosmarops/internal/exposure"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/rules"
)

// evaluate runs the rules layer over g with in's lists and raw scans.
func evaluate(in V2Input, g graph.Graph, fams map[string]string) rules.Result {
	return rules.Evaluate(rules.Input{
		Graph: g, Base: in.Base, Bases: in.bases(), RepoRoots: in.RepoRoots, Families: fams,
		Committed: committedOf(in.V1.Findings), Rotated: in.Rotated, Fixtures: in.Fixtures,
		Dismissed: in.Dismissed, Accepted: in.Accepted,
		ConfigFile: in.ConfigFile, Now: in.GeneratedAt, ClaudeSettings: in.ClaudeSettings, Home: in.Home,
		Hermes: in.Hermes, Codex: in.Codex, Grok: in.Grok, Cline: in.Cline,
	})
}

// factoryResult is the second, factory pass of a review (BV-235, R1): the
// index and the classes with every agent at its product defaults.
type factoryResult struct {
	idx     index.Index
	classes map[string]exposure.Agent // by agent id
}

// factoryPass evaluates in.FactoryGraph, the graph of this computer with
// every agent at its product defaults (pipeline.Factory), with the raw scans
// cut the same way. It is a pure function: no graph file, no memory, no
// history (CTO-48 §3.2).
func factoryPass(in V2Input, fams map[string]string) *factoryResult {
	if in.FactoryGraph == nil {
		return nil
	}
	fin := in
	fin.Graph = in.FactoryGraph
	if in.ClaudeSettings != nil {
		s := pipeline.FactorySettings(*in.ClaudeSettings)
		fin.ClaudeSettings = &s
	}
	fin.Hermes = pipeline.FactoryHermes(in.Hermes)
	fin.Codex = pipeline.FactoryCodex(in.Codex)
	fin.Grok = pipeline.FactoryGrok(in.Grok)
	fin.Cline = pipeline.FactoryCline(in.Cline)
	fin.Previous, fin.History, fin.HistoryReset = nil, nil, false
	fin.Memory, fin.MemoryReset = memory.File{}, false
	g := *in.FactoryGraph
	res := evaluate(fin, g, fams)
	vf := V2{Findings: res.Findings, Today: res.Today}
	if vf.Findings == nil {
		vf.Findings = []rules.Finding{}
	}
	vf.Coverage.SourcesFailed = g.Coverage.SourcesFailed
	if vf.Coverage.SourcesFailed == nil {
		vf.Coverage.SourcesFailed = []graph.SourceFailed{}
	}
	idx, _ := buildIndex(fin, res, vf)
	ov := agentsOverview(res.AgentsOverview, idx)
	return &factoryResult{idx: idx, classes: classes(ov, res.Findings, g, idx)}
}

// classes are the six-axis classes of every agent of an overview (BV-235,
// R0′; CTO-48 §4.2), with this computer's rows, findings and graph in place
// of R2's: opacity counts the hidden-at-rest assumptions of the agent's
// findings and of its cells.
func classes(ov []rules.OverviewAgent, fs []rules.Finding, g graph.Graph, idx index.Index) map[string]exposure.Agent {
	out := map[string]exposure.Agent{}
	for _, a := range ov {
		var assume []string
		for _, f := range fs {
			if len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == a.AgentID {
				assume = append(assume, f.Evidence.Assumptions...)
			}
		}
		band := ""
		for _, ai := range idx.Agents {
			if ai.AgentID == a.AgentID {
				band = ai.Band
			}
		}
		out[a.AgentID] = exposure.Compute(exposure.Inputs{
			Product: a.Product, Evaluated: a.Evaluated,
			Single: a, SingleAssumptions: assume,
			All: a, AllGraph: g, ClientID: a.AgentID,
			FactoryBand: band,
		})
	}
	return out
}

// classOf is an exposure class as report.v2 carries it, coherent against the
// band of the same review.
func classOf(c exposure.Agent, ok bool, band string) index.Class {
	out := index.NoClass()
	if !ok {
		return out
	}
	if c.Class != "" {
		cl := c.Class
		out.Class = &cl
		out.Coherent = c.Coherent(band)
	}
	out.F = c.F
	if c.ClassStatus != "" {
		st := c.ClassStatus
		out.Status = &st
	}
	if c.MissingAxes != nil {
		out.MissingAxes = append([]string{}, c.MissingAxes...)
	}
	return out
}

// classRank orders the classes, the worst last.
func classRank(c *string) int {
	if c == nil {
		return 0
	}
	return int((*c)[0]-'A') + 1
}

// applyClasses fills every agent's letter, factory score and factory letter,
// and the machine's (BV-235 points 1 and 2). The machine's letter is the class
// of the agent that gives its score; WorstLetterAgentID names the agent with
// a worse class when there is one.
func applyClasses(v *V2, own map[string]exposure.Agent, fr *factoryResult) {
	for i := range v.Index.Agents {
		a := &v.Index.Agents[i]
		c, ok := own[a.AgentID]
		a.Letter = classOf(c, ok, a.Band)
		a.Factory = index.Factory{Letter: index.NoClass()}
		reason := index.ReasonCoverage
		a.Factory.Unavailable = &reason
		if fr == nil {
			continue
		}
		for _, fa := range fr.idx.Agents {
			if fa.AgentID != a.AgentID {
				continue
			}
			if fa.Value != nil {
				val, band := *fa.Value, fa.Band
				a.Factory.Value, a.Factory.Band, a.Factory.Unavailable = &val, &band, nil
			} else if fa.Unavailable != "" {
				u := fa.Unavailable
				a.Factory.Unavailable = &u
			}
			fc, ok := fr.classes[fa.AgentID]
			a.Factory.Letter = classOf(fc, ok, fa.Band)
		}
	}
	m := &v.Index.Machine
	if m.Value == nil {
		return
	}
	var worst *index.AgentIndex
	for i := range v.Index.Agents {
		a := &v.Index.Agents[i]
		if a.AgentID == m.AgentID {
			l := a.Letter
			m.Letter = &l
		}
		if worst == nil || classRank(a.Letter.Class) > classRank(worst.Letter.Class) {
			worst = a
		}
	}
	if m.Letter != nil && worst != nil && classRank(worst.Letter.Class) > classRank(m.Letter.Class) {
		m.WorstLetterAgentID = worst.AgentID
	}
	if fr != nil && fr.idx.Machine.Value != nil {
		fv := *fr.idx.Machine.Value
		m.FactoryValue = &fv
		for _, fa := range fr.idx.Agents {
			if fa.AgentID == fr.idx.Machine.AgentID {
				fc, ok := fr.classes[fa.AgentID]
				l := classOf(fc, ok, fa.Band)
				m.FactoryLetter = &l
			}
		}
	}
}
