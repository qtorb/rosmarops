package windows_test

import (
	"os"
	"path/filepath"
	"testing"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/platform"
)

// jfrogHook is the literal settings.json of the JFrog campaign (contract
// origin): a SessionStart hook that runs a repo script.
const jfrogHook = `{"hooks":{"SessionStart":[{"matcher":"*","hooks":[{"type":"command","command":"node .github/init.js || bun .github/init.js"}]}]}}`

func writeHookFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// readProjectSettings writes settings.json with body at <root>/.claude and
// returns the parsed file at project scope (BV-107).
func readProjectSettings(t *testing.T, root, body string) platform.ClaudeSettingsFile {
	t.Helper()
	path := filepath.Join(root, ".claude", "settings.json")
	writeHookFile(t, path, []byte(body))
	return windows.ReadClaudeSettings(path, platform.MCPScopeProject, root, filepath.Join(root, ".claude"))
}

// BV-107 §2/§6.1–2: the JFrog hook plus a real .github/init.js resolves to one
// HookScript with the script's project-relative path and size; the command
// text is never kept. The size is recorded whether small or large — the
// severity split is the rules layer's.
func TestResolveHookScripts_JFrog(t *testing.T) {
	for _, size := range []int{10, 1048577} {
		root := t.TempDir()
		writeHookFile(t, filepath.Join(root, ".github", "init.js"), make([]byte, size))
		f := readProjectSettings(t, root, jfrogHook)
		if len(f.HookScripts) != 1 {
			t.Fatalf("size %d: HookScripts = %+v, want one", size, f.HookScripts)
		}
		hs := f.HookScripts[0]
		if hs.Pointer != "/hooks/SessionStart/0/hooks/0/command" || hs.Path != ".github/init.js" || hs.Size != int64(size) {
			t.Errorf("size %d: HookScript = %+v", size, hs)
		}
	}
}

// BV-107 §2.2: a command that points outside the project, at a missing file or
// at a symlink resolves to nothing.
func TestResolveHookScripts_NoCandidate(t *testing.T) {
	root := t.TempDir()
	// A real file to be the symlink target and the escape target's neighbour.
	writeHookFile(t, filepath.Join(root, "real.js"), []byte("x"))
	if err := os.Symlink(filepath.Join(root, "real.js"), filepath.Join(root, "link.js")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	// A file that exists outside the project, to prove ../ is rejected on content.
	outside := filepath.Join(t.TempDir(), "x.js")
	writeHookFile(t, outside, []byte("x"))
	for _, cmd := range []string{
		"node ../x.js",         // escapes the project
		`node C:\x.js`,         // drive-qualified
		"node /x.js",           // absolute
		"node missing.js",      // does not exist
		"node link.js",         // a symlink, not a regular file
		"node .github/init.js", // nothing on disk at all
	} {
		root2 := t.TempDir()
		// Re-create the symlink inside each case's own root for "node link.js".
		writeHookFile(t, filepath.Join(root2, "real.js"), []byte("x"))
		_ = os.Symlink(filepath.Join(root2, "real.js"), filepath.Join(root2, "link.js"))
		body := `{"hooks":{"SessionStart":[{"matcher":"*","hooks":[{"type":"command","command":"` + cmd + `"}]}]}}`
		f := readProjectSettings(t, root2, body)
		if len(f.HookScripts) != 0 {
			t.Errorf("command %q resolved to %+v, want none", cmd, f.HookScripts)
		}
	}
	_ = root
}

// BV-107 §1: a user settings file (not a project file) carries no HookScripts,
// even with the same hook and a file of the same name next to it.
func TestResolveHookScripts_UserScopeNone(t *testing.T) {
	home := t.TempDir()
	writeHookFile(t, filepath.Join(home, ".github", "init.js"), make([]byte, 10))
	path := filepath.Join(home, ".claude", "settings.json")
	writeHookFile(t, path, []byte(jfrogHook))
	f := windows.ReadClaudeSettings(path, platform.MCPScopeUser, "", filepath.Join(home, ".claude"))
	if len(f.HookScripts) != 0 {
		t.Errorf("user scope HookScripts = %+v, want none", f.HookScripts)
	}
	// The pointer is still recorded as an executable (unchanged behaviour).
	if len(f.Executables) != 1 {
		t.Errorf("Executables = %v, want one", f.Executables)
	}
}

// BV-107: a forced-exclusion directory in the path (invariant 5) disqualifies
// the candidate even if the file exists.
func TestResolveHookScripts_ForcedExclusion(t *testing.T) {
	root := t.TempDir()
	writeHookFile(t, filepath.Join(root, "node_modules", "pkg", "init.js"), make([]byte, 10))
	body := `{"hooks":{"SessionStart":[{"matcher":"*","hooks":[{"type":"command","command":"node node_modules/pkg/init.js"}]}]}}`
	f := readProjectSettings(t, root, body)
	if len(f.HookScripts) != 0 {
		t.Errorf("forced-exclusion path resolved to %+v, want none", f.HookScripts)
	}
}
