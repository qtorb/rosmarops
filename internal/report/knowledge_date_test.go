package report_test

import (
	"html"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/report"
)

// BV-148: both reports show when the agent knowledge was last reviewed, with the
// date formatted as the knowledge column already formats it, and warn when the
// running program is more than 14 days older than that review. The comparison is
// offline, against the run's own local date, injected here through GeneratedAt:
// 14 days is not old, 15 is.
func TestKnowledgeReviewDate(t *testing.T) {
	reviewed, err := time.Parse("2006-01-02", knowledge.ReviewedOn)
	if err != nil {
		t.Fatal(err)
	}
	g := v2Graph(t)
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		date := cat.DayLabel(reviewed) // the same formatter the knowledge column uses
		reviewedLine := cat.Text("html.knowledge.reviewed", map[string]string{"date": date})
		oldLine := cat.Text("html.knowledge.old", map[string]string{"date": date})
		if !strings.Contains(reviewedLine, date) {
			t.Fatalf("%s: the review-date text does not carry the formatted date", lang)
		}
		for _, tc := range []struct {
			name string
			days int
			old  bool
		}{
			{"14 days is not old", 14, false},
			{"15 days is old", 15, true},
		} {
			v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
			// The run's local date is injected through GeneratedAt (UTC below).
			v.GeneratedAt = reviewed.AddDate(0, 0, tc.days).Format(time.RFC3339)
			page := report.RenderHTML(v, cat, "/h", time.UTC)
			md := report.RenderV2Markdown(v, cat, time.UTC)
			// The HTML escapes its text; the Markdown does not.
			if !strings.Contains(page, html.EscapeString(reviewedLine)) {
				t.Errorf("%s/%s: HTML lacks the review-date line", lang, tc.name)
			}
			if !strings.Contains(md, reviewedLine) {
				t.Errorf("%s/%s: Markdown lacks the review-date line", lang, tc.name)
			}
			if got := strings.Contains(page, html.EscapeString(oldLine)); got != tc.old {
				t.Errorf("%s/%s: HTML stale warning present=%v, want %v", lang, tc.name, got, tc.old)
			}
			if got := strings.Contains(md, oldLine); got != tc.old {
				t.Errorf("%s/%s: Markdown stale warning present=%v, want %v", lang, tc.name, got, tc.old)
			}
		}
	}
}
