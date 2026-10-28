// Package repos implements per-repository security drift checks.
//
// Entry point: ScanAllRepos. Each repo is scanned in isolation — a panic or
// per-repo timeout does not abort the overall scan (invariant §7, §6).
//
// History scan is bounded to HistoryMaxCommits commits AND HistoryMaxAgeMonths months;
// whichever limit is reached first wins. Both limits are hardcoded (not configurable)
// and documented here as well as in the report's scan_metadata.
package repos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
	"github.com/qtorb/rosmarops/internal/state"
)

// ---------------------------------------------------------------------------
// Constants — all hardcoded, none configurable (CLAUDE.md invariants §5, §6).
// ---------------------------------------------------------------------------

const (
	// PerRepoTimeout is the maximum wall-clock time for scanning one repository.
	// Invariant §6: per-repo timeout 60 seconds.
	PerRepoTimeout = 60 * time.Second

	// HistoryMaxCommits caps the number of commits examined for secrets.
	// Combined with HistoryMaxAgeMonths — whichever limit is hit first applies.
	HistoryMaxCommits = 1000

	// HistoryMaxAgeMonths caps how far back in time git history is examined.
	HistoryMaxAgeMonths = 24

	// MaxFileBytes is the maximum file size opened for content scanning.
	// Invariant §6: files >2 MB are not opened.
	MaxFileBytes = 2 * 1024 * 1024
)

// forcedExclusions is the hardcoded directory exclusion list (CLAUDE.md invariant §5).
// Any tracked file whose path contains one of these as a directory component is skipped.
// Not user-configurable.
var forcedExclusions = []string{
	"node_modules", ".git", ".next", "dist", "build", "coverage", "target",
	".venv", "venv", "__pycache__", ".pytest_cache", ".mypy_cache",
	".ruff_cache", ".turbo", ".parcel-cache",
}

// IsForcedExclusion reports whether a directory name is in the hardcoded
// exclusion list (CLAUDE.md invariant §5), for other recursive walks.
func IsForcedExclusion(name string) bool {
	for _, x := range forcedExclusions {
		if name == x {
			return true
		}
	}
	return false
}

// ScanConfig holds scan-wide parameters shared across all repo checks.
type ScanConfig struct {
	// Patterns is the combined list of built-in + user-compiled secret patterns.
	// Build with: append(secrets.BuiltinPatterns(), compiledExtras...)
	Patterns []secrets.Pattern

	// AllowHooks is the allow-list for git hooks (keyed by path).
	AllowHooks []config.AllowEntryPath

	// AllowHistory suppresses known-historic secrets (keyed by pattern_type + commit_sha).
	AllowHistory []config.AllowEntrySecretHistory

	// Identify, when set, makes every secret the tracked-file and history
	// checks find into a salted reference while its value is in memory
	// (decision 2026-10-24, memoria, §2.2): the findings carry them in
	// Committed. The value is discarded at once.
	Identify secrets.Identify

	// OnProgress, when set, is called with the repositories done and the
	// total: once before the first starts and after each one ends, from
	// any goroutine, never at once (decision informe HTML v3 §9.6).
	OnProgress func(done, total int)

	// OnAllowHit, when set, is called with each warning allow_list.git_hooks
	// or allow_list.secrets_in_history hid (decision 2026-10-27, blindaje,
	// B9), from any goroutine, possibly at once: the caller synchronizes.
	OnAllowHit func(config.AllowHit)

	// OnFamilies, when set, receives the repository families (each a list of
	// roots in repoRoots order) once they are known, on the calling
	// goroutine. The v2 report groups its members by family.
	OnFamilies func(families [][]string)
}

// perRepoTimeout is the per-repo deadline ScanAllRepos applies. Always
// PerRepoTimeout; tests in this package shorten it.
var perRepoTimeout = PerRepoTimeout

const (
	whyScanTimeout = "The repository could not be scanned completely within the per-repo time limit, " +
		"so some of its checks did not run. Anything those checks would have found is not in this report."

	actionScanTimeout = "Check whether the repository is unusually large or has a very long history. " +
		"Run RosmarOps again; if the repository keeps timing out, review it by hand."
)

// trackedSecretsCheck and historySecretsCheck are checks 1 and 2. Always
// checkTrackedSecrets and checkHistorySecrets; the equivalence tests set the
// pre-BV-31 versions as the oracle.
var (
	trackedSecretsCheck = checkTrackedSecrets
	historySecretsCheck = checkHistorySecrets
)

// repoWorkers is how many repositories ScanAllRepos scans at once. Always 4;
// tests in this package set 1 for the sequential oracle.
var repoWorkers = 4

// repoStatus is how one repository's scan ended.
type repoStatus int

