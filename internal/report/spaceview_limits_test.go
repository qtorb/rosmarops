package report_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/report"
)

// withEnText returns a copy of the scenario space view whose first finding's
// English text is mutated, so the only thing that can fail the schema is that
// mutation.
func withEnText(base report.SpaceView, mut func(*report.SVText)) report.SpaceView {
	sv := base
	findings := make([]report.SVFinding, len(base.Findings))
	copy(findings, base.Findings)
	f := findings[0]
	text := map[string]report.SVText{}
	for k, v := range f.Text {
		text[k] = v
	}
	en := text["en"]
	en.Steps = append([]string(nil), en.Steps...)
	mut(&en)
	text["en"] = en
	f.Text = text
	findings[0] = f
	sv.Findings = findings
	return sv
}

// BV-152 point 2/5: the raised space-view limits — a step of 1000 and a paste
// block of 16000 validate; 1001 and 16001 do not.
func TestSpaceViewFieldLimits(t *testing.T) {
	_, base := buildSpaceViewScenario(t)
	if len(base.Findings) == 0 {
		t.Fatal("scenario has no findings")
	}
	cases := []struct {
		name  string
		mut   func(*report.SVText)
		valid bool
	}{
		{"step 1000", func(e *report.SVText) { e.Steps = []string{strings.Repeat("s", 1000)} }, true},
		{"step 1001", func(e *report.SVText) { e.Steps = []string{strings.Repeat("s", 1001)} }, false},
		{"paste 16000", func(e *report.SVText) { p := strings.Repeat("p", 16000); e.PasteBlock = &p }, true},
		{"paste 16001", func(e *report.SVText) { p := strings.Repeat("p", 16001); e.PasteBlock = &p }, false},
	}
	for _, c := range cases {
		_, err := report.RenderSpaceViewJSON(withEnText(base, c.mut))
		switch {
		case c.valid && err != nil:
			t.Errorf("%s: should validate, got %v", c.name, err)
		case !c.valid && err == nil:
			t.Errorf("%s: should fail the schema, but validated", c.name)
		}
	}
}

// BV-152 point 1 (stopper): the schema-validation error logged when a package is
// skipped carries only field paths and limits, never the offending value. A fake
// secret placed in an over-long step must not appear in the error.
func TestResultsPackageErrorHasNoValues(t *testing.T) {
	_, base := buildSpaceViewScenario(t)
	const secret = "ghp_FAKE_secret_value_must_not_leak_0000"
	sv := withEnText(base, func(e *report.SVText) {
		e.Steps = []string{secret + strings.Repeat("x", 1001)}
	})
	_, err := report.RenderSpaceViewJSON(sv)
	if err == nil {
		t.Fatal("an over-long step should fail the schema")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the schema error leaks the field value: %v", err)
	}
}
