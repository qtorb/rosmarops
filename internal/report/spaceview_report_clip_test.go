package report

import (
	"strings"
	"testing"
)

// BV-177 point 3: clipRunes cuts a text to its limit at the end with «…».
func TestClipRunes(t *testing.T) {
	got := clipRunes(strings.Repeat("a", 10), 5)
	if r := []rune(got); len(r) != 5 || r[len(r)-1] != '…' {
		t.Errorf("clip to 5 = %q (%d runes), want 5 ending in …", got, len([]rune(got)))
	}
	if s := clipRunes("abc", 5); s != "abc" {
		t.Errorf("clip of a short string changed it: %q", s)
	}
	// Cut by runes, not bytes.
	if r := []rune(clipRunes(strings.Repeat("ñ", 10), 4)); len(r) != 4 {
		t.Errorf("multibyte clip gave %d runes, want 4", len(r))
	}
	if s := clipRunes("abc", 0); s != "" {
		t.Errorf("clip to 0 = %q, want empty", s)
	}
}

// clampReport clamps lists to maxItems and texts to maxLength.
func TestClampReportLists(t *testing.T) {
	big := func(n, runes int) []SVT {
		out := make([]SVT, n)
		for i := range out {
			out[i] = SVT{EN: strings.Repeat("x", runes), ES: strings.Repeat("y", runes)}
		}
		return out
	}
	r := &SVReport{
		Step1:    SVStep1{Warnings: big(20, 800)},
		Coverage: big(80, 800),
		Signals:  big(40, 400),
		Rows:     make([]SVRow, 600),
	}
	clampReport(r)
	if len(r.Step1.Warnings) != svMaxWarn {
		t.Errorf("warnings not capped: %d", len(r.Step1.Warnings))
	}
	if len(r.Coverage) != svMaxCov || len(r.Signals) != svMaxSignal {
		t.Errorf("coverage/signals not capped: %d / %d", len(r.Coverage), len(r.Signals))
	}
	if len(r.Rows) != svMaxRows {
		t.Errorf("rows not capped: %d", len(r.Rows))
	}
	for _, w := range r.Step1.Warnings {
		if len([]rune(w.EN)) > svMax600 || len([]rune(w.ES)) > svMax600 {
			t.Errorf("a warning was not clamped to 600 runes")
		}
	}
}