const (
	repoNotStarted  repoStatus = iota // the global deadline expired first
	repoCompleted                     // every check ran
	repoTimedOut                      // its own 60s timeout expired
	repoInterrupted                   // the global deadline expired during it
)

// repoResult is the outcome of one repository's scan.
type repoResult struct {
	status   repoStatus
	findings []finding.Finding
	baseline state.RepoBaseline
	err      error
}

// ScanAllRepos runs ScanRepo for each entry in repoRoots, enforcing per-repo
// isolation and the 60-second per-repo timeout. Up to repoWorkers
// repositories are scanned at once, each in its own context with its own
// timeout and recover; the results are assembled in repoRoots order, so the
// findings, baselines and errors are those of a sequential scan.
//
// A panic or error in one repo is caught, converted to a ScanError, and the
// scan continues with the next repo (invariant §7). A repo that exceeds its
// timeout yields a yellow scan_timeout finding (invariant §6).
//
// When ctx (the global 300s deadline) expires, the remaining repos are not
// scanned. A repo that did not complete keeps its previous baseline, so a
// partial scan never replaces it.
//
// Contents already scanned in this call (a blob, a commit) are not scanned
// again (contentCache): worktrees and clones of one repository share them.
//
// Returns new baselines for all repos — callers must save these to state.
func ScanAllRepos(
	ctx context.Context,
	repoRoots []string,
	repoBaselines []state.RepoBaseline,
	cfg ScanConfig,
) (allFindings []finding.Finding, newBaselines []state.RepoBaseline, scanErrors []finding.ScanError) {
	// Build baseline lookup: repoPath → baseline.
	blByPath := make(map[string]*state.RepoBaseline, len(repoBaselines))
	for i := range repoBaselines {
		blByPath[repoBaselines[i].Path] = &repoBaselines[i]
	}
	cache := newContentCache()

	workers := repoWorkers
	if workers < 1 {
		workers = 1
	}
	// Repositories that share objects (a repository and its worktrees, or
	// clones of one origin) form a family. A family goes to one worker and
	// is scanned in repoRoots order, so the first repository fills the
	// content cache and the next ones reuse it instead of walking the same
	// commits at the same time.
	families := repoFamilies(ctx, repoRoots, workers)
	if cfg.OnFamilies != nil {
		roots := make([][]string, len(families))
		for i, fam := range families {
			for _, j := range fam {
				roots[i] = append(roots[i], repoRoots[j])
			}
		}
		cfg.OnFamilies(roots)
	}
	results := make([]repoResult, len(repoRoots))
	var progressMu sync.Mutex
	done := 0
	report := func(step int) {
		if cfg.OnProgress == nil {
			return
		}
		progressMu.Lock()
		defer progressMu.Unlock()
		done += step
		cfg.OnProgress(done, len(repoRoots))
	}
	report(0)
	jobs := make(chan []int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for family := range jobs {
				for _, i := range family {
					if ctx.Err() != nil {
						break // global 300s timeout expired: not started
					}
					results[i] = scanOneRepo(withCache(ctx, cache), repoRoots[i], blByPath[repoRoots[i]], cfg)
					report(1)
				}
			}
		}()
	}
	for _, family := range families {
		jobs <- family
	}
	close(jobs)
	wg.Wait()

	keepPrevious := func(root string) {
		if prev := blByPath[root]; prev != nil {
			newBaselines = append(newBaselines, *prev)
		}
	}
	scanned, timedOut, interrupted, notScanned := 0, 0, 0, 0
	for i, root := range repoRoots {
		r := results[i]
		if r.err != nil {
			scanErrors = append(scanErrors, finding.ScanError{
				Scope:   "repos:" + root,
				Message: r.err.Error(),
			})
		}
		allFindings = append(allFindings, r.findings...)

		switch r.status {
		case repoNotStarted:
			notScanned++
			keepPrevious(root)
		case repoCompleted:
			scanned++
			newBaselines = append(newBaselines, r.baseline)
		case repoInterrupted:
			// Interrupted by the global deadline: reported by the caller.
			interrupted++
			keepPrevious(root)
		case repoTimedOut:
			timedOut++
			f := finding.NewFinding(repoFindingID("scan_timeout", root, "per_repo"), finding.SeverityYellow,
				"scan_timeout", fmt.Sprintf("Repository scan timed out: %s", root))
			f.GroupDetail = "per_repo"
			f.WhyItMatters = whyScanTimeout
			f.SuggestedAction = actionScanTimeout
			f.Confidence = "high"
			f.Evidence = finding.Evidence{RepoPath: root}
			allFindings = append(allFindings, f)
			keepPrevious(root)
		}
	}
	slog.Info("repos: scan finished", "configured", len(repoRoots), "scanned", scanned,
		"timed_out", timedOut, "interrupted", interrupted, "not_scanned", notScanned)
	return allFindings, newBaselines, scanErrors
}

