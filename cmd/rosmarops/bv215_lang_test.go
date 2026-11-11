package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/firstrun"
)

// TestBV215SystemLangLogsToRunLog: the double click points slog at the run log
// before systemLang runs, so systemLang's line (on macOS: «language: system
// display language … not applicable») never reaches the console. The console
// is stood in for by the default slog handler in place before runNoArgs; a
// probe logged by systemLang must not land there, and must be in the run log.
func TestBV215SystemLangLogsToRunLog(t *testing.T) {
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

	prevDefault := slog.Default()
	var console bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&console, nil)))
	prevLang, prevDetect, prevPick, prevOpen, prevDelay := systemLang, detectProjects, pickFolder, openReportFile, closeDelay
	t.Cleanup(func() {
		systemLang, detectProjects, pickFolder, openReportFile, closeDelay = prevLang, prevDetect, prevPick, prevOpen, prevDelay
		slog.SetDefault(prevDefault)
	})
	const probe = "bv215 probe: system display language"
	systemLang = func() string { slog.Info(probe); return "en" }
	detectProjects = func() firstrun.Result { return firstrun.Result{} }
	pickFolder = func(string) (string, error) { return "", nil }
	openReportFile = func(string) error { return nil }
	closeDelay = 0

	if code := runNoArgs(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(console.String(), probe) {
		t.Errorf("systemLang logged to the console before the run log was set:\n%s", console.String())
	}
	log, err := os.ReadFile(filepath.Join(home, ".rosmarops", "reports", "data", RunLogName))
	if err != nil {
		t.Fatalf("run log: %v", err)
	}
	if !strings.Contains(string(log), probe) {
		t.Errorf("systemLang's line is not in the run log:\n%s", log)
	}
}
