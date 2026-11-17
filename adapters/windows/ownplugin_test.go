package windows_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-253 §B.3: RosmarOps's own Claude Code plugin is recognised only when
// every condition of §B.2 holds; a missing, unreadable or broken file is no
// error and no recognition.

const goodHooks = `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"rosmarops","args":["session-check"],"timeout":3}]}]}}`

// pluginTree writes a plugins folder with RosmarOps's plugin installed as
// Claude Code 2.1.326 writes it, and returns the folder and the copy.
func pluginTree(t *testing.T) (dir, copyDir string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "plugins")
	copyDir = filepath.Join(dir, "cache", "rosmarops", "rosmarops", "0.1.0")
	writeFile(t, filepath.Join(dir, "known_marketplaces.json"), `{"rosmarops":{"source":{"source":"github","repo":"qtorb/rosmarops-claude-code"},"installLocation":"x","lastUpdated":"2026-11-17T23:11:52.155Z"}}`)
	inst, _ := json.Marshal(map[string]any{"version": 2, "plugins": map[string]any{
		"rosmarops@rosmarops": []map[string]any{{"scope": "user", "installPath": copyDir, "version": "0.1.0"}},
	}})
	writeFile(t, filepath.Join(dir, "installed_plugins.json"), string(inst))
	writeFile(t, filepath.Join(copyDir, "hooks", "hooks.json"), goodHooks)
	writeFile(t, filepath.Join(copyDir, ".claude-plugin", "plugin.json"), `{"name":"rosmarops","displayName":"RosmarOps","version":"0.1.0","description":"TEXT-NOT-IN-REPORT","license":"AGPL-3.0"}`)
	writeFile(t, filepath.Join(copyDir, "skills", "rosmarops", "SKILL.md"), "---\nname: rosmarops\n---\n")
	return dir, copyDir
}

func withOwnBinary(t *testing.T, ok bool) {
	t.Helper()
	prev := windows.OwnBinary
	t.Cleanup(func() { windows.OwnBinary = prev })
	windows.OwnBinary = func(p string) bool { return ok && p == "rosmarops" }
}

func TestOwnPluginRecognised(t *testing.T) {
	withOwnBinary(t, true)
	dir, _ := pluginTree(t)
	if !windows.ReadOwnPlugin(dir) {
		t.Fatal("RosmarOps's plugin not recognised")
	}
}

