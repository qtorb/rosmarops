package report

import (
	"html"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/rules"
)

// The report's first screen, in three blocks (signed design of 18-11-2026):
// the result, the findings grouped by problem, and the template that keeps
// them from coming back. It replaces the sheet and the «What to do first» /
// «What remains» flow; the per-agent views, the template picker and every
// finding with its steps stay behind their links.

// r3Shown is how many problems the findings table shows; the rest are one
// last row with a link to them.
const r3Shown = 6

// r3Tokens are the design system's colours the three blocks use, light and
// dark (RosmarOps design system, tokens by name).
var r3Tokens = []cssToken{
	{"ds-surface", "#F6F5F2", "#141513"},
	{"ds-raised", "#FFFFFF", "#1C1E1B"},
	{"ds-line", "#E3E0D8", "#2E302C"},
	{"ds-ink", "#1B1A18", "#ECEAE4"},
	{"ds-on-ink", "#FFFFFF", "#141513"},
	{"ds-ink-2", "#3A3833", "#C9C5BC"},
	{"ds-muted", "#4F4C46", "#9C978D"},
	{"ds-brand", "#2346B8", "#8FA8F5"},
	{"ds-red", "#94200B", "#F08A70"},
	{"ds-red-tint", "#FDECE7", "#3A1E17"},
	{"ds-amber", "#6B4700", "#E8C062"},
	{"ds-amber-tint", "#FDF5DF", "#352B12"},
	{"ds-green", "#1A5532", "#7FCB9C"},
	{"ds-green-tint", "#E8F5EE", "#16301F"},
}

// pendingFindings are the findings the first screen counts: every finding of
// this run but the ones the person accepted, in the report's order.
func (c htmlCtx) pendingFindings() []rules.Finding {
	var out []rules.Finding
	for _, f := range c.v.Findings {
		if f.Accepted == nil {
			out = append(out, f)
		}
	}
	return out
}

// issues are this report's problems, in the first screen's order.
func (c htmlCtx) issues() []issue {
	return issuesOf(c.pendingFindings(), c.v.credServices)
}

// renderR3 is the first screen.
func (c htmlCtx) renderR3(iss []issue) {
	c.w(`<div class="r3" id="top">`)
	c.renderR3Head()
	c.renderSheetNotes()
	c.renderR3Result()
	c.renderR3Findings(iss)
	c.renderR3Template(iss)
	c.renderR3Folds()
	c.w(`<footer class="r3-foot"><span>%s</span><span>%s</span></footer>`,
		c.t("html.r3.footer", nil), c.t("html.r3.method", map[string]string{"method": index.ClassRule}))
	c.w(`</div>`)
}

func (c htmlCtx) renderR3Head() {
	version := c.v.engineVersion
	// The environment's tag and lines (a test environment, a container, WSL,
	// a virtual machine) stay under the title, as they were in the sheet.
	env := c.envTag()
	for _, line := range EnvTexts(c.v.Environment, c.cat.Text) {
		env += `<p class="r3-sub">` + html.EscapeString(line) + `</p>`
	}
	c.w(`<header class="r3-head"><div><h1>%s</h1>%s<p class="r3-sub">%s</p></div><p class="r3-meta">%s</p></header>`,
		c.t("html.r3.title", nil), env, c.t("html.r3.sub", nil),
		c.t("html.r3.meta", map[string]string{
			"date": c.local.Format("2006-01-02 15:04"), "os": osName(c.v.OS),
			"agents": strconv.Itoa(len(c.v.Index.Scope.Evaluated)), "projects": strconv.Itoa(c.v.repos),
			"version": version,
		}))
}

// r3Title is a block's title: its number in a circle and its name.
func (c htmlCtx) r3Title(n int, id, key string) string {
	return `<h2 class="r3-h" id="` + id + `"><span class="r3-n" aria-hidden="true">` + strconv.Itoa(n) + `</span>` + c.t(key, nil) + `</h2>`
}

// bandTone is the design system's score colour of a band.
func bandTone(band string) string {
	switch band {
	case index.BandExposed:
		return "red"
	case index.BandOpenGaps:
		return "amber"
	}
	return "green"
}

