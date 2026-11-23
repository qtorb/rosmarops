package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBV282NoHorizontalScroll checks in a real browser, when Playwright is
// installed (skipped otherwise), that the report has no horizontal scroll at
// 360 and 390 px, with and without the sample report's band: at 700 px or
// less the header breaks into two lines, the logo above and the language
// link and «Your space» below, 8 px apart. Above 700 px the header stays one
// line, 76 px tall.
func TestBV282NoHorizontalScroll(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node")
	}
	if exec.Command(node, "-e", "require('playwright')").Run() != nil {
		t.Skip("no playwright")
	}
	dir := t.TempDir()
	band := `<div role="note" style="background:#1B1A18;color:#FFFFFF;font:600 14px/1.4 system-ui,sans-serif;padding:10px 16px;text-align:center">Sample report, made with example data: the computer, the user, the projects and the keys are invented.</div>`
	for _, lang := range []string{"en", "es"} {
		_, page, _ := bv236Page(t, lang)
		for name, body := range map[string]string{"report": page, "sample": strings.Replace(page, "<body>", "<body>"+band, 1)} {
			file := filepath.Join(dir, name+"."+lang+".html")
			if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(node, "-e", bv282BrowserJS, file).CombinedOutput()
			if err != nil {
				t.Errorf("%s %s: %v\n%s", name, lang, err, out)
			}
			t.Logf("%s %s: %s", name, lang, out)
		}
	}
}

const bv282BrowserJS = `const { chromium } = require('playwright');
const [file] = process.argv.slice(1);
(async () => {
  const b = await chromium.launch();
  const fail = m => { console.error(m); process.exitCode = 1 };
  for (const w of [360, 390, 1280]) {
    const p = await b.newPage({ viewport: { width: w, height: 800 } });
    await p.goto('file://' + file);
    const m = await p.evaluate(() => {
      const bar = document.querySelector('header.bar'), logo = bar.querySelector('.logo'), lang = bar.querySelector('a.lang'), btn = bar.querySelector('a.spacego');
      const r = e => e.getBoundingClientRect();
      return { scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth,
        barH: r(bar).height, logoH: r(logo).height, logoBottom: r(logo).bottom, btnTop: r(btn).top, btnRight: r(btn).right, langLeft: r(lang).left, btnLeft: r(btn).left };
    });
    console.log(w + ': ' + JSON.stringify(m));
    if (m.scroll > m.client) fail(w + ' px: horizontal scroll (' + m.scroll + ' > ' + m.client + ')');
    if (m.btnRight > m.client) fail(w + ' px: «Your space» leaves the screen');
    if (m.logoH !== 46) fail(w + ' px: the logo is ' + m.logoH + ' px tall');
    if (w <= 700) {
      if (Math.round(m.btnTop - m.logoBottom) !== 8) fail(w + ' px: ' + (m.btnTop - m.logoBottom) + ' px between the two lines, want 8');
      if (m.langLeft !== 16 || m.btnLeft <= m.langLeft) fail(w + ' px: the second line is not the link then the button, from the left');
    } else if (m.barH !== 76) fail(w + ' px: the header is ' + m.barH + ' px tall, want 76');
    await p.close();
  }
  await b.close();
})();`
