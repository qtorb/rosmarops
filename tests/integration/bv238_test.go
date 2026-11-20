package integration_test

import (
	"encoding/json"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/profiles"
)

// TestBV238GuidedCopies: Cursor P0/P1 copy T2 and the literal sandbox.json;
// Grok Build P0/P1 copy T3 and a single ~/.grok/config.toml (no
// requirements.toml, Albert 15-11), with deny = ["WebSearch"] after rules and
// P1 on the "workspace" profile; Grok Build P3 copies the literal command,
// its task placeholder in the page's language (<task> in English).
func TestBV238GuidedCopies(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		_, page, cat := bv236Page(t, lang)
		for pid, want := range map[string]string{
			"gv-cursor-P0":     cat.Text("gp.cursor.T2", nil) + "\n\n" + `{ "readBoundary": "workspace" }`,
			"gv-cursor-P1":     cat.Text("gp.cursor.T2", nil) + "\n\n" + `{ "readBoundary": "workspace" }`,
			"gv-grok-build-P0": cat.Text("gp.grok.T3", nil) + "\n\n" + profiles.GuidedBlock(profiles.GuidedGrok, profiles.P0),
			"gv-grok-build-P1": cat.Text("gp.grok.T3", nil) + "\n\n" + profiles.GuidedBlock(profiles.GuidedGrok, profiles.P1),
			"gv-grok-build-P3": `grok -p "` + map[string]string{"en": "<task>", "es": "<tarea>"}[lang] + `" --permission-mode dontAsk --sandbox strict --deny "WebFetch"`,
		} {
			if !strings.Contains(page, `<pre class="ptext" id="`+pid+`" tabindex="-1">`+esc(want)+`</pre>`) {
				t.Errorf("%s: %s copies something else", lang, pid)
			}
		}
		// The task placeholder is text for the person: the English page has
		// none in Spanish.
		if lang == "en" && strings.Contains(page, "&lt;tarea&gt;") {
			t.Error("en: the page has <tarea>")
		}
		// Grok Bot has only steps on screen: no text to copy.
		if strings.Contains(page, `id="gv-grok-bot-`) {
			t.Errorf("%s: Grok Bot has a text to copy", lang)
		}
	}
	for _, id := range []string{profiles.P0, profiles.P1} {
		b := profiles.GuidedBlock(profiles.GuidedGrok, id)
		if strings.Contains(b, "requirements.toml") || strings.Contains(b, "disable_bypass") || strings.HasPrefix(b, "#") {
			t.Errorf("Grok Build %s: still more than config.toml:\n%s", id, b)
		}
		r, d := strings.Index(b, "rules = ["), strings.Index(b, `deny = ["WebSearch"]`)
		if r < 0 || d < r || strings.Index(b, "[sandbox]") < d {
			t.Errorf("Grok Build %s: deny = [\"WebSearch\"] is not in [permission] after rules", id)
		}
	}
	if !strings.Contains(profiles.GuidedBlock(profiles.GuidedGrok, profiles.P1), `profile = "workspace"`) || !strings.Contains(profiles.GuidedBlock(profiles.GuidedGrok, profiles.P0), `profile = "read-only"`) {
		t.Error("Grok Build's sandbox profiles are not read-only (P0) and workspace (P1)")
	}
}

