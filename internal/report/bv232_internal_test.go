package report

import (
	"strconv"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// BV-232 §6, §7: each counter's form for one, in EN and ES.

// TestProjectsSummaryForms: the four combinations of html.projects.summary.
func TestProjectsSummaryForms(t *testing.T) {
	want := map[string]map[[2]int]string{
		"en": {
			{1, 1}: "RosmarOps found 1 project in 1 folder and checked 1.",
			{1, 3}: "RosmarOps found 1 project in 3 folders and checked 1.",
			{4, 1}: "RosmarOps found 4 projects in 1 folder and checked 1.",
			{4, 3}: "RosmarOps found 4 projects in 3 folders and checked 1.",
		},
		"es": {
			{1, 1}: "RosmarOps encontró 1 proyecto en 1 carpeta y revisó 1.",
			{1, 3}: "RosmarOps encontró 1 proyecto en 3 carpetas y revisó 1.",
			{4, 1}: "RosmarOps encontró 4 proyectos en 1 carpeta y revisó 1.",
			{4, 3}: "RosmarOps encontró 4 proyectos en 3 carpetas y revisó 1.",
		},
	}
	for lang, cases := range want {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		for c, text := range cases {
			p := &ProjectsCoverage{Search: "complete", Found: c[0], Scanned: 1, Roots: make([]string, c[1])}
			var sb strings.Builder
			ctx := htmlCtx{v: V2{Coverage: V2Coverage{Projects: p}}, cat: cat, sb: &sb}
			ctx.renderProjects()
			if !strings.Contains(sb.String(), text) {
				t.Errorf("%s %v: no %q in %s", lang, c, text, sb.String())
			}
		}
	}
}

// TestLimitReachedForms: your one project, one of several, several.
func TestLimitReachedForms(t *testing.T) {
	en, _ := catalog.Load("en")
	es, _ := catalog.Load("es")
	for _, c := range []struct {
		left, projects int
		en, es         string
	}{
		{1, 1, "This review reached its time limit: your project was not checked, so it is not reported as clean.", "Esta revisión llegó a su límite de tiempo: tu proyecto quedó sin revisar, así que no se da por limpio."},
		{1, 5, "This review reached its time limit: 1 of 5 projects was not checked, so it is not reported as clean.", "Esta revisión llegó a su límite de tiempo: 1 de 5 proyectos quedó sin revisar, así que no se da por limpio."},
		{3, 5, "This review reached its time limit: 3 of 5 projects were not checked, so they are not reported as clean.", "Esta revisión llegó a su límite de tiempo: 3 de 5 proyectos quedaron sin revisar, así que no se dan por limpios."},
	} {
		p := map[string]string{"left": strconv.Itoa(c.left), "projects": strconv.Itoa(c.projects)}
		if got := en.Text(limitReachedKey(c.left, c.projects), p); got != c.en {
			t.Errorf("en %d/%d: %q", c.left, c.projects, got)
		}
		if got := es.Text(limitReachedKey(c.left, c.projects), p); got != c.es {
			t.Errorf("es %d/%d: %q", c.left, c.projects, got)
		}
	}
}

// TestOneFormsChosen: every §6 counter with 1 takes its form for one, and with
// more keeps today's key, in EN and ES (finding.cred_in_git.what and
// legacy.vscode.first_run have their form in English only).
func TestOneFormsChosen(t *testing.T) {
	both := []string{
		"html.today.reach", "html.chip.any_project", "html.f.own_instructions_writable.impact",
		"html.projects.new", "html.projects.gone", "html.card.silenced",
		"console.projects.not_searched", "problems.autosent.body",
	}
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		keys := both
		if lang == "en" {
			keys = append(append([]string{}, both...), "finding.cred_in_git.what", "legacy.vscode.first_run")
		}
		for _, k := range keys {
			if !cat.HasOwn(k + ".one") {
				t.Errorf("%s: %s.one has no text", lang, k)
			}
			if got := cat.One(k, 1); got != k+".one" {
				t.Errorf("%s: One(%s, 1) = %s", lang, k, got)
			}
			if got := cat.One(k, 2); got != k {
				t.Errorf("%s: One(%s, 2) = %s", lang, k, got)
			}
		}
		if got := oneOf(cat, "html.f.own_instructions_writable.impact", map[string]string{"count": "1"}); got != "html.f.own_instructions_writable.impact.one" {
			t.Errorf("%s: oneOf: %s", lang, got)
		}
	}
}
