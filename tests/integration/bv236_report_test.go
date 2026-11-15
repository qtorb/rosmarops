package integration_test

import (
	"html"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/profiles"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// bv236Page is the HTML report of the full test computer, on Windows.
func bv236Page(t *testing.T, lang string, opts ...func(*report.V2Input)) (report.V2, string, catalog.Catalog) {
	t.Helper()
	prev := graph.HostOS
	defer func() { graph.HostOS = prev }()
	graph.HostOS = "windows"
	v2 := bv236V2(t, opts...)
	v2.OS = "windows"
	cat, err := catalog.Load(lang)
	if err != nil {
		t.Fatal(err)
	}
	return v2, report.RenderHTML(v2, cat, fmHome, time.UTC), cat
}

func esc(s string) string { return html.EscapeString(s) }

// between is the part of page from the first from to the next to.
func between(t *testing.T, page, from, to string) string {
	t.Helper()
	i := strings.Index(page, from)
	if i < 0 {
		t.Fatalf("no %q", from)
	}
	rest := page[i:]
	j := strings.Index(rest, to)
	if j < 0 {
		t.Fatalf("no %q after %q", to, from)
	}
	return rest[:j]
}

// TestBV236Sheet: the sheet's numbers come from the engine (index.machine,
// its factory value, the kinds of the evaluated agents and the agents'
// groups), in EN and ES.
func TestBV236Sheet(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		v, page, cat := bv236Page(t, lang)
		sheet := between(t, page, `<article class="sheet"`, `</article>`)
		m := v.Index.Machine
		if m.Value == nil || m.FactoryValue == nil || m.Letter == nil || m.Letter.Class == nil {
			t.Fatalf("machine score incomplete: %+v", m)
		}
		var machineAgent string
		for _, a := range v.Index.Agents {
			if a.Value != nil && *a.Value == *m.Value && machineAgent == "" {
				machineAgent = a.Agent
			}
		}
		agents, repos := len(v.Index.Scope.Evaluated), v.Index.Scope.Repos
		for _, want := range []string{
			esc(cat.Text("sheet.title", nil)), esc(cat.Text("sheet.sub", nil)),
			esc(cat.Text("sheet.scope", map[string]string{
				"agents":   cat.Plural("sheet.scope.agents", agents, map[string]string{"n": strconv.Itoa(agents)}),
				"projects": cat.Plural("sheet.scope.projects", repos, map[string]string{"n": strconv.Itoa(repos)}),
			})),
			"RosmarOps 0.12.0", ">Windows<",
			`big">` + strconv.Itoa(*m.Value) + `</b>`,
			esc(cat.Text("sheet.result.label", map[string]string{"agent": machineAgent})),
			esc(cat.Text("sheet.class."+*m.Letter.Class, nil)),
			"<b>" + strconv.Itoa(*m.FactoryValue) + "</b>",
			esc(cat.Text("sheet.reading.L1", map[string]string{"agent": machineAgent})),
			esc(cat.Text("sheet.act.A1", nil)), esc(cat.Text("sheet.act.profile", nil)), esc(cat.Text("sheet.act.findings", nil)),
			esc(cat.Text("sheet.foot.P1", nil)),
		} {
			if !strings.Contains(sheet, want) {
				t.Errorf("%s: the sheet lacks %q", lang, want)
			}
		}
		if d := *m.Value - *m.FactoryValue; d == 0 && !strings.Contains(sheet, esc(cat.Text("sheet.reading.L4", nil))) {
			t.Errorf("%s: score equals the factory score but no L4", lang)
		}
		// The strip: five kinds, each the worst state among the evaluated agents.
		rank := map[string]int{rules.StateNobody: 7, rules.StateModel: 6, rules.StateBounded: 5, rules.StateYou: 4, rules.StateBlocked: 3, rules.StateUnknown: 2, rules.StateNoneKnown: 1}
		for _, kind := range rules.KindOrder {
			worst := rules.StateUnknown
			first := true
			for _, o := range v.AgentsOverview {
				if !o.Evaluated {
					continue
				}
				for _, k := range o.Kinds {
					if k.Kind == kind && (first || rank[k.State] > rank[worst]) {
						worst, first = k.State, false
					}
				}
			}
			if !strings.Contains(sheet, `data-kind="`+kind+`" data-state="`+worst+`"`) {
				t.Errorf("%s: kind %s is not %s", lang, kind, worst)
			}
		}
		// Findings: at most three rows, «{n} findings · the first 3».
		if n := strings.Count(between(t, sheet, `<tbody>`, `</tbody>`), "<tr>"); n != 3 {
			t.Errorf("%s: %d finding rows, want 3", lang, n)
		}
		if !strings.Contains(sheet, "/") || !regexp.MustCompile(`<em>\d+ `).MatchString(sheet) {
			t.Errorf("%s: no findings count", lang)
		}
		// Every «would go up to» names a group's score above the agent's.
		for _, mm := range regexp.MustCompile(`(\d+)</td></tr>`).FindAllStringSubmatch(sheet, -1) {
			n, _ := strconv.Atoi(mm[1])
			if n <= *m.Value {
				t.Errorf("%s: «would go up to %d» not above %d", lang, n, *m.Value)
			}
		}
		// The promise line stays, under the sheet.
		if !strings.Contains(between(t, page, `<div class="sheetnotes">`, `</div>`), esc(cat.Text("html.header", map[string]string{"date": cat.DayLabel(fmNow.UTC())}))) {
			t.Errorf("%s: the «sent nothing» line is gone", lang)
		}
		// «Your space» leads to today's section.
		if !strings.Contains(page, `<a class="spacego" href="#your-space">`) || !strings.Contains(page, `<span id="your-space"></span><section class="space">`) {
			t.Errorf("%s: «Your space» does not lead to its section", lang)
		}
	}
}

