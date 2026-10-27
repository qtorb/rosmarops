package repos_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/scan/repos"
)

// TestScanRepo_DoesNotRunFsmonitor: a repository whose .git/config names an
// fsmonitor hook (which `git ls-files` would run) is scanned without the hook
// ever executing.
func TestScanRepo_DoesNotRunFsmonitor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fsmonitor witness script is a POSIX shell script")
	}
	root := initGitRepo(t)
	writeFile(t, root, "README.md", "demo\n")
	gitCommit(t, root, "init", "README.md")

	witness := filepath.Join(t.TempDir(), "fsmonitor-ran")
	script := filepath.Join(t.TempDir(), "fsmonitor.sh")
	writeFile(t, filepath.Dir(script), filepath.Base(script), "#!/bin/sh\ntouch '"+witness+"'\nexit 1\n")
	if err := os.Chmod(script, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "config", "core.fsmonitor", script)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config core.fsmonitor: %v\n%s", err, out)
	}

	if _, _, err := repos.ScanRepo(context.Background(), root, nil, defaultCfg()); err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	if _, err := os.Stat(witness); err == nil {
		t.Fatal("the repository's core.fsmonitor hook ran during the scan")
	}
}

// TestScanRepo_NoAutomaticMaintenance (BV-79): every git call of a scan
// carries -c maintenance.auto=false -c gc.auto=0, so that no git command
// RosmarOps runs starts automatic maintenance, which writes to the
// repository. A git wrapper first in PATH records each call's arguments.
func TestScanRepo_NoAutomaticMaintenance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the recording git wrapper is a POSIX shell script")
	}
	root := initGitRepo(t)
	writeFile(t, root, "README.md", "demo\n")
	gitCommit(t, root, "init", "README.md")
	writeFile(t, root, "config.py", "x = 1\n")
	gitCommit(t, root, "second", "config.py")

	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	bin := t.TempDir()
	calls := filepath.Join(t.TempDir(), "calls.txt")
	writeFile(t, bin, "git", "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '"+calls+"'\nexec '"+realGit+"' \"$@\"\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, _, err := repos.ScanRepo(context.Background(), root, nil, defaultCfg()); err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	raw, err := os.ReadFile(calls)
	if err != nil {
		t.Fatalf("no git call recorded: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	seen := map[string]bool{}
	for _, l := range lines {
		if !strings.Contains(l, "-c maintenance.auto=false -c gc.auto=0") {
			t.Errorf("git call without the maintenance guard: %s", l)
		}
		for _, sub := range []string{"ls-files", "log", "rev-list", "config"} {
			seen[sub] = seen[sub] || strings.Contains(l, " "+sub+" ")
		}
	}
	for _, sub := range []string{"ls-files", "log"} {
		if !seen[sub] {
			t.Errorf("no git %s call recorded: test would be vacuous (%d calls)", sub, len(lines))
		}
	}
	t.Logf("%d git calls, all guarded", len(lines))
}
