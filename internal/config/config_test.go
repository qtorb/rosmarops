package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/config"
)

// writeJSON writes v as JSON to a temp file and returns the path.
func writeJSON(t *testing.T, dir string, v any) string {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal test JSON: %v", err)
	}
	path := filepath.Join(dir, "rosmarops.config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	return path
}

func TestDefaultConfig(t *testing.T) {
	cfg := config.DefaultConfig()

	if cfg.SchemaVersion != 1 {
		t.Errorf("schema_version: got %d, want 1", cfg.SchemaVersion)
	}
	if cfg.RepoRoots == nil {
		t.Error("repo_roots must not be nil (must be empty slice)")
	}
	if len(cfg.RepoRoots) != 0 {
		t.Errorf("repo_roots: got %d entries, want 0", len(cfg.RepoRoots))
	}
}

func TestLoadConfig_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeJSON(t, dir, map[string]any{
		"schema_version": 1,
		"repo_roots":     []string{"/home/user/projects"},
	})

	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: unexpected error: %v", err)
	}
	if cfg.SchemaVersion != 1 {
		t.Errorf("schema_version: got %d, want 1", cfg.SchemaVersion)
	}
	if len(cfg.RepoRoots) != 1 || cfg.RepoRoots[0] != "/home/user/projects" {
		t.Errorf("repo_roots: got %v, want [\"/home/user/projects\"]", cfg.RepoRoots)
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := config.LoadConfig("/nonexistent/path/rosmarops.config.json")
	if err == nil {
		t.Fatal("LoadConfig: expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "read config") {
		t.Errorf("LoadConfig: error should mention 'read config', got: %v", err)
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rosmarops.config.json")
	if err := os.WriteFile(path, []byte("{not valid json}"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := config.LoadConfig(path)
	if err == nil {
		t.Fatal("LoadConfig: expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Errorf("LoadConfig: error should mention 'parse config', got: %v", err)
	}
}

func TestLoadConfig_SchemaViolation_RepoRootsString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rosmarops.config.json")
	// repo_roots must be an array, not a string.
	raw := []byte(`{"schema_version": 1, "repo_roots": "not-an-array"}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := config.LoadConfig(path)
	if err == nil {
		t.Fatal("LoadConfig: expected schema validation error, got nil")
	}
	if !strings.Contains(err.Error(), "schema validation") {
		t.Errorf("LoadConfig: error should mention 'schema validation', got: %v", err)
	}
}

func TestLoadConfig_SchemaViolation_MissingRequired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rosmarops.config.json")
	// Missing required field repo_roots.
	raw := []byte(`{"schema_version": 1}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := config.LoadConfig(path)
	if err == nil {
		t.Fatal("LoadConfig: expected schema validation error for missing repo_roots, got nil")
	}
}

func TestWriteConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rosmarops.config.json")

	original := config.DefaultConfig()
	original.RepoRoots = []string{"/tmp/repo1", "/tmp/repo2"}

	if err := config.WriteConfig(path, original); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	loaded, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig after WriteConfig: %v", err)
	}
	if len(loaded.RepoRoots) != 2 {
		t.Errorf("round-trip repo_roots: got %v", loaded.RepoRoots)
	}
	if loaded.RepoRoots[0] != "/tmp/repo1" {
		t.Errorf("round-trip repo_roots[0]: got %q", loaded.RepoRoots[0])
	}
}

func TestLoadConfig_AllowListNoteMinLength(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rosmarops.config.json")
	// note must be >= 5 chars per schema.
	raw := []byte(`{
		"schema_version": 1,
		"repo_roots": [],
		"allow_list": {
			"extensions": [{"id": "foo.bar", "note": "ok"}]
		}
	}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := config.LoadConfig(path)
	if err == nil {
		t.Fatal("LoadConfig: expected schema error for note shorter than 5 chars, got nil")
	}
}

func TestReposUnder_DirectGitSubdirsOnly(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"beta/.git", "alpha/.git", "notes", "deep/inner/.git"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// A worktree or submodule has a .git file.
	if err := os.MkdirAll(filepath.Join(root, "gamma"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gamma", ".git"), []byte("gitdir: ../x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := config.ReposUnder(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "alpha"), filepath.Join(root, "beta"), filepath.Join(root, "gamma")}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v, want %v (direct repos only, alphabetical)", got, want)
	}
}

func TestReposUnder_MissingDirIsAnError(t *testing.T) {
	if _, err := config.ReposUnder(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("want error for a missing folder")
	}
}
