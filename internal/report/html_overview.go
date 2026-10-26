package report

import (
	"html"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/rules"
)

// «How your agents work» (informe HTML v3 §11; decision 2026-10-25, cómo
// trabajan tus agentes, cálculo): agents_overview as a table, one column per
// evaluated agent, and its plain table in the technical Markdown (§11.8).

// overviewCSS is §11's style, with the narrow layout of §11.6.
const overviewCSS = `
section.ov{margin-top:28px;border-top:1px solid var(--line);padding-top:20px}
#ov-title{font-size:22px;font-weight:800;letter-spacing:-.01em;margin:0 0 4px}
.ovi{font-size:15px;color:var(--mut);max-width:880px;margin:0 0 14px}
.ovl{display:flex;flex-wrap:wrap;gap:8px 22px;font-size:14px;color:var(--mut);margin:0 0 12px}
.ovl span{display:inline-flex;align-items:center;gap:7px}
table.ovt{width:100%;border-collapse:collapse;table-layout:fixed}
.ovt thead td{width:20%;border-bottom:2px solid var(--line)}
.ovt thead th{font-size:17px;font-weight:800;padding:0 14px 10px;border-bottom:2px solid var(--line);vertical-align:bottom;text-align:left}
.ovt thead th small{display:block;font-size:13px;font-weight:600}
.ovt tbody th{font-size:15px;font-weight:700;padding:12px 14px 12px 0;border-bottom:1px solid var(--line);vertical-align:top;text-align:left}
.ovt tbody th small{display:block;font-size:13px;font-weight:400;color:var(--mut);margin-top:2px}
.ovt td{font-size:15px;padding:12px 14px;border-bottom:1px solid var(--line);vertical-align:top}
.ovt .c{display:flex;gap:9px;align-items:flex-start}
.ovt .c b{font-weight:600}
.ovt .c em{display:block;font-style:normal;font-size:13px;color:var(--mut);margin-top:2px}
.ovp{width:10px;height:10px;border-radius:50%;flex:none;margin-top:6px}
.ovl .ovp{margin-top:0}
.ovp.nobody{background:var(--redfill)}
.ovp.model{background:var(--ambfill)}
.ovp.you{background:var(--grnfill)}
.ovp.blocked{border:2px solid var(--grnfill)}
span.tag{display:inline-block;font-size:12px;font-weight:600;color:var(--mut);border:1px dashed var(--mut);border-radius:6px;padding:0 6px;margin-left:6px;white-space:nowrap}
.ovn{font-size:14px;color:var(--mut);line-height:1.6;margin-top:10px}
.ovn p{margin:0}
.ovn b{color:var(--ink);font-weight:600}
.ovn .st{color:var(--red);font-weight:600}
.ovf{font-size:13px;color:var(--mut);margin:10px 0 0}
@media (max-width:1099px){table.ovt thead{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);clip-path:inset(50%);white-space:nowrap}.ovt tbody tr{display:block;border-bottom:1px solid var(--line);padding:12px 0}.ovt tbody th{display:block;padding:0 0 6px;border:0}.ovt td{display:block;padding:6px 0;border:0}.ovt td::before{content:attr(data-agent);display:block;font-size:13px;font-weight:700;text-transform:uppercase;letter-spacing:.05em;color:var(--mut);margin-bottom:2px}}`

// overviewDeciders are the legend's deciders, in their order (§11.2).
var overviewDeciders = []string{rules.DeciderNobody, rules.DeciderModel, rules.DeciderYou, rules.DeciderBlocked}

// overviewColumns are the evaluated agents of agents_overview in the order
// of v3 §3 point 6: lowest score first, ties by name; an agent with no score
// after them, by name (§11.3).
func overviewColumns(v V2) []rules.OverviewAgent {
	score := map[string]*index.AgentIndex{}
	for i := range v.Index.Agents {
		score[v.Index.Agents[i].AgentID] = &v.Index.Agents[i]
	}
	var out []rules.OverviewAgent
	for _, a := range v.AgentsOverview {
		if a.Evaluated {
			out = append(out, a)
		}
	}
	val := func(a rules.OverviewAgent) (int, bool) {
		if s, ok := score[a.AgentID]; ok && s.Value != nil {
			return *s.Value, true
		}
		return 0, false
	}
	sort.SliceStable(out, func(i, j int) bool {
		vi, oki := val(out[i])
		vj, okj := val(out[j])
		switch {
		case oki != okj:
			return oki
		case oki && vi != vj:
			return vi < vj
		}
		return out[i].Agent < out[j].Agent
	})
	return out
}

