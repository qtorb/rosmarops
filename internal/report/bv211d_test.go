package report_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/report"
)

// TestBV211dUnsentNoteGatedByWillAsk checks the §2 C «unsent problems» note is
// shown only when the run will not ask the person now (BV-211d). With
// ProblemsWillAsk set — the double click is about to show Window A — the note
// must not appear (it would wrongly say «we'll ask you next time» while asking
// now). With it unset — no one present, or the ask was already made — the note
// appears.
func TestBV211dUnsentNoteGatedByWillAsk(t *testing.T) {
	g := v2Graph(t)
	v1 := report.Build("t", []finding.Finding{}, nil, 0, 0, 0, 0, nil)

	base := buildV2(t, &g, v1)
	base.ProblemsUnsent = 1

	willAsk := base
	willAsk.ProblemsWillAsk = true
	if page := renderHTML(t, "en", willAsk); strings.Contains(visibleText(page), "unsent problems") {
		t.Error("with ProblemsWillAsk the unsent note must be suppressed (Window A will ask now)")
	}

	wont := base
	wont.ProblemsWillAsk = false
	if page := renderHTML(t, "en", wont); !strings.Contains(visibleText(page), "unsent problems") {
		t.Error("without ProblemsWillAsk the unsent note must appear")
	}
}
