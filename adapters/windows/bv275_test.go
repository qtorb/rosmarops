package windows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-275: the Cursor and Grok Build files RosmarOps now reads, each with a
// fake file.

func cursorFileOf(scan platform.CursorScan, kind, scope string) platform.CursorFile {
	for _, f := range scan.Files {
		if f.Kind == kind && f.Scope == scope {
			return f
		}
	}
	return platform.CursorFile{}
}

func TestReadCursor_CLISettings(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	writeFileCodex(t, filepath.Join(home, ".cursor", "cli-config.json"), `{
  "version": 1,
  "permissions": {"allow": ["Shell(ls)"], "deny": []},
  "approvalMode": "unrestricted",
  "sandbox": {"mode": "disabled", "networkAccess": "allow-all", "readBoundary": "system"}
}`)
	// A project's cli.json holds only permissions: its approvalMode is not
	// the CLI's.
	writeFileCodex(t, filepath.Join(repo, ".cursor", "cli.json"), `{"permissions": {"allow": [], "deny": []}, "approvalMode": "unrestricted"}`)
	scan := ReadCursor(home, "", []string{repo})
	f := cursorFileOf(scan, platform.CursorCLI, platform.CursorScopeUser)
	if f.Status != platform.MCPSourceRead || f.CLIApprovalMode != "unrestricted" || f.CLISandboxMode != "disabled" || f.CLINetworkAccess != "allow-all" {
		t.Fatalf("user cli-config.json: %+v", f)
	}
	if p := cursorFileOf(scan, platform.CursorCLI, platform.CursorScopeProject); p.CLIApprovalMode != "" || p.CLISandboxMode != "" {
		t.Errorf("project cli.json kept the CLI's own settings: %+v", p)
	}
}

func TestReadCursor_CLISettingsUnknownValues(t *testing.T) {
	home := t.TempDir()
	writeFileCodex(t, filepath.Join(home, ".cursor", "cli-config.json"), `{
  "approvalMode": "yolo",
  "sandbox": {"mode": true, "networkAccess": 3}
}`)
	f := cursorFileOf(ReadCursor(home, "", nil), platform.CursorCLI, platform.CursorScopeUser)
	if f.Status != platform.MCPSourceRead {
		t.Fatalf("a value of another type failed the file: %+v", f)
	}
	if f.CLIApprovalMode != "" || f.CLISandboxMode != "" || f.CLINetworkAccess != "" {
		t.Errorf("unknown values kept: %+v", f)
	}
}

func TestReadCursor_NetworkPolicy(t *testing.T) {
	for body, want := range map[string]string{
		`{"networkPolicy": {"default": "allow"}}`:                       "allow",
		`{"networkPolicy": {"default": "deny", "allow": ["pypi.org"]}}`: "deny",
		`{"networkPolicy": {"allow": ["registry.npmjs.org"]}}`:          "",
		`{"readBoundary": "workspace"}`:                                 "",
		`{"networkPolicy": {"default": "maybe"}}`:                       "",
	} {
		home := t.TempDir()
		writeFileCodex(t, filepath.Join(home, ".cursor", "sandbox.json"), body)
		f := cursorFileOf(ReadCursor(home, "", nil), platform.CursorSandbox, platform.CursorScopeUser)
		if f.Status != platform.MCPSourceRead || f.NetworkDefault != want {
			t.Errorf("%s: network default %q, want %q (%s)", body, f.NetworkDefault, want, f.Status)
		}
	}
}