// lowerFirst lowercases the first letter of a catalog phrase used inside a
// sentence (the class phrase after a colon).
func lowerFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToLower(r)) + s[n:]
}

func (c htmlCtx) renderR3Result() {
	m := c.sheetScore()
	c.w(`<section class="r3-card" aria-labelledby="r3-h1">%s<div class="r3-res">`, c.r3Title(1, "r3-h1", "html.r3.h1"))
	if m.Value == nil {
		reason := index.ReasonNotApplicable
		switch {
		case !c.v.Coverage.GraphBuilt:
			reason = index.ReasonCoverage
		case m.Unavailable != "":
			reason = m.Unavailable
		}
		c.w(`<p>%s</p></div></section>`, c.t("html.index.unavailable", map[string]string{"reason": c.cat.Text("html.index.reason."+reason, nil)}))
		return
	}
	tone := bandTone(m.Band)
	c.w(`<p class="r3-score %s"><b>%d</b><span>/100 · %s</span></p>`, tone, *m.Value, c.t("band."+m.Band, nil))
	letter := index.NoClass()
	if m.Letter != nil {
		letter = *m.Letter
	}
	class, phrase, prov := "—", "—", ""
	if letter.Class != nil {
		class = *letter.Class
		phrase = lowerFirst(c.cat.Text("sheet.class."+class, nil))
	}
	if provisional(letter) {
		prov = c.cat.Text("html.r3.provisional", nil)
	}
	ref := "—"
	if m.FactoryValue != nil {
		ref = strconv.Itoa(*m.FactoryValue)
	}
	const mark = "\x00"
	text := c.cat.Text("html.r3.result", map[string]string{
		"agent": mark, "class_phrase": phrase, "class": class, "provisional": prov, "reference": ref,
	})
	text = strings.Replace(html.EscapeString(text), mark, "<b>"+html.EscapeString(c.agentName(m.AgentID))+"</b>", 1)
	c.w(`<p class="r3-text">%s</p></div>`, text)
	if len(c.v.Dismissed) > 0 { // the score leaves out what the person dismissed
		c.w(`<p class="r3-note">%s</p>`, c.t("report.dismissed.note", nil))
	}
	c.w(`</section>`)
}

// joinNames joins names with commas: the lists of the problems' details.
func joinNames(names []string) string { return strings.Join(names, ", ") }

// issueTitle and issueDetail are a problem's cell: its title (bold) and its
// detail, escaped.
func (c htmlCtx) issueTitle(is issue) string {
	switch is.kind {
	case issueCred:
		n := len(is.readers())
		p := map[string]string{"service": serviceDisplay(is.service), "n": strconv.Itoa(n)}
		if is.project {
			return c.t(c.cat.One("html.r3.group.cred.project", n), p)
		}
		return html.EscapeString(c.cat.Plural("html.r3.group.cred", n, p))
	case issueConfig:
		return c.t("html.r3.group.config", map[string]string{"agent": c.agentName(is.agentID)})
	}
	return c.rowTitle(is.findings[0])
}

func (c htmlCtx) issueDetail(is issue) string {
	switch is.kind {
	case issueCred:
		var names []string
		for _, a := range is.readers() {
			names = append(names, c.agentName(a))
		}
		out := c.t("html.r3.group.cred.detail", map[string]string{"agents": joinNames(names)})
		if g := is.inGit(); len(g) > 0 {
			most := 0
			for _, f := range g {
				most = max(most, f.Params.Count)
			}
			out += " " + c.t(c.cat.One("html.r3.group.cred.git", most), map[string]string{"projects": strconv.Itoa(most)})
		}
		return out
	case issueConfig:
		var names []string
		for _, t := range is.targets() {
			names = append(names, c.agentName(t))
		}
		return c.t("html.r3.group.config.detail", map[string]string{"targets": joinNames(names)})
	}
	return ""
}

// serviceDisplay is a service's name as the report shows it.
func serviceDisplay(service string) string {
	if n, ok := serviceNames[service]; ok {
		return n
	}
	return service
}

