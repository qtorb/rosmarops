// Package integration holds tests that need tools outside Go.
package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/index"
)

// renderScript opens the card file on its own in Chromium (Playwright),
// checks that the root is an SVG element and that the score's text has a
// size (it is drawn), and saves a screenshot.
const renderScript = `const { chromium } = require('playwright');
(async () => {
  const [,, svg, png] = process.argv;
  const b = await chromium.launch();
  const p = await b.newPage({ viewport: { width: 1200, height: 630 } });
  await p.goto('file://' + svg);
  const r = await p.evaluate(() => {
    const root = document.documentElement;
    const t = document.querySelectorAll('text')[2];
    const box = t && t.getBBox ? t.getBBox() : { width: 0, height: 0 };
    return root.namespaceURI + ' ' + Math.round(box.width) + ' ' + Math.round(box.height);
  });
  await p.screenshot({ path: png });
  console.log(r);
  await b.close();
})().catch(e => { console.error(e); process.exit(1); });
`

// BV-41: the card saved from the report draws as a standalone .svg.
func TestCardFileRenders(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	if err := exec.Command(node, "-e", "require('playwright')").Run(); err != nil {
		t.Skip("playwright not available")
	}
	v := 27
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	svg, _, ok := index.Card(index.AgentIndex{Agent: "Claude Code", Score: index.Score{Value: &v, Band: index.BandExposed}, Red: 2, Yellow: 1}, 0,
		time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC), cat)
	if !ok {
		t.Fatal("no card")
	}
	dir := t.TempDir()
	svgPath, pngPath, script := filepath.Join(dir, "rosmarops-card-2026-10-24.svg"), filepath.Join(dir, "card.png"), filepath.Join(dir, "render.js")
	if err := os.WriteFile(svgPath, []byte(svg), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(renderScript), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, script, svgPath, pngPath).CombinedOutput()
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}
	f := strings.Fields(string(out))
	if len(f) != 3 || f[0] != index.SVGNamespace || f[1] == "0" || f[2] == "0" {
		t.Errorf("the card does not draw: %s", out)
	}
	if keep := os.Getenv("ROSMAROPS_CARD_PNG"); keep != "" {
		png, _ := os.ReadFile(pngPath)
		_ = os.WriteFile(keep, png, 0o600)
	}
}
