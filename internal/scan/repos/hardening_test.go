package repos_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
