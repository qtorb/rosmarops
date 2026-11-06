package windows

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-140 (CTO-16): VS Code's settings.json normally has comments and trailing
// commas. readVSCodeMode now reads it as JSONC, so the common case is read, not
// sent to coverage as parse_error.
func TestReadVSCodeMode_JSONC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	src := `{
  // the mode the Claude Code extension starts in
  "claudeCode.initialPermissionMode": "bypassPermissions",
  "editor.fontSize": 13, // trailing comma next
}`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	v := readVSCodeMode(path, filepath.Dir(path))
	if v == nil {
		t.Fatal("nil for an existing file")
	}
	if v.Status != platform.MCPSourceRead {
		t.Fatalf("status: got %q, want read (JSONC must parse)", v.Status)
	}
	if v.Mode != "bypassPermissions" {
		t.Errorf("mode: got %q, want bypassPermissions", v.Mode)
	}
}
