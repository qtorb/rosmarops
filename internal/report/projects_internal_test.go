package report

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/rules"
)

// v3 §15.2: {projects} are the folder names of the repositories the
// finding's locations are in, each once, sorted, joined as in v1 §3.2.
func TestParams_Projects(t *testing.T) {
	v := V2{repoRoots: []string{"/home/u/src/webapp", "/home/u/src/webapp-copy", "/home/u/src/api"}}
	f := rules.Finding{Evidence: rules.Evidence{Locations: []string{
		"/home/u/src/webapp-copy/vars_raw.txt:2", "/home/u/src/webapp/vars_raw.txt:2", "/home/u/src/webapp/scripts/smoke_test.ps1:1",
	}}}
	for lang, want := range map[string]string{"en": "webapp and webapp-copy", "es": "webapp y webapp-copy"} {
		cat, _ := catalog.Load(lang)
		c := htmlCtx{v: v, cat: cat}
		if got := c.params(f)["projects"]; got != want {
			t.Errorf("%s: projects %q, want %q", lang, got, want)
		}
	}
	// Outside every repository: the file's folder.
	c := htmlCtx{v: v, cat: catalog.Catalog{}}
	if got := c.projects(rules.Finding{Evidence: rules.Evidence{Locations: []string{"/home/u/notes/.env:1"}}}); len(got) != 1 || got[0] != "notes" {
		t.Errorf("outside: %v", got)
	}
}
