package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
)

// BV-93, B3: the run-could-not-finish page is red, carries the run_failed
// text in each language, and leaves no placeholder or raw catalog key.
func TestRenderRunFailedHTML(t *testing.T) {
	at := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	for _, lang := range catalog.Langs() {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		page := report.RenderRunFailedHTML(cat, at, time.UTC)
		if !strings.Contains(page, `<html lang="`+lang+`">`) {
			t.Errorf("%s: wrong lang attribute", lang)
		}
		if want := cat.Text("html.abort.run_failed", nil); !strings.Contains(page, want) {
			t.Errorf("%s: page lacks the run_failed text %q", lang, want)
		}
		// The body only: the <style> block legitimately uses braces.
		body := page[strings.Index(page, "<main>"):]
		if strings.Contains(body, "{") || strings.Contains(body, "[html.") {
			t.Errorf("%s: placeholder or raw key left in the body:\n%s", lang, body)
		}
	}
}
