package state_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/state"
)

const zeroHash = "0000000000000000000000000000000000000000000000000000000000000000"

var saltPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// defaultState returns DefaultState("0.1.0-dev"), failing the test on error.
func defaultState(t *testing.T) state.State {
	t.Helper()
	st, err := state.DefaultState("0.1.0-dev")
	if err != nil {
		t.Fatalf("DefaultState: %v", err)
	}
	return st
}

func TestDefaultState(t *testing.T) {
	st := defaultState(t)

	if st.SchemaVersion != 2 {
		t.Errorf("schema_version: got %d, want 2", st.SchemaVersion)
	}
	if !saltPattern.MatchString(st.LocalSalt) {
		t.Errorf("local_salt: got %q, want 64 lowercase hex chars", st.LocalSalt)
	}
	if st.EngineVersion != "0.1.0-dev" {
		t.Errorf("engine_version: got %q, want %q", st.EngineVersion, "0.1.0-dev")
	}
	if st.ConsecutiveGreen != 0 {
		t.Errorf("consecutive_green: got %d, want 0", st.ConsecutiveGreen)
	}
	if st.Integrity.VigilBinarySHA256 != zeroHash {
		t.Errorf("vigil_binary_sha256: got %q, want zero hash", st.Integrity.VigilBinarySHA256)
	}
}

func TestSaveAndLoadState_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	original := defaultState(t)
	original.ConsecutiveGreen = 3
	original.LastRunStatus = "green"

	if err := state.SaveState(path, original); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	loaded, err := state.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState after SaveState: %v", err)
	}

	if loaded.ConsecutiveGreen != 3 {
		t.Errorf("consecutive_green: got %d, want 3", loaded.ConsecutiveGreen)
	}
	if loaded.LastRunStatus != "green" {
		t.Errorf("last_run_status: got %q, want %q", loaded.LastRunStatus, "green")
	}
	if loaded.EngineVersion != "0.1.0-dev" {
		t.Errorf("engine_version: got %q, want %q", loaded.EngineVersion, "0.1.0-dev")
	}
}

func TestLoadState_MissingFile(t *testing.T) {
	_, err := state.LoadState("/nonexistent/path/state.json")
	if err == nil {
		t.Fatal("LoadState: expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "read state") {
		t.Errorf("LoadState: error should mention 'read state', got: %v", err)
	}
}

func TestLoadState_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("{invalid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := state.LoadState(path)
	if err == nil {
		t.Fatal("LoadState: expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "parse state") {
		t.Errorf("LoadState: error should mention 'parse state', got: %v", err)
	}
}

func TestLoadState_SchemaViolation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	// Missing required fields (engine_version, last_run, integrity, baselines).
	raw := []byte(`{"schema_version": 1}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := state.LoadState(path)
	if err == nil {
		t.Fatal("LoadState: expected schema validation error, got nil")
	}
	if !strings.Contains(err.Error(), "schema validation") {
		t.Errorf("LoadState: error should mention 'schema validation', got: %v", err)
	}
}

func TestSaveState_Atomic_TempFileCleanedOnError(t *testing.T) {
	// Write a valid state, verify no .tmp files remain in the directory.
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	st := defaultState(t)
	if err := state.SaveState(path, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file not cleaned up: %s", e.Name())
		}
	}
	// Exactly one file should exist: state.json.
	if len(entries) != 1 {
		t.Errorf("expected 1 file in dir, got %d: %v", len(entries), entries)
	}
}

func TestSaveState_FileHasNewline(t *testing.T) {
	// JSON output must end with a newline (canonical format).
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	st := defaultState(t)
	if err := state.SaveState(path, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("state file does not end with newline")
	}
}
