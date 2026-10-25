package repos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gitLsFiles returns the list of files tracked by git in repoRoot.
// Paths are git-relative, using forward slashes regardless of OS.
// Returns an empty slice if the repo has no commits or no tracked files.
func gitLsFiles(ctx context.Context, repoRoot string) ([]string, error) {
	out, err := runGit(ctx, repoRoot, "ls-files", "--full-name")
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	return splitLines(out), nil
}

// gitShowDiff returns the unified diff of a single commit (parent vs commit).
// Only lines from added (A) or modified (M) files are requested via --diff-filter.
// For the initial commit (no parent), git still produces the full file as additions.
//
// The caller should extract added lines via extractAddedLines before running secret
// patterns to avoid false positives on context and removed lines.
func gitShowDiff(ctx context.Context, repoRoot, hash string) ([]byte, error) {
	out, err := runGitBytes(
		ctx,
		repoRoot,
		"show",
		"--format=",                          // suppress commit metadata — diff only
		"--diff-filter=AM",                   // added + modified files only
		"--no-ext-diff",                      // never run an external diff driver
		"--no-textconv",                      // never run a textconv filter
		"--no-color",                         // color.diff=always would hide the "+" lines
		"--src-prefix=a/", "--dst-prefix=b/", // whatever diff.noprefix says: "+++ b/<file>"
		hash,
	)
	if err != nil {
		return nil, fmt.Errorf("git show %s: %w", safeShort(hash), err)
	}
	return out, nil
}

// gitLogHashes returns up to maxCount commit hashes reachable from HEAD
// that are more recent than since. Most recent commit is first.
// The list is bounded to satisfy the invariant:
//
//	last HistoryMaxCommits commits OR last HistoryMaxAgeMonths months, whichever is smaller.
func gitLogHashes(ctx context.Context, repoRoot string, maxCount int, since time.Time) ([]string, error) {
	sinceStr := since.UTC().Format("2006-01-02T15:04:05")
	out, err := runGit(
		ctx,
		repoRoot,
		"log",
		"--format=%H",
		fmt.Sprintf("--max-count=%d", maxCount),
		fmt.Sprintf("--since=%s", sinceStr),
	)
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	return splitLines(out), nil
}

