package report_test

import (
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// Informe HTML v3 §11.9: «How your agents work».

// ovSection is the page's section.ov.
func ovSection(t *testing.T, page string) string {
	t.Helper()
	s := regexp.MustCompile(`(?s)<section class="ov".*?</section>`).FindString(page)
	if s == "" {
		t.Fatal("no section.ov")
	}
	return s
}

// The real case of two agents: 6 rows and 2 agent columns in score order;
// the legend has only the deciders present; no unresolved key.
func TestRenderHTML_OverviewRealCase(t *testing.T) {
	v := twoAgents(t)
	if len(v.AgentsOverview) != 2 {
		t.Fatalf("%d agents in agents_overview", len(v.AgentsOverview))
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		sec := ovSection(t, page)
		if i, j := strings.Index(page, `<div class="flow">`), strings.Index(page, `<div class="more">`); !(i < strings.Index(page, sec) && strings.Index(page, sec) < j) {
			t.Errorf("%s: the section is not between .flow and .more", lang)
		}
		cols := regexp.MustCompile(`<th scope="col">([^<]*)<small`).FindAllStringSubmatch(sec, -1)
		if len(cols) != 2 || cols[0][1] != "Cursor" || cols[1][1] != "Claude Code" {
			t.Errorf("%s: columns %v, want Cursor (lower score) then Claude Code", lang, cols)
		}
		if n := strings.Count(sec, `<th scope="row">`); n != 6 {
			t.Errorf("%s: %d rows", lang, n)
		}
		if n := strings.Count(sec, `<td data-agent=`); n != 12 {
			t.Errorf("%s: %d cells", lang, n)
		}
		present := map[string]bool{}
		for _, a := range v.AgentsOverview {
			for _, c := range a.Rows {
				present[c.Decider] = true
			}
		}
		var want []string
		for _, d := range []string{rules.DeciderNobody, rules.DeciderModel, rules.DeciderYou, rules.DeciderBlocked} {
			if present[d] {
				want = append(want, d)
			}
		}
		legend := regexp.MustCompile(`(?s)<div class="ovl">.*?</div>`).FindString(sec)
		got := regexp.MustCompile(`<span class="ovp ([a-z_]+)"`).FindAllStringSubmatch(legend, -1)
		if len(got) != len(want) {
			t.Fatalf("%s: legend %v, want %v", lang, got, want)
		}
		for i, g := range got {
			if g[1] != want[i] || !strings.Contains(legend, htmlEscape(cat.Text("html.ov.legend."+want[i], nil))) {
				t.Errorf("%s: legend %d is %s, want %s", lang, i, g[1], want[i])
			}
		}
		if strings.Contains(sec, "{") || strings.Contains(sec, "html.ov.") {
			t.Errorf("%s: unresolved key in %s", lang, sec)
		}
		if !strings.Contains(sec, `<p class="ovf">`+htmlEscape(cat.Text("html.ov.foot", nil))+`</p>`) {
			t.Errorf("%s: no foot", lang)
		}
	}
}

// ovReport is the two agents' report with the given rows: Claude Code's
// and Cursor's (nil leaves them), and Cline detected without inference.
func ovReport(t *testing.T, cc, cu map[string]rules.OverviewCell, mod func(*report.V2)) report.V2 {
	t.Helper()
	v := cloneV2(twoAgents(t))
	var ov []rules.OverviewAgent
	for _, a := range v.AgentsOverview {
		a.Rows = append([]rules.OverviewCell{}, a.Rows...)
		set := cc
		if a.Agent == "Cursor" {
			set = cu
		}
		for i := range a.Rows {
			if c, ok := set[a.Rows[i].Row]; ok {
				c.Row = a.Rows[i].Row
				a.Rows[i] = c
			}
		}
		ov = append(ov, a)
	}
	r := rules.NotEvaluatedNoInference
	ov = append(ov, rules.OverviewAgent{Agent: "Cline", AgentID: "agent_client_0123456789abcdef", NotEvaluatedReason: &r, Rows: rules.NotEvaluatedRows()})
	v.AgentsOverview = ov
	if mod != nil {
		mod(&v)
	}
	return v
}

func ip(n int) *int       { return &n }
func sp(s string) *string { return &s }
func bp(b bool) *bool     { return &b }
func ovc(decider, scope, origin string) rules.OverviewCell {
	return rules.OverviewCell{Decider: decider, Scope: scope, Origin: origin}
}

// Each combination of §11.4 has a case: also, the three key counts,
// ignore_env, not_a_finding, pre-approved with and without launchers,
// net_exceptions, no_targets_out, byo_key_routed, session_env, blocked,
// changes.none, the mixed and assumed tags, run_everything.
func TestRenderHTML_OverviewCombinations(t *testing.T) {
	type text struct {
		key string
		p   map[string]string
		in  string // "b", "tag" or "em"
	}
	nobody, model, you, blocked := rules.DeciderNobody, rules.DeciderModel, rules.DeciderYou, rules.DeciderBlocked
	pd, cf, mixed := rules.OriginProductDefault, rules.OriginConfigured, rules.OriginMixed
	cases := []struct {
		name   string
		cursor bool
		cell   rules.OverviewCell
		row    string
		want   []text
		class  string
	}{
		{name: "also", row: rules.RowReadsFiles, cursor: true, cell: func() rules.OverviewCell {
			c := ovc(nobody, rules.ScopeSameProject, pd)
			c.Also = &rules.OverviewAlso{Decider: model, Scope: rules.ScopeMachine}
			c.Data.Excluded = []string{rules.ExcludedDefaultIgnoreFileTools}
			return c
		}(), want: []text{{"html.ov.also.model", map[string]string{"scope": "@html.ov.scope.files.machine"}, "em"}, {"html.ov.data.ignore_env.also", nil, "em"}}},
		{name: "ignore_env without also", row: rules.RowReadsFiles, cursor: true, cell: func() rules.OverviewCell {
			c := ovc(nobody, rules.ScopeSameProject, pd)
			c.Data.Excluded = []string{rules.ExcludedDefaultIgnoreFileTools}
			return c
		}(), want: []text{{"html.ov.data.ignore_env", nil, "em"}}},
		{name: "creds zero", row: rules.RowReadsFiles, cell: func() rules.OverviewCell {
			c := ovc(nobody, rules.ScopeMachine, pd)
			c.Data.CredentialsReachable, c.Data.CredentialsReachableHigh = ip(0), ip(0)
			return c
		}(), want: []text{{"html.ov.data.creds.zero", nil, "em"}}},
		{name: "creds one", row: rules.RowReadsFiles, cell: func() rules.OverviewCell {
			c := ovc(nobody, rules.ScopeMachine, pd)
			c.Data.CredentialsReachable, c.Data.CredentialsReachableHigh = ip(1), ip(1)
			return c
		}(), want: []text{{"html.ov.data.creds.one", nil, "em"}}},
		{name: "creds other", row: rules.RowReadsFiles, cell: func() rules.OverviewCell {
			c := ovc(nobody, rules.ScopeMachine, pd)
			c.Data.CredentialsReachable, c.Data.CredentialsReachableHigh = ip(5), ip(3)
			return c
		}(), want: []text{{"html.ov.data.creds.other", map[string]string{"count": "5", "high": "3"}, "em"}}},
		{name: "not_a_finding", row: rules.RowWritesFiles, cell: func() rules.OverviewCell {
			c := ovc(model, rules.ScopeMachine, pd)
			c.Assumed, c.NoFindingReason = true, sp(rules.NoFindingNotAFinding)
			return c
		}(), want: []text{{"html.ov.data.not_a_finding", nil, "em"}, {"html.ov.tag.assumed", nil, "tag"}}},
		{name: "preapproved one", row: rules.RowRunsCommands, cell: func() rules.OverviewCell {
			c := ovc(model, rules.ScopeMachine, pd)
			c.Data.PreapprovedRules, c.Data.LauncherRules = ip(1), ip(0)
			return c
		}(), want: []text{{"html.ov.data.preapproved.one", map[string]string{"count": "1"}, "em"}}},
		{name: "preapproved and 1 launcher", row: rules.RowRunsCommands, cell: func() rules.OverviewCell {
			c := ovc(nobody, rules.ScopeMachine, cf)
			c.Data.PreapprovedRules, c.Data.LauncherRules = ip(3), ip(1)
			return c
		}(), want: []text{{"html.ov.data.preapproved.other", map[string]string{"count": "3"}, "em"}, {"html.ov.data.launchers.one", map[string]string{"count": "1"}, "em"}}},
		{name: "preapproved and launchers", row: rules.RowRunsCommands, cell: func() rules.OverviewCell {
			c := ovc(nobody, rules.ScopeMachine, mixed)
			c.Data.PreapprovedRules, c.Data.LauncherRules = ip(147), ip(2)
			return c
		}(), want: []text{{"html.ov.data.preapproved.other", map[string]string{"count": "147"}, "em"}, {"html.ov.data.launchers.other", map[string]string{"count": "2"}, "em"}, {"html.ov.tag.mixed", nil, "tag"}}},
		{name: "net_exceptions and no_targets_out", row: rules.RowSendsOut, cell: func() rules.OverviewCell {
			c := ovc(nobody, rules.ScopeAnyDestination, cf)
			c.Data.Excluded, c.NoFindingReason = []string{rules.ExcludedDocumentedNetworkExceptions}, sp(rules.NoFindingNoTargets)
			return c
		}(), want: []text{{"html.ov.data.net_exceptions", nil, "em"}, {"html.ov.data.no_targets_out", nil, "em"}, {"html.ov.tag.configured", nil, "tag"}}},
		{name: "byo_key_routed", row: rules.RowModelProvider, cursor: true, cell: func() rules.OverviewCell {
			c := ovc(rules.DeciderNotApplicable, rules.ScopeNone, pd)
			c.Data.Recipient, c.Data.ByoKeyStillRouted = rules.RecipientVendorBackend, bp(true)
			return c
		}(), want: []text{{"html.ov.recipient.vendor_backend", map[string]string{"agent": "Cursor"}, "b"}, {"html.ov.data.byo_key_routed", nil, "em"}}},
		{name: "session_env", row: rules.RowModelProvider, cell: func() rules.OverviewCell {
			c := ovc(rules.DeciderNotApplicable, rules.ScopeNone, pd)
			c.Assumed, c.Data.Recipient, c.Data.Assumptions = true, rules.RecipientAnthropicAPI, []string{rules.AssumeSessionEnvNotReadable}
			return c
		}(), want: []text{{"html.ov.recipient.anthropic_api", nil, "b"}, {"html.ov.data.session_env", nil, "em"}}},
		{
			name: "blocked", row: rules.RowRunsCommands, cell: ovc(blocked, rules.ScopeNone, cf), class: "blocked",
			want: []text{{"html.ov.decider.blocked", nil, "b"}},
		},
		{
			name: "you", row: rules.RowSendsOut, cell: ovc(you, rules.ScopeNone, pd), class: "you",
			want: []text{{"html.ov.decider.you", nil, "b"}},
		},
		{
			name: "changes.none", row: rules.RowChangesOtherAgents, cell: ovc(you, rules.ScopeNone, rules.OriginNone), class: "you",
			want: []text{{"html.ov.changes.none", nil, "b"}},
		},
		{name: "changes config", row: rules.RowChangesOtherAgents, cell: func() rules.OverviewCell {
			c := ovc(model, rules.ScopeConfig, pd)
			c.Data.TargetAgents = []string{"agent_client_0123456789abcdef"}
			return c
		}(), class: "model"},
		{name: "run_everything", row: rules.RowRunsCommands, cursor: true, cell: func() rules.OverviewCell {
			c := ovc(model, rules.ScopeSandbox, pd)
			c.Assumed, c.Data.Assumptions = true, []string{graph.AssumeIDERunModeNotReadable, graph.AssumeAutoClassifierPermits}
			return c
		}(), want: []text{{"html.ov.cell", map[string]string{"decider": "@html.ov.decider.model", "scope": "@html.ov.scope.commands.sandbox"}, "b"}, {"html.ov.data.run_everything", nil, "em"}}},
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		resolve := func(tx text) string {
			p := map[string]string{}
			for k, v := range tx.p {
				if strings.HasPrefix(v, "@") {
					v = cat.Text(v[1:], nil)
				}
				p[k] = v
			}
			return cat.Text(tx.key, p)
		}
		for _, tc := range cases {
			set := map[string]rules.OverviewCell{tc.row: tc.cell}
			var v report.V2
			agent := "Claude Code"
			if tc.cursor {
				v, agent = ovReport(t, nil, set, nil), "Cursor"
			} else {
				v = ovReport(t, set, nil, nil)
			}
			sec := ovSection(t, renderHTML(t, lang, v))
			td := regexp.MustCompile(`(?s)<td data-agent="`+agent+`">(.*?)</td>`).FindAllStringSubmatch(sec, -1)
			cell := ""
			for i, r := range rules.AllOverviewRows {
				if r == tc.row && i < len(td) {
					cell = td[i][1]
				}
			}
			if cell == "" {
				t.Fatalf("%s %s: no cell", lang, tc.name)
			}
			for _, w := range tc.want {
				s := htmlEscape(resolve(w))
				var ok bool
				switch w.in {
				case "b":
					ok = strings.Contains(cell, "<b>"+s+"</b>")
				case "tag":
					ok = strings.Contains(cell, `<span class="tag">`+s+"</span>")
				default:
					ok = strings.Contains(cell, "<em>"+s+"</em>")
				}
				if !ok {
					t.Errorf("%s %s: no %s %q in %s", lang, tc.name, w.in, s, cell)
				}
			}
			if tc.class != "" && !strings.Contains(cell, `<span class="ovp `+tc.class+`" aria-hidden="true">`) {
				t.Errorf("%s %s: no %s dot in %s", lang, tc.name, tc.class, cell)
			}
			if tc.name == "changes config" && !strings.Contains(cell, "<b>"+htmlEscape(cat.Text("html.ov.cell.changes", map[string]string{
				"decider": cat.Text("html.ov.decider.model", nil),
				"scope":   cat.Text("html.ov.scope.changes.config", map[string]string{"agents": "agent_client_0123456789abcdef"}),
			}))+"</b>") {
				t.Errorf("%s: target agents not named: %s", lang, cell)
			}
			if tc.cell.Decider == rules.DeciderNotApplicable && !strings.Contains(cell, `<span class="ovp" aria-hidden="true"></span>`) {
				t.Errorf("%s %s: not_applicable has a dot", lang, tc.name)
			}
			if strings.Contains(cell, "{") {
				t.Errorf("%s %s: unresolved %s", lang, tc.name, cell)
			}
		}
		// run_everything only in Cursor's column.
		set := map[string]rules.OverviewCell{rules.RowRunsCommands: cases[len(cases)-1].cell}
		sec := ovSection(t, renderHTML(t, lang, ovReport(t, set, nil, nil)))
		for _, td := range regexp.MustCompile(`(?s)<td data-agent="Claude Code">(.*?)</td>`).FindAllStringSubmatch(sec, -1) {
			if !strings.Contains(td[1], htmlEscape(cat.Text("html.ov.data.run_everything", nil))) {
				continue
			}
			t.Errorf("%s: run_everything in Claude Code's column", lang)
		}
	}
}

// A detected agent not evaluated has no column and is in §11.5; the
// knowledge line, stale at 90 days (rules: TestOverview_KnowledgeStale).
func TestRenderHTML_OverviewNotesAndStale(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		for _, stale := range []bool{false, true} {
			v := ovReport(t, nil, nil, func(v *report.V2) {
				for i := range v.AgentsOverview {
					if v.AgentsOverview[i].Agent == "Claude Code" {
						v.AgentsOverview[i].Knowledge.Stale = stale
					}
				}
			})
			sec := ovSection(t, renderHTML(t, lang, v))
			if strings.Contains(sec, `<th scope="col">Cline`) || strings.Count(sec, `<th scope="col">`) != 2 {
				t.Errorf("%s: Cline has a column", lang)
			}
			cline := `<p><b>Cline</b> · ` + htmlEscape(cat.Text("html.ov.head.not_evaluated.no_inference", nil)) + `</p>`
			if !strings.Contains(sec, cline) {
				t.Errorf("%s: no %q", lang, cline)
			}
			date := ""
			for _, a := range v.AgentsOverview {
				if a.Agent == "Claude Code" {
					d, _ := time.Parse("2006-01-02", *a.Knowledge.Date)
					date = cat.DayLabel(d)
				}
			}
			line := `<p><b>Claude Code</b> · ` + htmlEscape(cat.Text("html.ov.head.knowledge", map[string]string{"date": date}))
			if stale {
				line += ` · <span class="st">` + htmlEscape(cat.Text("html.ov.head.knowledge.stale", nil)) + `</span>`
			}
			if !strings.Contains(sec, line+`</p>`) {
				t.Errorf("%s stale %v: no %q", lang, stale, line)
			}
			if !stale && strings.Contains(sec, `class="st"`) {
				t.Errorf("%s: stale line when not stale", lang)
			}
			if strings.Index(sec, "<b>Cline</b>") < strings.Index(sec, "<b>Claude Code</b>") {
				t.Errorf("%s: the agents not evaluated go after the columns' agents", lang)
			}
		}
	}
}

