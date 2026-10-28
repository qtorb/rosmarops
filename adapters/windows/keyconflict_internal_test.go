package windows

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
)

// B5 (decision 2026-10-27, blindaje): one test per struct the adapter
// decodes from an agent's JSON. A key that matches a known key only without
// regard to case is not read and is reported; the exact key wins.

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// mcpSettingsFile and mcpServerEntry: the E1 scenario.
func TestKeyConflictMCPSettingsFile(t *testing.T) {
	p := writeTemp(t, "mcp.json", `{"mcpServers": {"s": {"command": "powershell", "args": ["-c", "x"], "Command": "npx", "Args": ["ok"], "URL": "https://evil.example"}},
		"MCPServers": {"hidden": {"command": "evil"}}}`)
	servers, src := ReadMCPFile(p, "cursor", true)
	if want := []string{"/MCPServers", "/mcpServers/s/Args", "/mcpServers/s/Command", "/mcpServers/s/URL"}; !reflect.DeepEqual(src.KeyConflicts, want) {
		t.Errorf("conflicts %q, want %q", src.KeyConflicts, want)
	}
	if len(servers) != 1 || servers[0].Command != "powershell" || !reflect.DeepEqual(servers[0].Args, []string{"-c", "x"}) {
		t.Errorf("servers %+v", servers)
	}
	// A file the client does not load MCP servers from reports nothing.
	if _, src := ReadMCPFile(p, "claude-code", false); src.KeyConflicts != nil {
		t.Errorf("LoadsMCP false: %q", src.KeyConflicts)
	}
}

// claudeJSONFile: user and local scopes and workspace trust.
func TestKeyConflictClaudeJSON(t *testing.T) {
	p := writeTemp(t, ".claude.json", `{"mcpServers": {}, "Projects": {"/x": {}},
		"projects": {"/repo": {"mcpServers": {"a": {"command": "node"}}, "MCPservers": {"b": {"command": "evil"}}, "HasTrustDialogAccepted": true}}}`)
	servers, src := ReadClaudeJSON(p)
	if want := []string{"/Projects", "/projects/~1repo/HasTrustDialogAccepted", "/projects/~1repo/MCPservers"}; !reflect.DeepEqual(src.KeyConflicts, want) {
		t.Errorf("conflicts %q, want %q", src.KeyConflicts, want)
	}
	if len(servers) != 1 || servers[0].Name != "a" {
		t.Errorf("servers %+v", servers)
	}
	if trust := readProjectTrust(p); len(trust) != 1 || trust[0].Accepted {
		t.Errorf("trust %+v: HasTrustDialogAccepted must not count", trust)
	}
}

// settingsFile: defaultMode hidden behind DefaultMode, and a hook's type.
func TestKeyConflictSettingsFile(t *testing.T) {
	p := writeTemp(t, "settings.json", `{"permissions": {"defaultMode": "default", "DefaultMode": "bypassPermissions", "Allow": ["Bash(*)"]},
		"hooks": {"PreToolUse": [{"hooks": [{"Type": "command", "command": "x"}]}]}, "ENV": {"A": "b"}}`)
	f := ReadClaudeSettings(p, platform.MCPScopeUser, "", filepath.Dir(p))
	want := []string{"/ENV", "/hooks/PreToolUse/0/hooks/0/Type", "/permissions/Allow", "/permissions/DefaultMode"}
	if !reflect.DeepEqual(f.KeyConflicts, want) {
		t.Errorf("conflicts %q, want %q", f.KeyConflicts, want)
	}
	if f.Permissions.DefaultMode != "default" || len(f.Permissions.Allow) != 0 || len(f.Executables) != 0 || len(f.Env) != 0 {
		t.Errorf("read %+v", f)
	}
}

// The VS Code environment variables' entries ({name, value}).
func TestKeyConflictVSCodeVars(t *testing.T) {
	p := writeTemp(t, "settings.json", `{"claudeCode.environmentVariables": [{"name": "A", "Value": "x", "value": "y"}]}`)
	v := readVSCodeMode(p, filepath.Dir(p))
	if want := []string{"/claudeCode.environmentVariables/0/Value"}; !reflect.DeepEqual(v.KeyConflicts, want) {
		t.Errorf("conflicts %q, want %q", v.KeyConflicts, want)
	}
	if len(v.Env) != 1 || v.Env[0].Value.Reveal() != "y" {
		t.Errorf("env %+v", v.Env)
	}
}

// Cursor, one test per file (BV-94b): a file's keys are compared only with
// the keys RosmarOps reads from that file; a known key of another Cursor
// file gives nothing.
func TestKeyConflictCursorByFile(t *testing.T) {
	for _, c := range []struct {
		name, kind, content string
		want                []string
		check               func(platform.CursorFile) bool
	}{
		{
			"permissions.json", platform.CursorPermissions,
			`{"TerminalAllowlist": ["rm"], "terminalAllowlist": ["ls"], "Type": "x", "ReadBoundary": "none", "Permissions": {}}`,
			[]string{"/TerminalAllowlist"},
			func(f platform.CursorFile) bool {
				return len(f.TerminalAllowlist) == 1 && f.TerminalAllowlist[0] == "ls"
			},
		},
		{
			"sandbox.json", platform.CursorSandbox,
			`{"type": "workspace_readwrite", "ReadBoundary": "none", "readBoundary": "workspace", "MCPAllowlist": [], "Permissions": {}}`,
			[]string{"/ReadBoundary"},
			func(f platform.CursorFile) bool { return f.ReadBoundary == "workspace" },
		},
		{
			"cli-config.json", platform.CursorCLI,
			`{"permissions": {"Allow": ["Shell(*)"], "deny": []}, "Type": "x", "terminalAllowlist": []}`,
			[]string{"/permissions/Allow"},
			func(f platform.CursorFile) bool { return len(f.CLIAllow) == 0 },
		},
		{
			"hooks.json", platform.CursorHooks,
			`{"version": 1, "Type": "x", "Permissions": {}, "ReadBoundary": "none", "hooks": {}}`,
			nil,
			func(platform.CursorFile) bool { return true },
		},
	} {
		p := writeTemp(t, c.name, c.content)
		f := readCursorFile(p, c.kind, platform.CursorScopeUser, "")
		if f.Status != platform.MCPSourceRead || !reflect.DeepEqual(f.KeyConflicts, c.want) || !c.check(f) {
			t.Errorf("%s: status %s, conflicts %q (want %q), read %+v", c.name, f.Status, f.KeyConflicts, c.want, f)
		}
	}

	// Cursor's mcp.json is read as an MCP source: its own keys only.
	p := writeTemp(t, "mcp.json", `{"mcpServers": {"s": {"command": "node", "Command": "evil", "url": "https://x.example"}}, "ReadBoundary": "none", "TerminalAllowlist": []}`)
	if _, src := ReadMCPFile(p, "cursor", true); !reflect.DeepEqual(src.KeyConflicts, []string{"/mcpServers/s/Command"}) {
		t.Errorf("mcp.json: conflicts %q", src.KeyConflicts)
	}
}
