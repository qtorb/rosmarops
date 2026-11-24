package report

import (
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/rules"
)

// issueOwnMany: three or more agents with the same problem of their own
// (rewriting their own instructions, changing their own settings, acting
// without you) and the same level, one row with a folded line per agent.
// Its parts are each agent's problem, in their order.
const issueOwnMany = "own_many"

// ownManyMin is from how many agents those problems become one row; with
// fewer, each agent keeps its own problem and title.
const ownManyMin = 3

// ownManyKey is the single row's sentence for each rule it joins.
var ownManyKey = map[string]string{
	rules.RuleOwnInstructionsWritable: "html.r3.issue.own_instructions_many",
	rules.RuleOwnConfigWritable:       "html.r3.issue.own_config_many",
	rules.RuleActsWithoutYou:          "html.r3.issue.acts_many",
}

// ownManyKeys are the catalog codes of the single rows.
var ownManyKeys = []string{
	"html.r3.issue.own_instructions_many", "html.r3.issue.own_config_many", "html.r3.issue.acts_many",
}

// joinsOwn reports a problem of its own that a single row can take: one
// finding of a joined rule, with its agent. Changing a program the agent
// runs has a title of its own and stays apart from its settings.
func joinsOwn(is issue) bool {
	if is.kind != issueOne || len(is.findings) != 1 {
		return false
	}
	f := is.findings[0]
	if ownManyKey[f.Rule] == "" || len(f.Evidence.Nodes) == 0 {
		return false
	}
	return !(f.Rule == rules.RuleOwnConfigWritable && f.Params.ResourceType == graph.ResourceAgentExecutable)
}

// mergeOwnIssues joins, per rule and level, the problems of their own of
// three or more agents into one row in the place of the first, then orders
// the problems again by the same rule as always (sortIssues). Levels apart
// stay in rows apart.
func mergeOwnIssues(iss []issue) []issue {
	type group struct {
		at     int
		parts  []issue
		agents map[string]bool
	}
	var order []string
	groups := map[string]*group{}
	for i, is := range iss {
		if !joinsOwn(is) {
			continue
		}
		f := is.findings[0]
		k := f.Rule + "\x00" + f.Severity
		if groups[k] == nil {
			groups[k] = &group{at: i, agents: map[string]bool{}}
			order = append(order, k)
		}
		is.agentID = f.Evidence.Nodes[0]
		groups[k].parts = append(groups[k].parts, is)
		groups[k].agents[is.agentID] = true
	}
	joined := map[int]issue{}
	drop := map[string]bool{}
	for _, k := range order {
		g := groups[k]
		if len(g.agents) < ownManyMin {
			continue
		}
		m := issue{kind: issueOwnMany, key: "own_many:" + k, parts: g.parts}
		for _, p := range g.parts {
			m.findings = append(m.findings, p.findings...)
			drop[p.key] = true
		}
		joined[g.at] = m
	}
	if len(joined) == 0 {
		return iss
	}
	var out []issue
	for i, is := range iss {
		if m, ok := joined[i]; ok {
			out = append(out, m)
			continue
		}
		if !drop[is.key] {
			out = append(out, is)
		}
	}
	sortIssues(out)
	return out
}

// ownManyText is a single row's sentence in the two pieces the table
// shows, as configManyText.
func (c htmlCtx) ownManyText(is issue) (head, tail string) {
	return c.manyText(is, ownManyKey[is.findings[0].Rule])
}
