package trust_test

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// scenarioV2 builds the v2 report of a machine: Claude Code with user
// settings under home (the built-in auto mode), the credential file
// entries given, repo as the repository, salt as the local salt, at day.
func scenarioV2(t *testing.T, salt []byte, home, repo string, entries []platform.CredentialEntry, day time.Time) report.V2 {
	t.Helper()
	claudeDir := home + "/.claude"
	settings := platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{{
		Scope: platform.MCPScopeUser, ConfigPath: claudeDir + "/settings.json", ConfigDir: claudeDir,
		Status: platform.MCPSourceRead, Permissions: platform.ClaudePermissions{Allow: []string{"Bash(npm test)"}},
	}}}
	frag, _, err := graph.FromScans(platform.MCPScan{}, settings, platform.CredentialFileScan{Entries: entries}, salt, day)
	if err != nil {
		t.Fatal(err)
	}
	g, err := graph.Infer(graph.Build(frag, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt)}, day), []string{repo}, home)
	if err != nil {
		t.Fatal(err)
	}
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("0", 64), Base: base, RepoRoots: []string{repo},
		V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: day,
	})
	return v
}

func credEntry(path string, line int, key, value string) platform.CredentialEntry {
	return platform.CredentialEntry{Path: path, Line: line, Key: key, Value: secrets.NewSecretValue(value)}
}
