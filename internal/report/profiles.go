package report

import (
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// ProfileInput is one measured profile of Claude Code (CTO-50 §1, BV-236 §4):
// this computer's graph with the profile's block added to Claude Code's user
// settings (pipeline.Profile), built in memory and never written, and the
// settings scan it was built from.
type ProfileInput struct {
	ID       string // profiles.P0 or profiles.P1
	Graph    *graph.Graph
	Settings platform.ClaudeSettingsScan
}

// profilePass evaluates one profile's graph with its settings; every other
// agent keeps today's configuration. Like the factory pass, it writes
// nothing.
func profilePass(in V2Input, fams map[string]string, p ProfileInput) *factoryResult {
	if p.Graph == nil {
		return nil
	}
	fin := in
	s := p.Settings
	fin.ClaudeSettings = &s
	return passOf(fin, *p.Graph, fams)
}

// applyProfiles fills, for Claude Code, its score and class with each profile,
// and the computer's (BV-236 §4): the lowest score among the evaluated agents
// with the profile, the class of the agent that gives it, and that agent.
func applyProfiles(v *V2, in V2Input, fams map[string]string) {
	claude := map[string]bool{}
	for _, a := range v.AgentsOverview {
		if a.Product == knowledge.ClaudeCodeProduct && a.Evaluated {
			claude[a.AgentID] = true
		}
	}
	if len(claude) == 0 {
		return
	}
	for _, p := range in.Profiles {
		r := profilePass(in, fams, p)
		if r == nil {
			continue
		}
		for i := range v.Index.Agents {
			a := &v.Index.Agents[i]
			if !claude[a.AgentID] {
				continue
			}
			for _, pa := range r.idx.Agents {
				if pa.AgentID == a.AgentID && pa.Value != nil {
					c, ok := r.classes[pa.AgentID]
					a.Profiles = append(a.Profiles, index.Profile{Profile: p.ID, Value: *pa.Value, Letter: classOf(c, ok, pa.Band)})
				}
			}
		}
		m := r.idx.Machine
		if v.Index.Machine.Value == nil || m.Value == nil {
			continue
		}
		mp := index.MachineProfile{Profile: index.Profile{Profile: p.ID, Value: *m.Value, Letter: index.NoClass()}, AgentID: m.AgentID}
		for _, pa := range r.idx.Agents {
			if pa.AgentID == m.AgentID {
				c, ok := r.classes[pa.AgentID]
				mp.Letter = classOf(c, ok, pa.Band)
			}
		}
		v.Index.Machine.Profiles = append(v.Index.Machine.Profiles, mp)
	}
}
