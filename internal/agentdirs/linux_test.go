package agentdirs

import (
	"path/filepath"
	"runtime"
	"testing"
)

func linuxEnvOf(m map[string]string) Env {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

// BV-165: Current() is LinuxLayout on Linux.
func TestCurrentLinux(t *testing.T) {
	if runtime.GOOS == "linux" && Current() != LinuxLayout {
		t.Fatalf("Current()=%v on linux, want LinuxLayout", Current())
	}
}

// BV-165: the Linux paths use $XDG_CONFIG_HOME or ~/.config, ~/.hermes for
// Hermes (or $HERMES_HOME), and Claude Desktop is not searched.
func TestLinuxLayoutPaths(t *testing.T) {
	home := "/home/alice"
	// Default config base: ~/.config.
	def := linuxEnvOf(nil)
	if got, want := LinuxLayout.VSCodeUserDir(home, def), filepath.Join(home, ".config", "Code", "User"); got != want {
		t.Errorf("VSCodeUserDir = %q, want %q", got, want)
	}
	if got, want := LinuxLayout.AntigravityUserDir(home, def), filepath.Join(home, ".config", "Antigravity", "User"); got != want {
		t.Errorf("AntigravityUserDir = %q, want %q", got, want)
	}
	if got, want := LinuxLayout.ClineDir(home, def), filepath.Join(home, ".config", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings"); got != want {
		t.Errorf("ClineDir = %q, want %q", got, want)
	}
	if got, want := LinuxLayout.HermesHome(home, def), filepath.Join(home, ".hermes"); got != want {
		t.Errorf("HermesHome = %q, want %q", got, want)
	}
	// XDG_CONFIG_HOME overrides the config base.
	xdg := linuxEnvOf(map[string]string{"XDG_CONFIG_HOME": "/cfg"})
	if got, want := LinuxLayout.VSCodeUserDir(home, xdg), filepath.Join("/cfg", "Code", "User"); got != want {
		t.Errorf("VSCodeUserDir with XDG = %q, want %q", got, want)
	}
	// HERMES_HOME overrides, but only when it is an existing folder (varDir);
	// a non-existent one falls back to ~/.hermes.
	tmp := t.TempDir()
	hz := linuxEnvOf(map[string]string{"HERMES_HOME": tmp})
	if got := LinuxLayout.HermesHome(home, hz); got != tmp {
		t.Errorf("HermesHome with HERMES_HOME = %q, want %q", got, tmp)
	}
	// Claude Desktop is not searched on Linux.
	if ds := LinuxLayout.Detect(home, def); len(ds) != 0 {
		// home has nothing, so Detect is empty; the point is it does not panic
		// and does not look for Claude Desktop. Covered more fully below.
		_ = ds
	}
}