// familyProbeTimeout bounds the git calls that identify one repository's
// family (never more than the per-repo timeout); a repository whose probe
// fails or times out is a family of its own.
const familyProbeTimeout = 10 * time.Second

// repoFamilies groups repoRoots into families, as lists of indices in
// repoRoots order, the families ordered by their first index. Two
// repositories are one family when they share their common git directory
// (worktrees) or a root commit (clones of one origin): gitFamilyKeys. The
// probes run on up to workers repositories at once, under ctx.
func repoFamilies(ctx context.Context, repoRoots []string, workers int) [][]int {
	keys := make([][]string, len(repoRoots))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				probeCtx, cancel := context.WithTimeout(ctx, min(familyProbeTimeout, perRepoTimeout))
				k, err := gitFamilyKeys(probeCtx, repoRoots[i])
				cancel()
				if err != nil {
					slog.Debug("repos: family probe failed; scanned on its own", "repo", repoRoots[i], "err", err)
				}
				keys[i] = k
			}
		}()
	}
	for i := range repoRoots {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	// Union-find over the shared keys; the lowest index is a family's root.
	parent := make([]int, len(repoRoots))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	owner := map[string]int{}
	for i, ks := range keys {
		for _, k := range ks {
			j, seen := owner[k]
			if !seen {
				owner[k] = i
				continue
			}
			a, b := find(i), find(j)
			if a > b {
				a, b = b, a
			}
			parent[b] = a
		}
	}
	byRoot := map[int][]int{}
	var order []int
	for i := range repoRoots {
		r := find(i)
		if _, ok := byRoot[r]; !ok {
			order = append(order, r)
		}
		byRoot[r] = append(byRoot[r], i)
	}
	families := make([][]int, 0, len(order))
	largest := 0
	for _, r := range order {
		families = append(families, byRoot[r])
		largest = max(largest, len(byRoot[r]))
	}
	slog.Info("repos: families", "repos", len(repoRoots), "families", len(families), "largest", largest)
	return families
}

// scanOneRepo scans one repository in its own context, with the per-repo
// timeout, and logs its total.
func scanOneRepo(ctx context.Context, root string, baseline *state.RepoBaseline, cfg ScanConfig) repoResult {
	repoCtx, cancel := context.WithTimeoutCause(ctx, perRepoTimeout, errRepoTimeout)
	defer cancel()
	trace := &repoTrace{}
	repoStart := time.Now()
	repoFindings, newBL, err := scanRepoSafe(withTrace(repoCtx, trace), root, baseline, cfg)
	outcome := ctxOutcome(repoCtx, trace.errors)
	if err != nil && outcome == outcomeOK {
		outcome = outcomeError
	}
	// The history scan always walks the history from HEAD, bounded by
	// HistoryMaxCommits and HistoryMaxAgeMonths: there is no history
	// baseline, so history_from is always "full".
	attrs := append([]any{
		"repo", root, "duration_ms", time.Since(repoStart).Milliseconds(),
		"outcome", outcome, "history_commits_listed", trace.historyListed,
		"history_commits_scanned", trace.historyScanned, "history_from", "full",
		"commit_cache_hits", trace.commitHits, "commit_cache_misses", trace.commitMisses,
	}, trace.trackedAttrs()...)
	slog.Info("repos: repo done", attrs...)
	if err != nil {
		slog.Warn("repos: scan failed", "repo", root, "err", err)
	}

	r := repoResult{status: repoCompleted, findings: repoFindings, baseline: newBL, err: err}
	switch {
	case repoCtx.Err() == nil:
	case errors.Is(context.Cause(repoCtx), errRepoTimeout):
		slog.Warn("repos: scan timed out", "repo", root, "timeout", perRepoTimeout)
		r.status = repoTimedOut
	default:
		r.status = repoInterrupted
	}
	return r
}

// scanRepoSafe wraps ScanRepo with a recover so that a panic in any check
// never propagates past the per-repo boundary (invariant §7).
func scanRepoSafe(
	ctx context.Context,
	repoRoot string,
	baseline *state.RepoBaseline,
	cfg ScanConfig,
) (findings []finding.Finding, newBL state.RepoBaseline, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in repo scan: %v", r)
		}
	}()
	return ScanRepo(ctx, repoRoot, baseline, cfg)
}

