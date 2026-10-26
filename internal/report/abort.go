package report

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// RenderAbortHTML is the red report.html of an integrity abort (decision
// 2026-10-26, integridad y revisión semanal, J2; informe HTML v3 §9.5,
// §17.6): the header, the red headline, the line of a configuration change
// nobody confirmed (configRejected), under the headline the line of a
// scheduled review that did not run (unattended), and the pointer to the
// Markdown report mdBase. A lost state has its own headline (J11). Only
// catalog texts; no script.
func RenderAbortHTML(cat catalog.Catalog, at time.Time, loc *time.Location, mdBase string, configRejected, stateLost, unattended bool) string {
	if loc == nil {
		loc = time.Local
	}
	t := func(key string, p map[string]string) string { return html.EscapeString(cat.Text(key, p)) }
	page := fmt.Sprintf(`<!DOCTYPE html><html lang="%s"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title><style>%s</style></head><body><main>`,
		html.EscapeString(cat.Lang()), t("html.doc_title", nil), htmlCSS())
	page += `<div class="hdr">` + t("html.header", map[string]string{"date": cat.DayLabel(at.In(loc))}) + `</div>`
	if configRejected {
		page += `<p style="color:var(--red);font-size:15px;font-weight:600;margin:0 0 8px">` + t("html.config.rejected", nil) + `</p>`
	}
	headline := "html.status.red"
	if stateLost {
		headline = "html.integrity.state_lost"
	}
	page += `<h1 class="hl">` + t(headline, nil) + `</h1>`
	if unattended {
		page += `<p class="meta">` + t("html.unattended.pending", nil) + `</p>`
	}
	page += `<p class="foot">` + t("html.legacy.pointer", map[string]string{"file": mdBase + ".md"}) + `</p></main></body></html>` + "\n"
	return page
}

// WriteAbortHTML writes page as report.html in dir and returns its path.
func WriteAbortHTML(dir, page string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create report dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, HTMLFileName)
	if err := os.WriteFile(path, []byte(page), 0o600); err != nil {
		return "", fmt.Errorf("write html report: %w", err)
	}
	return path, nil
}