// gitConfigGet reads a single git config key from the repo-local config
// (i.e., .git/config only — not global or system config).
// Returns ("", err) if the key is not set.
func gitConfigGet(ctx context.Context, repoRoot, key string) (string, error) {
	out, err := runGit(ctx, repoRoot, "config", "--local", key)
	if err != nil {
		// exit code 1 means key not found — not a real error.
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// gitHooksDir returns the effective hooks directory for the repository.
// If core.hooksPath is set in repo-local config, that value is returned.
// Otherwise ".git/hooks" relative to repoRoot is used.
func gitHooksDir(ctx context.Context, repoRoot string) (string, error) {
	val, err := gitConfigGet(ctx, repoRoot, "core.hooksPath")
	if err != nil || val == "" {
		return ".git/hooks", nil
	}
	return val, nil
}

// extractAddedLines returns a byte slice containing only the content from
// lines that start with "+" in a unified diff, excluding the "+++" file header.
// This isolates additions so secret patterns do not fire on removed/context lines.
func extractAddedLines(diff []byte) []byte {
	lines := bytes.Split(diff, []byte("\n"))
	var result []byte
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		// Skip diff headers: "+++" and "---"
		if len(line) >= 3 && line[0] == '+' && line[1] == '+' && line[2] == '+' {
			continue
		}
		if line[0] == '+' {
			result = append(result, line[1:]...) // strip leading '+'
			result = append(result, '\n')
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// runGit runs a git command in repoRoot and returns stdout as a string.
// stderr is captured and attached to the error on failure.
func runGit(ctx context.Context, repoRoot string, args ...string) (string, error) {
	out, err := runGitBytes(ctx, repoRoot, args...)
	return string(out), err
}

// gitNoHooksPath is the hooks directory forced on every git call. It lies
// below /dev/null, which is a device file, so the directory cannot exist on
// Unix. On Windows the path resolves against the drive root and could exist;
// none of the git commands run here (ls-files, log, show, config) executes
// hooks.
const gitNoHooksPath = "/dev/null/rosmarops-no-hooks"

// gitHardening is prepended to every git invocation. The scanned repositories
// may have been modified by another agent, and their .git/config can name
// programs git would run: an fsmonitor hook (run by ls-files), hooks, an
// external diff driver. -c values override every configuration file.
//
//   - -c: https://git-scm.com/docs/git#Documentation/git.txt--cltnamegtltvaluegt
//   - core.fsmonitor, core.hooksPath: https://git-scm.com/docs/git-config#Documentation/git-config.txt-corefsmonitor
//   - diff.external: https://git-scm.com/docs/git-config#Documentation/git-config.txt-diffexternal
//   - protocol.allow: https://git-scm.com/docs/git-config#Documentation/git-config.txt-protocolallow
//   - color.ui: https://git-scm.com/docs/git-config#Documentation/git-config.txt-colorui
//     (a repository's color.ui=always would put color codes before the "+"
//     of diff lines; the diff commands also pass --no-color, which overrides
//     color.diff)
var gitHardening = []string{
	"-c", "core.fsmonitor=false",
	"-c", "core.hooksPath=" + gitNoHooksPath,
	"-c", "diff.external=",
	"-c", "protocol.allow=never",
	"-c", "color.ui=false",
}

// gitEnv is added to the inherited environment of every git process:
// no optional index refresh or lock (GIT_OPTIONAL_LOCKS) and no terminal
// prompt (GIT_TERMINAL_PROMPT). https://git-scm.com/docs/git#_environment_variables
var gitEnv = []string{
	"GIT_OPTIONAL_LOCKS=0",
	"GIT_TERMINAL_PROMPT=0",
}

// gitWaitDelay bounds how long a git process killed by its context may keep
// its output pipes open (a child it started can hold them): after it, the
// pipes are closed and the call returns.
// https://pkg.go.dev/os/exec#Cmd.WaitDelay
const gitWaitDelay = 2 * time.Second

// runGitBytes runs a git command in repoRoot and returns raw stdout bytes.
// The process is killed when ctx is done (the per-repo deadline, which
// inherits the global one).
func runGitBytes(ctx context.Context, repoRoot string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, gitHardening...), args...)...)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), gitEnv...)
	cmd.WaitDelay = gitWaitDelay
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	start := time.Now()
	out, err := cmd.Output()
	if d := time.Since(start); d > slowGit {
		slog.Debug("repos: slow git", "repo", repoRoot, "args", gitLogArgs(args), "duration_ms", d.Milliseconds())
	}
	if err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return nil, fmt.Errorf("%w; stderr: %s", err, stderrStr)
		}
		return nil, err
	}
	return out, nil
}