func TestOwnPluginEachConditionFails(t *testing.T) {
	cases := []struct {
		name   string
		binary bool
		edit   func(t *testing.T, dir, copyDir string)
	}{
		{"2: marketplace from another repo", true, func(t *testing.T, dir, _ string) {
			writeFile(t, filepath.Join(dir, "known_marketplaces.json"), `{"rosmarops":{"source":{"source":"github","repo":"someone/rosmarops-claude-code"}}}`)
		}},
		{"2: marketplace from a local folder", true, func(t *testing.T, dir, _ string) {
			writeFile(t, filepath.Join(dir, "known_marketplaces.json"), `{"rosmarops":{"source":{"source":"directory","path":"/x"}}}`)
		}},
		{"3: installPath outside cache/rosmarops/rosmarops", true, func(t *testing.T, dir, copyDir string) {
			other := filepath.Join(dir, "cache", "other", "rosmarops", "0.1.0")
			if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(copyDir, other); err != nil {
				t.Fatal(err)
			}
			inst, _ := json.Marshal(map[string]any{"version": 2, "plugins": map[string]any{"rosmarops@rosmarops": []map[string]any{{"installPath": other}}}})
			writeFile(t, filepath.Join(dir, "installed_plugins.json"), string(inst))
		}},
		{"3: not installed", true, func(t *testing.T, dir, _ string) {
			writeFile(t, filepath.Join(dir, "installed_plugins.json"), `{"version":2,"plugins":{}}`)
		}},
		{"4: another command", true, func(t *testing.T, _, copyDir string) {
			writeFile(t, filepath.Join(copyDir, "hooks", "hooks.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"sh","args":["-c","rosmarops session-check"],"timeout":3}]}]}}`)
		}},
		{"4: shell form", true, func(t *testing.T, _, copyDir string) {
			writeFile(t, filepath.Join(copyDir, "hooks", "hooks.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"rosmarops session-check","timeout":3}]}]}}`)
		}},
		{"4: timeout over 10", true, func(t *testing.T, _, copyDir string) {
			writeFile(t, filepath.Join(copyDir, "hooks", "hooks.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"rosmarops","args":["session-check"],"timeout":600}]}]}}`)
		}},
		{"4: a matcher", true, func(t *testing.T, _, copyDir string) {
			writeFile(t, filepath.Join(copyDir, "hooks", "hooks.json"), `{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"rosmarops","args":["session-check"],"timeout":3}]}]}}`)
		}},
		{"4: an if", true, func(t *testing.T, _, copyDir string) {
			writeFile(t, filepath.Join(copyDir, "hooks", "hooks.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"rosmarops","args":["session-check"],"timeout":3,"if":"x"}]}]}}`)
		}},
		{"4: another event", true, func(t *testing.T, _, copyDir string) {
			writeFile(t, filepath.Join(copyDir, "hooks", "hooks.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"rosmarops","args":["session-check"],"timeout":3}]}],"PreToolUse":[{"hooks":[{"type":"command","command":"x"}]}]}}`)
		}},
		{"5: plugin.json declares mcpServers", true, func(t *testing.T, _, copyDir string) {
			writeFile(t, filepath.Join(copyDir, ".claude-plugin", "plugin.json"), `{"name":"rosmarops","mcpServers":{"x":{"command":"x"}}}`)
		}},
		{"5: plugin.json declares hooks", true, func(t *testing.T, _, copyDir string) {
			writeFile(t, filepath.Join(copyDir, ".claude-plugin", "plugin.json"), `{"name":"rosmarops","hooks":"./more.json"}`)
		}},
		{"5: bin/ in the copy", true, func(t *testing.T, _, copyDir string) {
			writeFile(t, filepath.Join(copyDir, "bin", "rosmarops"), "x")
		}},
		{"5: .mcp.json in the copy", true, func(t *testing.T, _, copyDir string) {
			writeFile(t, filepath.Join(copyDir, ".mcp.json"), `{}`)
		}},
		{"6: rosmarops in the PATH is not ours", false, func(*testing.T, string, string) {}},
		{"absent: known_marketplaces.json", true, func(t *testing.T, dir, _ string) {
			_ = os.Remove(filepath.Join(dir, "known_marketplaces.json"))
		}},
		{"absent: hooks.json", true, func(t *testing.T, _, copyDir string) {
			_ = os.Remove(filepath.Join(copyDir, "hooks", "hooks.json"))
		}},
		{"broken: installed_plugins.json", true, func(t *testing.T, dir, _ string) {
			writeFile(t, filepath.Join(dir, "installed_plugins.json"), `{"plugins":`)
		}},
		{"broken: plugin.json", true, func(t *testing.T, _, copyDir string) {
			writeFile(t, filepath.Join(copyDir, ".claude-plugin", "plugin.json"), `not json`)
		}},
		{"unreadable: hooks.json is a folder", true, func(t *testing.T, _, copyDir string) {
			p := filepath.Join(copyDir, "hooks", "hooks.json")
			_ = os.Remove(p)
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withOwnBinary(t, c.binary)
			dir, copyDir := pluginTree(t)
			c.edit(t, dir, copyDir)
			if windows.ReadOwnPlugin(dir) {
				t.Errorf("recognised although %s", c.name)
			}
		})
	}
}

// Condition 1: enabledPlugins in the user or managed settings, not in a
// project's.
func TestOwnPluginEnabledOnlyUserOrManaged(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	for _, c := range []struct {
		body, scope string
		want        bool
	}{
		{`{"enabledPlugins":{"rosmarops@rosmarops":true}}`, platform.MCPScopeUser, true},
		{`{"enabledPlugins":{"rosmarops@rosmarops":true}}`, platform.MCPScopeManaged, true},
		{`{"enabledPlugins":{"rosmarops@rosmarops":true}}`, platform.MCPScopeProject, false},
		{`{"enabledPlugins":{"rosmarops@rosmarops":false}}`, platform.MCPScopeUser, false},
		{`{"enabledPlugins":{"rosmarops@other":true}}`, platform.MCPScopeUser, false},
		{`{}`, platform.MCPScopeUser, false},
	} {
		writeFile(t, p, c.body)
		f := windows.ReadClaudeSettings(p, c.scope, "", dir)
		if f.Status != platform.MCPSourceRead || f.OwnPluginEnabled != c.want {
			t.Errorf("%s %s: status %s, enabled %v, want %v", c.scope, c.body, f.Status, f.OwnPluginEnabled, c.want)
		}
	}
	_ = runtime.GOOS
}

// CTO-37 T3 applied to BV-253: of the plugins folder only the recognition
// reaches the scan the report is built from. The plugin files carry marker
// texts; none appears in the scan.
func TestOwnPluginOnlyRecognitionLeaves(t *testing.T) {
	withOwnBinary(t, true)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CLAUDE_CODE_PLUGIN_CACHE_DIR", "")
	claude := filepath.Join(home, ".claude")
	writeFile(t, filepath.Join(claude, "settings.json"), `{"enabledPlugins":{"rosmarops@rosmarops":true,"MARKER-OTHER-PLUGIN@x":true}}`)
	dir := filepath.Join(claude, "plugins")
	copyDir := filepath.Join(dir, "cache", "rosmarops", "rosmarops", "0.1.0")
	writeFile(t, filepath.Join(dir, "known_marketplaces.json"), `{"rosmarops":{"source":{"source":"github","repo":"qtorb/rosmarops-claude-code"},"installLocation":"MARKER-LOCATION"}}`)
	inst, _ := json.Marshal(map[string]any{"version": 2, "plugins": map[string]any{
		"rosmarops@rosmarops": []map[string]any{{"installPath": copyDir, "gitCommitSha": "MARKER-SHA"}},
	}})
	writeFile(t, filepath.Join(dir, "installed_plugins.json"), string(inst))
	writeFile(t, filepath.Join(copyDir, "hooks", "hooks.json"), goodHooks)
	writeFile(t, filepath.Join(copyDir, ".claude-plugin", "plugin.json"), `{"name":"rosmarops","description":"MARKER-DESCRIPTION"}`)
	writeFile(t, filepath.Join(dir, "data", "rosmarops-rosmarops", "x"), "MARKER-DATA")
	scan, err := windows.NewOn(windows.Options{Layout: agentdirs.Current()}).ListClaudeSettings(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !scan.OwnPlugin {
		t.Fatal("RosmarOps's plugin not recognised through ListClaudeSettings")
	}
	raw, _ := json.Marshal(scan)
	for _, m := range []string{"MARKER-OTHER-PLUGIN", "MARKER-LOCATION", "MARKER-SHA", "MARKER-DESCRIPTION", "MARKER-DATA", "rosmarops-claude-code", "0.1.0"} {
		if containsBytes(raw, m) {
			t.Errorf("the scan carries %q from the plugins folder", m)
		}
	}
}

func containsBytes(b []byte, s string) bool { return strings.Contains(string(b), s) }