// TestBV236ProfileViewWithoutJS: without JavaScript the profile views show
// below the sheet with «I write code» (P1 for Claude Code): the default
// fragments are not hidden, the others are; the text to copy is T1 and the
// literal block.
func TestBV236ProfileViewWithoutJS(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		v, page, cat := bv236Page(t, lang)
		// Only the class «js», set by the script, hides the views.
		if !strings.Contains(page, `.js .view{display:none}`) || regexp.MustCompile(`(^|[\n}])\.view\{display:none`).MatchString(page) {
			t.Errorf("%s: the views are hidden without JavaScript", lang)
		}
		sheetEnd, pc, flow := strings.Index(page, `</article>`), strings.Index(page, `<section class="view pcv" id="v-pc"`), strings.Index(page, `<div class="flow" id="todo">`)
		if sheetEnd < 0 || pc < sheetEnd || flow < pc {
			t.Errorf("%s: the profile view is not between the sheet and the flow", lang)
		}
		if !strings.Contains(page, `name="use" value="code" data-profile="P1" checked`) {
			t.Errorf("%s: «I write code» is not the default use", lang)
		}
		for _, frag := range regexp.MustCompile(`data-p="(P\d)"( hidden)?`).FindAllStringSubmatch(page, -1) {
			if hidden := frag[2] != ""; hidden != (frag[1] != profiles.P1) {
				t.Errorf("%s: a %s fragment hidden=%v", lang, frag[1], hidden)
			}
		}
		// The text to copy: T1 and the block, for P0 and P1; the command for P3.
		for id, want := range map[string]string{
			profiles.P0: cat.Text("cc.T1", nil) + "\n\n" + profiles.Block(profiles.P0),
			profiles.P1: cat.Text("cc.T1", nil) + "\n\n" + profiles.Block(profiles.P1),
			profiles.P3: profiles.Block(profiles.P3),
		} {
			if !strings.Contains(page, `<pre class="ptext" id="pc-`+id+`" tabindex="-1">`+esc(want)+`</pre>`) {
				t.Errorf("%s: %s copies something else", lang, id)
			}
		}
		// Claude Code's row: «Score 11 → 32» with P1, the machine's score with P1.
		cc := between(t, page, `<section class="view pcv"`, `</section>`)
		if !strings.Contains(cc, esc(cat.Text("pc.score", map[string]string{"a": "11", "b": "32"}))) {
			t.Errorf("%s: Claude Code's row lacks «11 → 32»", lang)
		}
		for _, mp := range v.Index.Machine.Profiles {
			if mp.Profile.Profile == profiles.P1 && !strings.Contains(cc, `<span class="arrow">→</span><span class="cls`) {
				t.Errorf("%s: «Your computer» has no after", lang)
			}
		}
		for _, k := range []string{"pc.N1", "pc.N2", "pc.B3", "cc.P1.nc"} {
			if !strings.Contains(cc, esc(cat.Text(k, nil))) {
				t.Errorf("%s: the profile view lacks %s", lang, k)
			}
		}
	}
}

// TestBV236P1bOffWindows: off Windows, P1's «Doesn't cover» is P1b.
func TestBV236P1bOffWindows(t *testing.T) {
	v, _, cat := bv236Page(t, "en")
	v.OS = "linux"
	page := report.RenderHTML(v, cat, fmHome, time.UTC)
	if strings.Contains(page, esc(cat.Text("cc.P1.nc", nil))) || !strings.Contains(page, esc(cat.Text("cc.P1b.nc", nil))) {
		t.Error("on Linux P1's «Doesn't cover» is not P1b")
	}
}

