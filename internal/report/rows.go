package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/rules"
)

// credRow is one row of the HTML and the Markdown (decision 2026-10-26,
// ruido del informe, point 2; v3 §12.2): the findings of one credential and
// one rule, cred_read_and_send or cred_readable, in several agents, shown
// once. Only presentation: the findings, their ids and the scores are
// untouched.
type credRow struct {
	rep    rules.Finding // the finding whose body the row shows
	ids    []string      // every finding of the row
	agents []string      // the agents' names, in the order of v3 §3 point 6
}

// credRows maps each finding id to its row: a finding with no other agent
// for its credential and rule is a row of its own.
func credRows(v V2) map[string]*credRow {
	rank := agentRank(v)
	groups := map[string][]rules.Finding{}
	out := map[string]*credRow{}
	for _, f := range v.Findings {
		if f.Rule != rules.RuleCredReadAndSend && f.Rule != rules.RuleCredReadable {
			out[f.ID] = &credRow{rep: f, ids: []string{f.ID}, agents: []string{f.Params.Agent}}
			continue
		}
		var creds []string
		for _, n := range f.Evidence.Nodes {
			if strings.HasPrefix(n, "cred_") {
				creds = append(creds, n)
			}
		}
		k := f.Rule + "\x00" + strings.Join(creds, "\x00")
		groups[k] = append(groups[k], f)
	}
	for _, fs := range groups {
		sort.SliceStable(fs, func(i, j int) bool { return rank(fs[i]) < rank(fs[j]) })
		r := &credRow{rep: fs[0]}
		seen := map[string]bool{}
		for _, f := range fs {
			r.ids = append(r.ids, f.ID)
			if !seen[f.Params.Agent] {
				seen[f.Params.Agent] = true
				r.agents = append(r.agents, f.Params.Agent)
			}
			out[f.ID] = r
		}
	}
	return out
}

// RowCounts are the red and yellow rows of the report (BV-55): the
// findings as v3 §12.2 groups them, one row per credential and rule in
// several agents, so they add up to the rows of steps 2 and 3. Every count
// of findings the user sees is this; the report's file name keeps counting
// findings (V2Counts).
func RowCounts(v V2) (red, yellow int) {
	seen := map[*credRow]bool{}
	for _, r := range credRows(v) {
		if seen[r] {
			continue
		}
		seen[r] = true
		switch r.rep.Severity {
		case rules.SeverityRed:
			red++
		case rules.SeverityYellow:
			yellow++
		}
	}
	return red, yellow
}

// agentRank orders a finding by its agent as v3 §3 point 6 does: the
// evaluated agents by score, lowest first, ties by name; then the others,
// by name.
func agentRank(v V2) func(rules.Finding) string {
	pos := map[string]string{}
	var scored []int
	for i, a := range v.Index.Agents {
		if a.Value != nil {
			scored = append(scored, i)
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		a, b := v.Index.Agents[scored[i]], v.Index.Agents[scored[j]]
		if *a.Value != *b.Value {
			return *a.Value < *b.Value
		}
		return a.Agent < b.Agent
	})
	for i, k := range scored {
		pos[v.Index.Agents[k].AgentID] = fmt.Sprintf("%04d", i)
	}
	return func(f rules.Finding) string {
		if len(f.Evidence.Nodes) > 0 {
			if p, ok := pos[f.Evidence.Nodes[0]]; ok {
				return p
			}
		}
		return "~" + f.Params.Agent
	}
}