// issueFix is a problem's «How to fix it» cell (the signed rules): a
// credential problem with a key in git is rotated (and the template applied
// when it takes some of its findings away); a problem every finding of which
// the template takes away is fixed by it; some of them, how many; none, the
// steps. Links lead to the problem's steps.
func (c htmlCtx) issueFix(is issue, n int) string {
	href := `href="#p-` + strconv.Itoa(n) + `"`
	k, total := templateFixesIn(c.v, is), len(is.findings)
	service := is.service
	if is.kind == issueOne {
		if f := is.findings[0]; f.Rule == rules.RuleCredInGit {
			for _, cr := range credsOf(f) {
				if s := c.v.credServices[cr]; s.service != "" && s.service != "unknown" {
					service = s.service
				}
			}
		}
	}
	if len(is.inGit()) > 0 && service != "" && service != "unknown" {
		if k > 0 {
			return `<a class="r3-fix" ` + href + `>` + c.t("html.r3.fix.rotate_tpl", nil) + `</a>`
		}
		return `<a class="r3-fix" ` + href + `>` + c.t("html.r3.fix.rotate", map[string]string{"service": serviceDisplay(service)}) + `</a>`
	}
	switch {
	case k > 0 && k == total:
		return c.t("html.r3.fix.tpl_all", nil)
	case k > 0:
		return c.t("html.r3.fix.tpl_some", map[string]string{"k": strconv.Itoa(k), "n": strconv.Itoa(total), "rest": strconv.Itoa(total - k)})
	}
	return `<a class="r3-fix" ` + href + `>` + c.t("html.r3.fix.steps", nil) + `</a>`
}

// pill is a problem's level.
func (c htmlCtx) pill(is issue) string {
	if is.red() {
		return `<span class="r3-pill red">` + c.t("html.group.red", nil) + `</span>`
	}
	return `<span class="r3-pill amber">` + c.t("html.group.yellow", nil) + `</span>`
}

func (c htmlCtx) renderR3Findings(iss []issue) {
	urgent, review, findings := 0, 0, 0
	for _, is := range iss {
		for _, f := range is.findings {
			findings++
			if f.Severity == rules.SeverityRed {
				urgent++
			} else {
				review++
			}
		}
	}
	c.w(`<section class="r3-card" aria-labelledby="r3-h2"><div class="r3-top">%s<p class="r3-counts">%s</p></div>`,
		c.r3Title(2, "r3-h2", "html.r3.h2"),
		c.t("html.r3.counts", map[string]string{
			"groups": strconv.Itoa(len(iss)), "findings": strconv.Itoa(findings),
			"urgent": strconv.Itoa(urgent), "review": strconv.Itoa(review),
		}))
	if len(iss) == 0 {
		empty := "html.none"
		if !c.v.Coverage.GraphBuilt {
			empty = "html.today.incomplete" // the review did not finish
		}
		c.w(`<p class="r3-empty">%s</p>`, c.t(empty, nil))
	}
	if len(iss) > 0 {
		c.w(`<table class="r3-tbl"><thead><tr><th scope="col" class="c-n">%s</th><th scope="col">%s</th><th scope="col" class="c-c">%s</th><th scope="col" class="c-l">%s</th><th scope="col" class="c-f">%s</th></tr></thead><tbody>`,
			c.t("html.r3.col.n", nil), c.t("html.r3.col.problem", nil), c.t("html.r3.col.count", nil), c.t("html.r3.col.level", nil), c.t("html.r3.col.fix", nil))
		for i, is := range iss {
			if i == r3Shown {
				rest := 0
				for _, r := range iss[r3Shown:] {
					rest += len(r.findings)
				}
				c.w(`<tr class="r3-more"><td></td><td colspan="4"><a href="#p-%d">%s</a></td></tr>`, r3Shown+1,
					c.t("html.r3.more", map[string]string{"groups": strconv.Itoa(len(iss) - r3Shown), "findings": strconv.Itoa(rest)}))
				break
			}
			n := i + 1
			detail := c.issueDetail(is)
			if detail != "" {
				detail = " " + detail
			}
			// On a phone the number, the count and the level go above the
			// problem: «{n} · {count} findings» (the count alone is left out
			// for one finding, which the catalog has no singular for).
			lead := strconv.Itoa(n)
			if len(is.findings) > 1 {
				lead += " · " + strconv.Itoa(len(is.findings)) + " " + html.EscapeString(lowerFirst(c.cat.Text("html.r3.col.count", nil)))
			}
			c.w(`<tr><th scope="row" class="c-n">%d</th><td class="c-p"><span class="r3-m">%s %s</span><b>%s</b>%s</td><td class="c-c">%d</td><td class="c-l">%s</td><td class="c-f">%s</td></tr>`,
				n, lead, c.pill(is), c.issueTitle(is), detail, len(is.findings), c.pill(is), c.issueFix(is, n))
		}
		c.w(`</tbody></table>`)
	}
	c.w(`</section>`)
}

