package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/report"
)

// TestBV211dUnsentNoteGatedByWillAsk checks the §2 C «unsent problems» note is
// shown only when the run will not ask the person now (BV-211d). With
// ProblemsWillAsk set — the double click is about to show Window A — the note
// must not appear; with it unset — no one present, or the ask was already made —
// the note appears.
func TestBV211dUnsentNoteGatedByWillAsk(t *testing.T) {
	g := v2Graph(t)
	v1 := report.Build("t", []finding.Finding{}, nil, 0, 0, 0, 0, nil)
	base := buildV2(t, &g, v1)
	base.ProblemsUnsent = 2

	willAsk := base
	willAsk.ProblemsWillAsk = true
	if page := renderHTML(t, "en", willAsk); strings.Contains(visibleText(page), "unsent problem") {
		t.Error("with ProblemsWillAsk the unsent note must be suppressed (Window A will ask now)")
	}

	wont := base
	wont.ProblemsWillAsk = false
	if page := renderHTML(t, "en", wont); !strings.Contains(visibleText(page), "unsent problem") {
		t.Error("without ProblemsWillAsk the unsent note must appear")
	}
}

// TestBV211dSingularPlural is BV-211d point 2/3: both report problem lines use
// the singular form at n == 1 and the plural otherwise, in EN and ES, in the
// HTML and the Markdown.
func TestBV211dSingularPlural(t *testing.T) {
	g := v2Graph(t)
	v1 := report.Build("t", []finding.Finding{}, nil, 0, 0, 0, 0, nil)
	base := buildV2(t, &g, v1)

	cases := []struct {
		lang      string
		n         int
		unsentHas string // substring that must be in the unsent line
		sentHas   string // substring that must be in the sent line
	}{
		{"en", 1, "There is 1 unsent problem.", "We sent 1 problem to RosmarOps."},
		{"en", 2, "There are 2 unsent problems.", "We sent 2 problems to RosmarOps."},
		{"es", 1, "Hay 1 problema sin enviar.", "Enviamos 1 problema a RosmarOps."},
		{"es", 2, "Hay 2 problemas sin enviar.", "Enviamos 2 problemas a RosmarOps."},
	}
	for _, c := range cases {
		v := base
		v.ProblemsUnsent = c.n
		v.ProblemsWillAsk = false // so the unsent line is rendered
		v.ProblemsSentThisRun = c.n
		// HTML.
		html := visibleText(renderHTML(t, c.lang, v))
		if !strings.Contains(html, c.unsentHas) {
			t.Errorf("%s n=%d HTML: unsent line should read %q", c.lang, c.n, c.unsentHas)
		}
		if !strings.Contains(html, c.sentHas) {
			t.Errorf("%s n=%d HTML: sent line should read %q", c.lang, c.n, c.sentHas)
		}
		// Markdown.
		cat, err := catalog.Load(c.lang)
		if err != nil {
			t.Fatal(err)
		}
		md := report.RenderV2Markdown(v, cat, time.UTC)
		if !strings.Contains(md, c.unsentHas) {
			t.Errorf("%s n=%d Markdown: unsent line should read %q", c.lang, c.n, c.unsentHas)
		}
		if !strings.Contains(md, c.sentHas) {
			t.Errorf("%s n=%d Markdown: sent line should read %q", c.lang, c.n, c.sentHas)
		}
	}
}