// TestBV238NoClassOrScore: no guided profile shows class or score before and
// after (BV-238): their rows and views carry no arrow and no «Score a → b»;
// the guided rows show G1 and the cards G3; N1 and N2 are gone; Grok Bot's
// row is always there, last.
func TestBV238NoClassOrScore(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		_, page, cat := bv236Page(t, lang)
		pc := between(t, page, `<section class="view pcv"`, `</section>`)
		for _, key := range []string{"cursor", "grok-build", "grok-bot"} {
			row := between(t, pc, `data-ga="`+key+`"><span class="ag">`, `</a></span></div>`)
			if strings.Contains(row, `class="arrow"`) || strings.Contains(row, "→") {
				t.Errorf("%s: %s's row shows a before → after", lang, key)
			}
			if !strings.Contains(row, esc(cat.Text("gp.G1", nil))) || !strings.Contains(row, esc(cat.Text("gp.see_steps", nil))) {
				t.Errorf("%s: %s's row lacks G1 or «See the steps»", lang, key)
			}
			view := between(t, page, `<section class="view gv" id="v-`+key+`"`, `</section>`)
			if strings.Contains(view, `class="arrow"`) || strings.Contains(view, "→") {
				t.Errorf("%s: %s's view shows a before → after", lang, key)
			}
			// BV-239: G4 carries the day each agent's documentation was read,
			// and Grok Bot's cards have their own G3.
			date := map[string]map[string]string{
				"cursor": {"es": "15-11-2026", "en": "2026-11-15"}, "grok-build": {"es": "15-11-2026", "en": "2026-11-15"},
				"grok-bot": {"es": "14-11-2026", "en": "2026-11-14"},
			}[key][lang]
			g3 := "gp.G3"
			if key == "grok-bot" {
				g3 = "gp.bot.G3"
			}
			agent := map[string]string{"cursor": "Cursor", "grok-build": "Grok Build", "grok-bot": "Grok Bot"}[key]
			if !strings.Contains(view, esc(cat.Text(g3, nil))) || !strings.Contains(view, esc(cat.Text("gp.G4", map[string]string{"agent": agent, "date": date}))) {
				t.Errorf("%s: %s's view lacks its G3 or G4 with %s", lang, key, date)
			}
			if key == "grok-bot" && strings.Contains(view, esc(cat.Text("gp.G3", nil))) {
				t.Errorf("%s: Grok Bot still shows the profiles' G3", lang)
			}
		}
		if strings.Index(pc, `<div class="plist gbotlist">`) < strings.Index(pc, `<div class="prow oth">`) {
			t.Errorf("%s: Grok Bot is not the last row", lang)
		}
		if !strings.Contains(pc, esc(cat.Text("gp.bot.q", nil))) || !strings.Contains(pc, esc(cat.Text("gp.G2", nil))) {
			t.Errorf("%s: no «Do you use Grok Bot?» or no G2", lang)
		}
		for _, gone := range []string{"Its web search can't be turned off", "Su búsqueda web no se puede cerrar", "The main settings are changed on its screen", "Lo principal se cambia en su pantalla"} {
			if strings.Contains(page, esc(gone)) {
				t.Errorf("%s: N1/N2 still there (%q)", lang, gone)
			}
		}
	}
}

// TestBV238Browser checks in a real browser, when Playwright is installed
// (skipped otherwise): «Computer profile» fits 1280x800 with the nine agents
// and Grok Bot for every use; the three guided views open and fit; choosing
// a use without a profile hides its steps; Grok Build's copy, with the
// clipboard denied, shows the text with C1.
func TestBV238Browser(t *testing.T) {
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
		out, err := exec.Command(node, "-e", bv238BrowserJS, file, cat.Text("copy.C1", nil)).CombinedOutput()
		if err != nil {
			t.Errorf("%s: %v\n%s", lang, err, out)
		}
		t.Logf("%s: %s", lang, out)
	}
}

const bv238BrowserJS = `const { chromium } = require('playwright');
const [file, c1] = process.argv.slice(1);
(async () => {
  const b = await chromium.launch();
  const p = await b.newPage({ viewport: { width: 1280, height: 800 } });
  await p.addInitScript(() => { Object.defineProperty(navigator, 'clipboard', { value: { writeText: () => Promise.reject(new Error('denied')) } }) });
  const errs = [];
  p.on('pageerror', e => errs.push(e.message));
  await p.goto('file://' + file);
  const fail = m => { console.error(m); process.exitCode = 1 };
  const height = () => p.evaluate(() => document.documentElement.scrollHeight);
  await p.click('a[data-view="v-pc"]');
  for (const u of ['code', 'docs', 'mail', 'tasks']) {
    await p.click('input[name=use][value=' + u + ']', { force: true });
    const h = await height();
    if (h > 800) fail('«Computer profile» with ' + u + ' is ' + h + 'px tall');
  }
  await p.click('input[name=use][value=docs]', { force: true });
  if (await p.isVisible('#v-pc a[data-view="v-cursor"]:not([hidden])')) fail('Cursor has «See the steps» for documents');
  await p.click('input[name=use][value=code]', { force: true });
  for (const a of ['cursor', 'grok-build', 'grok-bot']) {
    await p.click('#v-pc a[data-view="v-' + a + '"]:not([hidden])');
    if (!(await p.isVisible('#v-' + a))) fail(a + ': the view does not open');
    const h = await height();
    const n = await p.evaluate(s => document.querySelectorAll(s + ' label.card').length, '#v-' + a);
    console.log(a + ' view: ' + n + ' cards, ' + h + 'px');
    // BV-249 §3.3 (Albert 17-11): an agent's templates view may scroll; its
    // height is logged. Only «Computer templates» keeps the 800 px limit.
    if (a === 'grok-build') {
      await p.click('#v-grok-build .apply:not([hidden]) button[data-copy]');
      const live = await p.textContent('#v-grok-build .apply:not([hidden]) .live');
      if (live !== c1 || !(await p.isVisible('#v-grok-build .apply:not([hidden]) pre.ptext'))) fail('Grok Build: denied copy without C1');
      await p.click('#v-grok-build label.card:has(input[value="P3"])');
      if (!(await p.isVisible('#v-grok-build .apply[data-gs="P3"]'))) fail('Grok Build: choosing P3 does not change the bar');
    }
    await p.click('#v-' + a + ' a.back');
  }
  if (errs.length) fail('page errors: ' + errs.join('; '));
  await b.close();
})();`

