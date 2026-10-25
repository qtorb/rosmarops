package repos

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
	"github.com/qtorb/rosmarops/internal/state"
)

// gitT runs git in dir with a fixed identity and fails the test on error.
func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "init.defaultBranch=master"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-09-01T10:00:00Z", "GIT_COMMITTER_DATE=2026-09-01T10:00:00Z")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// token returns a distinct fake GitHub classic token (ghp_ + 36 chars).
func token(i int) string {
	return fmt.Sprintf("ghp_%s%06d", strings.Repeat("Q", 30), i)
}

// trickyRepo has what a diff parser can get wrong: a root commit, a rename,
// a binary file, a non-ASCII name, CRLF lines, a missing final newline, an
// added line that starts with "++", a line longer than the read buffer, a
// merge with a conflict resolution, a deletion and a mode change.
func trickyRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	r := t.TempDir()
	gitT(t, r, "init", "-q")
	writeT(t, filepath.Join(r, "a.txt"), "a "+token(1)+"\n")
	writeT(t, filepath.Join(r, "café.txt"), "crlf "+token(2)+"\r\nend\r\n")
	writeT(t, filepath.Join(r, "bin.dat"), "x\x00y"+token(3))
	writeT(t, filepath.Join(r, "nonl.txt"), "no newline "+token(4))
	gitT(t, r, "add", "-A")
	gitT(t, r, "commit", "-qm", "root")
	gitT(t, r, "mv", "a.txt", "b.txt")
	writeT(t, filepath.Join(r, "plus.txt"), "++x "+token(5)+"\n+"+strings.Repeat("L", 100000)+token(6)+"\n")
	gitT(t, r, "add", "-A")
	gitT(t, r, "commit", "-qm", "rename and plus")
	gitT(t, r, "checkout", "-qb", "side")
	writeT(t, filepath.Join(r, "b.txt"), "side "+token(7)+"\n")
	gitT(t, r, "commit", "-qam", "side")
	gitT(t, r, "checkout", "-q", "master")
	writeT(t, filepath.Join(r, "b.txt"), "main "+token(8)+"\n")
	gitT(t, r, "commit", "-qam", "main")
	cmd := exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@t", "merge", "-q", "side")
	cmd.Dir = r
	_ = cmd.Run() // conflict expected
	writeT(t, filepath.Join(r, "b.txt"), "resolved "+token(9)+"\n")
	gitT(t, r, "add", "b.txt")
	gitT(t, r, "commit", "-qm", "merge")
	gitT(t, r, "rm", "-q", "nonl.txt")
	gitT(t, r, "commit", "-qm", "delete")
	if err := os.Chmod(filepath.Join(r, "plus.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, r, "commit", "-qam", "mode")
	return r
}

// The stream yields, per commit, exactly the added lines extractAddedLines
// gets from that commit's `git show`.
func TestReadDiffStream_EqualsGitShowPerCommit(t *testing.T) {
	r := trickyRepo(t)
	ctx := context.Background()
	hashes, err := gitLogHashes(ctx, r, HistoryMaxCommits, time.Now().AddDate(-2, 0, 0))
	if err != nil || len(hashes) < 7 {
		t.Fatalf("hashes %v, %v", hashes, err)
	}
	want := map[string][]byte{}
	for _, h := range hashes {
		diff, err := gitShowDiff(ctx, r, h)
		if err != nil {
			t.Fatal(err)
		}
		if added := extractAddedLines(diff); len(added) > 0 {
			want[h] = added
		}
	}
	got := map[string][]byte{}
	var last string
	var lastAdded []byte
	err = gitStreamDiffs(ctx, r, hashes, func(rd io.Reader) error {
		var err error
		last, lastAdded, err = readDiffStream(rd, func(h string, added []byte) error {
			if len(added) > 0 {
				got[h] = append([]byte{}, added...)
			}
			return nil
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if last != "" && len(lastAdded) > 0 {
		got[last] = lastAdded
	}
	if len(got) != len(want) {
		t.Fatalf("commits with added lines: got %d, want %d", len(got), len(want))
	}
	for h, w := range want {
		if !bytes.Equal(got[h], w) {
			t.Errorf("commit %s: added lines differ\n got %q\nwant %q", safeShort(h), trunc(got[h]), trunc(w))
		}
	}
}

func trunc(b []byte) []byte {
	if len(b) > 200 {
		return b[:200]
	}
	return b
}

// family is an origin with 1,000 commits, three worktrees and two local
// clones. Worktree 1 adds a secret to a tracked file without committing;
// worktree 2 removes one.
func family(t *testing.T) []string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	base := t.TempDir()
	origin := filepath.Join(base, "origin")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, origin, "init", "-q")
	var stream bytes.Buffer
	when := 1788256800 // 2026-09-01
	for c := 1; c <= 1000; c++ {
		body := fmt.Sprintf("line %d\n", c)
		if c%97 == 0 {
			body += "key = " + token(c) + "\r\n"
		}
		msg := fmt.Sprintf("c%d\n", c)
		fmt.Fprintf(&stream, "commit refs/heads/master\nmark :%d\ncommitter t <t@t> %d +0000\ndata %d\n%s", c, when+c*60, len(msg), msg)
		if c > 1 {
			fmt.Fprintf(&stream, "from :%d\n", c-1)
		}
		fmt.Fprintf(&stream, "M 100644 inline src/f%d.txt\ndata %d\n%s\n", c%20, len(body), body)
	}
	imp := exec.Command("git", "fast-import", "--quiet")
	imp.Dir, imp.Stdin = origin, &stream
	if out, err := imp.CombinedOutput(); err != nil {
		t.Fatalf("fast-import: %v\n%s", err, out)
	}
	gitT(t, origin, "symbolic-ref", "HEAD", "refs/heads/master")
	gitT(t, origin, "checkout", "-q", "-f", "master")
	writeT(t, filepath.Join(origin, "config", "app.env.txt"), "token = "+token(5001)+"\n")
	writeT(t, filepath.Join(origin, "config", "other.txt"), "other = "+token(5002)+"\n")
	gitT(t, origin, "add", "-A")
	gitT(t, origin, "commit", "-qm", "config")

	roots := []string{origin}
	for i := 1; i <= 3; i++ {
		wt := filepath.Join(base, fmt.Sprintf("wt%d", i))
		gitT(t, origin, "worktree", "add", "-q", "-b", fmt.Sprintf("wt%d", i), wt)
		roots = append(roots, wt)
	}
	for i := 1; i <= 2; i++ {
		cl := filepath.Join(base, fmt.Sprintf("clone%d", i))
		gitT(t, base, "clone", "-q", "--local", origin, cl)
		roots = append(roots, cl)
	}
	// Uncommitted changes: the disk, not the blob, decides.
	writeT(t, filepath.Join(base, "wt1", "config", "app.env.txt"), "token = "+token(6001)+"\n")
	writeT(t, filepath.Join(base, "wt2", "config", "other.txt"), "nothing here\n")
	return roots
}

// scanWith runs ScanAllRepos with the given checks and worker count.
func scanWith(t *testing.T, roots []string, legacy bool, workers int) ([]finding.Finding, []state.RepoBaseline, []finding.ScanError, time.Duration) {
	t.Helper()
	prevT, prevH, prevW := trackedSecretsCheck, historySecretsCheck, repoWorkers
	t.Cleanup(func() { trackedSecretsCheck, historySecretsCheck, repoWorkers = prevT, prevH, prevW })
	if legacy {
		trackedSecretsCheck, historySecretsCheck = legacyCheckTrackedSecrets, legacyCheckHistorySecrets
	}
	repoWorkers = workers
	patterns := secrets.BuiltinPatterns()
	if legacy {
		patterns = secrets.WithoutPrefilters(patterns) // every regex on every content, as before BV-33
	}
	start := time.Now()
	f, bl, errs := ScanAllRepos(context.Background(), roots, nil, ScanConfig{Patterns: patterns})
	d := time.Since(start)
	trackedSecretsCheck, historySecretsCheck, repoWorkers = prevT, prevH, prevW
	for i := range f {
		f[i].FirstSeen = "" // creation time, not content
	}
	return f, bl, errs, d
}

// BV-31: the cached, single-process, 4-worker scan equals the sequential
// scan without cache, on a family of worktrees and clones.
func TestScanAllRepos_FamilyEqualsSequentialOracle(t *testing.T) {
	roots := family(t)
	oF, oB, oE, oD := scanWith(t, roots, true, 1)
	nF, nB, nE, nD := scanWith(t, roots, false, 4)
	t.Logf("family of %d repos: oracle (sequential, per-commit git show, no cache, no prefilter) %s; new %s", len(roots), oD, nD)

	var history, tracked int
	for _, f := range oF {
		switch f.Category {
		case "secret_in_history":
			history++
		case "secret_tracked":
			tracked++
		}
	}
	if history < 6*10 || tracked < 6 {
		t.Fatalf("scenario too weak: %d history, %d tracked findings", history, tracked)
	}
	if !reflect.DeepEqual(oF, nF) {
		t.Errorf("findings differ: oracle %d, new %d", len(oF), len(nF))
		for i := 0; i < len(oF) && i < len(nF); i++ {
			if !reflect.DeepEqual(oF[i], nF[i]) {
				t.Errorf("first difference at %d:\n oracle %+v\n    new %+v", i, oF[i], nF[i])
				break
			}
		}
	}
	if !reflect.DeepEqual(oB, nB) || !reflect.DeepEqual(oE, nE) {
		t.Errorf("baselines or errors differ")
	}
}

// An uncommitted change in a worktree: the finding shows the disk.
func TestScanAllRepos_UncommittedChangeReadFromDisk(t *testing.T) {
	roots := family(t)
	f, _, _, _ := scanWith(t, roots, false, 4)
	previews := map[string][]string{}
	for _, x := range f {
		if x.Category == "secret_tracked" {
			k := filepath.Base(x.Evidence.RepoPath) + "/" + x.Evidence.File
			previews[k] = append(previews[k], x.Evidence.RedactedPreview)
		}
	}
	if got, want := previews["wt1/config/app.env.txt"], []string{secrets.Redact(token(6001))}; !reflect.DeepEqual(got, want) {
		t.Errorf("wt1 app.env.txt: %v, want the disk token %v", got, want)
	}
	if got, want := previews["wt3/config/app.env.txt"], []string{secrets.Redact(token(5001))}; !reflect.DeepEqual(got, want) {
		t.Errorf("wt3 app.env.txt: %v, want the blob token %v", got, want)
	}
	if got := previews["wt2/config/other.txt"]; len(got) != 0 {
		t.Errorf("wt2 other.txt (secret removed on disk): %v", got)
	}
}

// Invariant 6 in the history: a file whose added lines exceed 2 MB is not
// scanned. The per-commit `git show` scanned it: the one case where the new
// scan differs, by design.
func TestCheckHistorySecrets_FileOverLimitNotScanned(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	r := t.TempDir()
	gitT(t, r, "init", "-q")
	big := strings.Repeat("x", MaxFileBytes) + "\nbig " + token(1) + "\n"
	writeT(t, filepath.Join(r, "big.txt"), big)
	writeT(t, filepath.Join(r, "small.txt"), "small "+token(2)+"\n")
	gitT(t, r, "add", "-A")
	gitT(t, r, "commit", "-qm", "big")
	ctx := context.Background()
	p := secrets.BuiltinPatterns()
	previews := func(fs []finding.Finding) []string {
		var out []string
		for _, f := range fs {
			out = append(out, f.Evidence.RedactedPreview)
		}
		return out
	}
	// One finding per pattern and commit: the first match. The oracle's is
	// the token in big.txt; the new scan never reads big.txt, so its finding
	// is the token in small.txt.
	if got, want := previews(legacyCheckHistorySecrets(ctx, r, p, nil)), []string{secrets.Redact(token(1))}; !reflect.DeepEqual(got, want) {
		t.Fatalf("oracle: %v, want %v", got, want)
	}
	if got, want := previews(checkHistorySecrets(ctx, r, p, nil)), []string{secrets.Redact(token(2))}; !reflect.DeepEqual(got, want) {
		t.Errorf("new: %v, want only the small file's token %v", got, want)
	}
}

// BV-33: worktrees and clones of one origin are one family; an unrelated
// repository is another. Families keep repoRoots order.
func TestRepoFamilies(t *testing.T) {
	roots := family(t)
	other := t.TempDir()
	gitT(t, other, "init", "-q")
	gitT(t, other, "commit", "-q", "--allow-empty", "-m", "other")
	empty := t.TempDir()
	gitT(t, empty, "init", "-q")
	all := append([]string{other}, append(roots, empty)...)
	got := repoFamilies(context.Background(), all, 4)
	want := [][]int{{0}, {1, 2, 3, 4, 5, 6}, {7}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("families %v, want %v", got, want)
	}
}

// BV-33: a family is scanned in order by one worker, so the repositories
// after the first one reuse its commits.
func TestScanAllRepos_FamilyReusesCommitCache(t *testing.T) {
	roots := family(t)
	buf := captureLog(t)
	ScanAllRepos(context.Background(), roots, nil, ScanConfig{Patterns: secrets.BuiltinPatterns()})
	log := buf.String()
	first := regexp.MustCompile(`msg="repos: repo done" repo=` + regexp.QuoteMeta(roots[0]) + ` .*commit_cache_hits=0 commit_cache_misses=1000 `)
	if !first.MatchString(log) {
		t.Errorf("origin did not walk its commits:\n%s", log)
	}
	for _, r := range roots[1:] {
		if !regexp.MustCompile(`msg="repos: repo done" repo=` + regexp.QuoteMeta(r) + ` .*commit_cache_hits=1000 commit_cache_misses=0 `).MatchString(log) {
			t.Errorf("%s did not reuse the family's commits", r)
		}
	}
}
