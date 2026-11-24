package integration_test

import (
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/report"
)

var (
	bv297Hidden = regexp.MustCompile(`(?s)<style.*?</style>|<script.*?</script>|<details class="tech">.*?</details>`)
	bv297Tag    = regexp.MustCompile(`<[^>]+>`)
)

// bv297Text is a page's visible text outside the technical details, one
// piece per text node.
func bv297Text(page string) []string {
	var out []string
	for _, s := range bv297Tag.Split(bv297Hidden.ReplaceAllString(page, ""), -1) {
		if s = strings.TrimSpace(html.UnescapeString(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// desktopTitle reports a desktop finding's title, in either language: one
// line (the export for your AI the page embeds is one text of many lines).
func desktopTitle(s string) bool {
	return !strings.Contains(s, "\n") && (strings.Contains(s, "on your desktop without asking") || strings.Contains(s, "escritorio sin preguntarte"))
}

// TestBV297FullComputerTitles: on the full test computer (Codex with 15
// desktop apps and every app allowed), the app id «*» nowhere outside the
// technical details and no title with
// more than three app ids, in the HTML, the Markdown and the export for
// your AI; the HTML's and the Markdown's «where» line names every app,
// with no «and n more»; the agents that can rewrite their own instructions
// are one row.
func TestBV297FullComputerTitles(t *testing.T) {
	out := os.Getenv("ROSMAROPS_BV297_PAGES_OUT")
	for _, lang := range []string{"en", "es"} {
		v, page, cat := bv236Page(t, lang)
		if out != "" {
			if err := os.WriteFile(filepath.Join(out, "report-"+lang+".html"), []byte(page), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		// The desktop app id «*» shows only in the technical details: not as
		// a place, and not in a title.
		if strings.Contains(bv297Hidden.ReplaceAllString(page, ""), "<code>*</code>") {
			t.Errorf("%s: «*» as a place outside the technical details", lang)
		}
		if !strings.Contains(page, "<code>*</code>") {
			t.Errorf("%s: the technical details lost the raw «*»", lang)
		}
		titles := 0
		for _, s := range bv297Text(page) {
			if desktopTitle(s) {
				titles++
				if strings.Contains(s, "*") {
					t.Errorf("%s: «*» in a title: %q", lang, s)
				}
				if n := strings.Count(s, "com.example.app"); n > 3 {
					t.Errorf("%s: a title with %d app ids: %q", lang, n, s)
				}
			}
		}
		if titles == 0 {
			t.Errorf("%s: no desktop title on the full test computer", lang)
		}
		where := regexp.MustCompile(`<p class="where">[^\n]*?com\.example\.app01[^\n]*?</p>`).FindString(page)
		if strings.Count(where, "com.example.app") != 15 || strings.Contains(where, esc(cat.Text("html.tech.more", map[string]string{"count": "14"}))) {
			t.Errorf("%s: the «where» line does not name the 15 apps: %q", lang, where)
		}
		if !strings.Contains(page, esc(cat.Text("html.r3.issue.own_instructions_many", map[string]string{"n": "6", "agents": "\x00"})[:12])) {
			t.Errorf("%s: no single row for the agents that can rewrite their own instructions", lang)
		}

		prev := graph.HostOS
		graph.HostOS = "windows"
		md := report.RenderV2Markdown(v, cat, time.UTC)
		ai := report.RenderAIExport(v, cat, fmHome)
		graph.HostOS = prev
		for name, doc := range map[string]string{"markdown": md, "ai": ai} {
			for _, l := range strings.Split(doc, "\n") {
				if (strings.HasPrefix(l, "#") || strings.HasPrefix(l, "- ")) && desktopTitle(l) {
					if n := strings.Count(l, "com.example.app"); n > 3 {
						t.Errorf("%s %s: a title with %d app ids: %q", lang, name, n, l)
					}
					if strings.Contains(l, "*") {
						t.Errorf("%s %s: «*» in a title: %q", lang, name, l)
					}
				}
			}
		}
		if got := strings.Count(md, "com.example.app"); got < 15 {
			t.Errorf("%s markdown: %d app ids, want the 15 in its «where»", lang, got)
		}
		mdWhere := cat.Text("html.where", nil) + " `com.example.app01`"
		if !strings.Contains(md, mdWhere) {
			t.Errorf("%s markdown: no «where» line %q", lang, mdWhere)
		}
	}
}
