package windows_test

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

func TestReadClaudeSettings_OnlyPermissionKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	writeFile(t, path, `{
		"env": {"API_KEY": "sk-should-never-be-read"},
		"hooks": {"PreToolUse": []},
		"apiKeyHelper": "/bin/helper",
		"disableAutoMode": "disable",
		"permissions": {
			"allow": ["Bash(npm test)"], "ask": ["Bash(git push *)"], "deny": ["Read(./.env)"],
			"defaultMode": "acceptEdits",
			"disableBypassPermissionsMode": "disable",
			"disableAutoMode": "disable",
			"additionalDirectories": ["../docs"],
			"blockReadsOutsideWorkingDirectories": true
		}
	}`)
	f := windows.ReadClaudeSettings(path, platform.MCPScopeUser, "", "/cfg")
	if f.Status != platform.MCPSourceRead {
		t.Fatalf("status %q: %s", f.Status, f.Reason)
	}
	want := platform.ClaudePermissions{
		Allow: []string{"Bash(npm test)"}, Ask: []string{"Bash(git push *)"}, Deny: []string{"Read(./.env)"},
		DefaultMode: "acceptEdits", DisableBypassPermissionsMode: "disable",
		AdditionalDirectories: []string{"../docs"}, BlockReadsOutsideWorkingDirectories: true,
		DisableAutoMode: "disable", DisableAutoModePointer: "/disableAutoMode",
	}
	if !reflect.DeepEqual(f.Permissions, want) {
		t.Errorf("permissions:\n got %+v\nwant %+v", f.Permissions, want)
	}
	if f.ConfigDir != "/cfg" || f.Scope != platform.MCPScopeUser {
		t.Errorf("scope/config dir: %+v", f)
	}
}

func TestReadClaudeSettings_DisableAutoModeUnderPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	writeFile(t, path, `{"permissions": {"disableAutoMode": "disable"}}`)
	p := windows.ReadClaudeSettings(path, platform.MCPScopeUser, "", "/cfg").Permissions
	if p.DisableAutoMode != "disable" || p.DisableAutoModePointer != "/permissions/disableAutoMode" {
		t.Errorf("got %q at %q", p.DisableAutoMode, p.DisableAutoModePointer)
	}
}

func TestReadClaudeSettings_Outcomes(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	writeFile(t, bad, `{"permissions": {"allow": "not a list"}}`)
	big := filepath.Join(dir, "big.json")
	writeFile(t, big, `{"x":"`+strings.Repeat("a", windows.MaxConfigBytes)+`"}`)
	empty := filepath.Join(dir, "empty.json")
	writeFile(t, empty, "  \n")

	cases := []struct {
		path   string
		status platform.MCPSourceStatus
		reason string
	}{
		{filepath.Join(dir, "absent.json"), platform.MCPSourceMissing, ""},
		{bad, platform.MCPSourceFailed, graph.ReasonParseError},
		{big, platform.MCPSourceFailed, graph.ReasonSizeLimitExceeded},
		{empty, platform.MCPSourceRead, ""},
		{dir, platform.MCPSourceFailed, graph.ReasonReadError},
	}
	for _, c := range cases {
		f := windows.ReadClaudeSettings(c.path, platform.MCPScopeUser, "", dir)
		if f.Status != c.status || (c.reason != "" && !strings.HasPrefix(f.Reason, c.reason+": ")) {
			t.Errorf("%s: got %q %q, want %q %q", filepath.Base(c.path), f.Status, f.Reason, c.status, c.reason)
		}
	}
}

func TestListClaudeSettings_LayersConfigDirAndTrust(t *testing.T) {
	e := newIdentityEnv(t)
	custom := filepath.Join(t.TempDir(), "claude-config")
	t.Setenv("CLAUDE_CONFIG_DIR", custom)
	writeFile(t, filepath.Join(custom, "settings.json"), `{"permissions": {"allow": ["Bash(ls)"]}}`)
	writeFile(t, filepath.Join(e.home, ".claude", "settings.json"), `{"permissions": {"allow": ["Bash(ignored)"]}}`)
	writeFile(t, filepath.Join(e.repoA, ".claude", "settings.json"), `{"permissions": {"allow": ["Bash(make)"]}}`)
	writeFile(t, filepath.Join(e.repoA, ".claude", "settings.local.json"), `{"permissions": {"deny": ["Read(x)"]}}`)
	writeFile(t, filepath.Join(e.home, ".claude.json"), `{"projects": {
		"`+filepath.ToSlash(e.repoA)+`": {"hasTrustDialogAccepted": true, "history": ["secret prompt"]},
		"`+filepath.ToSlash(e.repoB)+`": {"hasTrustDialogAccepted": false}
	}, "oauthAccount": {"emailAddress": "owner@example.com"}}`)

	scan, err := windows.New().ListClaudeSettings(context.Background(), []string{e.repoA, e.repoB})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]platform.ClaudeSettingsFile{}
	for _, f := range scan.Files {
		got[f.ConfigPath] = f
		if f.ConfigDir != custom {
			t.Errorf("%s: config dir %q, want CLAUDE_CONFIG_DIR", f.ConfigPath, f.ConfigDir)
		}
	}
	if len(scan.Files) != 5 {
		t.Errorf("files considered: got %d, want 5 (user + 2 per repo)", len(scan.Files))
	}
	user := got[filepath.Join(custom, "settings.json")]
	if user.Scope != platform.MCPScopeUser || !reflect.DeepEqual(user.Permissions.Allow, []string{"Bash(ls)"}) {
		t.Errorf("user settings must come from CLAUDE_CONFIG_DIR: %+v", user)
	}
	if _, read := got[filepath.Join(e.home, ".claude", "settings.json")]; read {
		t.Error("~/.claude/settings.json read although CLAUDE_CONFIG_DIR is set")
	}
	proj := got[filepath.Join(e.repoA, ".claude", "settings.json")]
	local := got[filepath.Join(e.repoA, ".claude", "settings.local.json")]
	if proj.Scope != platform.MCPScopeProject || proj.ScopePath != e.repoA || local.Scope != platform.MCPScopeLocal || local.ScopePath != e.repoA {
		t.Errorf("project/local: %+v / %+v", proj, local)
	}
	wantTrust := []platform.ProjectTrust{{Path: filepath.ToSlash(e.repoA), Accepted: true}, {Path: filepath.ToSlash(e.repoB), Accepted: false}}
	if !reflect.DeepEqual(scan.Trust, wantTrust) {
		t.Errorf("trust: got %+v, want %+v", scan.Trust, wantTrust)
	}
}

func TestListClaudeSettings_NoClaudeJSONMeansNoTrust(t *testing.T) {
	e := newIdentityEnv(t)
	scan, err := windows.New().ListClaudeSettings(context.Background(), []string{e.repoA})
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Trust) != 0 {
		t.Errorf("trust without ~/.claude.json: %+v", scan.Trust)
	}
	for _, f := range scan.Files {
		if f.Status != platform.MCPSourceMissing {
			t.Errorf("%s: %q", f.ConfigPath, f.Status)
		}
	}
}
