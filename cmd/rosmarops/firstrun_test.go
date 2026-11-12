package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/firstrun"
	"github.com/qtorb/rosmarops/internal/report"
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
	var cfg struct {
		RepoRoots []string `json:"repo_roots"`
	}
	raw, _ := os.ReadFile(filepath.Join(home, ".rosmarops", "config.json"))
	if err := json.Unmarshal(raw, &cfg); err != nil { // a Windows path is escaped in the JSON
		t.Fatal(err)
	}
	// CTO-38 §2.3: a first use that finds repositories leaves repo_roots
	// empty; every review searches again.
	if cfg.RepoRoots == nil || len(cfg.RepoRoots) != 0 {
		t.Errorf("config: %s", raw)
	}
	// The run's log is closed when runNoArgs returns: on Windows an open
	// file cannot be removed.
	if err := os.Remove(filepath.Join(report.DataDir(reports), RunLogName)); err != nil {
		t.Errorf("run log still open: %v", err)
	}
}

// U4 + CTO-38 §2.3: nothing detected, the picker returns a folder: it is a
// root of repo_roots, and the review's search walks it (node_modules out).
func TestDoubleClickPickedFolder(t *testing.T) {
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
	chosen := filepath.Join(root, "code")
	for _, p := range []string{"app", "node_modules"} {
		if err := os.MkdirAll(filepath.Join(chosen, p, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	prevLang, prevDetect, prevPick, prevOpen, prevDelay := systemLang, detectProjects, pickFolder, openReportFile, closeDelay
	t.Cleanup(func() {
		systemLang, detectProjects, pickFolder, openReportFile, closeDelay = prevLang, prevDetect, prevPick, prevOpen, prevDelay
	})
	systemLang = func() string { return "en" }
	detectProjects = func() firstrun.Result { return firstrun.Result{} }
	pickFolder = func(string) (string, error) { return chosen, nil }
	openReportFile = func(string) error { return nil }
	closeDelay = 0

	if code := runNoArgs(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var cfg struct {
		RepoRoots []string `json:"repo_roots"`
	}
	raw, _ := os.ReadFile(filepath.Join(home, ".rosmarops", "config.json"))
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.RepoRoots) != 1 || cfg.RepoRoots[0] != chosen {
		t.Errorf("repo_roots %q, want the chosen folder", cfg.RepoRoots)
	}
	var v2 struct {
		Coverage struct {
			Projects struct {
				Items []struct {
					Path, Source string
				} `json:"items"`
			} `json:"projects"`
		} `json:"coverage"`
	}
	b, _ := os.ReadFile(filepath.Join(home, ".rosmarops", "reports", report.DataSubdir, report.V2FileName))
	if err := json.Unmarshal(b, &v2); err != nil {
		t.Fatal(err)
	}
	items := v2.Coverage.Projects.Items
	if len(items) != 1 || items[0].Path != filepath.Join(chosen, "app") || items[0].Source != "root" {
		t.Errorf("projects checked %+v, want only %s from the chosen folder", items, filepath.Join(chosen, "app"))
	}
}
