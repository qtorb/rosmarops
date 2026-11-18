package report_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/report"
)

// buildV2Limit builds a V2 with the BV-155 limit fields set, for the given
// repo roots and V1 report.
func buildV2Limit(t *testing.T, roots []string, v1 report.Report, limitMin int, reached bool, notReviewed int) report.V2 {
	t.Helper()
	g := v2Graph(t)
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("ab", 32), Base: base, RepoRoots: roots,
		V1: v1, GeneratedAt: v2Now,
		LimitMinutes: limitMin, LimitReached: reached, ProjectsNotReviewed: notReviewed,
	})
	return v
}

// BV-155: the report shows the run's time limit, and, when it expired, a warn
// note with how many projects were not reviewed; the global_timeout scope is
// never shown as a code.
func TestLimitLine(t *testing.T) {
	roots48 := make([]string, 48)
	for i := range roots48 {
		roots48[i] = "/src/p" + strconv.Itoa(i)
	}

	t.Run("limit line, many projects", func(t *testing.T) {
		v := buildV2Limit(t, roots48, report.Build("t", nil, nil, 0, 0, 0, 0, nil), 12, false, 0)
		for _, lang := range []string{"en", "es"} {
			cat, _ := catalog.Load(lang)
			page := renderHTML(t, lang, v)
			// The projects checked are in the first screen's line of data.
			if !strings.Contains(page, `<p class="r3-meta">`) || !strings.Contains(page, " · 48 ") {
				t.Errorf("%s: the line of data does not count the 48 projects", lang)
			}
			// No warn line when the limit was not reached.
			if strings.Contains(page, htmlEscape(cat.Text("html.limit.reached", map[string]string{"left": "0", "projects": "48"}))) {
				t.Errorf("%s: warn line shown when the limit was not reached", lang)
			}
		}
	})

	t.Run("one project is counted", func(t *testing.T) {
		v := buildV2Limit(t, []string{"/src/only"}, report.Build("t", nil, nil, 0, 0, 0, 0, nil), 5, false, 0)
		cat, _ := catalog.Load("en")
		page := renderHTML(t, "en", v)
		// The signed line of data has one form, «{projects} projects»; its
		// project count is 1 here.
		want := htmlEscape(cat.Text("html.r3.meta", map[string]string{
			"date": "", "os": "", "agents": strconv.Itoa(len(v.Index.Scope.Evaluated)), "projects": "1", "version": "",
		}))
		_, tail, _ := strings.Cut(want, " · ")
		_, tail, _ = strings.Cut(tail, " · ")
		tail, _, _ = strings.Cut(tail, " · RosmarOps")
		if !strings.Contains(page, tail) {
			t.Errorf("the line of data does not count one project: %q", tail)
		}
	})

	t.Run("limit reached: warn with left, no global_timeout code", func(t *testing.T) {
		v1 := report.Build("t", nil, []finding.ScanError{{Scope: "global_timeout", Message: "global scan timeout"}}, 0, 0, 0, 0, nil)
		v := buildV2Limit(t, roots48, v1, 12, true, 3)
		for _, lang := range []string{"en", "es"} {
			cat, _ := catalog.Load(lang)
			page := renderHTML(t, lang, v)
			warn := htmlEscape(cat.Text("html.limit.reached", map[string]string{"left": "3", "projects": "48"}))
			if !strings.Contains(page, warn) {
				t.Errorf("%s: no limit-reached warn %q", lang, warn)
			}
			if strings.Contains(page, "global_timeout") {
				t.Errorf("%s: the global_timeout scope is shown as a code", lang)
			}
		}
	})
}
