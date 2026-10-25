package state_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/schemaval"

	"github.com/qtorb/rosmarops/internal/schemas"
	"github.com/qtorb/rosmarops/internal/state"
)

// writeJSON writes doc as JSON to a temp state file and returns its path.
func writeJSON(t *testing.T, doc map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// stateDoc returns a valid state as a JSON document, for mutation.
func stateDoc(t *testing.T) map[string]any {
	t.Helper()
	raw, err := json.Marshal(defaultState(t))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return doc
}

// v1Doc is a valid schema_version 1 state: no local_salt.
func v1Doc(t *testing.T) map[string]any {
	doc := stateDoc(t)
	doc["schema_version"] = 1
	delete(doc, "local_salt")
	doc["consecutive_green"] = 4
	return doc
}

func TestLoadState_V1MigratesWithValidSalt(t *testing.T) {
	path := writeJSON(t, v1Doc(t))

	st, err := state.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState(v1): %v", err)
	}
	if st.SchemaVersion != 2 {
		t.Errorf("schema_version after migration: got %d, want 2", st.SchemaVersion)
	}
	if !saltPattern.MatchString(st.LocalSalt) {
		t.Errorf("migrated local_salt: got %q, want 64 lowercase hex chars", st.LocalSalt)
	}
	if st.ConsecutiveGreen != 4 {
		t.Errorf("migration lost data: consecutive_green got %d, want 4", st.ConsecutiveGreen)
	}

	// The file on disk is untouched until the next SaveState, which writes v2.
	if err := state.SaveState(path, st); err != nil {
		t.Fatalf("SaveState(migrated): %v", err)
	}
	reloaded, err := state.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState(saved migrated): %v", err)
	}
	if reloaded.LocalSalt != st.LocalSalt {
		t.Error("salt not persisted by SaveState after migration")
	}
}

func TestLoadState_V2RoundTripKeepsSalt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := defaultState(t)
	if err := state.SaveState(path, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	loaded, err := state.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.SchemaVersion != 2 || loaded.LocalSalt != st.LocalSalt {
		t.Errorf("round trip: got version %d salt %q, want 2 and %q",
			loaded.SchemaVersion, loaded.LocalSalt, st.LocalSalt)
	}
}

func TestLoadState_InvalidSaltRejectedBySchema(t *testing.T) {
	for name, salt := range map[string]any{
		"uppercase hex": strings.Repeat("A", 64),
		"too short":     "abcd",
		"not hex":       strings.Repeat("z", 64),
		"not a string":  42,
	} {
		t.Run(name, func(t *testing.T) {
			doc := stateDoc(t)
			doc["local_salt"] = salt
			_, err := state.LoadState(writeJSON(t, doc))
			if err == nil || !strings.Contains(err.Error(), "schema validation") {
				t.Errorf("local_salt %v: got %v, want schema validation error", salt, err)
			}
		})
	}
}

func TestLoadState_V2WithoutSaltRejected(t *testing.T) {
	doc := stateDoc(t)
	delete(doc, "local_salt")
	if _, err := state.LoadState(writeJSON(t, doc)); err == nil {
		t.Error("v2 state without local_salt: got nil error")
	}
}

func TestLoadState_UnsupportedVersion(t *testing.T) {
	doc := stateDoc(t)
	doc["schema_version"] = 3
	_, err := state.LoadState(writeJSON(t, doc))
	if err == nil || !strings.Contains(err.Error(), "unsupported schema_version") {
		t.Errorf("schema_version 3: got %v, want unsupported schema_version error", err)
	}
}

func TestSaveState_WrittenStateValidatesAgainstV2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := state.SaveState(path, defaultState(t)); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	schema, err := schemaval.Compile(schemas.StateV2Schema)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	result, err := schema.Validate(raw)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !result.Valid() {
		t.Errorf("saved state is not valid v2: %v", result.Errors())
	}
}

func TestSaveState_RejectsInvalidState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := defaultState(t)
	st.LocalSalt = ""
	if err := state.SaveState(path, st); err == nil {
		t.Fatal("SaveState with empty salt: got nil error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("SaveState wrote a file for an invalid state")
	}
}

func TestNewSalt_RandomHex(t *testing.T) {
	a, err := state.NewSalt()
	if err != nil {
		t.Fatalf("NewSalt: %v", err)
	}
	b, err := state.NewSalt()
	if err != nil {
		t.Fatalf("NewSalt: %v", err)
	}
	if !saltPattern.MatchString(a) || !saltPattern.MatchString(b) {
		t.Errorf("salts not 64 lowercase hex chars: %q %q", a, b)
	}
	if a == b {
		t.Error("two salts are identical")
	}
}

func TestState_SaltDecodes(t *testing.T) {
	st := defaultState(t)
	salt, err := st.Salt()
	if err != nil || len(salt) != 32 {
		t.Errorf("Salt: got %d bytes, err %v; want 32 bytes", len(salt), err)
	}
	st.LocalSalt = "nothex"
	if _, err := st.Salt(); err == nil {
		t.Error("Salt with invalid hex: got nil error")
	}
}
