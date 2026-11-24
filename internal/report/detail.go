package report

import (
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/rules"
)

// A detail a person can manage (0.20): the report shows causes, not
// combinations. Problems with the same title, the same rule and the same
// level are one; inside the single «between agents» problem each agent is a
// folded line with its findings joined by type; only the first problems are
// open. All of it only while the page is drawn: no finding, its number, the
// score or any JSON output changes.

// issueSame: problems with the same title, rule and level, joined. Its
// parts are the problems it joins, in their order.
const issueSame = "same"

// detailOpen is how many problems the detail shows open: the same ones the
// table shows.
const detailOpen = r3Shown

// mergeSameTitle joins the problems of their own (one finding's problem)
// whose title, rule and level are the same. Each joined finding stays its
// own row, with its own «where» line and steps, so the join only happens
// when those rows are told apart by their «where» line: none empty, no two
// alike. A malicious package can have no location (an MCP server's
// package), so its rule is never joined.
func (c htmlCtx) mergeSameTitle(iss []issue) []issue {
	type group struct {
		at    int
		parts []issue
	}
	var order []string
	groups := map[string]*group{}
	for i, is := range iss {
		if is.kind != issueOne || len(is.findings) != 1 || is.findings[0].Rule == rules.RulePackageKnownMalicious {
			continue
		}
		f := is.findings[0]
		k := f.Rule + "\x00" + f.Severity + "\x00" + c.issueTitle(is)
		if groups[k] == nil {
			groups[k] = &group{at: i}
			order = append(order, k)
		}
		groups[k].parts = append(groups[k].parts, is)
	}
	joined := map[int]issue{}
	drop := map[int]bool{}
	for _, k := range order {
		g := groups[k]
		if len(g.parts) < 2 || !c.toldApart(g.parts) {
			continue
		}
		m := issue{kind: issueSame, key: "same:" + g.parts[0].key, parts: g.parts}
		for _, p := range g.parts {
			m.findings = append(m.findings, p.findings...)
		}
		joined[g.at] = m
		for _, p := range g.parts {
			for i, is := range iss {
				if is.key == p.key && i != g.at {
					drop[i] = true
				}
			}
		}
	}
	if len(joined) == 0 {
		return iss
	}
	var out []issue
	for i, is := range iss {
		if drop[i] {
			continue
		}
		if m, ok := joined[i]; ok {
			is = m
		}
		out = append(out, is)
	}
	sortIssues(out)
	return out
}

// toldApart reports that the distinct rows of parts each have a «where»
// line and no two have the same one.
func (c htmlCtx) toldApart(parts []issue) bool {
	seen := map[string]bool{}
	for _, r := range c.distinctRows(parts) {
		w := c.whereHTML(r.rep)
		if w == "" || seen[w] {
			return false
		}
		seen[w] = true
	}
	return true
}

