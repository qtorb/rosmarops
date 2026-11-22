package report

import (
	"html"
	"sort"
	"strconv"
	"strings"

	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/rules"
)

// resolvedItem is one finding fixed since the last check (BV-273): what the
// memory holds, and the finding as the previous report had it, for its title.
// Finding is nil when the previous report no longer holds it (it was last seen
// longer ago).
type resolvedItem struct {
	memory.Gone
	Finding *rules.Finding
}

// resolvedItems pairs the memory's resolved findings with the previous
// report's, the most severe first, then by rule, then by when they opened.
func resolvedItems(gone []memory.Gone, prev []rules.Finding, salt []byte) []resolvedItem {
	if len(gone) == 0 {
		return nil
	}
	byMid := map[string]*rules.Finding{}
	for i := range prev {
		byMid[memory.Ref(salt, prev[i].ID)] = &prev[i]
	}
	out := make([]resolvedItem, 0, len(gone))
	for _, g := range gone {
		out = append(out, resolvedItem{Gone: g, Finding: byMid[g.Mid]})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Severity == rules.SeverityRed) != (b.Severity == rules.SeverityRed) {
			return a.Severity == rules.SeverityRed
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.OpenSince < b.OpenSince
	})
	return out
}

// renderR3Done is «Since the last check» inside block 1 (BV-273): drawn only
// when a finding was fixed since the last check or the sheet's score went up.
// The count only with fixed findings; the score line only when the score
// changed (a direct comparison); the three most severe fixed findings, the
// rest folded; the series only with three or more comparable scores.
func (c htmlCtx) renderR3Done(m index.MachineScore) {
	var prev, now *int
	agent := ""
	for i := range c.v.Index.Agents {
		a := &c.v.Index.Agents[i]
		if a.AgentID != m.AgentID || a.Value == nil {
			continue
		}
		agent = a.Agent
		if cmp := a.Comparison; cmp.Status == index.CompareDirect && cmp.Previous != nil && *cmp.Previous != *a.Value {
			prev, now = cmp.Previous, a.Value
		}
		break
	}
	fixed := c.v.resolved
	up := prev != nil && *now > *prev
	if len(fixed) == 0 && !up {
		return
	}
	c.w(`<div class="r3-done"><p class="r3-done-h">%s</p>`, c.t("html.r3.done.title", nil))
	if n := len(fixed); n > 0 {
		c.w(`<p>%s</p>`, html.EscapeString(c.r3Count("html.r3.done.count", n)))
	}
	if prev != nil {
		c.w(`<p>%s</p>`, c.t("html.r3.done.score", map[string]string{"prev": strconv.Itoa(*prev), "now": strconv.Itoa(*now)}))
	}
	if len(fixed) > 0 {
		head, rest := fixed, []resolvedItem(nil)
		if len(fixed) > 3 {
			head, rest = fixed[:3], fixed[3:]
		}
		c.w(`<ul>`)
		for _, it := range head {
			c.w(`<li>%s</li>`, c.resolvedTitle(it))
		}
		c.w(`</ul>`)
		if len(rest) > 0 {
			c.w(`<details><summary>%s</summary><ul>`, c.t("html.r3.done.more", map[string]string{"n": strconv.Itoa(len(rest))}))
			for _, it := range rest {
				c.w(`<li>%s</li>`, c.resolvedTitle(it))
			}
			c.w(`</ul></details>`)
		}
	}
	if s := c.v.seriesBy[agent]; agent != "" && len(s) >= 3 {
		if len(s) > 6 {
			s = s[len(s)-6:]
		}
		parts := make([]string, len(s))
		for i, v := range s {
			parts[i] = strconv.Itoa(v)
		}
		c.w(`<p class="r3-done-series">%s</p>`, c.t("html.memory.evolution", map[string]string{"series": strings.Join(parts, " → ")}))
	}
	c.w(`</div>`)
}

// resolvedTitle is a fixed finding's title as its row had it (rowTitle), from
// the previous report; when that report no longer holds it, its rule code,
// as the memory's lines name it.
func (c htmlCtx) resolvedTitle(it resolvedItem) string {
	if it.Finding != nil {
		return c.rowTitle(*it.Finding)
	}
	return html.EscapeString(it.Rule)
}

// sinceCounts is html.since.counts with the counts at 0 left out, each with
// its separator, except «still there» (BV-273). The text's parts are split on
// « · », and each part names one count.
func (c htmlCtx) sinceCounts(n map[string]int) string {
	params := map[string]string{}
	for k, v := range n {
		params[k] = strconv.Itoa(v)
	}
	var keep []string
	for _, part := range strings.Split(c.cat.Text("html.since.counts", nil), " · ") {
		drop := false
		for k, v := range n {
			if k != "still" && v == 0 && strings.Contains(part, "{"+k+"}") {
				drop = true
			}
		}
		if !drop {
			keep = append(keep, part)
		}
	}
	return fillParams(strings.Join(keep, " · "), params)
}

// fillParams replaces {name} placeholders in t from params.
func fillParams(t string, params map[string]string) string {
	for k, v := range params {
		t = strings.ReplaceAll(t, "{"+k+"}", v)
	}
	return t
}