func (c htmlCtx) renderR3Template(iss []issue) {
	k := 0
	var groups []string
	for i, is := range iss {
		got := false
		for _, f := range is.findings {
			if f.Severity == rules.SeverityRed && c.v.templateFixes[f.ID] {
				k++
				got = true
			}
		}
		if got {
			groups = append(groups, strconv.Itoa(i+1))
		}
	}
	fixes := ""
	if k > 0 {
		fixes = c.t("html.r3.tpl.fixes", map[string]string{"k": strconv.Itoa(k), "groups": strings.Join(groups, ", ")})
	} else {
		fixes = c.t("html.r3.tpl.fixes_none", map[string]string{"agent": c.templateAgent()})
	}
	c.w(`<section class="r3-card r3-tpl" aria-labelledby="r3-h3"><div class="r3-tpl-text">%s<p class="r3-tpl-body">%s</p><p class="r3-tpl-fixes">%s</p></div><div class="r3-tpl-act"><a class="r3-btn" href="#v-pc" data-view="v-pc">%s</a><span>%s</span></div></section>`,
		c.r3Title(3, "r3-h3", "html.r3.h3"), c.t("html.r3.tpl.body", nil), fixes, c.t("html.r3.tpl.button", nil), c.t("html.r3.tpl.note", nil))
}

// templateAgent is the agent the template is for: Claude Code when it is
// evaluated, else the agent with the least control.
func (c htmlCtx) templateAgent() string {
	for _, a := range c.v.Index.Agents {
		if strings.HasPrefix(a.AgentID, "agent_client_"+knowledge.ClaudeCodeProduct) {
			return a.Agent
		}
	}
	if a := c.claudeAgent(); a != nil {
		return a.Agent
	}
	return c.agentName(c.sheetScore().AgentID)
}

func (c htmlCtx) renderR3Folds() {
	c.w(`<div class="r3-folds"><details><summary>%s</summary><div class="r3-fold">`, c.t("html.r3.how", nil))
	c.renderStrip()
	c.w(`</div></details><details><summary>%s</summary><div class="r3-fold"><ul>`, c.t("html.r3.assumptions", nil))
	c.renderAssumeItems(c.v.SpaceConnected)
	c.w(`</ul></div></details></div>`)
}

// renderR3Steps are the steps of each problem, behind the table's links: the
// problem's title, how the computer's score would move with all of it fixed
// (one problem at a time: gains do not add up), and each finding with its
// steps, as the findings list showed them.
func (c htmlCtx) renderR3Steps(iss []issue) {
	if len(iss) == 0 {
		return
	}
	c.w(`<div class="r3-steps">`)
	for i, is := range iss {
		n := i + 1
		c.w(`<section class="r3-p" id="p-%d" aria-labelledby="p-%d-h"><h2 id="p-%d-h"><span class="r3-n" aria-hidden="true">%d</span><span>%s</span></h2>`, n, n, n, n, c.issueTitle(is))
		if d := c.issueDetail(is); d != "" {
			c.w(`<p class="r3-p-d">%s</p>`, d)
		}
		if now, then, ok := issueGain(c.v, is); ok {
			c.w(`<p class="proj">%s</p>`, c.t("html.gain", map[string]string{"now": strconv.Itoa(now), "then": strconv.Itoa(then)}))
		}
		done := map[*credRow]bool{}
		var reps []rules.Finding
		for _, f := range is.findings {
			r := c.rows[f.ID]
			if r == nil {
				reps = append(reps, f)
				continue
			}
			if !done[r] {
				done[r] = true
				reps = append(reps, r.rep)
			}
		}
		sort.SliceStable(reps, func(a, b int) bool {
			return reps[a].Severity == rules.SeverityRed && reps[b].Severity != rules.SeverityRed
		})
		for _, f := range reps {
			c.renderRow(f)
		}
		c.w(`</section>`)
	}
	c.w(`</div>`)
}

