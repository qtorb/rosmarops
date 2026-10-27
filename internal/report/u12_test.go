package report_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

// U12 (decision 2026-10-27, primer uso con un solo .exe): agents detected
// and none evaluated (only GitHub Copilot) is not «no AI agents found»: the
// score is unavailable because none can be evaluated yet, and step 1 lists
// the agents detected. «No AI agents found» stays for a machine with no
// agent at all; with an evaluated agent, nothing changes.
func TestRenderHTML_DetectedNoneEvaluated(t *testing.T) {
	prov := graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: "k", HostID: "h"}
	copilot := graph.Build(graph.AddExtensions(graph.Fragment{},
		[]platform.VSCodeExtension{{ID: "GitHub.Copilot", Version: "1.300.0"}}, "/h/.vscode/extensions", v2Now), prov, v2Now)
	only := buildV2(t, &copilot, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	if len(only.Index.Agents) != 0 || strings.Join(only.Index.Scope.NotEvaluated, ",") != "GitHub Copilot" ||
		only.Index.Machine.Unavailable != index.ReasonAgentsNotEvaluated {
		t.Fatalf("index: agents %d, not evaluated %v, machine %+v", len(only.Index.Agents), only.Index.Scope.NotEvaluated, only.Index.Machine)
	}
	empty := graph.Build(graph.Fragment{}, prov, v2Now)
	none := buildV2(t, &empty, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	g := v2Graph(t)
	evaluated := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))

	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		unavailable := func(reason string) string {
			return htmlEscape(cat.Text("html.index.unavailable", map[string]string{"reason": cat.Text("html.index.reason."+reason, nil)}))
		}
		listed := htmlEscape(cat.Text("html.step.1.coverage.none", map[string]string{"pending": "GitHub Copilot"}))
		notApplicable := htmlEscape(cat.Text("html.index.reason.not_applicable", nil))

		page := renderHTML(t, lang, only)
		if !strings.Contains(page, `<div class="unavail">`+unavailable(index.ReasonAgentsNotEvaluated)+`</div>`) ||
			!strings.Contains(page, `<br>`+listed+`</p>`) || strings.Contains(page, notApplicable) {
			t.Errorf("%s: only Copilot: reason or list wrong", lang)
		} else {
			t.Logf("%s, only Copilot: %q · %q", lang, cat.Text("html.index.unavailable", map[string]string{"reason": cat.Text("html.index.reason.agents_not_evaluated", nil)}),
				cat.Text("html.step.1.coverage.none", map[string]string{"pending": "GitHub Copilot"}))
		}

		page = renderHTML(t, lang, none)
		if !strings.Contains(page, `<div class="unavail">`+unavailable(index.ReasonNotApplicable)+`</div>`) ||
			strings.Contains(page, htmlEscape(strings.SplitN(cat.Text("html.step.1.coverage.none", nil), "{", 2)[0])) {
			t.Errorf("%s: no agent: not «%s»", lang, cat.Text("html.index.reason.not_applicable", nil))
		} else {
			t.Logf("%s, no agent: %q", lang, cat.Text("html.index.unavailable", map[string]string{"reason": cat.Text("html.index.reason.not_applicable", nil)}))
		}

		// Claude Code evaluated and another agent detected: the score and
		// the usual coverage line, no new text (the reference pages pin the
		// rest).
		page = renderHTML(t, lang, evaluated)
		if !strings.Contains(page, `<div class="score"`) || strings.Contains(page, `<div class="unavail">`) ||
			!strings.Contains(page, htmlEscape(strings.SplitN(cat.Text("html.step.1.coverage", nil), "{", 2)[0])) ||
			strings.Contains(page, htmlEscape(cat.Text("html.index.reason.agents_not_evaluated", nil))) {
			t.Errorf("%s: with Claude Code evaluated, the page changed", lang)
		}
	}
}