// splitLines splits s into non-empty trimmed lines.
func splitLines(s string) []string {
	raw := strings.Split(strings.TrimRight(s, "\n"), "\n")
	result := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

// safeShort returns a safe short form of a git hash for error messages.
func safeShort(hash string) string {
	if len(hash) >= 8 {
		return hash[:8]
	}
	return hash
}

// indexEntry is one line of `git ls-files -v -s`: the tag (H for a normal
// cached entry; lowercase for assume-unchanged, S for skip-worktree), mode,
// blob id, stage and path as git prints it.
type indexEntry struct {
	tag, mode, blob, stage, path string
}

// gitLsFilesStaged lists the index with blob ids, in the order and with the
// path spelling of gitLsFiles (`git ls-files -v -s --full-name`). The path is
// what follows the tab, trimmed as splitLines trims gitLsFiles lines.
func gitLsFilesStaged(ctx context.Context, repoRoot string) ([]indexEntry, error) {
	out, err := runGit(ctx, repoRoot, "ls-files", "-v", "-s", "--full-name")
	if err != nil {
		return nil, fmt.Errorf("git ls-files -s: %w", err)
	}
	var entries []indexEntry
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		meta, path, ok := strings.Cut(line, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 4 || strings.TrimSpace(path) == "" {
			continue
		}
		entries = append(entries, indexEntry{tag: f[0], mode: f[1], blob: f[2], stage: f[3], path: strings.TrimSpace(path)})
	}
	return entries, nil
}

// statHardening keeps a repository's configuration from hiding working-tree
// changes from `git ls-files -m`: every file is stat-checked in full.
// https://git-scm.com/docs/git-config#Documentation/git-config.txt-coreignoreStat
// and #Documentation/git-config.txt-corecheckStat.
var statHardening = []string{"-c", "core.ignoreStat=false", "-c", "core.checkStat=default", "-c", "core.trustctime=true"}

// gitLsFilesModified returns the paths, spelled as gitLsFiles spells them,
// whose working-tree file differs from the index (`git ls-files -m`:
// modified, deleted, or unmerged).
func gitLsFilesModified(ctx context.Context, repoRoot string) (map[string]bool, error) {
	out, err := runGitBytes(ctx, repoRoot, append(append([]string{}, statHardening...), "ls-files", "-m", "--full-name")...)
	if err != nil {
		return nil, fmt.Errorf("git ls-files -m: %w", err)
	}
	set := map[string]bool{}
	for _, p := range splitLines(string(out)) {
		set[p] = true
	}
	return set, nil
}

// gitStreamDiffs writes hashes to `git log --no-walk=unsorted --stdin --cc`
// with the diff options of gitShowDiff and the commit hash as a NUL-led
// marker line, and passes its stdout to read as it streams. The process is
// killed when ctx is done. One process serves every commit.
//
// The commits are selected beforehand (gitLogHashes): passing --max-count
// with --diff-filter to git log would select other commits, since the
// filter drops commits without added or modified files from the walk.
func gitStreamDiffs(ctx context.Context, repoRoot string, hashes []string, read func(io.Reader) error) error {
	args := []string{
		"log", "--no-walk=unsorted", "--stdin",
		"--cc",                               // merges as git show shows them
		"--format=%x00%H",                    // marker line: NUL + commit hash
		"--diff-filter=AM",                   // added + modified files only
		"--no-ext-diff",                      // never run an external diff driver
		"--no-textconv",                      // never run a textconv filter
		"--no-color",                         // color.diff=always would hide the "+" lines
		"--src-prefix=a/", "--dst-prefix=b/", // whatever diff.noprefix says: "+++ b/<file>"
	}
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, gitHardening...), args...)...)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), gitEnv...)
	cmd.WaitDelay = gitWaitDelay
	cmd.Stdin = strings.NewReader(strings.Join(hashes, "\n") + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("git log --stdin: %w", err)
	}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("git log --stdin: %w", err)
	}
	readErr := read(stdout)
	if readErr != nil {
		_, _ = io.Copy(io.Discard, stdout) // let git finish writing
	}
	waitErr := cmd.Wait()
	if d := time.Since(start); d > slowGit {
		slog.Debug("repos: slow git", "repo", repoRoot, "args", gitLogArgs(args), "duration_ms", d.Milliseconds())
	}
	switch {
	case readErr != nil:
		return readErr
	case waitErr != nil:
		if s := strings.TrimSpace(stderr.String()); s != "" {
			return fmt.Errorf("git log --stdin: %w; stderr: %s", waitErr, s)
		}
		return fmt.Errorf("git log --stdin: %w", waitErr)
	}
	return nil
}

// gitHasHEAD reports whether HEAD names a commit. A repository with no
// commits yet is told apart explicitly by the exit status of
// `git rev-parse --verify --quiet HEAD`: 1 when HEAD does not resolve,
// without reading stderr. Any other failure (not a repository, git
// missing) is an error.
func gitHasHEAD(ctx context.Context, repoRoot string) (bool, error) {
	_, err := runGitBytes(ctx, repoRoot, "rev-parse", "--verify", "--quiet", "HEAD")
	var exit *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case ctx.Err() == nil && errors.As(err, &exit) && exit.ExitCode() == 1:
		return false, nil
	}
	return false, err
}

// gitFamilyKeys returns what identifies a repository's family (BV-33): its
// common git directory (shared by a repository and its worktrees) and its
// root commits (shared by clones of one origin), as "dir:<path>" and
// "root:<hash>" keys. A repository with no commits has no root key.
func gitFamilyKeys(ctx context.Context, repoRoot string) ([]string, error) {
	out, err := runGit(ctx, repoRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return nil, fmt.Errorf("git rev-parse --git-common-dir: %w", err)
	}
	dir := strings.TrimSpace(out)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repoRoot, dir)
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	keys := []string{"dir:" + filepath.Clean(dir)}
	has, err := gitHasHEAD(ctx, repoRoot)
	if err != nil || !has {
		return keys, err
	}
	out, err = runGit(ctx, repoRoot, "rev-list", "--max-parents=0", "HEAD")
	if err != nil {
		return keys, fmt.Errorf("git rev-list --max-parents=0: %w", err)
	}
	for _, h := range splitLines(out) {
		keys = append(keys, "root:"+h)
	}
	return keys, nil
}