// overviewNotEvaluated are the detected agents not evaluated, by name
// (§11.5).
func overviewNotEvaluated(v V2) []rules.OverviewAgent {
	var out []rules.OverviewAgent
	for _, a := range v.AgentsOverview {
		if !a.Evaluated {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Agent < out[j].Agent })
	return out
}

// scopeKey is the label of a cell's scope by row (§11.4); empty for none.
func scopeKey(row, scope string) string {
	if scope == rules.ScopeNone {
		return ""
	}
	switch row {
	case rules.RowReadsFiles, rules.RowWritesFiles:
		return "html.ov.scope.files." + scope
	case rules.RowRunsCommands:
		return "html.ov.scope.commands." + scope
	case rules.RowSendsOut:
		return "html.ov.scope.sends_out." + scope
	case rules.RowChangesOtherAgents:
		return "html.ov.scope.changes." + scope
	}
	return ""
}

// overviewText renders a cell's texts from the catalog (§11.4): the main
// line, the tags and the secondary lines, unescaped.
type overviewText struct {
	cat   catalog.Catalog
	names map[string]string // agent node id → name, for target_agents
}

func (o overviewText) scope(row, scope string, d rules.OverviewData) string {
	key := scopeKey(row, scope)
	if key == "" {
		return ""
	}
	return o.cat.Text(key, map[string]string{"agents": o.targets(d.TargetAgents)})
}

