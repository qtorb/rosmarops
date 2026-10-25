package repos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// Stable check names of the run log, in run order (BV-29 D6, BV-35).
const (
	checkNameHooks            = "hooks"
	checkNameCoreHooksPath    = "core_hooks_path"
	checkNameTrackedSecrets   = "tracked_secrets"
	checkNameHistorySecrets   = "history_secrets"
	checkNameEnvTracked       = "env_tracked"
	checkNameLifecycleNetwork = "lifecycle_network"
	checkNameWorkflows        = "workflows"
)

// Check outcomes of the run log.
const (
	outcomeOK          = "ok"
	outcomeTimeout     = "timeout"
	outcomeError       = "error"
	outcomeInterrupted = "interrupted"
)

// slowGit is the duration above which a git call is logged at DEBUG.
const slowGit = 5 * time.Second

// errRepoTimeout is the cause of a per-repo context that hit
// perRepoTimeout, telling it apart from the global deadline.
var errRepoTimeout = errors.New("per-repo timeout")

// repoTrace collects, for the run log of one repository, the errors of
// its checks and the size of its history scan. It only counts; nothing in
// it changes what is scanned.
type repoTrace struct {
	errors         int // check errors other than the context ending
	historyListed  int // commits git log returned
	historyScanned int // commits whose diff was read

	// tracked_secrets (BV-33): files listed, read from disk, their bytes,
	// skipped over the size limit; content cache lookups; the two git
	// listings' durations.
	trackedFiles, filesRead, filesSkippedSize int
	bytesRead                                 int64
	blobHits, blobMisses                      int
	lsFilesStagedMs, lsFilesModifiedMs        int64

	// history_secrets: commit cache lookups.
	commitHits, commitMisses int

	// tracked_secrets time split (BV-35): disk reads and secrets.Match, in
	// total and per pattern.
	readTime, matchTime time.Duration
	patternTime         map[string]time.Duration
}

// topPatterns are the n patterns with the most match time, slowest first
// (ties by id), as "id:ms" items.
func (t *repoTrace) topPatterns(n int) string {
	ids := make([]string, 0, len(t.patternTime))
	for id := range t.patternTime {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := t.patternTime[ids[i]], t.patternTime[ids[j]]
		if a != b {
			return a > b
		}
		return ids[i] < ids[j]
	})
	if len(ids) > n {
		ids = ids[:n]
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = fmt.Sprintf("%s:%d", id, t.patternTime[id].Milliseconds())
	}
	return strings.Join(out, ",")
}

// trackedAttrs are the tracked_secrets measurements of the run log.
func (t *repoTrace) trackedAttrs() []any {
	return []any{
		"tracked_files", t.trackedFiles, "files_read", t.filesRead, "bytes_read", t.bytesRead,
		"files_skipped_size", t.filesSkippedSize, "blob_cache_hits", t.blobHits, "blob_cache_misses", t.blobMisses,
		"ls_files_staged_ms", t.lsFilesStagedMs, "ls_files_modified_ms", t.lsFilesModifiedMs,
		"read_ms", t.readTime.Milliseconds(), "match_ms", t.matchTime.Milliseconds(), "top_patterns_ms", t.topPatterns(3),
	}
}

type traceKey struct{}

func withTrace(ctx context.Context, t *repoTrace) context.Context {
	return context.WithValue(ctx, traceKey{}, t)
}

func traceOf(ctx context.Context) *repoTrace {
	if t, ok := ctx.Value(traceKey{}).(*repoTrace); ok {
		return t
	}
	return &repoTrace{}
}

// markError counts a check error, unless it comes from the context ending.
func markError(ctx context.Context) {
	if ctx.Err() == nil {
		traceOf(ctx).errors++
	}
}

// ctxOutcome is the outcome of a check that ran under ctx and ended with
// errs new errors: timeout for the per-repo deadline, interrupted for the
// global one (or any other end of the context), error, or ok.
func ctxOutcome(ctx context.Context, errs int) string {
	switch {
	case ctx.Err() != nil && errors.Is(context.Cause(ctx), errRepoTimeout):
		return outcomeTimeout
	case ctx.Err() != nil:
		return outcomeInterrupted
	case errs > 0:
		return outcomeError
	}
	return outcomeOK
}

// timeCheck runs one check (unless ctx has already ended) and logs:
//
//	repos: check repo=<path> check=<name> duration_ms=<n> outcome=<ok|timeout|error|interrupted>
//
// A check that does not start because ctx ended is logged with
// duration_ms=0 and the outcome of ctx.
func timeCheck(ctx context.Context, repoRoot, name string, run func(), extra ...func() []any) {
	t := traceOf(ctx)
	errsBefore := t.errors
	start := time.Now()
	if ctx.Err() == nil {
		run()
	}
	attrs := []any{
		"repo", repoRoot, "check", name,
		"duration_ms", time.Since(start).Milliseconds(), "outcome", ctxOutcome(ctx, t.errors-errsBefore),
	}
	for _, f := range extra {
		attrs = append(attrs, f()...)
	}
	slog.Info("repos: check", attrs...)
}

// gitLogArgs are the git arguments safe to log: the subcommand and its
// flags. Positional arguments (commit hashes, config keys), the values of
// flags and the -c configuration pairs are dropped.
func gitLogArgs(args []string) string {
	var out []string
	sub := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case !sub && a == "-c":
			i++ // skip the configuration pair
		case !sub && !strings.HasPrefix(a, "-"):
			out, sub = append(out, a), true
		case sub && strings.HasPrefix(a, "-"):
			name, _, _ := strings.Cut(a, "=")
			out = append(out, name)
		}
	}
	return strings.Join(out, " ")
}
