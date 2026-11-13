package report

import (
	"html"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/scan/projects"
)

// TestAssumeTextPerAgent (BV-229 §4): every agent the engine knows has its
// own analysis assumption, html.assume.<product>, in English and in Spanish
// (never the English fallback in the Spanish page).
func TestAssumeTextPerAgent(t *testing.T) {
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(bases) < 9 {
		t.Fatalf("only %d knowledge bases: the test is not seeing the engine's agents", len(bases))
	}
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		for product := range bases {
			if key := "html.assume." + product; !cat.HasOwn(key) {
				t.Errorf("%s: %s has no text", lang, key)
			}
		}
	}
}

// TestNotSearchedOneForm (BV-229 §3, §4): a reason with 1 folder shows its
// 1 form; with more, today's key, the plural form.
func TestNotSearchedOneForm(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		for _, count := range []int{1, 3} {
			p := &ProjectsCoverage{Search: "complete", Found: 2}
			for _, r := range projects.ReasonOrder {
				p.NotSearched = append(p.NotSearched, ProjectNotSearched{Reason: string(r), Count: count})
			}
			var sb strings.Builder
			c := htmlCtx{v: V2{Coverage: V2Coverage{Projects: p}}, cat: cat, sb: &sb}
			c.renderProjects()
			for _, r := range projects.ReasonOrder {
				key := "html.projects.not_searched." + string(r)
				if count == 1 {
					key += ".one"
				}
				if !cat.HasOwn(key) {
					t.Errorf("%s: %s has no text", lang, key)
					continue
				}
				want := html.EscapeString(cat.Text(key, map[string]string{"count": "3"}))
				if !strings.Contains(sb.String(), want) {
					t.Errorf("%s, %d %s: the page does not show %q", lang, count, r, want)
				}
			}
		}
	}
}
