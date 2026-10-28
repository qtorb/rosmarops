package repos

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/proc"
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
		"--no-show-signature", // never verify signatures (BV-93, B1/P2)
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

// gitHooksDir returns the effective hooks directory for the repository, as
// git resolves it: `git rev-parse --git-path hooks` honours core.hooksPath
// and any include.path that sets it, so a repository cannot hide its real
// hooks folder behind an include (BV-93, B1/P6). The path may be relative to
// repoRoot or absolute; the caller handles both.
func gitHooksDir(ctx context.Context, repoRoot string) (string, error) {
	out, err := runGitBytesHardened(ctx, repoRoot, gitHardeningBase, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return "", fmt.Errorf("git rev-parse --git-path hooks: %w", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("git rev-parse --git-path hooks: empty")
	}
	return dir, nil
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
//   - maintenance.auto, gc.auto: https://git-scm.com/docs/git-config#Documentation/git-config.txt-maintenanceauto
//     and https://git-scm.com/docs/git-config#Documentation/git-config.txt-gcauto
//     (no automatic maintenance, which would write to the repository; none
//     of the commands run here starts it today, this keeps it that way)
//   - log.showSignature, gpg.program, gpg.ssh.program, gpg.x509.program
//     (BV-93, B1/P2): a repository can name the GPG program git runs to
//     verify a commit's signature. With showSignature off and the programs
//     emptied, git never runs a signature verifier the repository chose.
//     https://git-scm.com/docs/git-config#Documentation/git-config.txt-logshowSignature
//     and #Documentation/git-config.txt-gpgprogram
//
// core.hooksPath is NOT in this base list: it is appended by gitHardening()
// for every call except gitHooksDir, which must resolve the repository's
// real hooks directory (honouring the repository's own core.hooksPath) and
// would be defeated by our override (BV-93, B1/P6).
var gitHardeningBase = []string{
	"-c", "core.fsmonitor=false",
	"-c", "diff.external=",
	"-c", "protocol.allow=never",
	"-c", "color.ui=false",
	"-c", "maintenance.auto=false",
	"-c", "gc.auto=0",
	"-c", "log.showSignature=false",
	"-c", "gpg.program=",
	"-c", "gpg.ssh.program=",
	"-c", "gpg.x509.program=",
}

// gitHardening returns the -c overrides prepended to a git invocation: the
// base set plus the core.hooksPath override that keeps git from executing a
// hook the repository names. gitHooksDir is the one caller that omits the
// hooksPath override (it needs the repository's real hooks path).
func gitHardening() []string {
	return append(append([]string{}, gitHardeningBase...), "-c", "core.hooksPath="+gitNoHooksPath)
}

// gitEnv is the controlled environment of every git process, replacing the
// GIT_* variables of the inherited environment (gitChildEnv strips them):
// no optional index refresh or lock (GIT_OPTIONAL_LOCKS), no terminal prompt
// (GIT_TERMINAL_PROMPT), no system config (GIT_CONFIG_NOSYSTEM) and no global
// config (GIT_CONFIG_GLOBAL empty), so a repository is read with only its own
// -c overrides, never a GIT_DIR, GIT_CONFIG_PARAMETERS or GIT_EXEC_PATH
// planted in the environment (BV-93, B1/P5).
// https://git-scm.com/docs/git#_environment_variables
var gitEnv = []string{
	"GIT_OPTIONAL_LOCKS=0",
	"GIT_TERMINAL_PROMPT=0",
	"GIT_CONFIG_NOSYSTEM=1",
	"GIT_CONFIG_GLOBAL=",
}

// gitChildEnv is the environment for a git child: the inherited environment
// with every GIT_* variable removed, then gitEnv appended. Removing the
// inherited GIT_* keeps a variable in RosmarOps' own environment from
// changing what git reads (BV-93, B1/P5).
func gitChildEnv() []string {
	base := os.Environ()
	out := make([]string, 0, len(base)+len(gitEnv))
	for _, kv := range base {
		if strings.HasPrefix(kv, "GIT_") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, gitEnv...)
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
	return runGitBytesHardened(ctx, repoRoot, gitHardening(), args...)
}

// runGitBytesHardened is runGitBytes with the -c overrides given explicitly.
// Every caller but gitHooksDir uses gitHardening() (with the core.hooksPath
// override); gitHooksDir passes gitHardeningBase so git resolves the
// repository's real hooks path.
func runGitBytesHardened(ctx context.Context, repoRoot string, hardening []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, hardening...), args...)...)
	proc.Hide(cmd) // no window of its own (J3)
	cmd.Dir = repoRoot
	cmd.Env = gitChildEnv()
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