// targets are the names of target_agents, each once, joined as in v1 §3.2.
func (o overviewText) targets(ids []string) string {
	var names []string
	seen := map[string]bool{}
	for _, id := range ids {
		n := o.names[id]
		if n == "" {
			n = id
		}
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	return listNames(o.cat, names)
}

// main is the cell's main line (§11.4).
func (o overviewText) main(agent string, c rules.OverviewCell) string {
	switch {
	case c.Row == rules.RowModelProvider:
		return o.cat.Text("html.ov.recipient."+c.Data.Recipient, map[string]string{"agent": agent})
	case c.Row == rules.RowChangesOtherAgents && c.Scope == rules.ScopeNone:
		return o.cat.Text("html.ov.changes.none", nil)
	case c.Scope == rules.ScopeNone:
		return o.cat.Text("html.ov.decider."+c.Decider, nil)
	}
	return o.cat.Text("html.ov.cell", map[string]string{
		"decider": o.cat.Text("html.ov.decider."+c.Decider, nil), "scope": o.scope(c.Row, c.Scope, c.Data),
	})
}

// originTag is the key of the origin's tag; empty for none.
var originTag = map[string]string{
	rules.OriginProductDefault:           "html.ov.tag.product_default",
	rules.OriginConfigured:               "html.ov.tag.configured",
	rules.OriginMixed:                    "html.ov.tag.mixed",
	rules.OriginProductDefaultNotChecked: "html.ov.tag.not_checked_here",
}

// tags are the cell's tags (§11.4): its origin and, when assumed, assumed.
func (o overviewText) tags(c rules.OverviewCell) []string {
	var out []string
	if k := originTag[c.Origin]; k != "" {
		out = append(out, o.cat.Text(k, nil))
	}
	if c.Assumed {
		out = append(out, o.cat.Text("html.ov.tag.assumed", nil))
	}
	return out
}

// lines are the cell's secondary lines, in the order of §11.4.
func (o overviewText) lines(a rules.OverviewAgent, c rules.OverviewCell) []string {
	var out []string
	d := c.Data
	has := func(ss []string, s string) bool {
		for _, x := range ss {
			if x == s {
				return true
			}
		}
		return false
	}
	if c.Also != nil {
		out = append(out, o.cat.Text("html.ov.also."+c.Also.Decider, map[string]string{"scope": o.scope(c.Row, c.Also.Scope, d)}))
	}
	if c.Row == rules.RowReadsFiles && d.CredentialsReachable != nil {
		n, high := *d.CredentialsReachable, 0
		if d.CredentialsReachableHigh != nil {
			high = *d.CredentialsReachableHigh
		}
		switch n {
		case 0:
			out = append(out, o.cat.Text("html.ov.data.creds.zero", nil))
		case 1:
			out = append(out, o.cat.Text("html.ov.data.creds.one", nil))
		default:
			out = append(out, o.cat.Text("html.ov.data.creds.other", map[string]string{"count": strconv.Itoa(n), "high": strconv.Itoa(high)}))
		}
	}
	if c.Row == rules.RowReadsFiles && has(d.Excluded, rules.ExcludedDefaultIgnoreFileTools) {
		out = append(out, o.cat.Text("html.ov.data.ignore_env", nil))
	}
	if c.Row == rules.RowWritesFiles && c.NoFindingReason != nil && *c.NoFindingReason == rules.NoFindingNotAFinding {
		out = append(out, o.cat.Text("html.ov.data.not_a_finding", nil))
	}
	if c.Row == rules.RowRunsCommands && d.PreapprovedRules != nil && *d.PreapprovedRules > 0 {
		n := *d.PreapprovedRules
		out = append(out, o.cat.Plural("html.ov.data.preapproved", n, map[string]string{"count": strconv.Itoa(n)}))
		if d.LauncherRules != nil && *d.LauncherRules > 0 {
			out = append(out, o.cat.Text("html.ov.data.launchers", map[string]string{"count": strconv.Itoa(*d.LauncherRules)}))
		}
	}
	if c.Row == rules.RowSendsOut && has(d.Excluded, rules.ExcludedDocumentedNetworkExceptions) {
		out = append(out, o.cat.Text("html.ov.data.net_exceptions", nil))
	}
	if c.Row == rules.RowSendsOut && c.NoFindingReason != nil && *c.NoFindingReason == rules.NoFindingNoTargets {
		out = append(out, o.cat.Text("html.ov.data.no_targets_out", nil))
	}
	if c.Row == rules.RowModelProvider && d.ByoKeyStillRouted != nil && *d.ByoKeyStillRouted {
		out = append(out, o.cat.Text("html.ov.data.byo_key_routed", nil))
	}
	if c.Row == rules.RowModelProvider && has(d.Assumptions, rules.AssumeSessionEnvNotReadable) {
		out = append(out, o.cat.Text("html.ov.data.session_env", nil))
	}
	if isCursor(a) && c.Decider == rules.DeciderModel && has(d.Assumptions, graph.AssumeIDERunModeNotReadable) {
		out = append(out, o.cat.Text("html.ov.data.run_everything", nil))
	}
	return out
}

// isCursor reports Cursor's column (anexo 2, point 5, of the calculation).
func isCursor(a rules.OverviewAgent) bool {
	return a.Product == knowledge.CursorProduct || a.Product == "" && a.Agent == graph.ClientDisplayName(knowledge.CursorProduct)
}

// knowledgeDate is the knowledge line's date in the report's format.
func knowledgeDate(cat catalog.Catalog, date string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return cat.DayLabel(t)
}

// overviewNotes are §11.5's lines, as (name, text, stale text) per agent.
type overviewNote struct{ name, text, stale string }

func overviewNotes(cat catalog.Catalog, cols, notEval []rules.OverviewAgent) []overviewNote {
	var out []overviewNote
	for _, a := range cols {
		n := overviewNote{name: a.Agent}
		if a.Knowledge.Date != nil {
			n.text = cat.Text("html.ov.head.knowledge", map[string]string{"date": knowledgeDate(cat, *a.Knowledge.Date)})
		}
		if a.Knowledge.Stale {
			n.stale = cat.Text("html.ov.head.knowledge.stale", nil)
		}
		out = append(out, n)
	}
	for _, a := range notEval {
		if a.NotEvaluatedReason == nil || *a.NotEvaluatedReason == rules.NotEvaluatedGraphMissing {
			continue
		}
		out = append(out, overviewNote{name: a.Agent, text: cat.Text("html.ov.head.not_evaluated."+*a.NotEvaluatedReason, nil)})
	}
	return out
}

// renderOverview writes section.ov (§11.1–§11.7); nothing without
// agents_overview or with no agent in it.
func (c htmlCtx) renderOverview() {
	if len(c.v.AgentsOverview) == 0 {
		return
	}
	o := overviewText{cat: c.cat, names: c.v.agentNames}
	cols := overviewColumns(c.v)
	c.w(`<section class="ov" aria-labelledby="ov-title"><h2 id="ov-title">%s</h2><p class="ovi">%s</p>`, c.t("html.ov.title", nil), c.t("html.ov.intro", nil))
	present := map[string]bool{}
	for _, a := range cols {
		for _, cell := range a.Rows {
			present[cell.Decider] = true
		}
	}
	var legend strings.Builder
	for _, d := range overviewDeciders {
		if present[d] {
			legend.WriteString(`<span><span class="ovp ` + d + `" aria-hidden="true"></span>` + c.t("html.ov.legend."+d, nil) + `</span>`)
		}
	}
	if legend.Len() > 0 {
		c.w(`<div class="ovl">%s</div>`, legend.String())
	}
	if len(cols) > 0 {
		c.w(`<table class="ovt"><thead><tr><td></td>`)
		for _, a := range cols {
			c.w(`<th scope="col">%s<small%s</small></th>`, html.EscapeString(a.Agent), c.headScore(a))
		}
		c.w(`</tr></thead><tbody>`)
		for i, row := range rules.AllOverviewRows {
			sub := ""
			if row == rules.RowReadsFiles || row == rules.RowModelProvider {
				sub = `<small>` + c.t("html.ov.row."+row+".sub", nil) + `</small>`
			}
			c.w(`<tr><th scope="row">%s%s</th>`, c.t("html.ov.row."+row, nil), sub)
			for _, a := range cols {
				c.w(`<td data-agent="%s">`, html.EscapeString(a.Agent))
				if i < len(a.Rows) {
					c.renderOverviewCell(o, a, a.Rows[i])
				}
				c.w(`</td>`)
			}
			c.w(`</tr>`)
		}
		c.w(`</tbody></table>`)
	}
	c.w(`<div class="ovn">`)
	for _, n := range overviewNotes(c.cat, cols, overviewNotEvaluated(c.v)) {
		c.w(`<p><b>%s</b>`, html.EscapeString(n.name))
		if n.text != "" {
			c.w(` · %s`, html.EscapeString(n.text))
		}
		if n.stale != "" {
			c.w(` · <span class="st">%s</span>`, html.EscapeString(n.stale))
		}
		c.w(`</p>`)
	}
	c.w(`</div><p class="ovf">%s</p></section>`, c.t("html.ov.foot", nil))
}

// headScore is the rest of a column's small (§11.3): the attributes and
// the score line in its band's colour, or the unavailable line.
func (c htmlCtx) headScore(a rules.OverviewAgent) string {
	if s, ok := c.score[a.AgentID]; ok && s.Value != nil {
		return ` style="color:` + bandVar[s.Band] + `">` + c.t("html.ov.head.score", map[string]string{
			"score": strconv.Itoa(*s.Value), "band": c.cat.Text("band."+s.Band, nil),
		})
	}
	reason := index.ReasonCoverage
	if s, ok := c.score[a.AgentID]; ok && s.Unavailable != "" {
		reason = s.Unavailable
	}
	return ` style="color:var(--mut)">` + c.t("html.index.unavailable", map[string]string{"reason": c.cat.Text("html.index.reason."+reason, nil)})
}

// renderOverviewCell writes one cell (§11.4).
func (c htmlCtx) renderOverviewCell(o overviewText, a rules.OverviewAgent, cell rules.OverviewCell) {
	dot := `<span class="ovp" aria-hidden="true"></span>`
	switch {
	case cell.Decider == rules.DeciderNotApplicable:
	case cell.Row == rules.RowChangesOtherAgents && cell.Scope == rules.ScopeNone:
		dot = `<span class="ovp you" aria-hidden="true"></span>`
	default:
		dot = `<span class="ovp ` + cell.Decider + `" aria-hidden="true"></span>`
	}
	c.w(`<div class="c">%s<div><b>%s</b>`, dot, html.EscapeString(o.main(a.Agent, cell)))
	for _, tg := range o.tags(cell) {
		c.w(`<span class="tag">%s</span>`, html.EscapeString(tg))
	}
	for _, l := range o.lines(a, cell) {
		c.w(`<em>%s</em>`, html.EscapeString(l))
	}
	c.w(`</div></div>`)
}

// renderOverviewMarkdown writes «How your agents work» in the technical
// Markdown (§11.8): the table with each cell's main line and tags, and the
// lines of §11.5 as paragraphs.
func renderOverviewMarkdown(v V2, cat catalog.Catalog, w func(string, ...any)) {
	if len(v.AgentsOverview) == 0 {
		return
	}
	o := overviewText{cat: cat, names: v.agentNames}
	cols := overviewColumns(v)
	w("## %s\n\n", cat.Text("html.ov.title", nil))
	if len(cols) > 0 {
		w("|")
		for _, a := range cols {
			w(" | %s", mdCell(a.Agent))
		}
		w(" |\n|---")
		for range cols {
			w("|---")
		}
		w("|\n")
		for i, row := range rules.AllOverviewRows {
			w("| %s", mdCell(cat.Text("html.ov.row."+row, nil)))
			for _, a := range cols {
				text := ""
				if i < len(a.Rows) {
					cell := a.Rows[i]
					text = o.main(a.Agent, cell)
					if tags := o.tags(cell); len(tags) > 0 {
						text += " (" + strings.Join(tags, ", ") + ")"
					}
				}
				w(" | %s", mdCell(text))
			}
			w(" |\n")
		}
		w("\n")
	}
	for _, n := range overviewNotes(cat, cols, overviewNotEvaluated(v)) {
		w("**%s**", n.name)
		if n.text != "" {
			w(" · %s", n.text)
		}
		if n.stale != "" {
			w(" · %s", n.stale)
		}
		w("\n\n")
	}
}

// mdCell escapes a Markdown table cell.
func mdCell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

// overviewKeys are the catalog codes §11 can emit, for HTMLKeys and
// CatalogKeys.
func overviewKeys() []string {
	keys := []string{
		"html.ov.title", "html.ov.intro", "html.ov.foot", "html.ov.cell", "html.ov.changes.none",
		"html.ov.row.reads_files.sub", "html.ov.row.model_provider.sub",
		"html.ov.data.creds.zero", "html.ov.data.creds.one", "html.ov.data.creds.other", "html.ov.data.ignore_env",
		"html.ov.data.not_a_finding", "html.ov.data.preapproved.one", "html.ov.data.preapproved.other", "html.ov.data.launchers",
		"html.ov.data.net_exceptions", "html.ov.data.no_targets_out", "html.ov.data.byo_key_routed", "html.ov.data.session_env",
		"html.ov.data.run_everything", "html.ov.head.score", "html.ov.head.knowledge", "html.ov.head.knowledge.stale",
		"html.ov.head.not_evaluated.no_inference", "html.ov.head.not_evaluated.coverage", "html.ov.tag.assumed",
	}
	for _, r := range rules.AllOverviewRows {
		keys = append(keys, "html.ov.row."+r)
	}
	for _, d := range overviewDeciders {
		keys = append(keys, "html.ov.legend."+d, "html.ov.decider."+d)
	}
	for _, d := range []string{rules.DeciderNobody, rules.DeciderModel, rules.DeciderYou} {
		keys = append(keys, "html.ov.also."+d)
	}
	for row, scopes := range rules.RowScopes {
		for _, s := range scopes {
			if k := scopeKey(row, s); k != "" {
				keys = append(keys, k)
			}
		}
	}
	for _, k := range originTag {
		keys = append(keys, k)
	}
	for _, r := range []string{
		rules.RecipientAnthropicAPI, rules.RecipientCustomGateway, rules.RecipientAWSBedrock, rules.RecipientGoogleVertex,
		rules.RecipientMicrosoftFoundry, rules.RecipientAnthropicAWS, rules.RecipientVendorBackend, rules.RecipientNotReadable,
	} {
		keys = append(keys, "html.ov.recipient."+r)
	}
	sort.Strings(keys)
	out := keys[:0]
	for i, k := range keys {
		if i == 0 || k != keys[i-1] {
			out = append(out, k)
		}
	}
	return out
}