// r3Keys are the catalog codes the first screen emits.
func r3Keys() []string {
	return []string{
		"html.r3.title", "html.r3.sub", "html.r3.meta", "html.r3.h1", "html.r3.h2", "html.r3.h3",
		"html.r3.result", "html.r3.provisional", "html.r3.counts",
		"html.r3.col.n", "html.r3.col.problem", "html.r3.col.count", "html.r3.col.level", "html.r3.col.fix",
		"html.r3.group.cred.one", "html.r3.group.cred.other", "html.r3.group.cred.project", "html.r3.group.cred.project.one",
		"html.r3.group.cred.detail", "html.r3.group.cred.git", "html.r3.group.cred.git.one",
		"html.r3.group.config", "html.r3.group.config.detail",
		"html.r3.fix.rotate", "html.r3.fix.rotate_tpl", "html.r3.fix.tpl_all", "html.r3.fix.tpl_some", "html.r3.fix.steps",
		"html.r3.more", "html.r3.tpl.body", "html.r3.tpl.fixes", "html.r3.tpl.fixes_none", "html.r3.tpl.button", "html.r3.tpl.note",
		"html.r3.how", "html.r3.assumptions", "html.r3.footer", "html.r3.method",
		"report.dismissed.note", "html.today.incomplete",
	}
}

// r3CSS is the style of the three blocks and of the problems' steps.
func r3CSS() string {
	var light, dark strings.Builder
	for _, t := range r3Tokens {
		light.WriteString("--" + t.Name + ":" + t.Light + ";")
		dark.WriteString("--" + t.Name + ":" + t.Dark + ";")
	}
	return ":root{" + light.String() + "}\n@media (prefers-color-scheme:dark){:root{" + dark.String() + "}}\n" + r3Rules
}

