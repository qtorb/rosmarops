package repos

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// captureLog sends slog output, DEBUG included, to a buffer for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// longHistoryRepo creates a repository with n commits (git fast-import).
func longHistoryRepo(t *testing.T, n int) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	if out, err := exec.Command(git, "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	var stream bytes.Buffer
	now := 1790000000 // 2026-09-21, inside HistoryMaxAgeMonths
	for c := 1; c <= n; c++ {
		msg := fmt.Sprintf("c%d\n", c)
		body := fmt.Sprintf("line %d\n", c)
		fmt.Fprintf(&stream, "commit refs/heads/master\nmark :%d\ncommitter t <t@t> %d +0000\ndata %d\n%s", c, now+c*60, len(msg), msg)
		if c > 1 {
			fmt.Fprintf(&stream, "from :%d\n", c-1)
		}
		fmt.Fprintf(&stream, "M 100644 inline f%d.txt\ndata %d\n%s\n", c%10, len(body), body)
	}
	imp := exec.Command(git, "fast-import", "--quiet")
	imp.Dir, imp.Stdin = repo, &stream
	if out, err := imp.CombinedOutput(); err != nil {
		t.Fatalf("fast-import: %v\n%s", err, out)
	}
	for _, args := range [][]string{{"symbolic-ref", "HEAD", "refs/heads/master"}, {"checkout", "-q", "-f", "master"}} {
		cmd := exec.Command(git, args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}

// BV-30: a repository with a long history logs one line per check, in run
// order, and a total with the history size.
func TestScanAllRepos_PerCheckTiming(t *testing.T) {
	repo := longHistoryRepo(t, 1200)
	buf := captureLog(t)

	ScanAllRepos(context.Background(), []string{repo}, nil, ScanConfig{})
	log := buf.String()

	re := regexp.MustCompile(`msg="repos: check" repo=(\S+) check=(\S+) duration_ms=(\d+) outcome=(\S+)`)
	var checks []string
	for _, m := range re.FindAllStringSubmatch(log, -1) {
		if m[1] != repo || m[4] != outcomeOK {
			t.Errorf("check line: %s", m[0])
		}
		checks = append(checks, m[2])
	}
	want := []string{
		checkNameHooks, checkNameCoreHooksPath, checkNameTrackedSecrets, checkNameHistorySecrets,
		checkNameEnvTracked, checkNameLifecycleNetwork, checkNameWorkflows,
	}
	if strings.Join(checks, ",") != strings.Join(want, ",") {
		t.Errorf("checks logged: %v, want %v", checks, want)
	}
	total := regexp.MustCompile(`msg="repos: repo done" repo=(\S+) duration_ms=\d+ outcome=ok history_commits_listed=(\d+) history_commits_scanned=(\d+) history_from=full`)
	m := total.FindStringSubmatch(log)
	if m == nil || m[1] != repo || m[2] != fmt.Sprint(HistoryMaxCommits) || m[3] != fmt.Sprint(HistoryMaxCommits) {
		t.Errorf("total line: %q\nlog:\n%s", m, log)
	}
}

// A check cut by the per-repo timeout logs timeout; the checks after it log
// timeout with duration 0, and the total says timeout.
func TestScanAllRepos_TimingOutcomeTimeout(t *testing.T) {
	hangingGit(t)
	shortRepoTimeout(t, 300*time.Millisecond)
	repo := t.TempDir()
	buf := captureLog(t)

	ScanAllRepos(context.Background(), []string{repo}, nil, ScanConfig{})
	log := buf.String()
	if !strings.Contains(log, "check=tracked_secrets duration_ms=") ||
		!regexp.MustCompile(`check=tracked_secrets duration_ms=\d+ outcome=timeout`).MatchString(log) ||
		!strings.Contains(log, "check=workflows duration_ms=0 outcome=timeout") ||
		!regexp.MustCompile(`msg="repos: repo done" repo=\S+ duration_ms=\d+ outcome=timeout`).MatchString(log) {
		t.Errorf("log:\n%s", log)
	}
	if n := strings.Count(log, `msg="repos: check"`); n != 7 {
		t.Errorf("check lines: %d, want 7", n)
	}
}

func TestGitLogArgs_NoPositionalValues(t *testing.T) {
	got := gitLogArgs([]string{"show", "--format=", "--diff-filter=AM", "--no-ext-diff", "0123456789abcdef0123456789abcdef01234567"})
	if got != "show --format --diff-filter --no-ext-diff" {
		t.Errorf("show: %q", got)
	}
	if got := gitLogArgs([]string{"config", "--local", "core.hooksPath"}); got != "config --local" {
		t.Errorf("config: %q", got)
	}
	if got := gitLogArgs([]string{"log", "--format=%H", "--max-count=1000", "--since=2024-10-24T00:00:00"}); got != "log --format --max-count --since" {
		t.Errorf("log: %q", got)
	}
}

// BV-33: the tracked_secrets line and the total carry the scan metrics.
func TestScanAllRepos_ScanMetrics(t *testing.T) {
	repo := longHistoryRepo(t, 5)
	buf := captureLog(t)
	ScanAllRepos(context.Background(), []string{repo}, nil, ScanConfig{Patterns: secrets.BuiltinPatterns()})
	log := buf.String()
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`check=tracked_secrets duration_ms=\d+ outcome=ok tracked_files=\d+ files_read=\d+ bytes_read=\d+ files_skipped_size=0 blob_cache_hits=0 blob_cache_misses=\d+ ls_files_staged_ms=\d+ ls_files_modified_ms=\d+ read_ms=\d+ match_ms=\d+ top_patterns_ms=([a-z0-9_]+:\d+,){2}[a-z0-9_]+:\d+`),
		regexp.MustCompile(`msg="repos: repo done" .* commit_cache_hits=0 commit_cache_misses=5 tracked_files=\d+ files_read=\d+ bytes_read=\d+`),
	} {
		if !want.MatchString(log) {
			t.Errorf("log lacks %s:\n%s", want, log)
		}
	}
}

