package trust_test

// Shared sandbox for the black-box trust tests (no_writes_out_of_scope_test.go
// and no_network_test.go): the real binary, run against an isolated root that
// holds every per-user location a child process might write to.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// sandbox is an isolated root plus the binary under test.
type sandbox struct {
	root   string // isolated root; every redirected location lives here
	home   string // HOME and USERPROFILE
	work   string // working directory of every rosmarops invocation
	repo   string // test git repository listed in repo_roots
	binary string
	env    []string
}

// externalTools are the programs rosmarops may run as child processes. They
// are taken from the real PATH; a missing one is logged, never a failure.
var externalTools = []string{"npm", "pnpm", "git", "code"}

// newSandbox builds the binary (with the caller's real environment, so the Go
// build cache stays where it is) and lays out the isolated root: a git repo
// and a ~/.claude.json declaring one MCP server.
func newSandbox(t *testing.T) *sandbox {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sandboxed black-box run is implemented for Unix-like hosts")
	}
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "rosmarops")
	build := exec.Command("go", "build", "-o", bin, "./cmd/rosmarops")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build rosmarops: %v\n%s", err, out)
	}

	root := t.TempDir()
	s := &sandbox{
		root:   root,
		home:   filepath.Join(root, "home"),
		work:   filepath.Join(root, "work"),
		repo:   filepath.Join(root, "src", "demo-repo"),
		binary: bin,
	}
	redirect := map[string]string{
		"HOME":            s.home,
		"USERPROFILE":     s.home,
		"APPDATA":         filepath.Join(root, "appdata"),
		"LOCALAPPDATA":    filepath.Join(root, "localappdata"),
		"XDG_CONFIG_HOME": filepath.Join(root, "xdg-config"),
		"XDG_CACHE_HOME":  filepath.Join(root, "xdg-cache"),
		"TMPDIR":          filepath.Join(root, "tmp"),
		"TMP":             filepath.Join(root, "tmp"),
		"TEMP":            filepath.Join(root, "tmp"),
	}
	for _, dir := range redirect {
		mustMkdir(t, dir)
	}
	mustMkdir(t, s.work)
	s.env = sandboxEnv(redirect)

	for _, tool := range externalTools {
		if p, err := exec.LookPath(tool); err == nil {
			t.Logf("tool present: %s (%s)", tool, p)
		} else {
			t.Logf("tool absent: %s", tool)
		}
	}

	claudeJSON := `{"mcpServers":{"demo":{"command":"node","args":["server.js"]}}}`
	mustWrite(t, filepath.Join(s.home, ".claude.json"), claudeJSON)
	// Cursor (BV-48): its folder and the files its collector reads, so the
	// run reads them and the write test covers them.
	mustMkdir(t, filepath.Join(s.home, ".cursor"))
	mustWrite(t, filepath.Join(s.home, ".cursor", "permissions.json"), "// jsonc\n{\"terminalAllowlist\": [\"cat\"]}")
	mustWrite(t, filepath.Join(s.home, ".cursor", "sandbox.json"), `{"readBoundary": "system"}`)
	mustWrite(t, filepath.Join(s.home, ".cursor", "hooks.json"), `{"version": 1, "hooks": {}}`)
	s.initRepo(t)
	return s
}

// sandboxEnv is the real environment with every redirected variable replaced.
func sandboxEnv(redirect map[string]string) []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if _, ok := redirect[name]; ok {
			continue
		}
		env = append(env, kv)
	}
	for name, val := range redirect {
		env = append(env, name+"="+val)
	}
	sort.Strings(env)
	return env
}

// initRepo creates a one-commit git repository inside the sandbox. Skips the
// repository (logged) when git is absent: the scan then has no repo to read.
func (s *sandbox) initRepo(t *testing.T) {
	t.Helper()
	mustMkdir(t, s.repo)
	if _, err := exec.LookPath("git"); err != nil {
		t.Log("git absent: test repository not created")
		return
	}
	mustWrite(t, filepath.Join(s.repo, "README.md"), "demo\n")
	// A committed fake key (CTO-7 §2.2): the run scans the repository's
	// values, and its outputs never hold them.
	mustWrite(t, filepath.Join(s.repo, "deploy.sh"), "export TOKEN="+fakeCommittedToken+"\n")
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "README.md", "deploy.sh"},
		{"-c", "user.name=trust", "-c", "user.email=trust@example.invalid", "commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = s.repo
		cmd.Env = s.env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// configPath is where `rosmarops init` writes the config inside the sandbox.
func (s *sandbox) configPath() string {
	return filepath.Join(s.home, ".rosmarops", "config.json")
}

// setRepoRoots lists the sandbox repository in the generated config.
func (s *sandbox) setRepoRoots(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(s.configPath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	cfg["repo_roots"] = []string{s.repo}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("encode config: %v", err)
	}
	mustWrite(t, s.configPath(), string(out))
}

// runAll runs init, baseline and run in order. wrap, when non-nil, turns the
// rosmarops argv into the argv actually executed (e.g. under strace) for the
// given step.
func (s *sandbox) runAll(t *testing.T, wrap func(step string, argv []string) []string) {
	t.Helper()
	for _, step := range []string{"init", "baseline", "run"} {
		argv := []string{s.binary, step}
		if wrap != nil {
			argv = wrap(step, argv)
		}
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = s.work
		cmd.Env = s.env
		out, err := cmd.CombinedOutput()
		var exitErr *exec.ExitError
		switch {
		case err == nil:
		case errors.As(err, &exitErr) && step == "run":
			// A yellow or red run exits non-zero; the run still completed.
			t.Logf("rosmarops run exit code %d", exitErr.ExitCode())
		default:
			t.Fatalf("rosmarops %s: %v\n%s", step, err, out)
		}
		if step == "init" {
			s.setRepoRoots(t)
		}
	}
	reports := filepath.Join(s.home, ".rosmarops", "reports")
	// Every file of the run, the BV-40 ones included, lands in the reports
	// directory: the write test covers them.
	for _, f := range []string{"graph.v2.json", "report.v2.json", "report.html", "index-history.json", "findings-memory.json"} {
		if _, err := os.Stat(filepath.Join(reports, f)); err != nil {
			t.Fatalf("run did not complete: no %s in %s: %v", f, reports, err)
		}
	}
}

// entry is one path of a tree snapshot. Directories carry no mtime: creating
// a child changes the parent's mtime, which is not a write to the parent.
type entry struct {
	mode   fs.FileMode
	size   int64
	mtime  int64
	sha256 string
	link   string
}

// snapshot records every path under root: type, and for regular files size,
// mtime and SHA-256; for symlinks their target.
func snapshot(t *testing.T, root string) map[string]entry {
	t.Helper()
	snap := map[string]entry{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		e := entry{mode: info.Mode().Type()}
		switch {
		case info.Mode().IsRegular():
			e.size = info.Size()
			e.mtime = info.ModTime().UnixNano()
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			e.sha256 = hex.EncodeToString(sum[:])
		case info.Mode()&fs.ModeSymlink != 0:
			e.link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		snap[path] = e
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return snap
}

// changedPaths lists every path created, modified or deleted between two
// snapshots, each with a short reason.
func changedPaths(before, after map[string]entry) []string {
	var out []string
	for p, a := range after {
		b, ok := before[p]
		switch {
		case !ok:
			out = append(out, "created: "+p)
		case a != b:
			out = append(out, "modified: "+p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			out = append(out, "deleted: "+p)
		}
	}
	sort.Strings(out)
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// pathUnder reports whether path is dir or inside it.
func pathUnder(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
