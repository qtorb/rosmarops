package winpath

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// BV-93, B4: the resolved paths hang off the API-given directories, and off
// Windows every function is the empty stub.
func TestResolvedPaths(t *testing.T) {
	sys, win := SystemDir(), WindowsDir()
	if runtime.GOOS != "windows" {
		for name, got := range map[string]string{
			"SystemDir": sys, "WindowsDir": win,
			"PowerShellExe": PowerShellExe(), "ExplorerExe": ExplorerExe(),
			"SystemModules": SystemModules(), "Module": Module("ScheduledTasks"),
		} {
			if got != "" {
				t.Errorf("%s off Windows = %q, want empty", name, got)
			}
		}
		return
	}
	// On Windows the composites are built under the API directories.
	if sys == "" || win == "" {
		t.Fatalf("API gave no dirs: system=%q windows=%q", sys, win)
	}
	if got, want := PowerShellExe(), filepath.Join(sys, "WindowsPowerShell", "v1.0", "powershell.exe"); got != want {
		t.Errorf("PowerShellExe = %q, want %q", got, want)
	}
	if got, want := ExplorerExe(), filepath.Join(win, "explorer.exe"); got != want {
		t.Errorf("ExplorerExe = %q, want %q", got, want)
	}
	if got, want := Module("ScheduledTasks"), filepath.Join(sys, "WindowsPowerShell", "v1.0", "Modules", "ScheduledTasks"); got != want {
		t.Errorf("Module = %q, want %q", got, want)
	}
}

// BV-93, B4: ChildEnv drops every inherited PSModulePath and sets the system
// module directory as the only value (on Windows); off Windows it leaves the
// environment unchanged.
func TestChildEnv(t *testing.T) {
	base := []string{"FOO=bar", "PSModulePath=C:\\attacker\\modules", "psmodulepath=C:\\also\\evil"}
	got := ChildEnv(base)
	if runtime.GOOS != "windows" {
		if len(got) != len(base) {
			t.Errorf("off Windows ChildEnv changed the env: %v", got)
		}
		return
	}
	var psmods []string
	for _, kv := range got {
		if strings.HasPrefix(strings.ToUpper(kv), "PSMODULEPATH=") {
			psmods = append(psmods, kv)
		}
	}
	if len(psmods) != 1 {
		t.Fatalf("want exactly one PSModulePath, got %v", psmods)
	}
	if want := "PSModulePath=" + SystemModules(); psmods[0] != want {
		t.Errorf("PSModulePath = %q, want %q", psmods[0], want)
	}
	// Unrelated variables are kept.
	var keptFoo bool
	for _, kv := range got {
		keptFoo = keptFoo || kv == "FOO=bar"
	}
	if !keptFoo {
		t.Error("ChildEnv dropped an unrelated variable")
	}
}
