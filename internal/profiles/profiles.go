// Package profiles holds the Claude Code profiles of CTO-50 §1 (signed by
// Albert 14-11), as the report gives them to the person to paste: P0 Read
// only and P1 Local development, JSON blocks for ~/.claude/settings.json, and
// P3 Unattended, a launch command. The engine measures P0 and P1 by adding
// their block to this computer's Claude Code user settings (BV-236 §4); P3 is a
// launch command and is not measured.
package profiles

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/qtorb/rosmarops/internal/platform"
)

// The profiles' ids, as report.v2 names them.
const (
	P0 = "P0"
	P1 = "P1"
	P3 = "P3"
)

// Measured are the profiles the engine measures, in order.
var Measured = []string{P0, P1}

// p0JSON and p1JSON are CTO-50 §1's blocks, literal.
const p0JSON = `{
  "permissions": {
    "defaultMode": "plan",
    "disableAutoMode": "disable",
    "disableBypassPermissionsMode": "disable",
    "blockReadsOutsideWorkingDirectories": true,
    "deny": ["Edit", "Bash", "WebFetch", "WebSearch", "mcp__*"]
  }
}`

const p1JSON = `{
  "permissions": {
    "defaultMode": "acceptEdits",
    "disableAutoMode": "disable",
    "disableBypassPermissionsMode": "disable",
    "blockReadsOutsideWorkingDirectories": true,
    "allow": [
      "Bash(npm run lint)", "Bash(npm run test *)",
      "Bash(git status)", "Bash(git diff)"
    ],
    "ask": [
      "Edit(~/.codex/**)", "Edit(**/.codex/**)",
      "Edit(~/.cursor/**)", "Edit(**/.cursor/**)",
      "Edit(~/.cline/**)", "Edit(**/.clinerules/**)",
      "Edit(~/.gemini/**)", "Edit(**/.gemini/**)",
      "Edit(~/.grok/**)", "Edit(**/.grok/**)", "Edit(~/.hermes/**)",
      "Edit(~/AppData/Roaming/**)",
      "Edit(**/AGENTS.md)", "Edit(**/CLAUDE.md)", "Edit(**/GEMINI.md)",
      "Edit(**/.github/copilot-instructions.md)"
    ],
    "deny": ["WebFetch", "WebSearch", "Bash(curl *)", "Bash(wget *)"]
  }
}`

// p3Command is CTO-50 §1's launch command, literal.
const p3Command = `claude -p "<tarea>" \
  --permission-mode dontAsk \
  --strict-mcp-config --mcp-config '{"mcpServers":{}}' \
  --settings '{"disableAllHooks": true, "permissions": {"allow": ["Bash(npm run lint)", "Bash(npm run test *)", "Bash(git status)", "Bash(git diff)"], "deny": ["WebFetch", "WebSearch"]}}'`

// Block is the text of a profile as the report copies it: the JSON block of
// P0 or P1, or P3's command.
func Block(id string) string {
	switch id {
	case P0:
		return p0JSON
	case P1:
		return p1JSON
	case P3:
		return p3Command
	}
	return ""
}

type block struct {
	Permissions struct {
		DefaultMode                         string   `json:"defaultMode"`
		DisableAutoMode                     string   `json:"disableAutoMode"`
		DisableBypassPermissionsMode        string   `json:"disableBypassPermissionsMode"`
		BlockReadsOutsideWorkingDirectories bool     `json:"blockReadsOutsideWorkingDirectories"`
		Allow                               []string `json:"allow"`
		Ask                                 []string `json:"ask"`
		Deny                                []string `json:"deny"`
	} `json:"permissions"`
}

// Permissions are the permissions of a measured profile's block.
func Permissions(id string) (platform.ClaudePermissions, error) {
	if id != P0 && id != P1 {
		return platform.ClaudePermissions{}, fmt.Errorf("profile %s is not measured", id)
	}
	var b block
	if err := json.Unmarshal([]byte(Block(id)), &b); err != nil {
		return platform.ClaudePermissions{}, fmt.Errorf("profile %s: %w", id, err)
	}
	p := b.Permissions
	return platform.ClaudePermissions{
		DefaultMode: p.DefaultMode, DisableAutoMode: p.DisableAutoMode, DisableAutoModePointer: "/permissions/disableAutoMode",
		DisableBypassPermissionsMode: p.DisableBypassPermissionsMode, BlockReadsOutsideWorkingDirectories: p.BlockReadsOutsideWorkingDirectories,
		Allow: p.Allow, Ask: p.Ask, Deny: p.Deny,
	}, nil
}

// Apply is s with the profile's block added to Claude Code's user settings,
// as Claude Code merges a pasted block: the lists add, the single values are
// the profile's (CTO-50 §1, «Lists merge instead of overriding»). With no
// user settings file yet, one is added in Claude Code's configuration folder.
// ok is false when the scan names no such folder (nothing to apply it to).
func Apply(s platform.ClaudeSettingsScan, id string) (platform.ClaudeSettingsScan, bool) {
	p, err := Permissions(id)
	if err != nil {
		return s, false
	}
	out := s
	out.Files = append([]platform.ClaudeSettingsFile(nil), s.Files...)
	for i, f := range out.Files {
		if f.Scope != platform.MCPScopeUser || f.Status != platform.MCPSourceRead {
			continue
		}
		q := f.Permissions
		q.Allow = append(append([]string(nil), q.Allow...), p.Allow...)
		q.Ask = append(append([]string(nil), q.Ask...), p.Ask...)
		q.Deny = append(append([]string(nil), q.Deny...), p.Deny...)
		q.DefaultMode, q.DisableAutoMode, q.DisableAutoModePointer = p.DefaultMode, p.DisableAutoMode, p.DisableAutoModePointer
		q.DisableBypassPermissionsMode, q.BlockReadsOutsideWorkingDirectories = p.DisableBypassPermissionsMode, p.BlockReadsOutsideWorkingDirectories
		f.Permissions = q
		out.Files[i] = f
		return out, true
	}
	configDir := ""
	for _, f := range s.Files {
		if f.ConfigDir != "" {
			configDir = f.ConfigDir
			break
		}
	}
	if configDir == "" {
		return s, false
	}
	out.Files = append(out.Files, platform.ClaudeSettingsFile{
		Scope: platform.MCPScopeUser, ConfigPath: filepath.Join(configDir, "settings.json"), ConfigDir: configDir,
		Status: platform.MCPSourceRead, Permissions: p,
	})
	return out, true
}
