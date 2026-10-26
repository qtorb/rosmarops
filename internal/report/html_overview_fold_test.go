package report_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// Informe HTML v3 §16.3: «How your agents work» folded.

// The real case of two agents: two summary lines and the button; the table
// only inside the window, once.
func TestRenderHTML_OverviewFolded(t *testing.T) {
	v := twoAgents(t)
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		folded := ovFolded(t, page)
		if n := strings.Count(folded, `<p class="ovs">`); n != 2 {
			t.Errorf("%s: %d summary lines", lang, n)
		}
		if !strings.Contains(folded, `<button type="button" class="b2" id="ov-open">`+htmlEscape(cat.Text("html.ov.open", nil))+`</button>`) {
			t.Errorf("%s: no open button", lang)
		}
		if strings.Contains(folded, `<table`) || strings.Count(page, `<table class="ovt"`) != 1 || !strings.Contains(ovSection(t, page), `<table class="ovt"`) {
			t.Errorf("%s: the table is not once, inside the window", lang)
		}
		dlg := ovSection(t, page)
		if !strings.HasPrefix(dlg, `<dialog id="ov-dialog" aria-labelledby="ov-dlg-title" hidden><button type="button" class="b2" id="ov-close">`+htmlEscape(cat.Text("html.ov.close", nil))+`</button><h2 id="ov-dlg-title">`) {
			t.Errorf("%s: window head %.200s", lang, dlg)
		}
		if !strings.HasSuffix(page[:strings.Index(page, "</main>")], "</dialog>") {
			t.Errorf("%s: the window is not at the end of main", lang)
		}
		for _, js := range []string{"showModal()", ".close()", "oo.focus()", "classList.add('inline')"} {
			if !strings.Contains(page, js) {
				t.Errorf("%s: script lacks %s", lang, js)
			}
		}
	}
}

// The summary: groups nobody then model, the verbs in T1's order joined
// with «y»/«and»; model_provider and changes_other_agents with scope none
// do not count.
func TestRenderHTML_OverviewSummary(t *testing.T) {
	nobody := ovc(rules.DeciderNobody, rules.ScopeMachine, rules.OriginConfigured)
	model := ovc(rules.DeciderModel, rules.ScopeMachine, rules.OriginProductDefault)
	cc := map[string]rules.OverviewCell{
		rules.RowReadsFiles:         nobody,
		rules.RowWritesFiles:        model,
		rules.RowRunsCommands:       nobody,
		rules.RowSendsOut:           model,
		rules.RowChangesOtherAgents: ovc(rules.DeciderYou, rules.ScopeNone, rules.OriginNone),
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		folded := ovFolded(t, renderHTML(t, lang, ovReport(t, cc, nil, nil)))
		line := regexp.MustCompile(`<p class="ovs"><b>Claude Code</b> · (.*?)\.</p>`).FindStringSubmatch(folded)
		verb := func(r string) string { return cat.Text("html.ov.verb."+r, nil) }
		want := `<span class="ovp nobody" aria-hidden="true"></span>` +
			htmlEscape(cat.Text("html.ov.summary.nobody", map[string]string{"actions": verb(rules.RowReadsFiles) + cat.Text("html.list.and", nil) + verb(rules.RowRunsCommands)})) +
			` · <span class="ovp model" aria-hidden="true"></span>` +
			htmlEscape(cat.Text("html.ov.summary.model", map[string]string{"actions": verb(rules.RowWritesFiles) + cat.Text("html.list.and", nil) + verb(rules.RowSendsOut)}))
		if line == nil || line[1] != want {
			t.Errorf("%s: summary %v, want %q", lang, line, want)
		}
		if line != nil && (strings.Contains(line[1], htmlEscape(verb(rules.RowChangesOtherAgents))) || strings.Contains(line[1], "ovp you")) {
			t.Errorf("%s: changes_other_agents with scope none counted: %s", lang, folded)
		}
		if line != nil && strings.Contains(line[1], "Anthropic") || strings.Contains(folded, "Anthropic") {
			t.Errorf("%s: model_provider in the summary", lang)
		}
		if !strings.Contains(folded, `<p class="ovne"><b>Cline</b> · `+htmlEscape(cat.Text("html.ov.head.not_evaluated.no_inference", nil))+`</p>`) {
			t.Errorf("%s: no line of the agents not evaluated", lang)
		}
	}
}

// With no evaluated agent: no button and no window; the section keeps the
// agents detected.
func TestRenderHTML_OverviewNoEvaluated(t *testing.T) {
	v := ovReport(t, nil, nil, func(v *report.V2) {
		var keep []rules.OverviewAgent
		for _, a := range v.AgentsOverview {
			if !a.Evaluated {
				keep = append(keep, a)
			}
		}
		v.AgentsOverview = keep
	})
	page := renderHTML(t, "es", v)
	if strings.Contains(page, `id="ov-open"`) || strings.Contains(page, `<dialog`) || strings.Contains(page, `<table class="ovt"`) {
		t.Error("a button or a window with no evaluated agent")
	}
	if !strings.Contains(ovFolded(t, page), `<p class="ovne"><b>Cline</b>`) {
		t.Error("no line of the agents detected")
	}
}