// ScanRepo performs all checks for a single git repository.
// It is safe to call directly in tests; all checks honour ctx for cancellation.
//
// Returns findings, an updated repo baseline (for persisting to state),
// and an error if the repo root is not accessible (directory does not exist).
// Errors in individual checks are logged but do not abort the scan.
func ScanRepo(
	ctx context.Context,
	repoRoot string,
	baseline *state.RepoBaseline,
	cfg ScanConfig,
) ([]finding.Finding, state.RepoBaseline, error) {
	if _, err := os.Stat(repoRoot); err != nil {
		return nil, state.RepoBaseline{Path: repoRoot},
			fmt.Errorf("repo root not accessible: %w", err)
	}

	newBL := state.RepoBaseline{Path: repoRoot}

	var findings []finding.Finding

	if _, ok := ctx.Value(traceKey{}).(*repoTrace); !ok {
		ctx = withTrace(ctx, &repoTrace{})
	}
	if cfg.OnAllowHit != nil {
		ctx = context.WithValue(ctx, allowHitKey{}, cfg.OnAllowHit)
	}
	// Every check logs its duration and outcome (timeCheck, BV-30).

	// Checks 6 and 7 (hooks, core.hooksPath) run first: they are cheap and
	// must not be starved by the tracked-file or history scans, which can
	// use up the per-repo timeout (BV-29 D6, BV-35).

	// --- Check 6: git hooks — network calls + new/modified (RED + YELLOW) ---
	timeCheck(ctx, repoRoot, checkNameHooks, func() {
		f, hooks := checkHooks(ctx, repoRoot, baseline, cfg.AllowHooks)
		findings = append(findings, f...)
		newBL.Hooks = hooks
	})

	// --- Check 7: core.hooksPath redirection (RED) ---
	timeCheck(ctx, repoRoot, checkNameCoreHooksPath, func() {
		f, hooksPath := checkCoreHooksPath(ctx, repoRoot, baseline)
		findings = append(findings, f...)
		newBL.CoreHooksPath = hooksPath
	})

	if cfg.Identify != nil {
		ctx = withIdentify(ctx, identifyCtx{identify: cfg.Identify, hasRemote: hasRemote(ctx, repoRoot)})
	}

	// --- Check 1: secrets in currently tracked files (RED) ---
	trace := traceOf(ctx)
	timeCheck(ctx, repoRoot, checkNameTrackedSecrets, func() {
		findings = append(findings, trackedSecretsCheck(ctx, repoRoot, cfg.Patterns)...)
	}, trace.trackedAttrs)

	// --- Check 2: secrets in git commit history (RED) ---
	timeCheck(ctx, repoRoot, checkNameHistorySecrets, func() {
		findings = append(findings, historySecretsCheck(ctx, repoRoot, cfg.Patterns, cfg.AllowHistory)...)
	})

	// --- Check 3: .env-like files tracked by git (RED) ---
	timeCheck(ctx, repoRoot, checkNameEnvTracked, func() {
		findings = append(findings, checkEnvTracked(ctx, repoRoot)...)
	})

	// --- Check 4: lifecycle scripts in package.json with network calls (YELLOW) ---
	timeCheck(ctx, repoRoot, checkNameLifecycleNetwork, func() {
		findings = append(findings, checkLifecycleNetwork(repoRoot)...)
	})

	// --- Check 5: workflow files — dangerous patterns + new/modified (RED + YELLOW) ---
	timeCheck(ctx, repoRoot, checkNameWorkflows, func() {
		f, workflowFiles := checkWorkflows(ctx, repoRoot, baseline)
		findings = append(findings, f...)
		newBL.WorkflowFiles = workflowFiles
	})

	return findings, newBL, nil
}

// ---------------------------------------------------------------------------
// Shared path utilities
// ---------------------------------------------------------------------------

// isExcludedDir returns true if any directory component of the git-relative
// path matches a forced-exclusion entry (CLAUDE.md invariant §5).
// relPath uses forward slashes (as returned by git ls-files).
func isExcludedDir(relPath string) bool {
	parts := strings.Split(relPath, "/")
	// The last element is the filename — only check directory components.
	for _, part := range parts[:len(parts)-1] {
		for _, excl := range forcedExclusions {
			if part == excl {
				return true
			}
		}
	}
	return false
}

// readFileSized reads the file at absPath if it is ≤ MaxFileBytes.
// Returns nil, nil for oversized files (not an error — silently skipped).
// Returns nil, err only for unexpected I/O failures.
func readFileSized(absPath string) ([]byte, error) {
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	if info.Size() > MaxFileBytes {
		slog.Debug("repos: skipping oversized file", "path", absPath, "size", info.Size())
		return nil, nil
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	return data, nil
}

// repoRelPath converts a git-relative forward-slash path to an OS-specific
// absolute path under repoRoot.
func repoRelPath(repoRoot, relPath string) string {
	return filepath.Join(repoRoot, filepath.FromSlash(relPath))
}
