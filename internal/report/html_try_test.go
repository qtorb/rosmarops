package report

import (
	"html"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/profiles"
)

// BV-249 §8: «Try it just this once» renders one command per project (a list
// to choose when there are several), the Linux note on Linux, and nothing on
// Windows.
func TestRenderTry(t *testing.T) {
	es, err := catalog.Load("es")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		os    string
		roots []string
	}{{"linux", []string{"/home/ana/web"}}, {"macos", []string{"/Users/ana/web", "/Users/ana/My app"}}, {"windows", []string{`C:\src\web`}}} {
		var sb strings.Builder
		ctx := htmlCtx{v: V2{OS: c.os, repoRoots: c.roots}, cat: es, sb: &sb}
		ctx.renderApply("cc")
		page := sb.String()
		has := strings.Contains(page, "Probar solo esta vez")
		if has != (c.os != "windows") {
			t.Errorf("%s: button %v", c.os, has)
			continue
		}
		if c.os == "windows" {
			continue
		}
		for _, r := range c.roots {
			if !strings.Contains(page, html.EscapeString(profiles.ClaudeSessionCommand(r))) {
				t.Errorf("%s: no command for %s", c.os, r)
			}
		}
		if (len(c.roots) > 1) != strings.Contains(page, `<select data-try=`) {
			t.Errorf("%s: list for %d projects", c.os, len(c.roots))
		}
		if strings.Contains(page, "bubblewrap") != (c.os == "linux") {
			t.Errorf("%s: Linux note", c.os)
		}
	}
}
