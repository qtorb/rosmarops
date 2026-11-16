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
	P2 = "P2"
	P3 = "P3"
)

// Measured are the profiles the engine measures in a run, in order.
//
// BV-249 §3 (WIP, stopped): P2 «Sandboxed development» is built (Block,
// ProjectBlock, Apply) but not yet measured in a run: it measures 76 yellow B
// on R2, not the signed 99 green B, and report.v2 does not have P2 in its
// enum yet. MeasuredOn adds it only where Claude Code has a sandbox, once
// signed.
var Measured = []string{P0, P1}

// MeasuredOn are the measured profiles on goos (BV-249 §3: P2 only on macOS
// and Linux; on native Windows Claude Code runs commands unsandboxed).
func MeasuredOn(goos string) []string {
	return Measured
}

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

// p2JSON and p2ProjectJSON are CTO-58 §1.1's blocks (BV-249 §3.1): the user's
// is P1's with the sandbox object; the project's keeps the terminal from the
// rest of the home folder («.» is the project in a project's settings).
const p2JSON = `{
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
  },
  "sandbox": {
    "enabled": true,
    "failIfUnavailable": true,
    "allowUnsandboxedCommands": false,
    "network": { "allowedDomains": ["registry.npmjs.org", "github.com"], "strictAllowlist": true },
    "filesystem": { "denyRead": ["~/.ssh", "~/.aws"] },
    "credentials": { "files": [ { "path": "~/.aws/credentials", "mode": "deny" }, { "path": "~/.ssh", "mode": "deny" } ] }
  }
}`

const p2ProjectJSON = `{
  "sandbox": { "filesystem": { "denyRead": ["~/"], "allowRead": ["."] } }
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
	case P2:
		return p2JSON
	case P3:
		return p3Command
	}
	return ""
}

// ProjectBlock is the second text of a profile, for each project's
// .claude/settings.json: P2's; empty for the others.
func ProjectBlock(id string) string {
	if id == P2 {
		return p2ProjectJSON
	}
	return ""
}

type sandboxBlock struct {
	Enabled                  *bool `json:"enabled"`
	FailIfUnavailable        *bool `json:"failIfUnavailable"`
	AllowUnsandboxedCommands *bool `json:"allowUnsandboxedCommands"`
	Network                  struct {
		AllowedDomains []string `json:"allowedDomains"`
	} `json:"network"`
	Filesystem struct {
		DenyRead  []string `json:"denyRead"`
		AllowRead []string `json:"allowRead"`
	} `json:"filesystem"`
}

func (b sandboxBlock) sandbox() platform.ClaudeSandbox {
	var s platform.ClaudeSandbox
	if b.Enabled != nil {
		s.Enabled, s.EnabledSet = *b.Enabled, true
	}
	if b.FailIfUnavailable != nil {
		s.FailIfUnavailable, s.FailIfUnavailableSet = *b.FailIfUnavailable, true
	}
	if b.AllowUnsandboxedCommands != nil {
		s.AllowUnsandboxedCommands, s.AllowUnsandboxedCommandsSet = *b.AllowUnsandboxedCommands, true
	}
	s.AllowedDomains, s.DenyRead, s.AllowRead = b.Network.AllowedDomains, b.Filesystem.DenyRead, b.Filesystem.AllowRead
	return s
}

type block struct {
	Sandbox     sandboxBlock `json:"sandbox"`
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
	if id != P0 && id != P1 && id != P2 {
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
		Allow: p.Allow, Ask: p.Ask, Deny: p.Deny, Sandbox: b.Sandbox.sandbox(),
	}, nil
}

// projectSandbox is the sandbox of a profile's project block.
func projectSandbox(id string) (platform.ClaudeSandbox, bool) {
	if ProjectBlock(id) == "" {
		return platform.ClaudeSandbox{}, false
	}
	var b block
	if err := json.Unmarshal([]byte(ProjectBlock(id)), &b); err != nil {
		return platform.ClaudeSandbox{}, false
	}
	return b.Sandbox.sandbox(), true
}

// mergeSandbox is s with add pasted on it: the keys add sets win, the lists
// add.
func mergeSandbox(s, add platform.ClaudeSandbox) platform.ClaudeSandbox {
	if add.EnabledSet {
		s.Enabled, s.EnabledSet = add.Enabled, true
	}
	if add.FailIfUnavailableSet {
		s.FailIfUnavailable, s.FailIfUnavailableSet = add.FailIfUnavailable, true
	}
	if add.AllowUnsandboxedCommandsSet {
		s.AllowUnsandboxedCommands, s.AllowUnsandboxedCommandsSet = add.AllowUnsandboxedCommands, true
	}
	s.AllowedDomains = append(append([]string(nil), s.AllowedDomains...), add.AllowedDomains...)
	s.DenyRead = append(append([]string(nil), s.DenyRead...), add.DenyRead...)
	s.AllowRead = append(append([]string(nil), s.AllowRead...), add.AllowRead...)
	return s
}

// applyProject adds a profile's project block to the .claude/settings.json of
// every project in roots, as the person pastes it in each one (BV-249 §3.1).
func applyProject(out platform.ClaudeSettingsScan, id string, roots []string) platform.ClaudeSettingsScan {
	sb, ok := projectSandbox(id)
	if !ok {
		return out
	}
	// A project file's ConfigDir is Claude Code's configuration folder, as
	// the reader sets it (the same agent), not the project's .claude.
	configDir := ""
	for _, f := range out.Files {
		if f.ConfigDir != "" {
			configDir = f.ConfigDir
			break
		}
	}
	for _, root := range roots {
		done := false
		for i, f := range out.Files {
			if f.Scope == platform.MCPScopeProject && f.Status == platform.MCPSourceRead && filepath.Clean(f.ScopePath) == filepath.Clean(root) &&
				filepath.Base(f.ConfigPath) == "settings.json" {
				f.Permissions.Sandbox = mergeSandbox(f.Permissions.Sandbox, sb)
				out.Files[i], done = f, true
				break
			}
		}
		if !done {
			out.Files = append(out.Files, platform.ClaudeSettingsFile{
				Scope: platform.MCPScopeProject, ScopePath: root, ConfigPath: filepath.Join(root, ".claude", "settings.json"),
				ConfigDir: configDir, Status: platform.MCPSourceRead,
				Permissions: platform.ClaudePermissions{Sandbox: sb},
			})
		}
	}
	return out
}

// Apply is s with the profile's block added to Claude Code's user settings,
// as Claude Code merges a pasted block: the lists add, the single values are
// the profile's (CTO-50 §1, «Lists merge instead of overriding»). With no
// user settings file yet, one is added in Claude Code's configuration folder.
// ok is false when the scan names no such folder (nothing to apply it to).
//
// A profile with a project block (P2) adds it to every project in roots.
func Apply(s platform.ClaudeSettingsScan, id string, roots ...string) (platform.ClaudeSettingsScan, bool) {
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
		q.Sandbox = mergeSandbox(q.Sandbox, p.Sandbox)
		f.Permissions = q
		out.Files[i] = f
		return applyProject(out, id, roots), true
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
	return applyProject(out, id, roots), true
}
