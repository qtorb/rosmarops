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
	"strings"

	"github.com/qtorb/rosmarops/internal/platform"
)

// The profiles' ids, as report.v2 names them.
const (
	P0 = "P0"
	P1 = "P1"
	P2 = "P2"
	P3 = "P3"
)

// Measured are the profiles the engine measures, in order; P2 «Sandboxed
// development» only where Claude Code has a sandbox (MeasuredOn).
var Measured = []string{P0, P1, P2}

// MeasuredOn are the measured profiles on goos (BV-249 §3): P2 only on macOS
// and Linux; on native Windows Claude Code runs commands unsandboxed.
func MeasuredOn(goos string) []string {
	if goos == "windows" {
		return []string{P0, P1}
	}
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

// p2JSON is CTO-58 §1.1's user block (BV-249 §3.1): P1's with the sandbox
// object. It has no project block since BV-254 (CTO-68): with
// blockReadsOutsideWorkingDirectories, Claude Code ignores a repository's
// allowRead, so that block kept the terminal out of the project itself.
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
    "autoAllowBashIfSandboxed": false,
    "network": { "allowedDomains": ["registry.npmjs.org", "github.com"], "strictAllowlist": true },
    "filesystem": { "denyRead": ["~/.ssh", "~/.aws"] },
    "credentials": { "files": [ { "path": "~/.aws/credentials", "mode": "deny" }, { "path": "~/.ssh", "mode": "deny" } ] }
  }
}`

// p3Command is CTO-50 §1's launch command, literal.
const p3Command = `claude -p "<tarea>" \
  --permission-mode dontAsk \
  --strict-mcp-config --mcp-config '{"mcpServers":{}}' \
  --settings '{"disableAllHooks": true, "permissions": {"allow": ["Bash(npm run lint)", "Bash(npm run test *)", "Bash(git status)", "Bash(git diff)"], "deny": ["WebFetch", "WebSearch"]}}'`

// taskPlaceholder is the launch commands' placeholder for the task, as their
// literal text writes it; Placeholder gives it in the report's language.
const taskPlaceholder = "<tarea>"

// Placeholder is a launch command (P3) in lang. The command is CTO-50's; its
// task placeholder is text for the person, so it follows their language, as
// the steps around it do (cc.S6, gp.grok.P3.step.2): «<tarea>» in Spanish,
// «<task>» in every other language. Nothing substitutes it: the person does.
func Placeholder(command, lang string) string {
	if lang == "es" {
		return command
	}
	return strings.ReplaceAll(command, taskPlaceholder, "<task>")
}

// Block is the text of a profile as the report copies it: the JSON block of
// P0 or P1, or P3's command (with its Spanish placeholder: see Placeholder).
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

type sandboxBlock struct {
	Enabled                  *bool `json:"enabled"`
	FailIfUnavailable        *bool `json:"failIfUnavailable"`
	AllowUnsandboxedCommands *bool `json:"allowUnsandboxedCommands"`
	AutoAllowBashIfSandboxed *bool `json:"autoAllowBashIfSandboxed"`
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
	if b.AutoAllowBashIfSandboxed != nil {
		s.AutoAllowBash, s.AutoAllowBashSet = *b.AutoAllowBashIfSandboxed, true
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
	if add.AutoAllowBashSet {
		s.AutoAllowBash, s.AutoAllowBashSet = add.AutoAllowBash, true
	}
	s.AllowedDomains = append(append([]string(nil), s.AllowedDomains...), add.AllowedDomains...)
	s.DenyRead = append(append([]string(nil), s.DenyRead...), add.DenyRead...)
	s.AllowRead = append(append([]string(nil), s.AllowRead...), add.AllowRead...)
	return s
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
		q.Sandbox = mergeSandbox(q.Sandbox, p.Sandbox)
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
