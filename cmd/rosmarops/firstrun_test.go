package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/firstrun"
)

// U1, U5, U7: a double click on a Spanish Windows (no --lang) writes
// report.html in Spanish, opens it, and the first use's repositories come
// from the detection; the picker is not opened when something is found.
func TestDoubleClickSpanishOpensReport(t *testing.T) {
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
	proj := filepath.Join(home, "work", "app")
	if err := os.MkdirAll(filepath.Join(proj, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}

	var opened []string
	picked := false
	prevLang, prevDetect, prevPick, prevOpen, prevDelay := systemLang, detectProjects, pickFolder, openReportFile, closeDelay
	t.Cleanup(func() {
		systemLang, detectProjects, pickFolder, openReportFile, closeDelay = prevLang, prevDetect, prevPick, prevOpen, prevDelay
	})
	systemLang = func() string { return "es" }
	detectProjects = func() firstrun.Result { return firstrun.Result{Repos: []string{proj}} }
	pickFolder = func(string) (string, error) { picked = true; return "", nil }
	openReportFile = func(p string) error { opened = append(opened, p); return nil }
	closeDelay = 0

	if code := runNoArgs(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	reports := filepath.Join(home, ".rosmarops", "reports")
	if len(opened) != 1 || opened[0] != filepath.Join(reports, "report.html") || picked {
		t.Errorf("opened %v, picker %v", opened, picked)
	}
	run, _ := os.ReadFile(filepath.Join(reports, "report.html"))
	es, _ := os.ReadFile(filepath.Join(reports, "report.es.html"))
	if len(run) == 0 || string(run) != string(es) || !strings.Contains(string(run), `<html lang="es">`) {
		t.Error("report.html is not the Spanish page")
	}
	raw, _ := os.ReadFile(filepath.Join(home, ".rosmarops", "config.json"))
	if !strings.Contains(string(raw), proj) {
		t.Errorf("config: %s", raw)
	}
}
