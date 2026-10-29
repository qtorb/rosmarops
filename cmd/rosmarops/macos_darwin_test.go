//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/firstrun"
	"github.com/qtorb/rosmarops/internal/report"
)

// BV-102 (decision 2026-10-29, macOS vista previa, points 3–5): a double
// click on macOS lists, in the report, every check the macOS adapter does
// not run yet (they return platform.ErrNotApplicable), with the macOS scope
// and no weekly review; the report is opened (a fake: no process runs).
func TestDoubleClickMacOSNotChecked(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		dir := filepath.Join(root, strings.ToLower(name))
		if name == "HOME" || name == "USERPROFILE" {
			dir = home
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(name, dir)
	}
	t.Chdir(root)

	var opened []string
	prevLang, prevDetect, prevPick, prevOpen, prevDelay := systemLang, detectProjects, pickFolder, openReportFile, closeDelay
	t.Cleanup(func() {
		systemLang, detectProjects, pickFolder, openReportFile, closeDelay = prevLang, prevDetect, prevPick, prevOpen, prevDelay
	})
	systemLang = func() string { return "en" }
	detectProjects = func() firstrun.Result { return firstrun.Result{} }
	pickFolder = func(string) (string, error) { return "", nil }
	openReportFile = func(p string) error { opened = append(opened, p); return nil }
	closeDelay = 0

	if code := runNoArgs(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(opened) != 1 {
		t.Fatalf("opened %v", opened)
	}
	raw, err := os.ReadFile(opened[0])
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	cat, _ := catalog.Load("en")
	esc := func(s string) string {
		return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;").Replace(s)
	}
	for _, code := range report.NotCheckedCodes {
		if !strings.Contains(page, "<li>"+esc(cat.Text("html.na."+code, nil))+"</li>") {
			t.Errorf("%s not listed", code)
		}
	}
	for _, want := range []string{`<li id="not-checked">`, esc(cat.Text("html.assume.scope.macos", nil)), `<div class="more two">`} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, gone := range []string{esc(cat.Text("html.weekly.title", nil)), "Register-ScheduledTask", esc(cat.Text("html.assume.scope", nil))} {
		if strings.Contains(page, gone) {
			t.Errorf("on macOS: %s", gone)
		}
	}
}
