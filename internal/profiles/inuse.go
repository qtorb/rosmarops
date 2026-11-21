package profiles

import (
	"encoding/json"

	"github.com/qtorb/rosmarops/internal/platform"
)

// Templates in use (BV-273). A template is in use when the person's user
// settings hold everything it sets: every allow, ask and deny entry, each
// single value and each sandbox key. Entries of their own do not stop it. When
// several match, the strictest wins: «Read only», then «Sandboxed
// development», then «Local development». Only what RosmarOps already reads is
// compared, from the files it already reads.

// strictness is the order in which a match wins: the strictest first.
var strictness = []string{P0, P2, P1}

// Next is the next stricter template for the same use on goos, or "" when
// this one is the strictest for it: «Local development» leads to «Sandboxed
// development», which exists only where the agent has a sandbox (never on
// Windows); «Read only» and «Sandboxed development» are each the strictest of
// their use.
func Next(id, goos string) string {
	if id == P1 && goos != "windows" {
		return P2
	}
	return ""
}

// InUse is the Claude Code template the user settings of s already hold,
// among the measured ones on goos, or "".
func InUse(s platform.ClaudeSettingsScan, goos string) string {
	offered := map[string]bool{}
	for _, id := range MeasuredOn(goos) {
		offered[id] = true
	}
	for _, f := range s.Files {
		if f.Scope != platform.MCPScopeUser || f.Status != platform.MCPSourceRead {
			continue
		}
		for _, id := range strictness {
			if !offered[id] {
				continue
			}
			if p, err := Permissions(id); err == nil && holds(f.Permissions, p) {
				return id
			}
		}
	}
	return ""
}

// holds reports whether have sets everything want sets.
func holds(have, want platform.ClaudePermissions) bool {
	if have.DefaultMode != want.DefaultMode || have.DisableAutoMode != want.DisableAutoMode ||
		have.DisableBypassPermissionsMode != want.DisableBypassPermissionsMode ||
		want.BlockReadsOutsideWorkingDirectories && !have.BlockReadsOutsideWorkingDirectories {
		return false
	}
	if !contains(have.Allow, want.Allow) || !contains(have.Ask, want.Ask) || !contains(have.Deny, want.Deny) {
		return false
	}
	h, w := have.Sandbox, want.Sandbox
	sameBool := func(hv, hs, wv, ws bool) bool { return !ws || hs && hv == wv }
	return sameBool(h.Enabled, h.EnabledSet, w.Enabled, w.EnabledSet) &&
		sameBool(h.FailIfUnavailable, h.FailIfUnavailableSet, w.FailIfUnavailable, w.FailIfUnavailableSet) &&
		sameBool(h.AllowUnsandboxedCommands, h.AllowUnsandboxedCommandsSet, w.AllowUnsandboxedCommands, w.AllowUnsandboxedCommandsSet) &&
		sameBool(h.AutoAllowBash, h.AutoAllowBashSet, w.AutoAllowBash, w.AutoAllowBashSet) &&
		sameBool(h.StrictAllowlist, h.StrictAllowlistSet, w.StrictAllowlist, w.StrictAllowlistSet) &&
		contains(h.AllowedDomains, w.AllowedDomains) && contains(h.DenyRead, w.DenyRead) &&
		contains(h.AllowRead, w.AllowRead) && contains(h.CredentialFiles, w.CredentialFiles)
}

// contains reports whether have holds every entry of want.
func contains(have, want []string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

// grokTemplate is what a Grok Build template sets in ~/.grok/config.toml.
type grokTemplate struct {
	mode, sandbox string
	rules         []platform.GrokRule
}

var grokTemplates = map[string]grokTemplate{
	P0: {mode: "ask", sandbox: "read-only", rules: []platform.GrokRule{
		{Action: "deny", Tool: "edit"},
		{Action: "deny", Tool: "bash"},
		{Action: "deny", Tool: "webfetch"},
		{Action: "deny", Tool: "mcp"},
	}},
	P1: {mode: "ask", sandbox: "workspace", rules: grokP1Rules},
	P2: {mode: "ask", sandbox: "strict", rules: grokP1Rules},
}

// GrokInUse is the Grok Build template the user's config.toml of s already
// holds, among those offered on goos, or "": its permission mode, its sandbox
// profile, every one of its rules, deny = ["WebSearch"] and both hooks off.
func GrokInUse(s platform.GrokScan, goos string) string {
	for _, l := range s.Layers {
		if l.Kind != platform.GrokLayerUser || l.Status != platform.MCPSourceRead {
			continue
		}
		for _, id := range strictness {
			if id == P2 && goos == "windows" {
				continue
			}
			t := grokTemplates[id]
			if l.Yolo || !l.HasPermissionMode || l.PermissionMode != t.mode || l.SandboxProfile != t.sandbox ||
				!contains(l.Deny, []string{"WebSearch"}) || !grokRules(l.Rules, t.rules) {
				continue
			}
			if v, ok := l.Compat["claude.hooks"]; !ok || v {
				continue
			}
			if v, ok := l.Compat["cursor.hooks"]; !ok || v {
				continue
			}
			return id
		}
	}
	return ""
}

func grokRules(have, want []platform.GrokRule) bool {
	set := map[string]bool{}
	for _, r := range have {
		b, _ := json.Marshal(r)
		set[string(b)] = true
	}
	for _, r := range want {
		b, _ := json.Marshal(r)
		if !set[string(b)] {
			return false
		}
	}
	return true
}

// CursorInUse is the Cursor template ~/.cursor/sandbox.json already holds, or
// "": both of Cursor's templates set readBoundary to "workspace", and the rest
// of each is in the app, where RosmarOps does not read; the strictest of the
// two that match is «Read only».
func CursorInUse(s platform.CursorScan) string {
	for _, f := range s.Files {
		if f.Kind == platform.CursorSandbox && f.Scope == platform.CursorScopeUser && f.Status == platform.MCPSourceRead && f.ReadBoundary == "workspace" {
			return P0
		}
	}
	return ""
}
