package report

import (
	"strconv"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/memory"
)

// NotificationText is a scheduled review's notification (decision
// 2026-10-26, integridad y revisión semanal, J7): the agent of step 1's
// score, its score and the counts, nothing else; false without a scored
// agent. Not comparable with the last run (the memory or the score):
// notify.done.not_comparable; new findings (new or observed again):
// notify.done.new.*; else anything that changed (a finding no longer
// observed, a score that moved, a new agent or MCP server):
// notify.done.changed; else notify.done.same.
func NotificationText(v V2, cat catalog.Catalog) (string, bool) {
	c := htmlCtx{v: v}
	a, ok := c.scored()
	if !ok {
		return "", false
	}
	p := map[string]string{"agent": a.Agent, "score": strconv.Itoa(*a.Value)}
	m := v.Memory
	cmp := a.Comparison.Status
	if m.Reason != "" || cmp == index.CompareNotComparableRules || cmp == index.CompareNotComparableScope {
		return cat.Text("notify.done.not_comparable", p), true
	}
	if n := m.Counts[memory.VerdictNew] + m.Counts[memory.VerdictObservedAgain]; n > 0 {
		p["count"] = strconv.Itoa(n)
		return cat.Plural("notify.done.new", n, p), true
	}
	moved := false
	for _, x := range v.Index.Agents {
		moved = moved || x.Value != nil && x.Comparison.Status == index.CompareDirect && x.Comparison.Previous != nil && *x.Comparison.Previous != *x.Value
	}
	if m.Counts[memory.VerdictNoLongerObserved] > 0 || moved || len(v.NewAgents)+len(v.NewMCP) > 0 {
		return cat.Text("notify.done.changed", p), true
	}
	return cat.Text("notify.done.same", p), true
}
