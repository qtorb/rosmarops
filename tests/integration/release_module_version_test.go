package integration_test

// The module version Go stamps into the binary comes from the checkout's
// tags. On a manual release, release.yml creates the tag only in publish, so
// build tags the commit locally first; msix.yml, which rebuilds the tag to
// check the published bytes, builds without the tag when the release was
// built without one (v0.17.0). These tests pin both workflow steps and the Go
// behaviour they rely on.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func workflowText(t *testing.T, name string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	// Windows checkouts may have CRLF line endings.
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

func TestReleaseBuildTagsLocallyBeforeBuilding(t *testing.T) {
	wf := workflowText(t, "release.yml")
	start := strings.Index(wf, "\n  build:\n")
	end := strings.Index(wf, "\n  journeys:")
	if start < 0 || end < start {
		t.Fatal("no build job in release.yml")
	}
	job := wf[start:end]
	tag := strings.Index(job, `run: git tag "$TAG"`)
	build := strings.Index(job, "go build")
	if tag < 0 || build < 0 || tag > build {
		t.Fatalf("release.yml build must run `git tag \"$TAG\"` before its first go build (tag at %d, build at %d)", tag, build)
	}
	if !strings.Contains(job[:tag], "if: github.event_name == 'workflow_dispatch'") {
		t.Error("the local tag must be made only on a manual run: a tag push already has the tag")
	}
	if strings.Contains(job, "git push") {
		t.Error("build must never push the tag")
	}
}

func TestMsixBuildsAsTheReleaseSawIt(t *testing.T) {
	wf := workflowText(t, "msix.yml")
	drop := strings.Index(wf, `git -C src tag -d "v$VERSION"`)
	repro := strings.Index(wf, "Reproducible build")
	if drop < 0 || repro < 0 || drop > repro {
		t.Fatalf("msix.yml must decide on the tag before the reproducible build (tag -d at %d, build at %d)", drop, repro)
	}
	if !strings.Contains(wf, `if [ "$built" != "$released" ]`) {
		t.Error("the exact comparison with the released exe must stay")
	}
}

func TestJourneysPreviousBuildsAsTheReleaseSawIt(t *testing.T) {
	wf := workflowText(t, "journeys.yml")
	drop := strings.Index(wf, `git -C src tag -d "$TAG"`)
	repro := strings.Index(wf, "Rebuild and compare with the release's SHA256SUMS.txt")
	if drop < 0 || repro < 0 || drop > repro {
		t.Fatalf("journeys.yml previous must decide on the tag before it rebuilds (tag -d at %d, rebuild at %d)", drop, repro)
	}
}

// What both steps rely on: with no tag Go stamps a pseudo-version, with a
// tag on the commit it stamps the tag.
func TestGoStampsTheModuleVersionFromTags(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir, out := t.TempDir(), t.TempDir()
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/stamp\n\ngo 1.22\n")
	write("main.go", "package main\n\nfunc main() {}\n")
	run := func(name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t",
			"GIT_COMMITTER_EMAIL=t@example.com", "GIT_AUTHOR_DATE=2026-11-20T16:12:56Z", "GIT_COMMITTER_DATE=2026-11-20T16:12:56Z",
			"GOFLAGS=", "GOTOOLCHAIN=local")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return string(out)
	}
	run("git", "init", "-q")
	run("git", "add", ".")
	run("git", "commit", "-q", "-m", "x")
	mod := func() string {
		t.Helper()
		bin := filepath.Join(out, "stamp.bin") // outside the checkout, which would make it dirty
		run("go", "build", "-trimpath", "-o", bin, ".")
		for _, line := range strings.Split(run("go", "version", "-m", bin), "\n") {
			if f := strings.Fields(line); len(f) >= 3 && f[0] == "mod" {
				return f[2]
			}
		}
		return ""
	}
	if got := mod(); !strings.HasPrefix(got, "v0.0.0-20261120161256-") {
		t.Errorf("without a tag: module version %q, want a pseudo-version", got)
	}
	run("git", "tag", "v1.2.3")
	if got := mod(); got != "v1.2.3" {
		t.Errorf("with a local tag: module version %q, want v1.2.3", got)
	}
}
