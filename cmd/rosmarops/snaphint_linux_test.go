//go:build linux

package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
)

func TestSnapDetectorsParse(t *testing.T) {
	for in, want := range map[string]bool{
		"firefox_firefox.desktop\n": true,
		"firefox.desktop":           false,
		"google-chrome.desktop":     false,
		"":                          false,
	} {
		if got := isSnapDesktop(in); got != want {
			t.Errorf("isSnapDesktop(%q) = %v, want %v", in, got, want)
		}
	}
	for in, want := range map[string]bool{
		"/snap/bin/firefox": true,
		"/usr/bin/firefox":  false,
		"":                  false,
	} {
		if got := isSnapPath(in); got != want {
			t.Errorf("isSnapPath(%q) = %v, want %v", in, got, want)
		}
	}
}

// BV-219 B4: after console.report_opened, the snap hint with the report's
// absolute path when either detector finds a snap browser; never otherwise.
func TestReportSnapHint(t *testing.T) {
	prevXDG, prevFF := xdgDefaultBrowser, firefoxPath
	t.Cleanup(func() { xdgDefaultBrowser, firefoxPath = prevXDG, prevFF })
	none := errors.New("not found")
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		path := "/home/u/.rosmarops/reports/report.html"
		opened := cat.Text("console.report_opened", nil)
		hint := cat.Text("console.report_snap_hint", map[string]string{"path": path})
		if !strings.Contains(hint, path) || strings.Contains(hint, "{path}") {
			t.Fatalf("%s: hint without the path: %q", lang, hint)
		}
		cases := []struct {
			name     string
			xdg      string
			xdgErr   error
			ff       string
			ffErr    error
			wantHint bool
		}{
			{"xdg snap only", "firefox_firefox.desktop\n", nil, "", none, true},
			{"path snap only", "google-chrome.desktop\n", nil, "/snap/bin/firefox", nil, true},
			{"path snap, xdg fails", "", none, "/snap/bin/firefox", nil, true},
			{"no snap", "firefox.desktop\n", nil, "/usr/bin/firefox", nil, false},
			{"nothing found", "", none, "", none, false},
		}
		for _, c := range cases {
			xdgDefaultBrowser = func() (string, error) { return c.xdg, c.xdgErr }
			firefoxPath = func() (string, error) { return c.ff, c.ffErr }
			var b bytes.Buffer
			printReportOpened(&b, cat, path)
			want := opened + "\n"
			if c.wantHint {
				want += hint + "\n"
			}
			if b.String() != want {
				t.Errorf("%s/%s: got %q, want %q", lang, c.name, b.String(), want)
			}
		}
	}
}
