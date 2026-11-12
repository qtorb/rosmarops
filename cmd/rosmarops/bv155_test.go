package main

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/scan"
)

// BV-155: the scan context's deadline is the proportional global limit for the
// number of projects, counted from the start of the project search (CTO-38 G2).
func TestNewScanContextUsesLimit(t *testing.T) {
	for _, projects := range []int{0, 21, 48, 500} {
		ctx, cancel := newScanContext(time.Now(), projects)
		dl, ok := ctx.Deadline()
		cancel()
		if !ok {
			t.Fatalf("projects=%d: context has no deadline", projects)
		}
		want := scan.GlobalTimeoutFor(projects)
		if got := time.Until(dl); got < want-5*time.Second || got > want+5*time.Second {
			t.Errorf("projects=%d: deadline in %s, want ~%s", projects, got, want)
		}
	}
}

// BV-155: the console «checking» line depends on the number of projects and
// shows the time limit in minutes, in English and Spanish.
func TestCheckingLine(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			projects, minutes int
			wantKey           string
			wantContains      []string
		}{
			{0, 5, "console.checking.agents_only", nil},
			{1, 5, "console.checking.one", []string{"5"}},
			{48, 12, "console.checking.other", []string{"48", "12"}},
		}
		for _, c := range cases {
			got := checkingLine(cat, c.projects, c.minutes)
			// It is the right catalog entry (its fixed prefix, before any placeholder).
			prefix := cat.Text(c.wantKey, map[string]string{"minutes": "\x00", "projects": "\x00"})
			if head, _, _ := strings.Cut(prefix, "\x00"); !strings.HasPrefix(got, head) {
				t.Errorf("%s projects=%d: %q is not %s", lang, c.projects, got, c.wantKey)
			}
			for _, w := range c.wantContains {
				if !strings.Contains(got, w) {
					t.Errorf("%s projects=%d: %q lacks %q", lang, c.projects, got, w)
				}
			}
		}
		// The agents-only line carries no project count or {minutes} placeholder.
		if strings.Contains(checkingLine(cat, 0, 5), "{") {
			t.Errorf("%s: agents_only line has an unfilled placeholder", lang)
		}
	}
}
