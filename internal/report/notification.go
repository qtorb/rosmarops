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
// observed, a score that moved, a new agent or MCP server, or an MCP server
// that points somewhere else now — BV-225 A; moved findings do not count):
// notify.done.changed; else notify.done.same, which NothingChanged reports
// and a scheduled review no longer shows (BV-225 B).
func NotificationText(v V2, cat catalog.Catalog) (string, bool) {
	key, p, ok := notificationKey(v)
	if !ok {
		return "", false
	}
	if key == "notify.done.new" {
		n, _ := strconv.Atoi(p["count"])
		return cat.Plural(key, n, p), true
	}
	return cat.Text(key, p), true
}

// NothingChanged reports whether a scheduled review's notification would be
// notify.done.same: a scored agent, comparable with the last run, and nothing
// new or changed. Then the review shows no notification and opens nothing
// (BV-225 B); the report is written as always.
func NothingChanged(v V2) bool {
	key, _, ok := notificationKey(v)
	return ok && key == "notify.done.same"
}

// notificationKey is NotificationText's choice: the catalog key (the plural
// base for new findings), its parameters, and false without a scored agent.
func notificationKey(v V2) (string, map[string]string, bool) {
	c := htmlCtx{v: v}
	a, ok := c.scored()
	if !ok {
		return "", nil, false
	}
	p := map[string]string{"agent": a.Agent, "score": strconv.Itoa(*a.Value)}
	m := v.Memory
	cmp := a.Comparison.Status
	if m.Reason != "" || cmp == index.CompareNotComparableRules || cmp == index.CompareNotComparableScope {
		return "notify.done.not_comparable", p, true
	}
	if n := m.Counts[memory.VerdictNew] + m.Counts[memory.VerdictObservedAgain]; n > 0 {
		p["count"] = strconv.Itoa(n)
		return "notify.done.new", p, true
	}
	moved := false
	for _, x := range v.Index.Agents {
		moved = moved || x.Value != nil && x.Comparison.Status == index.CompareDirect && x.Comparison.Previous != nil && *x.Comparison.Previous != *x.Value
	}
	if m.Counts[memory.VerdictNoLongerObserved] > 0 || moved || len(v.NewAgents)+len(v.NewMCP)+len(v.MCPChanged) > 0 {
		return "notify.done.changed", p, true
	}
	return "notify.done.same", p, true
}
