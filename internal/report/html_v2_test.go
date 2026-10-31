package report_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// helpRe is one «i» of informe HTML v2 §4: the button, its question for
// screen readers, and right after it the hidden answer it controls.
var helpRe = regexp.MustCompile(`<button type="button" class="i" aria-expanded="false" aria-controls="(g\d+)"><span aria-hidden="true">i</span><span class="vh">([^<]*)</span></button><span class="gl" id="(g\d+)" hidden><b>([^<]*)</b> ([^<]*)</span>`)

// v2 §4: the seven terms at their places, with unique ids, each answer
// right after its button; §2: one h1, the traffic light as an image named
// by the status, the copy notice.
func TestRenderHTML_HelpAndAccessibility(t *testing.T) {
	g := v2Graph(t)
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		q := map[string]string{}
		for _, term := range []string{"agent", "score", "master", "auto", "env", "config", "settings"} {
			q[htmlEscape(cat.Text("html.gloss."+term+".q", nil))] = term
		}
		seen := map[string]bool{}
		ids := map[string]bool{}
		for _, m := range helpRe.FindAllStringSubmatch(page, -1) {
			if m[1] != m[3] || m[2] != m[4] {
				t.Errorf("%s: «i» %s controls %s", lang, m[1], m[3])
			}
			if ids[m[1]] {
				t.Errorf("%s: id %s twice", lang, m[1])
			}
			ids[m[1]] = true
			term, ok := q[m[2]]
			if !ok || m[5] != htmlEscape(cat.Text("html.gloss."+term+".a", nil)) {
				t.Errorf("%s: «i» %q", lang, m[2])
			}
			seen[term] = true
		}
		if n := strings.Count(page, `class="i"`); n != len(ids) {
			t.Errorf("%s: %d «i», %d well formed", lang, n, len(ids))
		}
		for _, term := range q {
			if !seen[term] {
				t.Errorf("%s: no «i» of %s", lang, term)
			}
		}
		// Places (v3 §3–§5): score after the index name, agent after the
		// agent and band, auto after html.today.reach.
		reach := htmlEscape(cat.Text("html.today.reach", map[string]string{"agent": "Claude Code", "repos": "2"}))
		a := v.Index.Agents[0]
		for _, want := range []string{
			`<div class="iname"><span>` + htmlEscape(cat.Text("html.index.name", nil)) + `</span><button type="button" class="i"`,
			`<p class="sagent">Claude Code · ` + htmlEscape(cat.Text("band."+a.Band, nil)) + `<button type="button" class="i"`,
			reach + `<button type="button" class="i"`,
		} {
			if !strings.Contains(page, want) {
				t.Errorf("%s: no %q", lang, want)
			}
		}

		// v3 §7: one h1, six h2 (step 1, today's action, step 3, §11's section
		// and its window, v3 §16, and the assumptions' window, §17.5), the
		// score an image named by its value. The «Your space» section (BV-119)
		// is not here: this scenario has no key (BV-132).
		if strings.Count(page, "<h1") != 1 || strings.Count(page, "<h2") != 6 {
			t.Errorf("%s: headings h1 %d, h2 %d", lang, strings.Count(page, "<h1"), strings.Count(page, "<h2"))
		}
		of := cat.Text("html.index.of", nil)
		if !strings.Contains(page, `<div class="score" role="img" aria-label="`+htmlEscape(strconv.Itoa(*a.Value)+of)+`"`) {
			t.Errorf("%s: score not an image named by its value", lang)
		}
		if !strings.Contains(page, `</button><span class="live" aria-live="polite"></span>`) {
			t.Errorf("%s: no copy notice", lang)
		}
	}
}

