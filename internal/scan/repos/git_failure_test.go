package repos_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/qtorb/rosmarops/internal/scan/repos"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// fakeGitDir writes a "git" onto PATH that always exits non-zero, so every
// git command a repository check runs fails (BV-202 S3).
func fakeGitDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		// cmd.exe resolves git.bat via PATHEXT.
		if err := os.WriteFile(filepath.Join(dir, "git.bat"), []byte("@echo off\r\nexit /b 7\r\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\necho 'fake git failure' >&2\nexit 7\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestGitFailureMarksRepo (BV-202 S3, CTO-24 §0): when a repository's git
// commands fail, the repository does not come back completed with no findings.
// Each failed analysis yields a visible yellow repo_check_failed finding naming
// the analysis, so the fail-secure reducer raises the run to at least yellow.
func TestGitFailureMarksRepo(t *testing.T) {
	gitDir := fakeGitDir(t)
	t.Setenv("PATH", gitDir) // only the failing git is reachable

	repo := t.TempDir()
	// A plausible file to scan; the fake git fails before any content matters.
	if err := os.WriteFile(filepath.Join(repo, "app.py"), []byte("print('hi')\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	findings, _, _ := repos.ScanAllRepos(context.Background(), []string{repo}, nil,
		repos.ScanConfig{Patterns: secrets.BuiltinPatterns()})

	var checkFailed []string
	for _, f := range findings {
		if f.Category == "repo_check_failed" {
			if f.Severity != "yellow" {
				t.Errorf("repo_check_failed severity = %q, want yellow", f.Severity)
			}
			if f.Evidence.RepoPath != repo {
				t.Errorf("repo_check_failed RepoPath = %q, want %q", f.Evidence.RepoPath, repo)
			}
			checkFailed = append(checkFailed, f.GroupDetail)
		}
	}
	if len(checkFailed) == 0 {
		t.Fatalf("a repo whose git commands all failed produced no repo_check_failed finding; findings=%+v", findings)
	}
	// The tracked-secrets analysis is the first to run git; it must be named.
	var hasTracked bool
	for _, a := range checkFailed {
		if a == "tracked_secrets" {
			hasTracked = true
		}
	}
	if !hasTracked {
		t.Errorf("repo_check_failed did not name the tracked_secrets analysis: %v", checkFailed)
	}
}
