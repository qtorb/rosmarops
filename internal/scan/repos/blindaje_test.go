package repos

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// BV-93, B1. These tests prove the hardening invariant: a scan reads a
// repository without ever running a program the repository names, and resolves
// the repository's real hooks directory even while every other git call is
// forbidden from executing hooks.

// TestGitLsFilesModified_NoCleanFilterRuns: computing the modified set must not
// run `git ls-files -m`, because that command applies the repository's clean
// filter — a program the repository chose. A clean filter that drops a witness
// file proves the point: after gitLsFilesModified, the witness is absent, and
// the modified file is still detected.
func TestGitLsFilesModified_NoCleanFilterRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the clean filter is a POSIX shell script")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	r := t.TempDir()
	gitT(t, r, "init", "-q")
	writeT(t, filepath.Join(r, "data.txt"), "one\n")
	gitT(t, r, "add", "data.txt")
	gitT(t, r, "commit", "-qm", "init")

	// A clean filter that writes a witness whenever git runs it.
	witness := filepath.Join(t.TempDir(), "clean-ran")
	clean := filepath.Join(t.TempDir(), "clean.sh")
	writeT(t, clean, "#!/bin/sh\ntouch '"+witness+"'\ncat\n")
	if err := os.Chmod(clean, 0o700); err != nil {
		t.Fatal(err)
	}
	gitT(t, r, "config", "filter.witness.clean", clean)
	writeT(t, filepath.Join(r, ".gitattributes"), "data.txt filter=witness\n")

	// Modify the tracked file so it differs from the index.
	writeT(t, filepath.Join(r, "data.txt"), "one\ntwo\n")

	mod, err := gitLsFilesModified(context.Background(), r)
	if err != nil {
		t.Fatalf("gitLsFilesModified: %v", err)
	}
	if !mod["data.txt"] {
		t.Errorf("modified file not detected: %v", mod)
	}
	if _, err := os.Stat(witness); err == nil {
		t.Fatal("the repository's clean filter ran during the modified-file scan")
	}
}

// TestGitLsFilesModified_MatchesGitLsFilesM: in a repository with no clean/eol
// filters, the Go computation agrees with `git ls-files -m` exactly, over a mix
// of unchanged, content-changed, deleted and symlinked tracked files.
func TestGitLsFilesModified_MatchesGitLsFilesM(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	r := t.TempDir()
	gitT(t, r, "init", "-q")
	writeT(t, filepath.Join(r, "unchanged.txt"), "stays\n")
	writeT(t, filepath.Join(r, "changed.txt"), "before\n")
	writeT(t, filepath.Join(r, "deleted.txt"), "gone\n")
	writeT(t, filepath.Join(r, "sub", "nested.txt"), "deep\n")
	if runtime.GOOS != "windows" {
		if err := os.Symlink("unchanged.txt", filepath.Join(r, "link")); err != nil {
			t.Fatal(err)
		}
	}
	gitT(t, r, "add", "-A")
	gitT(t, r, "commit", "-qm", "init")

	// Make changes of each kind.
	writeT(t, filepath.Join(r, "changed.txt"), "after\n")
	if err := os.Remove(filepath.Join(r, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Remove(filepath.Join(r, "link")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("changed.txt", filepath.Join(r, "link")); err != nil {
			t.Fatal(err)
		}
	}

	got, err := gitLsFilesModified(context.Background(), r)
	if err != nil {
		t.Fatalf("gitLsFilesModified: %v", err)
	}
	var gotPaths []string
	for p := range got {
		gotPaths = append(gotPaths, p)
	}
	sort.Strings(gotPaths)

	wantPaths := splitLines(gitT(t, r, "ls-files", "-m", "--full-name"))
	sort.Strings(wantPaths)

	if strings.Join(gotPaths, ",") != strings.Join(wantPaths, ",") {
		t.Errorf("modified set mismatch:\n go: %v\n git: %v", gotPaths, wantPaths)
	}
}

// TestGitHooksDir_HonoursRedirect: gitHooksDir resolves the repository's real
// hooks directory. A normal repository resolves to .git/hooks; a repository
// that redirects core.hooksPath resolves to that directory — the hardening's
// own core.hooksPath override must not mask it (BV-93, B1/P6), or a redirected
// hooks folder would be invisible.
func TestGitHooksDir_HonoursRedirect(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	r := t.TempDir()
	gitT(t, r, "init", "-q")

	dir, err := gitHooksDir(context.Background(), r)
	if err != nil {
		t.Fatalf("gitHooksDir: %v", err)
	}
	if filepath.ToSlash(dir) != ".git/hooks" {
		t.Errorf("normal repo hooks dir = %q, want .git/hooks", dir)
	}

	gitT(t, r, "config", "core.hooksPath", "my-hooks")
	dir, err = gitHooksDir(context.Background(), r)
	if err != nil {
		t.Fatalf("gitHooksDir (redirected): %v", err)
	}
	if filepath.ToSlash(dir) != "my-hooks" {
		t.Errorf("redirected hooks dir = %q, want my-hooks", dir)
	}
}

// TestGitChildEnv_StripsAndControls: the git child environment carries no GIT_*
// inherited from RosmarOps' own environment, and sets the controlled values
// (no system config, empty global config) (BV-93, B1/P5).
func TestGitChildEnv_StripsAndControls(t *testing.T) {
	t.Setenv("GIT_DIR", "/evil/.git")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.pager=evil'")
	t.Setenv("GIT_EXEC_PATH", "/evil/exec")
	env := gitChildEnv()
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "GIT_DIR", "GIT_CONFIG_PARAMETERS", "GIT_EXEC_PATH":
			t.Errorf("inherited %s leaked into the git child env", k)
		}
	}
	want := map[string]bool{
		"GIT_CONFIG_NOSYSTEM=1": false, "GIT_CONFIG_GLOBAL=": false,
		"GIT_OPTIONAL_LOCKS=0": false, "GIT_TERMINAL_PROMPT=0": false,
	}
	for _, kv := range env {
		if _, ok := want[kv]; ok {
			want[kv] = true
		}
	}
	for kv, seen := range want {
		if !seen {
			t.Errorf("git child env missing %q", kv)
		}
	}
}

// TestGitHardening_SignatureAndHooksPath: the signature-verification programs a
// repository could name are emptied on every call (BV-93, B1/P2), and the
// core.hooksPath override is present on a normal call but absent from the base
// set gitHooksDir uses.
func TestGitHardening_SignatureAndHooksPath(t *testing.T) {
	full := strings.Join(gitHardening(), " ")
	for _, want := range []string{
		"log.showSignature=false", "gpg.program=", "gpg.ssh.program=", "gpg.x509.program=",
		"core.hooksPath=" + gitNoHooksPath,
	} {
		if !strings.Contains(full, want) {
			t.Errorf("gitHardening() missing %q: %s", want, full)
		}
	}
	base := strings.Join(gitHardeningBase, " ")
	if strings.Contains(base, "core.hooksPath") {
		t.Errorf("gitHardeningBase must omit core.hooksPath: %s", base)
	}
	for _, want := range []string{"log.showSignature=false", "gpg.program="} {
		if !strings.Contains(base, want) {
			t.Errorf("gitHardeningBase missing %q: %s", want, base)
		}
	}
}
