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
	ID       string // profiles.P0 or profiles.P1; for Grok Bot cloud or local
	Graph    *graph.Graph
	Settings platform.ClaudeSettingsScan

	// Product is the agent the profile is for (BV-248): "" for Claude Code,
	// cursor or grok-bot; Declared and GrokBot are the pass's answers and scan.
	Product  string
	Declared map[string]map[string]string
	GrokBot  platform.GrokBotScan
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
	if p.Product != "" {
		fin.Declared, fin.GrokBot = p.Declared, p.GrokBot
	}
	return passOf(fin, *p.Graph, fams)
}

// applyProfiles fills, for Claude Code, its score and class with each profile,
// and the computer's (BV-236 §4): the lowest score among the evaluated agents
// with the profile, the class of the agent that gives it, and that agent.
func applyProfiles(v *V2, in V2Input, fams map[string]string) {
	byProduct := map[string]map[string]bool{}
	for _, a := range v.AgentsOverview {
		if a.Evaluated {
			if byProduct[a.Product] == nil {
				byProduct[a.Product] = map[string]bool{}
			}
			byProduct[a.Product][a.AgentID] = true
		}
	}
	for _, p := range in.Profiles {
		product := p.Product
		if product == "" {
			product = knowledge.ClaudeCodeProduct
		}
		agents := byProduct[product]
		if len(agents) == 0 {
			continue
		}
		r := profilePass(in, fams, p)
		if r == nil {
			continue
		}
		for i := range v.Index.Agents {
			a := &v.Index.Agents[i]
			if !agents[a.AgentID] {
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
		// The computer's score with a profile is Claude Code's (BV-236 §4).
		if product != knowledge.ClaudeCodeProduct || v.Index.Machine.Value == nil || m.Value == nil {
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
