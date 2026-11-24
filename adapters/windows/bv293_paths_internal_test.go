package windows

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-293: off Windows, the readers look for VS Code's user settings where VS
// Code keeps them (code.visualstudio.com/docs/configure/settings, «Settings
// file locations»), and for Codex's system requirements.toml in /etc/codex
// (learn.chatgpt.com/docs/enterprise/managed-configuration).

func TestBV293_VSCodeUserDirPerSystem(t *testing.T) {
	home := filepath.FromSlash("/home/ana")
	none := envOf(map[string]string{})
	for _, c := range []struct {
		name   string
		layout agentdirs.Layout
		env    agentdirs.Env
		want   string
	}{
		{"linux", agentdirs.LinuxLayout, none, filepath.Join(home, ".config", "Code", "User")},
		{"linux, XDG_CONFIG_HOME", agentdirs.LinuxLayout, envOf(map[string]string{"XDG_CONFIG_HOME": filepath.FromSlash("/xdg")}), filepath.Join(filepath.FromSlash("/xdg"), "Code", "User")},
		{"macos", agentdirs.MacOSLayout, none, filepath.Join(home, "Library", "Application Support", "Code", "User")},
		{"windows", agentdirs.WindowsLayout, envOf(map[string]string{"APPDATA": filepath.FromSlash("/w/Roaming")}), filepath.Join(filepath.FromSlash("/w/Roaming"), "Code", "User")},
		{"windows, no APPDATA", agentdirs.WindowsLayout, none, filepath.Join(home, "AppData", "Roaming", "Code", "User")},
	} {
		if got := copilotVSCodeUserDir(c.layout, home, c.env); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// Copilot's user settings are read from VS Code's real folder on Linux and
// macOS: chat.agent.enabled = false is seen.
func TestBV293_CopilotSettingsReadOffWindows(t *testing.T) {
	for _, layout := range []agentdirs.Layout{agentdirs.LinuxLayout, agentdirs.MacOSLayout} {
		home := t.TempDir()
		env := envOf(map[string]string{})
		dir := copilotVSCodeUserDir(layout, home, env)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"chat.agent.enabled": false}`), 0o600); err != nil {
			t.Fatal(err)
		}
		scan := ReadCopilot(context.Background(), dir, home, env, nil, nil)
		found := false
		for _, l := range scan.Layers {
			if l.Kind == platform.CopilotLayerUser && l.Status == platform.MCPSourceRead && l.AgentEnabled != nil && !*l.AgentEnabled {
				found = true
			}
		}
		if !found {
			t.Errorf("layout %v: the user settings in %s were not read: %+v", layout, dir, scan.Layers)
		}
	}
}

// Claude Code's VS Code mode is read from VS Code's real folder off Windows.
func TestBV293_ClaudeCodeVSCodeModeOffWindows(t *testing.T) {
	for _, layout := range []agentdirs.Layout{agentdirs.LinuxLayout, agentdirs.MacOSLayout} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("APPDATA", "")
		dir := layout.VSCodeUserDir(home, envOf(map[string]string{}))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"claudeCode.initialPermissionMode": "acceptEdits"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		scan, err := NewOn(Options{Layout: layout}).ListClaudeSettings(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if scan.VSCode == nil || scan.VSCode.Status != platform.MCPSourceRead || scan.VSCode.Mode != "acceptEdits" {
			t.Errorf("layout %v: VS Code mode in %s not read: %+v", layout, dir, scan.VSCode)
		}
	}
}

func TestBV293_CodexRequirementsOffWindows(t *testing.T) {
	env := envOf(map[string]string{"ProgramData": `C:\PD`})
	for _, layout := range []agentdirs.Layout{agentdirs.LinuxLayout, agentdirs.MacOSLayout} {
		if got := resolveCodexRequirements(context.Background(), layout, env, nil); got != "/etc/codex/requirements.toml" {
			t.Errorf("layout %v: %q, want /etc/codex/requirements.toml", layout, got)
		}
	}
}
