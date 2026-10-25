package repos

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/scan"
	"github.com/qtorb/rosmarops/internal/state"
)

// hangingGit puts first in PATH a fake git that never answers. It does not
// exec sleep, so killing it leaves a child holding stdout open: the call
// must still return (gitWaitDelay).
func hangingGit(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake git is a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nsleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func shortRepoTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := perRepoTimeout
	perRepoTimeout = d
	t.Cleanup(func() { perRepoTimeout = prev })
}

func TestPerRepoTimeoutValue(t *testing.T) {
	if PerRepoTimeout != 60*time.Second || perRepoTimeout != PerRepoTimeout {
		t.Fatalf("per-repo timeout %s/%s, invariant §6 requires 60s", PerRepoTimeout, perRepoTimeout)
	}
}

// A repo whose git hangs yields scan_timeout, keeps its previous baseline,
// and the scan continues with the next repo; the run ends within the cap.
func TestScanAllRepos_HangingGitScanTimeout(t *testing.T) {
	hangingGit(t)
	shortRepoTimeout(t, 300*time.Millisecond)

	a, b := t.TempDir(), t.TempDir()
	prev := state.RepoBaseline{
		Path:          a,
		Hooks:         []state.HookBaseline{{Name: "pre-commit", SHA256: "aa"}},
		CoreHooksPath: ".husky",
	}

	// The cap is injected: a 10s global deadline stands in for 300s.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var (
		findings  []finding.Finding
		baselines []state.RepoBaseline
	)
	begin := time.Now()
	timeoutErr := scan.Run(ctx, []scan.Step{{Name: "repos", Run: func(ctx context.Context) func() {
		f, bl, _ := ScanAllRepos(ctx, []string{a, b}, []state.RepoBaseline{prev}, ScanConfig{})
		return func() { findings, baselines = f, bl }
	}}})
	elapsed := time.Since(begin)

	if timeoutErr != nil {
		t.Fatalf("run hit the global cap: %+v", timeoutErr)
	}
	// The family probes, then two repos, each bounded by the timeout plus
	// gitWaitDelay.
	if max := 3*(300*time.Millisecond+gitWaitDelay) + time.Second; elapsed > max {
		t.Errorf("took %s, want <= %s", elapsed, max)
	}

	var timeouts []string
	for _, f := range findings {
		if f.Category == "scan_timeout" {
			if f.Severity != finding.SeverityYellow {
				t.Errorf("scan_timeout severity %s", f.Severity)
			}
			timeouts = append(timeouts, f.Evidence.RepoPath)
		}
	}
	if !reflect.DeepEqual(timeouts, []string{a, b}) {
		t.Errorf("scan_timeout repos: got %v, want [%s %s]", timeouts, a, b)
	}
	if !reflect.DeepEqual(baselines, []state.RepoBaseline{prev}) {
		t.Errorf("baselines: got %+v, want only the previous one of %s", baselines, a)
	}
}

