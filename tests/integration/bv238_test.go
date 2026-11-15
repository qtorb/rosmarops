package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/profiles"
)

// TestBV238GuidedCopies: Cursor P0/P1 copy T2 and the literal sandbox.json;
// Grok Build P0/P1 copy T3 and a single ~/.grok/config.toml (no
// requirements.toml, Albert 15-11), with deny = ["WebSearch"] after rules and
// P1 on the "workspace" profile; Grok Build P3 copies the literal command.
func TestBV238GuidedCopies(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		_, page, cat := bv236Page(t, lang)
		for pid, want := range map[string]string{
			"gv-cursor-P0":     cat.Text("gp.cursor.T2", nil) + "\n\n" + `{ "readBoundary": "workspace" }`,
			"gv-cursor-P1":     cat.Text("gp.cursor.T2", nil) + "\n\n" + `{ "readBoundary": "workspace" }`,
			"gv-grok-build-P0": cat.Text("gp.grok.T3", nil) + "\n\n" + profiles.GuidedBlock(profiles.GuidedGrok, profiles.P0),
			"gv-grok-build-P1": cat.Text("gp.grok.T3", nil) + "\n\n" + profiles.GuidedBlock(profiles.GuidedGrok, profiles.P1),
			"gv-grok-build-P3": `grok -p "<tarea>" --permission-mode dontAsk --sandbox strict --deny "WebFetch"`,
		} {
			if !strings.Contains(page, `<pre class="ptext" id="`+pid+`" tabindex="-1">`+esc(want)+`</pre>`) {
				t.Errorf("%s: %s copies something else", lang, pid)
			}
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
			if !strings.Contains(view, esc(cat.Text("gp.G3", nil))) || !strings.Contains(view, esc(cat.Text("gp.G4", map[string]string{"agent": map[string]string{"cursor": "Cursor", "grok-build": "Grok Build", "grok-bot": "Grok Bot"}[key]}))) {
				t.Errorf("%s: %s's view lacks G3 or G4", lang, key)
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
    if (h > 800) fail(a + ': the view is ' + h + 'px tall');
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
