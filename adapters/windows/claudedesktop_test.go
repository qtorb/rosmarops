package windows_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/company"
	"github.com/qtorb/rosmarops/internal/platform"
)

// desktopHome is a home with Claude Desktop installed on layout l.
func desktopHome(t *testing.T, l agentdirs.Layout) (home string, env agentdirs.Env) {
	t.Helper()
	home = t.TempDir()
	vars := map[string]string{"APPDATA": filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA": filepath.Join(home, "AppData", "Local")}
	env = func(k string) (string, bool) { v, ok := vars[k]; return v, ok }
	writeFile(t, filepath.Join(l.ClaudeDesktopDir(home, env), "claude_desktop_config.json"), `{"mcpServers": {}}`)
	return home, env
}

func plainRead(p string) ([]byte, error) { return os.ReadFile(p) }

// The extension ids are the file names of Claude Extensions Settings; the
// files themselves are never opened (one is made unreadable to prove it).
func TestClaudeDesktop_ExtensionsByNameOnly(t *testing.T) {
	home, env := desktopHome(t, agentdirs.WindowsLayout)
	dir := filepath.Join(agentdirs.WindowsLayout.ClaudeDesktopDir(home, env), "Claude Extensions Settings")
	writeFile(t, filepath.Join(dir, "ant.dir.filesystem.json"), `{"isEnabled": true, "userConfig": {"token": "secret-value"}}`)
	writeFile(t, filepath.Join(dir, "local.github.json"), `{}`)
	writeFile(t, filepath.Join(dir, ".hidden.json"), `{}`)
	writeFile(t, filepath.Join(dir, "notes.txt"), ``)
	if err := os.Chmod(filepath.Join(dir, "ant.dir.filesystem.json"), 0); err != nil {
		t.Fatal(err)
	}
	msix := filepath.Join(agentdirs.WindowsLayout.ClaudeDesktopMSIXDir(home, env), "Claude Extensions Settings")
	writeFile(t, filepath.Join(msix, "store.only.json"), `{}`)
	scan := windows.ReadClaudeDesktop(context.Background(), agentdirs.WindowsLayout, home, env, nil, nil, plainRead)
	if !scan.Present || strings.Join(scan.Extensions, ",") != "ant.dir.filesystem,local.github,store.only" {
		t.Fatalf("scan = %+v", scan)
	}
}

// Not installed: nothing is read.
func TestClaudeDesktop_Absent(t *testing.T) {
	home := t.TempDir()
	scan := windows.ReadClaudeDesktop(context.Background(), agentdirs.LinuxLayout, home, func(string) (string, bool) { return "", false }, nil, []string{filepath.Join(home, "none.json")}, plainRead)
	if scan.Present || scan.Managed.Path != "" {
		t.Fatalf("scan = %+v", scan)
	}
}

// Linux: ~/.config/Claude is found, and the managed JSON is read after the
// ownership check; a file that fails it is not read and says why.
func TestClaudeDesktop_LinuxManagedJSON(t *testing.T) {
	home, env := desktopHome(t, agentdirs.LinuxLayout)
	if got := agentdirs.LinuxLayout.ClaudeDesktopDir(home, env); got != filepath.Join(home, ".config", "Claude") {
		t.Fatalf("Linux folder = %s", got)
	}
	managed := filepath.Join(t.TempDir(), "managed-settings.json")
	writeFile(t, managed, `{"isLocalDevMcpEnabled": false, "isDesktopExtensionEnabled": true, "other": [1]}`)
	scan := windows.ReadClaudeDesktop(context.Background(), agentdirs.LinuxLayout, home, env, nil, []string{managed}, plainRead)
	m := scan.Managed
	if !scan.Present || m.Status != platform.MCPSourceRead || m.LocalMCP == nil || *m.LocalMCP || m.Extensions == nil || !*m.Extensions || m.PersistentAlwaysAllow != nil {
		t.Fatalf("managed = %+v", m)
	}
	untrusted := func(p string) ([]byte, error) {
		return nil, fmt.Errorf("%w: %s is not owned by root", company.ErrUntrusted, p)
	}
	m = windows.ReadClaudeDesktop(context.Background(), agentdirs.LinuxLayout, home, env, nil, []string{managed}, untrusted).Managed
	if m.Status != platform.MCPSourceFailed || m.LocalMCP != nil || !strings.Contains(m.Reason, "not owned by an administrator") {
		t.Fatalf("untrusted managed file = %+v", m)
	}
}

// macOS: the machine's managed preferences plist (binary) is the first
// looked for; the user's applies only when the machine's is absent.
func TestClaudeDesktop_MacManagedPlist(t *testing.T) {
	home, env := desktopHome(t, agentdirs.MacOSLayout)
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "plistsubset", "testdata", "managed.binary.plist"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	machine, userFile := filepath.Join(dir, "machine.plist"), filepath.Join(dir, "user", "user.plist")
	writeFile(t, userFile, string(raw))
	m := windows.ReadClaudeDesktop(context.Background(), agentdirs.MacOSLayout, home, env, nil, []string{machine, userFile}, plainRead).Managed
	if m.Path != userFile || m.LocalMCP == nil || *m.LocalMCP || m.PersistentAlwaysAllow == nil || *m.PersistentAlwaysAllow {
		t.Fatalf("user plist = %+v", m)
	}
	writeFile(t, machine, "not a plist")
	m = windows.ReadClaudeDesktop(context.Background(), agentdirs.MacOSLayout, home, env, nil, []string{machine, userFile}, plainRead).Managed
	if m.Path != machine || m.Status != platform.MCPSourceFailed {
		t.Fatalf("a broken machine plist is reported, not skipped for the user's: %+v", m)
	}
}

// Windows: HKLM, else HKCU; with any HKLM value, HKCU is ignored.
func TestClaudeDesktop_WindowsRegistry(t *testing.T) {
	home, env := desktopHome(t, agentdirs.WindowsLayout)
	key := `SOFTWARE\Policies\Claude`
	reg := fakeRegistry{
		"HKCU|" + key + "|isLocalDevMcpEnabled":            "0",
		"HKCU|" + key + "|mcpPersistentAlwaysAllowEnabled": "0",
	}
	m := windows.ReadClaudeDesktop(context.Background(), agentdirs.WindowsLayout, home, env, reg, nil, plainRead).Managed
	if m.Path != `registry:HKCU\`+key || m.LocalMCP == nil || *m.LocalMCP || m.PersistentAlwaysAllow == nil {
		t.Fatalf("HKCU = %+v", m)
	}
	reg["HKLM|"+key+"|isDesktopExtensionEnabled"] = "1"
	m = windows.ReadClaudeDesktop(context.Background(), agentdirs.WindowsLayout, home, env, reg, nil, plainRead).Managed
	if m.Path != `registry:HKLM\`+key || m.LocalMCP != nil || m.Extensions == nil || !*m.Extensions {
		t.Fatalf("HKLM wins whole = %+v", m)
	}
}