// When the global deadline expires during the repo scan, the repo in
// progress and those after it keep their baselines and are not reported as
// scan_timeout (the caller records global_timeout).
func TestScanAllRepos_GlobalDeadline(t *testing.T) {
	hangingGit(t)

	roots := []string{t.TempDir(), t.TempDir(), t.TempDir()}
	var prev []state.RepoBaseline
	for _, r := range roots {
		prev = append(prev, state.RepoBaseline{Path: r, CoreHooksPath: ".husky"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	begin := time.Now()
	findings, baselines, errs := ScanAllRepos(ctx, roots, prev, ScanConfig{})
	if elapsed, max := time.Since(begin), 300*time.Millisecond+gitWaitDelay+time.Second; elapsed > max {
		t.Errorf("took %s, want <= %s", elapsed, max)
	}
	for _, f := range findings {
		if f.Category == "scan_timeout" {
			t.Errorf("scan_timeout under the global deadline: %+v", f)
		}
	}
	if len(errs) != 0 {
		t.Errorf("errors: %+v", errs)
	}
	if !reflect.DeepEqual(baselines, prev) {
		t.Errorf("baselines: got %+v, want %+v", baselines, prev)
	}
}

// BV-29 D6: with a git that hangs only on the history command, the repo
// times out but the hook checks, which run before the history scan, still
// report.
func TestScanAllRepos_HooksBeforeHistory(t *testing.T) {
	assertHooksReportedWhenGitHangsOn(t, "log")
}

// BV-35: the hook checks run first, so a git that hangs on ls-files (the
// tracked-file scan) still lets them report.
func TestScanAllRepos_HooksBeforeTrackedSecrets(t *testing.T) {
	assertHooksReportedWhenGitHangsOn(t, "ls-files")
}

// assertHooksReportedWhenGitHangsOn scans a repo with a network hook through
// a fake git that hangs whenever its arguments include subcommand, and wants
// both scan_timeout and git_hook_with_network.
func assertHooksReportedWhenGitHangsOn(t *testing.T, subcommand string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake git is a shell script")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command(realGit, args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ncurl https://example.invalid/x\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	script := "#!/bin/sh\nfor a in \"$@\"; do [ \"$a\" = " + subcommand + " ] && exec sleep 30; done\nexec " + realGit + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	shortRepoTimeout(t, 2*time.Second)

	findings, _, _ := ScanAllRepos(context.Background(), []string{repo}, nil, ScanConfig{})
	var timedOut, hookNetwork bool
	for _, f := range findings {
		timedOut = timedOut || f.Category == "scan_timeout"
		hookNetwork = hookNetwork || f.Category == "git_hook_with_network"
	}
	if !timedOut || !hookNetwork {
		t.Errorf("hang on %s: scan_timeout %v, git_hook_with_network %v: want both", subcommand, timedOut, hookNetwork)
	}
}

// BV-31: with more repos than workers, every repo still gets its own
// timeout, and the results keep the repos' order.
func TestScanAllRepos_PoolPerRepoTimeoutsInOrder(t *testing.T) {
	hangingGit(t)
	shortRepoTimeout(t, 300*time.Millisecond)
	var roots []string
	for i := 0; i < 6; i++ {
		roots = append(roots, t.TempDir())
	}
	begin := time.Now()
	findings, _, _ := ScanAllRepos(context.Background(), roots, nil, ScanConfig{})
	// Two rounds of 4 family probes, then two rounds of 4 scans, each
	// bounded by the timeout plus gitWaitDelay.
	if max := 4*(300*time.Millisecond+gitWaitDelay) + time.Second; time.Since(begin) > max {
		t.Errorf("took %s, want <= %s", time.Since(begin), max)
	}
	var got []string
	for _, f := range findings {
		if f.Category == "scan_timeout" {
			got = append(got, f.Evidence.RepoPath)
		}
	}
	if !reflect.DeepEqual(got, roots) {
		t.Errorf("scan_timeout repos: %v, want %v in order", got, roots)
	}
}

// BV-31: the global deadline with the pool: repos in progress are
// interrupted, the others not started; all keep their baselines, in order,
// and none is a scan_timeout.
func TestScanAllRepos_PoolGlobalDeadline(t *testing.T) {
	hangingGit(t)
	var roots []string
	var prev []state.RepoBaseline
	for i := 0; i < 6; i++ {
		r := t.TempDir()
		roots = append(roots, r)
		prev = append(prev, state.RepoBaseline{Path: r, CoreHooksPath: ".husky"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	begin := time.Now()
	findings, baselines, errs := ScanAllRepos(ctx, roots, prev, ScanConfig{})
	if max := 300*time.Millisecond + gitWaitDelay + time.Second; time.Since(begin) > max {
		t.Errorf("took %s, want <= %s", time.Since(begin), max)
	}
	for _, f := range findings {
		if f.Category == "scan_timeout" {
			t.Errorf("scan_timeout under the global deadline: %+v", f)
		}
	}
	if len(errs) != 0 || !reflect.DeepEqual(baselines, prev) {
		t.Errorf("errors %+v; baselines %+v, want %+v", errs, baselines, prev)
	}
}
