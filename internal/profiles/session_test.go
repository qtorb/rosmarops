package profiles

import (
	"encoding/json"
	"strings"
	"testing"
)

// BV-249 §8: the command's JSON is valid, with the project's path escaped
// (spaces and quotes), and it is what the shell receives once unquoted.
func TestClaudeSessionCommand(t *testing.T) {
	for _, root := range []string{"/Users/ana/proyecto", `/home/ana/My "quoted" project's dir`, "/srv/a b\\c"} {
		cmd := ClaudeSessionCommand(root)
		if !strings.HasPrefix(cmd, "claude --settings '") || !strings.HasSuffix(cmd, "'") {
			t.Fatalf("%q", cmd)
		}
		// Undo the shell quoting: '...'\''...' -> the JSON.
		arg := strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(cmd, "claude --settings '"), "'"), `'\''`, "'")
		var s claudeSession
		if err := json.Unmarshal([]byte(arg), &s); err != nil {
			t.Fatalf("%s: invalid JSON: %v", root, err)
		}
		if len(s.Sandbox.Filesystem.AllowRead) != 1 || s.Sandbox.Filesystem.AllowRead[0] != root {
			t.Errorf("allowRead %v, want %q", s.Sandbox.Filesystem.AllowRead, root)
		}
		if !s.Sandbox.Enabled || !s.Sandbox.FailIfUnavailable || s.Sandbox.AllowUnsandboxedCommands || s.Sandbox.AutoAllowBashIfSandboxed ||
			!s.Sandbox.Network.StrictAllowlist || len(s.Sandbox.Network.AllowedDomains) != 0 || !s.Permissions.BlockReadsOutsideWorkingDirectories ||
			len(s.Permissions.Allow) != 4 || strings.Join(s.Permissions.Deny, ",") != "WebFetch,WebSearch" {
			t.Errorf("settings %+v", s)
		}
	}
}