func TestReadCursor_TeamHooks(t *testing.T) {
	home := t.TempDir()
	if got := ReadCursor(home, "", nil).TeamHooks; len(got) != 0 {
		t.Fatalf("no team hooks folder: %v", got)
	}
	hooks := filepath.Join(home, ".cursor", "managed", "team_42", "hooks")
	writeFileCodex(t, filepath.Join(hooks, "audit.ps1"), "SECRET-SCRIPT-BODY")
	writeFileCodex(t, filepath.Join(hooks, "guard.sh"), "SECRET-SCRIPT-BODY")
	// Not a team folder, and a sub-folder: neither is listed.
	writeFileCodex(t, filepath.Join(home, ".cursor", "managed", "other", "hooks", "x.sh"), "x")
	if err := os.MkdirAll(filepath.Join(hooks, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := ReadCursor(home, "", nil).TeamHooks
	if len(got) != 2 || !strings.HasSuffix(got[0], "audit.ps1") || !strings.HasSuffix(got[1], "guard.sh") {
		t.Errorf("team hooks: %v", got)
	}
}

func TestReadGrok_SystemLayersInOrder(t *testing.T) {
	home := t.TempDir()
	system := filepath.Join(home, "etc-grok")
	grok := filepath.Join(home, ".grok")
	writeFileCodex(t, filepath.Join(grok, "config.toml"), "[sandbox]\nprofile = \"off\"\n")
	writeFileCodex(t, filepath.Join(system, "managed_config.toml"), "[sandbox]\nprofile = \"workspace\"\n")
	writeFileCodex(t, filepath.Join(system, "requirements.toml"), "[sandbox]\nprofile = \"strict\"\n")
	scan := ReadGrokWith(home, system, envOf(nil), nil)
	var order []string
	for _, l := range scan.Layers {
		tag := l.Kind
		if l.System {
			tag = "system:" + tag
		}
		order = append(order, tag)
	}
	want := "system:managed,managed,user,requirements,system:requirements"
	if strings.Join(order, ",") != want {
		t.Errorf("layers %v, want %s", order, want)
	}
	// Without a system folder (Windows), no /etc/grok layer.
	for _, l := range ReadGrok(home, envOf(nil), nil).Layers {
		if l.System {
			t.Errorf("system layer read without a system folder: %+v", l)
		}
	}
}

func TestReadGrok_Hooks(t *testing.T) {
	home := t.TempDir()
	grok := filepath.Join(home, ".grok")
	trusted, untrusted := filepath.Join(home, "ok"), filepath.Join(home, "no")
	writeFileCodex(t, filepath.Join(grok, "config.toml"), "")
	writeFileCodex(t, filepath.Join(grok, "trusted_folders.toml"), "[folders.'"+trusted+"']\ntrusted = true\n")
	writeFileCodex(t, filepath.Join(grok, "hooks", "safety.json"), `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "bin/check.sh"}]}]}}`)
	writeFileCodex(t, filepath.Join(grok, "hooks", "notes.txt"), "not a hooks file")
	writeFileCodex(t, filepath.Join(grok, "hooks", "broken.json"), `{`)
	writeFileCodex(t, filepath.Join(trusted, ".grok", "hooks", "p.json"), `{"hooks": {}}`)
	writeFileCodex(t, filepath.Join(untrusted, ".grok", "hooks", "p.json"), `{"hooks": {}}`)
	scan := ReadGrok(home, envOf(nil), []string{trusted, untrusted})
	got := map[string]platform.GrokHookFile{}
	for _, h := range scan.Hooks {
		rel, _ := filepath.Rel(home, h.Path)
		got[filepath.ToSlash(rel)] = h
	}
	if len(got) != 4 {
		t.Fatalf("hooks files: %+v", got)
	}
	if h := got[".grok/hooks/safety.json"]; h.Status != platform.MCPSourceRead || !h.Trusted {
		t.Errorf("user hooks: %+v", h)
	}
	if h := got[".grok/hooks/broken.json"]; h.Status != platform.MCPSourceFailed {
		t.Errorf("broken hooks: %+v", h)
	}
	if h := got["ok/.grok/hooks/p.json"]; !h.Trusted || h.Root != trusted {
		t.Errorf("trusted project hooks: %+v", h)
	}
	if h := got["no/.grok/hooks/p.json"]; h.Trusted {
		t.Errorf("untrusted project hooks marked trusted: %+v", h)
	}
}

func TestReadGrok_SandboxToml(t *testing.T) {
	home := t.TempDir()
	grok := filepath.Join(home, ".grok")
	writeFileCodex(t, filepath.Join(grok, "config.toml"), "[sandbox]\nprofile = \"project\"\n")
	writeFileCodex(t, filepath.Join(grok, "sandbox.toml"), `[profiles.project]
extends = "strict"
restrict_network = false
read_write = ["/tmp/scratch"]
deny = ["**/.env"]

[profiles.bare]
`)
	scan := ReadGrok(home, envOf(nil), nil)
	if len(scan.Sandboxes) != 1 || scan.Sandboxes[0].Status != platform.MCPSourceRead {
		t.Fatalf("sandbox.toml: %+v", scan.Sandboxes)
	}
	p := scan.Sandboxes[0].Profiles["project"]
	if p.Extends != "strict" || p.RestrictNetwork == nil || *p.RestrictNetwork || len(p.ReadWrite) != 1 || len(p.Deny) != 1 {
		t.Errorf("profile project: %+v", p)
	}
	if b := scan.Sandboxes[0].Profiles["bare"]; b.Extends != "workspace" || b.RestrictNetwork != nil {
		t.Errorf("profile bare: %+v", b)
	}
}

// The compat cells follow the official docs: Cursor and Claude, five
// surfaces each; codex and sessions are not read.
func TestReadGrok_CompatCells(t *testing.T) {
	home := t.TempDir()
	writeFileCodex(t, filepath.Join(home, ".grok", "config.toml"), `[compat.cursor]
hooks = false
agents = false
sessions = false
[compat.codex]
hooks = false
`)
	user, _ := grokLayerByKindRoot(ReadGrok(home, envOf(nil), nil), platform.GrokLayerUser, "")
	if len(user.Compat) != 2 || user.Compat["cursor.hooks"] || user.Compat["cursor.agents"] {
		t.Errorf("compat: %+v", user.Compat)
	}
}