const r3Rules = `.r3{max-width:1040px;margin:0 auto;padding:28px 16px 8px;display:flex;flex-direction:column;gap:20px;color:var(--ds-ink)}
body{background:var(--ds-surface)}
.r3 a{color:var(--ds-brand)}
.r3 :focus-visible,.r3-steps :focus-visible{outline:2px solid var(--ds-brand);outline-offset:2px;border-radius:4px}
.r3-head{display:flex;justify-content:space-between;align-items:flex-end;gap:8px 24px;flex-wrap:wrap;padding-bottom:14px;border-bottom:2px solid var(--ds-ink)}
.r3-head>div{flex:1 1 420px;min-width:0}
.r3-head h1{margin:0;font-size:24px;line-height:1.25;font-weight:700}
.r3-sub{margin:4px 0 0;font-size:15px;color:var(--ds-ink-2)}
.r3-meta{margin:0;font-size:14px;color:var(--ds-muted);text-align:right}
.r3 .sheetnotes:empty{display:none}
.r3-card{background:var(--ds-raised);border:1px solid var(--ds-line);border-radius:16px;padding:22px 28px;display:flex;flex-direction:column;gap:14px}
.r3-h{margin:0;display:flex;align-items:center;gap:12px;font-size:20px;line-height:1.3;font-weight:700}
.r3-n{width:30px;height:30px;flex:none;border-radius:999px;background:var(--ds-ink);color:var(--ds-on-ink);font-size:15px;font-weight:700;display:inline-flex;align-items:center;justify-content:center}
.r3-res{display:flex;gap:20px;align-items:center;flex-wrap:wrap}
.r3-score{margin:0;flex:none;display:flex;align-items:baseline;gap:4px;border-radius:10px;padding:8px 14px}
.r3-score b{font-size:36px;line-height:1;font-weight:700}
.r3-score span{font-size:15px}
.r3-score.red{background:var(--ds-red-tint);color:var(--ds-red)}
.r3-score.amber{background:var(--ds-amber-tint);color:var(--ds-amber)}
.r3-score.green{background:var(--ds-green-tint);color:var(--ds-green)}
.r3-note,.r3-empty{margin:0;font-size:14px;color:var(--ds-muted)}
.r3-text{margin:0;flex:999 1 420px;min-width:0;font-size:16px;line-height:1.5}
.r3-top{display:flex;justify-content:space-between;align-items:center;gap:12px;flex-wrap:wrap}
.r3-counts{margin:0;font-size:14px;color:var(--ds-muted)}
.r3-tbl{width:100%;border-collapse:collapse;font-size:15px;line-height:1.45}
.r3-tbl thead tr{border-bottom:1px solid var(--ds-ink)}
.r3-tbl th[scope=col]{font-size:13px;font-weight:700;color:var(--ds-muted);text-align:left;padding:8px;white-space:nowrap}
.r3-tbl tbody tr{vertical-align:top;border-bottom:1px solid var(--ds-line)}
.r3-tbl tbody tr:last-child{border-bottom:0}
.r3-tbl td,.r3-tbl th[scope=row]{padding:12px 8px;text-align:left}
.r3-tbl th[scope=row]{font-weight:700}
.r3-tbl .c-n{width:28px;padding-left:0;white-space:nowrap}
.r3-tbl .c-c{width:64px}
.r3-tbl .c-l{width:96px}
.r3-tbl .c-f{width:170px;padding-right:0}
.r3-m{display:none}
.r3-pill{display:inline-block;font-size:13px;font-weight:600;border-radius:999px;padding:2px 10px;white-space:nowrap}
.r3-pill.red{color:var(--ds-red);background:var(--ds-red-tint)}
.r3-pill.amber{color:var(--ds-amber);background:var(--ds-amber-tint)}
a.r3-fix{font-weight:600;color:var(--ds-brand)}
.r3-tpl{border:2px solid var(--ds-ink);flex-direction:row;align-items:center;gap:24px;flex-wrap:wrap}
.r3-tpl-text{flex:999 1 460px;min-width:0;display:flex;flex-direction:column;gap:10px}
.r3-tpl-body{margin:0;font-size:16px;line-height:1.5}
.r3-tpl-fixes{margin:0;font-size:15px;line-height:1.5;color:var(--ds-ink-2)}
.r3-tpl-act{flex:1 0 220px;display:flex;flex-direction:column;gap:6px}
.r3-btn{display:flex;align-items:center;justify-content:center;min-height:52px;padding:0 24px;border-radius:12px;background:var(--ds-ink);color:var(--ds-on-ink)!important;font-size:17px;font-weight:600;text-decoration:none}
.r3-tpl-act span{font-size:13px;color:var(--ds-muted);text-align:center}
.r3-folds{border-top:1px solid var(--ds-line)}
.r3-folds details{border-bottom:1px solid var(--ds-line);padding:12px 0}
.r3-folds summary{font-size:15px;font-weight:600;min-height:28px;display:flex;align-items:center;cursor:pointer;list-style:none}
.r3-folds summary::-webkit-details-marker{display:none}
.r3-fold{padding-top:10px;font-size:14px;color:var(--ds-ink-2)}
.r3-fold ul{margin:0;padding-left:20px}
.r3-foot{display:flex;justify-content:space-between;gap:12px;flex-wrap:wrap;font-size:13px;color:var(--ds-muted)}
.r3-steps{display:flex;flex-direction:column;gap:24px;margin:0 0 24px}
.r3-p h2{display:flex;align-items:center;gap:12px;font-size:20px;line-height:1.3;margin:0 0 8px}
.r3-p-d{margin:0 0 12px;color:var(--ds-ink-2)}
@media (max-width:700px){
.r3-meta{text-align:left}
.r3-card{padding:16px}
.r3-h{font-size:18px}
.r3-n{width:26px;height:26px}
.r3-score b{font-size:30px}
.r3-tbl thead{display:none}
.r3-tbl,.r3-tbl tbody,.r3-tbl tr{display:block;width:100%}
.r3-tbl tbody tr{padding:12px 0}
.r3-tbl td,.r3-tbl th[scope=row]{display:block;width:auto;padding:0}
.r3-tbl th.c-n,.r3-tbl td.c-c,.r3-tbl td.c-l{display:none}
.r3-tbl .c-f{width:auto;margin-top:6px}
.r3-m{display:flex;gap:8px;align-items:center;font-size:14px;color:var(--ds-muted);margin-bottom:4px}
.r3-tpl-act{flex-basis:100%}
}`