// v2 §5 in the More strip (v3 §6.2): the first run shows only
// html.memory.first and no verdict; the second the evolution, the counts
// and a verdict per row (v3 §5), and the line of a finding no longer
// observed links its run's HTML when it exists.
func TestRenderHTML_Memory(t *testing.T) {
	sbSecret := "sb_secret_" + strings.Repeat("S", 22) + "_" + strings.Repeat("T", 8)
	// An instructions file in both runs: a finding still observed in the
	// step 3 list.
	instructions := []string{"/src/a/CLAUDE.md"}
	first := v2GraphWith(t, []platform.CredentialEntry{
		{Path: "/src/a/dashboard/.env.local", Line: 4, Key: "SUPABASE_SERVICE_ROLE_KEY", Value: secrets.NewSecretValue(serviceRoleJWT)},
		{Path: "/src/b/.env", Line: 1, Key: "SUPABASE_SECRET_KEY", Value: secrets.NewSecretValue(sbSecret)},
	}, instructions)
	second := v2GraphWith(t, []platform.CredentialEntry{
		{Path: "/src/a/dashboard/.env.local", Line: 4, Key: "SUPABASE_SERVICE_ROLE_KEY", Value: secrets.NewSecretValue(serviceRoleJWT)},
	}, instructions)
	run1 := memoryRun(t, &first, memory.File{}, v2Now, "")
	run2 := memoryRun(t, &second, run1.NextMemory(), v2Now.Add(24*time.Hour), "")
	if len(run2.Memory.NoLongerObserved) == 0 {
		t.Fatalf("memory %+v", run2.Memory)
	}
	gone := run2.Memory.NoLongerObserved[0].LastReport

	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		p1 := renderHTML(t, lang, run1)
		box := memoryBox(t, p1, cat)
		if box != `<p>`+htmlEscape(cat.Text("html.memory.first", nil))+`</p>` {
			t.Errorf("%s: first run memory %q", lang, box)
		}
		if !strings.Contains(p1, `<span class="x"></span>`) || strings.Contains(p1, `<span class="x">`+htmlEscape(cat.Text("html.verdict.new", nil))) {
			t.Errorf("%s: verdict on the first run, or no row", lang)
		}

		// The earlier run's page in this language (U7); before U7, its one
		// page.
		dir, old := t.TempDir(), t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, report.PageName(gone, lang)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(old, gone+".html"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := report.WriteHTMLPages(run2, lang, "/h", time.UTC, old, "run2"); err != nil {
			t.Fatal(err)
		}
		if raw, _ := os.ReadFile(filepath.Join(old, report.HTMLFileName)); !strings.Contains(string(raw), `<a href="`+gone+`.html">`) {
			t.Errorf("%s: no link to the earlier run's one page", lang)
		}
		path, err := report.WriteHTMLPages(run2, lang, "/h", time.UTC, dir, "run2")
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(path)
		p2 := string(raw)
		box = memoryBox(t, p2, cat)
		// v3 §17.2: one line of counts (a finding gone), no series.
		for _, want := range []string{
			htmlEscape(strings.SplitN(cat.Text("html.since.counts", nil), "{", 2)[0]),
			`<a href="` + report.PageName(gone, lang) + `">` + report.PageName(gone, lang) + `</a>`,
		} {
			if !strings.Contains(box, want) {
				t.Errorf("%s: memory box lacks %q: %s", lang, want, box)
			}
		}
		// BV-55, v3 §6.2: the fold's label counts its lines.
		n := len(run2.Memory.Moved) + len(run2.Memory.NoLongerObserved)
		fold := `<details><summary>` + htmlEscape(cat.Plural("html.memory.more", n, map[string]string{"count": strconv.Itoa(n)})) + `</summary>`
		if !strings.Contains(box, fold) || strings.Contains(box, " → ") {
			t.Errorf("%s: memory fold not %q: %s", lang, fold, box)
		}
		// v3 §5: the verdict as text in each row.
		rows := strings.Count(p2, `<details class="row">`)
		still := htmlEscape(cat.Text("html.verdict.still", map[string]string{"date": cat.DayMonthLabel(v2Now)}))
		if n := strings.Count(p2, `<span class="x">`+still+`</span>`); rows == 0 || n != rows {
			t.Errorf("%s: %d verdicts %q for %d rows", lang, n, still, rows)
		}
		// Without the earlier HTML, the report's name as text.
		if page := renderHTML(t, lang, run2); !strings.Contains(memoryBox(t, page, cat), htmlEscape(gone)+".") || strings.Contains(page, `<a href=`) {
			t.Errorf("%s: link without the file", lang)
		}
	}
}

// memoryBox is column 1 of the bottom strip, «Since the last review»
// (v3 §17.2), after its heading.
func memoryBox(t *testing.T, page string, cat catalog.Catalog) string {
	t.Helper()
	head := `<div class="more"><div><h3>` + htmlEscape(cat.Text("html.since.title", nil)) + `</h3>`
	i := strings.Index(page, head)
	if i < 0 {
		t.Fatal("no memory block")
	}
	rest := page[i+len(head):]
	return rest[:strings.Index(rest, "</div>")]
}
