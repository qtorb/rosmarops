package profiles

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
)

// userSettings is a Claude Code scan whose user settings hold id's block, with
// the person's own entries added.
func userSettings(t *testing.T, id string, extra func(*platform.ClaudePermissions)) platform.ClaudeSettingsScan {
	t.Helper()
	p, err := Permissions(id)
	if err != nil {
		t.Fatal(err)
	}
	if extra != nil {
		extra(&p)
	}
	return platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{{
		Scope: platform.MCPScopeUser, Status: platform.MCPSourceRead, Permissions: p,
	}}}
}

// BV-273: «Local development» applied, with entries of the person's own, is
// in use; «Sandboxed development» is the next one only where it exists.
func TestInUseWithExtraEntries(t *testing.T) {
	s := userSettings(t, P1, func(p *platform.ClaudePermissions) {
		p.Allow = append([]string{"Bash(make build)"}, p.Allow...)
		p.Deny = append(p.Deny, "Bash(rm -rf *)")
	})
	for _, goos := range []string{"windows", "linux", "darwin"} {
		if got := InUse(s, goos); got != P1 {
			t.Errorf("%s: in use %q, want P1", goos, got)
		}
	}
	if Next(P1, "windows") != "" {
		t.Error("windows: «Sandboxed development» offered as the next one")
	}
	if Next(P1, "linux") != P2 || Next(P2, "linux") != "" || Next(P0, "darwin") != "" {
		t.Error("next: P1 → P2 → none, P0 → none")
	}
}

// A setting that differs, or an entry missing, is not the template.
func TestInUseNeedsEverything(t *testing.T) {
	for name, mod := range map[string]func(*platform.ClaudePermissions){
		"mode":    func(p *platform.ClaudePermissions) { p.DefaultMode = "default" },
		"deny":    func(p *platform.ClaudePermissions) { p.Deny = p.Deny[1:] },
		"ask":     func(p *platform.ClaudePermissions) { p.Ask = nil },
		"reads":   func(p *platform.ClaudePermissions) { p.BlockReadsOutsideWorkingDirectories = false },
		"no auto": func(p *platform.ClaudePermissions) { p.DisableAutoMode = "" },
	} {
		if got := InUse(userSettings(t, P1, mod), "linux"); got != "" {
			t.Errorf("%s: in use %q, want none", name, got)
		}
	}
	// Not the user settings: a project's file does not count.
	s := userSettings(t, P1, nil)
	s.Files[0].Scope = platform.MCPScopeProject
	if got := InUse(s, "linux"); got != "" {
		t.Errorf("project settings: in use %q", got)
	}
}

// «Sandboxed development» holds «Local development» too: the strictest wins
// where it is offered, and on Windows, where it is not, «Local development».
func TestInUseStrictest(t *testing.T) {
	s := userSettings(t, P2, nil)
	if got := InUse(s, "linux"); got != P2 {
		t.Errorf("linux: %q, want P2", got)
	}
	if got := InUse(s, "windows"); got != P1 {
		t.Errorf("windows: %q, want P1 (P2 is not offered there)", got)
	}
	// Without the network allowlist key, the sandbox is not the template's.
	s = userSettings(t, P2, func(p *platform.ClaudePermissions) { p.Sandbox.StrictAllowlistSet = false })
	if got := InUse(s, "linux"); got != P1 {
		t.Errorf("no strictAllowlist: %q, want P1", got)
	}
	if got := InUse(userSettings(t, P0, nil), "windows"); got != P0 {
		t.Errorf("read only: %q", got)
	}
}

func grokUser(mode, sandbox string, rules []platform.GrokRule, hooksOff bool) platform.GrokScan {
	return platform.GrokScan{Layers: []platform.GrokLayer{{
		Kind: platform.GrokLayerUser, Status: platform.MCPSourceRead,
		PermissionMode: mode, HasPermissionMode: true, SandboxProfile: sandbox,
		Rules: rules, Deny: []string{"WebSearch"},
		Compat: map[string]bool{"claude.hooks": !hooksOff, "cursor.hooks": !hooksOff},
	}}}
}

func TestGrokInUse(t *testing.T) {
	extra := append([]platform.GrokRule{{Action: "allow", Tool: "bash", Pattern: "make"}}, grokP1Rules...)
	if got := GrokInUse(grokUser("ask", "workspace", extra, true), "linux"); got != P1 {
		t.Errorf("P1 with an extra rule: %q", got)
	}
	if got := GrokInUse(grokUser("ask", "strict", grokP1Rules, true), "linux"); got != P2 {
		t.Errorf("P2: %q", got)
	}
	if got := GrokInUse(grokUser("ask", "strict", grokP1Rules, true), "windows"); got != "" {
		t.Errorf("P2 on windows: %q, want none", got)
	}
	if got := GrokInUse(grokUser("ask", "workspace", grokP1Rules, false), "linux"); got != "" {
		t.Errorf("hooks on: %q, want none", got)
	}
	if got := GrokInUse(grokUser("ask", "workspace", grokP1Rules[1:], true), "linux"); got != "" {
		t.Errorf("a rule missing: %q, want none", got)
	}
}

func TestCursorInUse(t *testing.T) {
	s := platform.CursorScan{Files: []platform.CursorFile{{Kind: platform.CursorSandbox, Scope: platform.CursorScopeUser, Status: platform.MCPSourceRead, ReadBoundary: "workspace"}}}
	if got := CursorInUse(s); got != P0 {
		t.Errorf("readBoundary workspace: %q, want P0", got)
	}
	s.Files[0].ReadBoundary = ""
	if got := CursorInUse(s); got != "" {
		t.Errorf("no readBoundary: %q", got)
	}
}