// BV-33: a repository with no commits: history_secrets is ok with nothing
// listed, and there is no error and no WARN.
func TestScanAllRepos_EmptyRepository(t *testing.T) {
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	buf := captureLog(t)
	findings, _, errs := ScanAllRepos(context.Background(), []string{repo}, nil, ScanConfig{})
	log := buf.String()
	if len(errs) != 0 || len(findings) != 0 {
		t.Errorf("errors %+v, findings %+v", errs, findings)
	}
	if strings.Contains(log, "level=WARN") {
		t.Errorf("WARN in the log:\n%s", log)
	}
	if !strings.Contains(log, "check=history_secrets duration_ms=") || !regexp.MustCompile(`check=history_secrets duration_ms=\d+ outcome=ok`).MatchString(log) ||
		!regexp.MustCompile(`msg="repos: repo done" repo=\S+ duration_ms=\d+ outcome=ok history_commits_listed=0`).MatchString(log) {
		t.Errorf("log:\n%s", log)
	}
}

// Not a repository at all is still an error, not an empty repository.
func TestGitHasHEAD_NotARepository(t *testing.T) {
	if _, err := gitHasHEAD(context.Background(), t.TempDir()); err == nil {
		t.Error("a folder without git is not an empty repository")
	}
}

// BV-37: OnFamilies receives the families as lists of roots, in repoRoots
// order, once: two repositories with the same root commit are one family, a
// folder that is not a repository is its own.
func TestScanAllRepos_OnFamilies(t *testing.T) {
	a, b, other := longHistoryRepo(t, 1), longHistoryRepo(t, 2), t.TempDir()
	var calls int
	var got [][]string
	ScanAllRepos(context.Background(), []string{a, other, b}, nil, ScanConfig{OnFamilies: func(f [][]string) { calls++; got = f }})
	if want := [][]string{{a, b}, {other}}; calls != 1 || !reflect.DeepEqual(got, want) {
		t.Errorf("calls %d, families %v, want %v", calls, got, want)
	}
}