// §11.6: the narrow rule; §11.7: the dots hidden from assistive technology,
// th scopes.
func TestRenderHTML_OverviewNarrowAndA11y(t *testing.T) {
	page := renderHTML(t, "es", twoAgents(t))
	for _, want := range []string{
		`@media (max-width:1099px){table.ovt thead{position:absolute;`,
		`.ovt tbody tr{display:block;border-bottom:1px solid var(--line);padding:12px 0}`,
		`.ovt td::before{content:attr(data-agent);display:block;font-size:13px;font-weight:700;text-transform:uppercase;letter-spacing:.05em;color:var(--mut);margin-bottom:2px}`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("no %q", want)
		}
	}
	sec := ovSection(t, page)
	if strings.Count(sec, `<span class="ovp`) != strings.Count(sec, `aria-hidden="true"></span>`) {
		t.Error("a dot is not aria-hidden")
	}
	if strings.Count(sec, "<th ") != strings.Count(sec, `<th scope="col">`)+strings.Count(sec, `<th scope="row">`) {
		t.Error("a th without scope")
	}
}

// §11.8: the Markdown has the table, the tags in brackets and §11.5's
// lines, and no secondary line.
func TestRenderV2Markdown_Overview(t *testing.T) {
	cat, _ := catalog.Load("en")
	v := twoAgents(t)
	md := report.RenderV2Markdown(v, cat, time.UTC)
	i, j, k := strings.Index(md, "## "+cat.Text("section.posture", nil)), strings.Index(md, "## How your agents work"), strings.Index(md, "## "+cat.Text("section.memory", nil))
	if j < 0 || !(i < j && (k < 0 || j < k)) {
		t.Fatalf("section at %d (posture %d, memory %d)", j, i, k)
	}
	sec := md[j:]
	if k > j {
		sec = md[j:k]
	}
	if !strings.Contains(sec, "| | Cursor | Claude Code |\n|---|---|---|\n| Reads your files | ") {
		t.Errorf("no table head:\n%s", sec)
	}
	if !strings.Contains(sec, " ("+cat.Text("html.ov.tag.product_default", nil)) {
		t.Errorf("no tags in brackets:\n%s", sec)
	}
	if !strings.Contains(sec, "**Claude Code** · "+strings.SplitN(cat.Text("html.ov.head.knowledge", map[string]string{"date": ""}), " ", 2)[0]) {
		t.Errorf("no knowledge line:\n%s", sec)
	}
	// The also line follows the tags (v3 §15.6); no other secondary line.
	alsos := 0
	for _, a := range v.AgentsOverview {
		for _, c := range a.Rows {
			if c.Also != nil {
				alsos++
			}
		}
	}
	if alsos == 0 || strings.Count(sec, "); "+cat.Text("html.ov.also.model", nil)[:8]) != alsos {
		t.Errorf("%d also lines, want %d after the tags:\n%s", strings.Count(sec, "); "), alsos, sec)
	}
	page := ovSection(t, renderHTML(t, "en", v))
	for _, m := range regexp.MustCompile(`<em>([^<]*)</em>`).FindAllStringSubmatch(page, -1) {
		if strings.HasPrefix(html.UnescapeString(m[1]), cat.Text("html.ov.also.model", nil)[:8]) {
			continue
		}
		if strings.Contains(sec, m[1]) || strings.Contains(sec, html.UnescapeString(m[1])) {
			t.Errorf("secondary line in the Markdown: %q", m[1])
		}
	}
}

// BV-62: the launchers' line is a one/other pair, literal in en and es.
func TestRenderHTML_OverviewLaunchersPlural(t *testing.T) {
	want := map[string]map[int]string{
		"en": {1: "1 of them can run any code", 2: "2 of them can run any code"},
		"es": {1: "1 de ellos puede ejecutar cualquier código", 2: "2 de ellos pueden ejecutar cualquier código"},
	}
	for lang, byN := range want {
		for n, line := range byN {
			c := ovc(rules.DeciderNobody, rules.ScopeMachine, rules.OriginConfigured)
			c.Data.PreapprovedRules, c.Data.LauncherRules = ip(5), ip(n)
			sec := ovSection(t, renderHTML(t, lang, ovReport(t, map[string]rules.OverviewCell{rules.RowRunsCommands: c}, nil, nil)))
			if !strings.Contains(sec, "<em>"+htmlEscape(line)+"</em>") {
				t.Errorf("%s %d: no %q", lang, n, line)
			}
		}
	}
}