// safeWord is a word that would promise safety (BV-239, BV-240): seguro,
// segura, seguros, seguras, safe, safely or safety, as a whole word, in any
// case.
var safeWord = regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}])(seguro|segura|seguros|seguras|safe|safely|safety)([^\p{L}\p{N}]|$)`)

// protectWord is a word that starts with proteg or protect (BV-249 §2): the
// report never says something protects.
var protectWord = regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}])(proteg|protect)\p{L}*`)

// protectWordExceptions are the catalog keys allowed a proteg*/protect* word
// (BV-249 §2): html.cred.supabase.service_role.impact describes the database
// (its row-level protection), and gp.cursor.P1.step.3 names Cursor's own
// screen («External-File Protection», «File-Deletion Protection»).
//
// decl.c4_external_file_protection.q and html.assume.cursor (BV-248, signed
// 16-11) name the same Cursor setting, «External-File Protection», for the
// same reason as gp.cursor.P1.step.3.
var protectWordExceptions = map[string]bool{
	"html.cred.supabase.service_role.impact": true,
	"gp.cursor.P1.step.3":                    true,
	"decl.c4_external_file_protection.q":     true,
	"html.assume.cursor":                     true,
}

// safeWordExceptions are the catalog keys allowed to use one of those words
// (BV-240): they are not promises of the product. kit.recovery_code tells
// the person to keep their recovery code somewhere safe; findings.when.default
// asks whether the person is sure («Si estás seguro…») before dismissing.
var safeWordExceptions = map[string]bool{
	"kit.recovery_code":     true,
	"findings.when.default": true,
}

// visibleText is a page's text as the person reads it: no script, no style,
// no tags, entities decoded.
func visibleText(page string) string {
	page = regexp.MustCompile(`(?s)<script.*?</script>|<style.*?</style>`).ReplaceAllString(page, " ")
	return html.UnescapeString(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(page, " "))
}

// TestBV239NoSafeWord: no page of the report, in Spanish or English, and no
// text of any catalog (every language; the values, not the keys) says
// seguro, segura, seguros, seguras, safe, safely or safety as a word, but
// for the two exceptions above. The pages are the full test computer (every
// view) and the one-agent golden pages.
func TestBV239NoSafeWord(t *testing.T) {
	pages := map[string]string{}
	for _, lang := range []string{"en", "es"} {
		_, page, _ := bv236Page(t, lang)
		pages["full test computer, "+lang] = page
		raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "report", "testdata", "html_one_agent_bv56."+lang+".html"))
		if err != nil {
			t.Fatal(err)
		}
		pages["one agent, "+lang] = string(raw)
	}
	// The exceptions' own texts, which a page may show (BV-249 §2).
	var excepted []string
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		for k := range protectWordExceptions {
			excepted = append(excepted, cat.Text(k, nil))
		}
	}
	for name, page := range pages {
		text := visibleText(page)
		for _, m := range safeWord.FindAllStringSubmatchIndex(text, -1) {
			t.Errorf("%s: %q", name, text[max(0, m[0]-60):min(len(text), m[1]+30)])
		}
		rest := text
		for _, e := range excepted {
			rest = strings.ReplaceAll(rest, e, " ")
		}
		for _, m := range protectWord.FindAllStringIndex(rest, -1) {
			t.Errorf("%s: %q", name, rest[max(0, m[0]-60):min(len(rest), m[1]+30)])
		}
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "internal", "catalog", "messages", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no catalogs: %v", err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var msgs map[string]string
		if err := json.Unmarshal(raw, &msgs); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for k, v := range msgs {
			if !safeWordExceptions[k] && safeWord.MatchString(v) {
				t.Errorf("%s: %s: %q", filepath.Base(f), k, v)
			}
			if !protectWordExceptions[k] && protectWord.MatchString(v) {
				t.Errorf("%s: %s: %q (proteg*/protect*)", filepath.Base(f), k, v)
			}
		}
	}
}