// gitLsFilesModified returns the tracked paths, spelled as gitLsFiles spells
// them, whose working-tree file differs from the index. It is computed in Go,
// never with `git ls-files -m`, because that command applies the repository's
// clean filters, which run a program the repository names — code chosen by a
// repository RosmarOps only reads (BV-93, B1/P1). Instead, for each index
// entry the working-tree file's git blob id is computed from its raw bytes and
// compared with the index blob id; no filter, no eol conversion, no git
// subprocess touches the working tree.
//
// This matches `git ls-files -m` on a repository with no clean/eol filters:
// a file is modified when its content differs, and a deleted file is modified.
// A repository that does declare a filter may show more files as modified than
// `git ls-files -m` would (the unfiltered bytes differ from the filtered
// blob); that only makes the content cache skip those files, never unsafe.
// assume-unchanged and skip-worktree entries (tag not "H") are excluded, as
// `git ls-files -m` excludes them. Files over the content-scan limit are
// reported modified without hashing: they are skipped for secrets anyway, so
// only the cache gate matters, and reading a huge file just to hash it is not
// worth it.
func gitLsFilesModified(ctx context.Context, repoRoot string) (map[string]bool, error) {
	entries, err := gitLsFilesStaged(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, e := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if e.tag != "H" || e.stage != "0" {
			continue // assume-unchanged, skip-worktree or unmerged: as ls-files -m
		}
		if modified := worktreeDiffersFromIndex(repoRelPath(repoRoot, e.path), e); modified {
			set[e.path] = true
		}
	}
	return set, nil
}

// worktreeDiffersFromIndex reports whether the working-tree file at absPath
// differs from its index entry e, by comparing git blob ids computed from the
// raw bytes (no filter). A missing file or a type that cannot be hashed
// (a directory where a file is expected) counts as modified, as does an
// unreadable or oversized file (conservatively: the cache is then not used).
func worktreeDiffersFromIndex(absPath string, e indexEntry) bool {
	info, err := os.Lstat(absPath)
	if err != nil {
		return true // deleted or unreadable
	}
	var content []byte
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		// git stores a symlink as a blob of its target path (mode 120000).
		target, err := os.Readlink(absPath)
		if err != nil {
			return true
		}
		content = []byte(filepath.ToSlash(target))
	case info.Mode().IsRegular():
		if info.Size() > MaxFileBytes {
			return true // not hashed; skipped for secrets anyway
		}
		content, err = os.ReadFile(absPath)
		if err != nil {
			return true
		}
	default:
		return true // not a regular file where the index has one
	}
	return gitBlobHash(len(e.blob), content) != e.blob
}

// gitBlobHash is git's object id of a blob with the given content: the hash of
// "blob <len>\x00" followed by the content. The object format follows the
// index blob id's length: 64 hex is SHA-256, otherwise SHA-1 (git's default).
func gitBlobHash(blobHexLen int, content []byte) string {
	header := fmt.Sprintf("blob %d\x00", len(content))
	if blobHexLen == 64 {
		h := sha256.New()
		h.Write([]byte(header))
		h.Write(content)
		return hex.EncodeToString(h.Sum(nil))
	}
	h := sha1.New()
	h.Write([]byte(header))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
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
		"--no-show-signature",                // never verify signatures (BV-93, B1/P2)
		"--cc",                               // merges as git show shows them
		"--format=%x00%H",                    // marker line: NUL + commit hash
		"--diff-filter=AM",                   // added + modified files only
		"--no-ext-diff",                      // never run an external diff driver
		"--no-textconv",                      // never run a textconv filter
		"--no-color",                         // color.diff=always would hide the "+" lines
		"--src-prefix=a/", "--dst-prefix=b/", // whatever diff.noprefix says: "+++ b/<file>"
	}
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, gitHardening()...), args...)...)
	proc.Hide(cmd) // no window of its own (J3)
	cmd.Dir = repoRoot
	cmd.Env = gitChildEnv()
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
