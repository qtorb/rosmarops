package repos

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// syntheticRepo builds (once per dir) a repository of files tracked files
// totalling about totalBytes of source-like text, a few holding fake secrets.
func syntheticRepo(t testing.TB, dir string, files, totalBytes int) string {
	t.Helper()
	repo := filepath.Join(dir, fmt.Sprintf("synthetic-%d", files))
	if _, err := os.Stat(filepath.Join(repo, ".git")); err == nil {
		return repo
	}
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	line := "\tfunc handler(w http.ResponseWriter, r *http.Request) { log.Printf(\"request %s\", r.URL.Path) }\n"
	per := totalBytes / files
	body := strings.Repeat(line, per/len(line)+1)[:per]
	var stream bytes.Buffer
	fmt.Fprintf(&stream, "commit refs/heads/master\ncommitter t <t@t> 1790000000 +0000\ndata 5\ninit\n")
	for i := 0; i < files; i++ {
		content := body
		if i%1000 == 0 {
			content = body + fmt.Sprintf("token = \"ghp_%s%06d\"\n", strings.Repeat("S", 30), i)
		}
		fmt.Fprintf(&stream, "M 100644 inline src/d%03d/f%05d.go\ndata %d\n%s\n", i%200, i, len(content), content)
	}
	imp := exec.Command("git", "fast-import", "--quiet")
	imp.Dir, imp.Stdin = repo, &stream
	if out, err := imp.CombinedOutput(); err != nil {
		t.Fatalf("fast-import: %v\n%s", err, out)
	}
	for _, args := range [][]string{{"symbolic-ref", "HEAD", "refs/heads/master"}, {"checkout", "-q", "-f", "master"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}

// TestProfileTrackedSecrets profiles check 1 on a synthetic repository of
// 20,000 tracked files and ~400 MB. Opt-in: ROSMAROPS_PROFILE_DIR names a
// folder for the repository and the CPU profile (BV-33, point 3).
func TestProfileTrackedSecrets(t *testing.T) {
	dir := os.Getenv("ROSMAROPS_PROFILE_DIR")
	if dir == "" {
		t.Skip("set ROSMAROPS_PROFILE_DIR to profile")
	}
	repo := syntheticRepo(t, dir, 20000, 400*1024*1024)
	ctx := context.Background()
	patterns := secrets.BuiltinPatterns()

	phase := func(name string, f func()) time.Duration {
		start := time.Now()
		f()
		d := time.Since(start)
		t.Logf("%-28s %8.0f ms", name, float64(d.Milliseconds()))
		return d
	}
	var entries []indexEntry
	phase("git ls-files -v -s", func() { entries, _ = gitLsFilesStaged(ctx, repo) })
	phase("git ls-files -m", func() { _, _ = gitLsFilesModified(ctx, repo) })
	var contents [][]byte
	var total int
	phase("read files", func() {
		for _, e := range entries {
			b, _ := readFileSized(repoRelPath(repo, e.path))
			contents = append(contents, b)
			total += len(b)
		}
	})
	t.Logf("files %d, bytes %d", len(contents), total)
	var n int
	phase("regex (secrets.Match)", func() {
		for _, c := range contents {
			n += len(secrets.Match(c, patterns))
		}
	})
	t.Logf("matches %d", n)

	prof, err := os.Create(filepath.Join(dir, "tracked.cpu.pprof"))
	if err != nil {
		t.Fatal(err)
	}
	defer prof.Close()
	if err := pprof.StartCPUProfile(prof); err != nil {
		t.Fatal(err)
	}
	var with []finding.Finding
	phase("checkTrackedSecrets (whole)", func() { with = checkTrackedSecrets(ctx, repo, patterns) })
	pprof.StopCPUProfile()
	t.Logf("findings %d", len(with))

	// BV-33 equivalence: the same findings without the prefilter.
	var without []finding.Finding
	phase("without prefilter (oracle)", func() { without = checkTrackedSecrets(ctx, repo, secrets.WithoutPrefilters(patterns)) })
	for _, fs := range [][]finding.Finding{with, without} {
		for i := range fs {
			fs[i].FirstSeen = ""
		}
	}
	if !reflect.DeepEqual(with, without) {
		t.Errorf("findings differ: %d with prefilter, %d without", len(with), len(without))
	}
}
