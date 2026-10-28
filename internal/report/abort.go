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

// RenderRunFailedHTML is the red report.html written when a scheduled review
// or a double click could not finish (BV-93, B3): the run produced no new
// report, so the page says the previous one is not up to date and the person
// must open rosmarops.exe to see the error. A run that cannot finish must not
// leave an earlier green report showing as if it were today's. Only catalog
// texts; no script.
func RenderRunFailedHTML(cat catalog.Catalog, at time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	t := func(key string, p map[string]string) string { return html.EscapeString(cat.Text(key, p)) }
	page := fmt.Sprintf(`<!DOCTYPE html><html lang="%s"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title><style>%s</style></head><body><main>`,
		html.EscapeString(cat.Lang()), t("html.doc_title", nil), htmlCSS())
	page += `<div class="hdr">` + t("html.header", map[string]string{"date": cat.DayLabel(at.In(loc))}) + `</div>`
	page += `<h1 class="hl">` + t("html.status.red", nil) + `</h1>`
	page += `<p class="meta">` + t("html.abort.run_failed", nil) + `</p>`
	page += `</main></body></html>` + "\n"
	return page
}

// WriteAbortHTML writes the abort's pages (decision 2026-10-27, primer uso
// con un solo .exe, U7): report.<lang>.html in each language of pages, and
// report.html in runLang, so that no page is left showing an earlier
// report. It returns report.html's path.
func WriteAbortHTML(dir string, pages map[string]string, runLang string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create report dir %s: %w", dir, err)
	}
	run, ok := pages[runLang]
	if !ok {
		return "", fmt.Errorf("abort html: no page in %q", runLang)
	}
	for lang, page := range pages {
		if err := os.WriteFile(filepath.Join(dir, LangFileName(lang)), []byte(page), 0o600); err != nil {
			return "", fmt.Errorf("write html report: %w", err)
		}
	}
	path := filepath.Join(dir, HTMLFileName)
	if err := os.WriteFile(path, []byte(run), 0o600); err != nil {
		return "", fmt.Errorf("write html report: %w", err)
	}
	return path, nil
}
