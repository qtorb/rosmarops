package graph

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
)

func boolRef(b bool) *bool { return &b }

// BV-274: enableAllProjectMcpServers: true in the user's settings and in a
// project's, with the precedence approvalState uses.
func TestProjectAutoApprovals(t *testing.T) {
	file := func(scope, path, root string, v *bool) platform.ClaudeSettingsFile {
		return platform.ClaudeSettingsFile{Scope: scope, ConfigPath: path, ScopePath: root, Status: platform.MCPSourceRead, EnableAllProjectMcpServers: v}
	}
	paths := func(s platform.ClaudeSettingsScan) string {
		var out []string
		for _, a := range ProjectAutoApprovals(s) {
			out = append(out, a.Scope+":"+a.ConfigPath)
		}
		return strings.Join(out, ",")
	}
	trust := []platform.ProjectTrust{{Path: "/r/ok", Accepted: true}, {Path: "/r/local", Accepted: true}}
	cases := []struct {
		name string
		scan platform.ClaudeSettingsScan
		want string
	}{
		{"user true", platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{file(platform.MCPScopeUser, "/u/settings.json", "", boolRef(true))}}, "user:/u/settings.json"},
		{"user false", platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{file(platform.MCPScopeUser, "/u/settings.json", "", boolRef(false))}}, ""},
		{"unset", platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{file(platform.MCPScopeUser, "/u/settings.json", "", nil)}}, ""},
		{
			"project true, trusted folder",
			platform.ClaudeSettingsScan{Trust: trust, Files: []platform.ClaudeSettingsFile{file(platform.MCPScopeProject, "/r/ok/.claude/settings.json", "/r/ok", boolRef(true))}},
			"project:/r/ok/.claude/settings.json",
		},
		{
			"project true, untrusted folder",
			platform.ClaudeSettingsScan{Trust: trust, Files: []platform.ClaudeSettingsFile{file(platform.MCPScopeProject, "/r/no/.claude/settings.json", "/r/no", boolRef(true))}},
			"",
		},
		{
			"local false over project true",
			platform.ClaudeSettingsScan{Trust: trust, Files: []platform.ClaudeSettingsFile{
				file(platform.MCPScopeProject, "/r/local/.claude/settings.json", "/r/local", boolRef(true)),
				file(platform.MCPScopeLocal, "/r/local/.claude/settings.local.json", "/r/local", boolRef(false)),
			}},
			"",
		},
		{
			"local true in a trusted folder",
			platform.ClaudeSettingsScan{Trust: trust, Files: []platform.ClaudeSettingsFile{file(platform.MCPScopeLocal, "/r/local/.claude/settings.local.json", "/r/local", boolRef(true))}},
			"local:/r/local/.claude/settings.local.json",
		},
		{
			"managed true",
			platform.ClaudeSettingsScan{Managed: []platform.ManagedSource{{Kind: platform.ManagedFiles, Files: []platform.ClaudeSettingsFile{{
				ConfigPath: "/etc/claude-code/managed-settings.json", Status: platform.MCPSourceRead, HasPolicyKey: true, EnableAllProjectMcpServers: boolRef(true),
			}}}}},
			"managed:/etc/claude-code/managed-settings.json",
		},
	}
	for _, c := range cases {
		if got := paths(c.scan); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