// distinctRows are the rows of parts, each once, in order.
func (c htmlCtx) distinctRows(parts []issue) []*credRow {
	var out []*credRow
	seen := map[*credRow]bool{}
	for _, p := range parts {
		for _, r := range c.issueRows(p) {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	return out
}

// whereHTML is a finding's «where» line as its row shows it.
func (c htmlCtx) whereHTML(f rules.Finding) string {
	var sb strings.Builder
	d := c
	d.sb = &sb
	d.findingWhere(f)
	return sb.String()
}

// isGenericSecret reports a problem whose findings are the reading of a
// secret the report calls «A secret» (no known service, no name of its own).
func (c htmlCtx) isGenericSecret(is issue) bool {
	if len(is.findings) == 0 {
		return false
	}
	f := is.findings[0]
	return (f.Rule == rules.RuleCredReadAndSend || f.Rule == rules.RuleCredReadable) && c.credKey(f) == "generic"
}

// sameTitle is a joined problem's title, escaped: for secrets with no known
// service, how many and within reach of whom; otherwise the shared title and
// how many times. With one distinct row (one secret its agents' findings
// split), the shared title alone.
func (c htmlCtx) sameTitle(is issue) string {
	first := c.issueTitle(is.parts[0])
	n := len(c.distinctRows(is.parts))
	if n < 2 {
		return first
	}
	if c.isGenericSecret(is) {
		var names []string
		if r := c.rows[is.findings[0].ID]; r != nil {
			names = r.agents
		}
		return c.t("html.r3.group.secret.many", map[string]string{"n": strconv.Itoa(n), "agents": listNames(c.cat, names)})
	}
	s := html.EscapeString(c.cat.Text("html.r3.group.same", map[string]string{"title": r3Mark, "n": strconv.Itoa(n)}))
	return strings.Replace(s, r3Mark, first, 1)
}

// writeType is the kind of a «between agents» finding: the configuration,
// a program the other agent runs, or its instructions (memory included).
func writeType(f rules.Finding) string {
	switch {
	case f.Rule == rules.RuleForeignAgentInstructionsWritable:
		return "instructions"
	case f.Params.ResourceType == graph.ResourceAgentExecutable:
		return "program"
	}
	return "config"
}

// renderAgentLines is a single row's parts (the «between agents» problem,
// or one of its own shared by three agents or more): one folded line per
// agent, the first open, with its count and the oldest «open since» of its
// findings; inside, whom it can change (between agents only), its score
// gain and its findings, three or more of one kind joined in one row.
func (c htmlCtx) renderAgentLines(is issue) {
	for i, part := range is.parts {
		open := ""
		if i == 0 {
			open = " open"
		}
		count := len(c.issueRows(part))
		since := ""
		if d := c.openSince(part); d != "" {
			since = `<span class="r3-ag-s">` + c.t("html.verdict.still", map[string]string{"date": d}) + `</span>`
		}
		c.w(`<details class="r3-ag"%s><summary><span class="chev" aria-hidden="true">›</span><span class="r3-ag-n">%s</span><span class="r3-ag-c">%s</span>%s</summary><div class="r3-ag-b">`,
			open, html.EscapeString(c.agentName(part.agentID)), c.r3Count("html.r3.cfg.agent.count", count), since)
		if is.kind == issueConfigMany { // a problem of its own has no one else to change
			var names []string
			for _, t := range part.targets() {
				names = append(names, c.agentName(t))
			}
			c.w(`<p class="r3-ag-t">%s</p>`, c.t("html.r3.cfg.agent.targets", map[string]string{"targets": joinNames(names)}))
		}
		c.renderGain(part)
		c.renderTypedRows(part)
		c.w(`</div></details>`)
	}
}

// openSince is the oldest «open since» of a problem's findings, as a day
// label; "" with no saved date (the first review).
func (c htmlCtx) openSince(is issue) string {
	if !c.v.memoryPrev {
		return ""
	}
	oldest := ""
	for _, f := range is.findings {
		if f.Memory == nil || f.Memory.OpenSince == "" {
			continue
		}
		if oldest == "" || f.Memory.OpenSince < oldest {
			oldest = f.Memory.OpenSince
		}
	}
	if oldest == "" {
		return ""
	}
	if d, err := time.Parse("2006-01-02", oldest); err == nil {
		return c.cat.DayMonthLabel(d)
	}
	return oldest
}

// renderTypedRows are an agent's findings in the rows of today, those of a
// kind with three or more joined in one row that holds them.
func (c htmlCtx) renderTypedRows(part issue) {
	reps := c.rankedReps(part)
	byType := map[string][]rules.Finding{}
	for _, f := range reps {
		byType[writeType(f)] = append(byType[writeType(f)], f)
	}
	done := map[string]bool{}
	agent := c.agentName(part.agentID)
	for _, f := range reps {
		t := writeType(f)
		if len(byType[t]) < 3 {
			c.renderRow(f)
			continue
		}
		if done[t] {
			continue
		}
		done[t] = true
		c.w(`<details class="row"><summary><span class="chev" aria-hidden="true">›</span><span class="t">%s</span></summary><div class="rb r3-many">`,
			c.t("html.r3.cfg.many."+t, map[string]string{"agent": agent, "n": strconv.Itoa(len(byType[t]))}))
		for _, g := range byType[t] {
			c.renderRow(g)
		}
		c.w(`</div></details>`)
	}
}

// renderFoldedHead is the folded line of a problem past the first ones: its
// number, its family's small box (the family's name as its text), its
// title on one line, its count, its level and the arrow.
func (c htmlCtx) renderFoldedHead(n int, is issue) {
	fam := issueFamily(is)
	c.w(`<details class="r3-fold"><summary><span class="r3-n">%d</span>%s<span class="r3-fold-t">%s</span><span class="r3-fold-r"><span class="r3-fold-c">%s</span>%s<span class="chev" aria-hidden="true">›</span></span></summary>`,
		n, c.familyIconSmall(fam, is.red()), c.issueTitle(is), c.r3Count("html.r3.cfg.agent.count", len(c.issueRows(is))), c.pill(is))
}

// familyIconSmall is the folded line's 28 px box: the family's name is its
// text alternative, since the line does not write it.
func (c htmlCtx) familyIconSmall(family string, red bool) string {
	d, ok := familyIcons[family]
	if !ok {
		return ""
	}
	cls := "r3-ico sm amber"
	if red {
		cls = "r3-ico sm red"
	}
	return `<span class="` + cls + `" role="img" aria-label="` + c.t("html.family."+family, nil) + `"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + d + `</svg></span>`
}

// familyIconNeutral is a family's box with no level: the orchestrators'
// card's header.
func familyIconNeutral(family string) string {
	return strings.Replace(familyIcon(family, false), `class="r3-ico amber"`, `class="r3-ico neutral"`, 1)
}

// detailKeys are the catalog codes of the detail's new texts.
func detailKeys() []string {
	return append(ownManyKeys, []string{
		"html.r3.cfg.agent.count", "html.r3.cfg.agent.count.one", "html.r3.cfg.agent.targets",
		"html.r3.cfg.many.config", "html.r3.cfg.many.instructions", "html.r3.cfg.many.program",
		"html.r3.group.secret.many", "html.r3.group.same",
		"html.r3.rest.title", "html.r3.rest.title.one", "html.r3.rest.sub",
	}...)
}

// foldJS opens a folded problem when a link leads to it, for the browsers
// that do not open a <details> on their own: on a click on a #p-N link, on
// hashchange and on load. Local only: no network, no storage.
const foldJS = `(function(){function op(h){if(!h||h.indexOf('#p-')!==0)return;var t=document.getElementById(h.slice(1));if(!t)return;var d=t.closest('details');while(d){d.open=true;d=d.parentElement?d.parentElement.closest('details'):null}}
window.addEventListener('hashchange',function(){op(location.hash)});
document.addEventListener('click',function(e){var a=e.target.closest('a[href^="#p-"]');if(a){op(a.getAttribute('href'))}});
op(location.hash)})();`

// detailCSS is the style of the agent lines, the folded problems and the
// small and neutral boxes (0.20 detail, §4 and §6).
const detailCSS = `
.r3-ico.sm{width:28px;height:28px;border-radius:9px}
.r3-ico.sm svg{width:16px;height:16px}
.r3-ico.neutral{background:var(--ds-surface);color:var(--ds-ink-2)}
details.r3-ag{border-top:1px solid var(--ds-line)}
details.r3-ag:last-of-type{border-bottom:1px solid var(--ds-line)}
details.r3-ag>summary{display:flex;align-items:center;gap:10px;min-height:48px;list-style:none;cursor:pointer;flex-wrap:wrap}
details.r3-ag>summary::-webkit-details-marker{display:none}
.r3-ag-n{font-size:16px;font-weight:700}
.r3-ag-c{font-size:14px;color:var(--ds-muted)}
.r3-ag-s{margin-left:auto;font-size:13px;color:var(--ds-muted)}
.r3-ag-b{padding-left:22px}
.r3-ag-t{margin:0 0 6px;font-size:15px;color:var(--ds-ink-2)}
.r3-many{padding-left:22px}
.r3-rest-h{margin:12px 0 8px;font-size:18px;line-height:1.3;font-weight:700}
.r3-rest-s{margin:0 0 12px;font-size:15px;color:var(--ds-muted)}
details.r3-fold{border-top:1px solid var(--ds-line)}
details.r3-fold:last-of-type{border-bottom:1px solid var(--ds-line)}
details.r3-fold>summary{display:flex;align-items:center;gap:12px;min-height:52px;list-style:none;cursor:pointer}
details.r3-fold>summary::-webkit-details-marker{display:none}
details.r3-fold>summary .r3-n{width:26px;height:26px;font-size:13px}
.r3-fold-t{flex:1;min-width:0;font-size:16px;font-weight:700;line-height:1.35;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.r3-fold-r{display:flex;align-items:center;gap:10px;flex:none}
.r3-fold-c{font-size:13px;color:var(--ds-muted)}
details.r3-fold>.r3-p{padding:8px 0 16px}
@media (max-width:700px){
details.r3-fold>summary{flex-wrap:wrap;align-items:flex-start;padding:8px 0}
.r3-fold-t{white-space:normal;flex-basis:calc(100% - 38px - 40px)}
.r3-fold-r{flex-basis:100%;padding-left:78px}
.r3-ag-s{margin-left:0;flex-basis:100%;padding-left:20px}
}`