// TestBV236AcceptedInReport: an accepted finding leaves the pending ones
// (the sheet, «What to do first», «What remains»), keeps counting in the
// score, and is in «Accepted by you (n)» with its date.
func TestBV236AcceptedInReport(t *testing.T) {
	v0, _, _ := bv236Page(t, "es")
	first := v0.Today.FindingID
	if first == "" {
		t.Fatal("no today finding")
	}
	v, page, cat := bv236Page(t, "es", func(in *report.V2Input) {
		in.Accepted = []config.AllowEntryAccepted{{ID: first, Date: "2026-11-14"}}
	})
	if v.Today.FindingID == first {
		t.Error("the accepted finding is still today's")
	}
	if *v.Index.Machine.Value != *v0.Index.Machine.Value {
		t.Errorf("accepting changed the score: %d → %d", *v0.Index.Machine.Value, *v.Index.Machine.Value)
	}
	acc := between(t, page, `<span id="accepted">`, `</ul>`)
	if !strings.Contains(acc, esc(cat.Text("report.accepted.title", map[string]string{"n": "1"}))) ||
		!strings.Contains(acc, esc(cat.Text("report.accepted.on", map[string]string{"date": "14 de noviembre de 2026"}))) {
		t.Errorf("no «Asumidos por ti (1)» with the date:\n%s", acc)
	}
	if strings.Contains(between(t, page, `<div class="flow" id="todo">`, `<section aria-labelledby="accepted">`), `id="`+first+`"`) {
		t.Error("the accepted finding is still pending in the flow")
	}
}

// TestBV236PassesWriteNothing: building the report with the factory and
// profile passes writes no file (HOME and the working folder unchanged).
func TestBV236PassesWriteNothing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Chdir(root)
	count := func() int {
		n := 0
		_ = filepath.WalkDir(root, func(string, fs.DirEntry, error) error { n++; return nil })
		return n
	}
	before := count()
	_, page, _ := bv236Page(t, "en")
	if page == "" || count() != before {
		t.Errorf("the passes wrote: %d entries before, %d after", before, count())
	}
}

// TestBV236Browser checks in a real browser, when Playwright is installed
// (skipped otherwise): the sheet fits 1280×800, the views open and close,
// and with the clipboard denied the text shows, selected, with C1 — in the
// profile bar and in today's copies.
func TestBV236Browser(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node")
	}
	if exec.Command(node, "-e", "require('playwright')").Run() != nil {
		t.Skip("no playwright")
	}
	dir := t.TempDir()
	for _, lang := range []string{"en", "es"} {
		_, page, cat := bv236Page(t, lang)
		file := filepath.Join(dir, lang+".html")
		if err := os.WriteFile(file, []byte(page), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(node, "-e", bv236BrowserJS, file, cat.Text("copy.C1", nil)).CombinedOutput()
		if err != nil {
			t.Errorf("%s: %v\n%s", lang, err, out)
		}
	}
}

const bv236BrowserJS = `const { chromium } = require('playwright');
const [file, c1] = process.argv.slice(1);
(async () => {
  const b = await chromium.launch();
  const p = await b.newPage({ viewport: { width: 1280, height: 800 } });
  await p.addInitScript(() => { Object.defineProperty(navigator, 'clipboard', { value: { writeText: () => Promise.reject(new Error('denied')) } }) });
  const errs = [];
  p.on('pageerror', e => errs.push(e.message));
  await p.goto('file://' + file);
  const fail = m => { console.error(m); process.exitCode = 1 };
  const bottom = await p.evaluate(() => document.querySelector('.sheet').getBoundingClientRect().bottom);
  if (bottom > 800) fail('the sheet ends at ' + bottom + 'px, below 800');
  if (await p.isVisible('#v-pc')) fail('the profile view shows with JavaScript before it is opened');
  await p.click('a[data-view="v-pc"]');
  if (!(await p.isVisible('#v-pc')) || await p.isVisible('.sheet')) fail('the profile view does not open');
  await p.click('#v-pc .apply:not([hidden]) button[data-copy]');
  const pre = '#v-pc .apply:not([hidden]) pre.ptext';
  if (!(await p.isVisible(pre))) fail('denied: the text is not shown');
  const live = await p.textContent('#v-pc .apply:not([hidden]) .live');
  if (live !== c1) fail('denied: no C1 (' + live + ')');
  const sel = await p.evaluate(() => window.getSelection().toString().length);
  if (sel === 0) fail('denied: the text is not selected');
  await p.click('#v-pc a[data-view="v-cc"]');
  await p.click('#v-cc label.card:has(input[value="P3"])');
  if (!(await p.isVisible('#v-cc .apply[data-p="P3"]')) || await p.isVisible('#v-cc .apply[data-p="P1"]')) fail('choosing P3 does not change the bar');
  await p.click('#v-cc a[data-view="v-pc"]');
  await p.click('#v-pc a.back');
  if (!(await p.isVisible('.sheet'))) fail('«← Report» does not close the view');
  const today = await p.$('button[data-copied]');
  if (today) {
    await today.scrollIntoViewIfNeeded();
    await today.click();
    const ok = await p.evaluate(b => { const pre = b.previousElementSibling, l = b.nextElementSibling; return !pre.hidden && l.textContent === document.querySelector('[data-copyfail]').getAttribute('data-copyfail') }, today);
    if (!ok) fail('denied: today\'s copy does not show the text with C1');
  }
  if (errs.length) fail('page errors: ' + errs.join('; '));
  await b.close();
})();`
