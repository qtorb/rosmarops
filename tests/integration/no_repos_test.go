package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// BV-52: the launcher's first run with no projects folder (empty answer, or
// a folder that does not exist) runs init without --repos-under: init and
// run both work with no repository, and the report is written.
func TestInitAndRunWithoutRepos(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("black-box run is implemented for Unix-like hosts")
	}
	bin := filepath.Join(t.TempDir(), "rosmarops")
	build := buildCmd(t, bin, "")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	home := t.TempDir()
	for _, d := range []string{"appdata", "localappdata", "xdg", "tmp"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if code, out, errOut := cliRun(t, bin, home, "init"); code != 0 {
		t.Fatalf("init: exit %d\n%s%s", code, out, errOut)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".rosmarops", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		RepoRoots []string `json:"repo_roots"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil || len(cfg.RepoRoots) != 0 {
		t.Fatalf("repo_roots %v, %v", cfg.RepoRoots, err)
	}
	code, out, errOut := cliRun(t, bin, home, "run")
	if code != 0 {
		t.Fatalf("run: exit %d\n%s%s", code, out, errOut)
	}
	for _, f := range []string{"report.html", "report.v2.json", "graph.v2.json"} {
		if _, err := os.Stat(filepath.Join(home, ".rosmarops", "reports", f)); err != nil {
			t.Errorf("no %s: %v", f, err)
		}
	}
}
