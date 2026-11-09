package schemaval_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-203 point 4 (CTO-24 E6): regenerate the two fixtures that cannot be copied
// from a committed source — the space view and the all-node-types graph — with
// the real builders, so the differential oracle and TestGraphFixtureCoversEvery
// NodeType run against real engine output. Guarded by an env var so a normal
// run never rewrites them:
//
//	ROSMAROPS_REGEN_FIXTURES=1 go test ./internal/schemaval/ -run TestRegenFixtures
//
// The other new fixtures (intel.v1, kit.v1, results.v1, findings-memory.v3) are
// copied from committed sources (internal/intel/data, testdata/cross) or, for
// findings-memory.v3, the minimal valid v3 document.
func TestRegenFixtures(t *testing.T) {
	if os.Getenv("ROSMAROPS_REGEN_FIXTURES") != "1" {
		t.Skip("set ROSMAROPS_REGEN_FIXTURES=1 to rewrite the generated fixtures")
	}
	dir := filepath.Join("testdata", "valid")

	if err := os.WriteFile(filepath.Join(dir, "space-view.v1.json"), regenSpaceView(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.v2.all-node-types.json"), everyNodeTypeGraphJSON(t), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Log("regenerated space-view.v1.json and graph.v2.all-node-types.json")
}

// regenSpaceView builds a one-agent machine through the real pipeline and
// returns its space view JSON (validated against space-view.v1 by
// RenderSpaceViewJSON).
func regenSpaceView(t *testing.T) []byte {
	t.Helper()
	const (
		home   = `/home/fixture`
		repo   = `/home/fixture/src/app`
		claude = home + `/.claude`
	)
	salt := []byte("0123456789abcdef0123456789abcdef")
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	settings := platform.ClaudeSettingsScan{
		Files: []platform.ClaudeSettingsFile{{Scope: platform.MCPScopeUser, ConfigPath: claude + "/settings.json", ConfigDir: claude, Status: platform.MCPSourceRead}},
		Trust: []platform.ProjectTrust{{Path: repo, Accepted: true}},
	}
	creds := platform.CredentialFileScan{Entries: []platform.CredentialEntry{
		{Path: home + "/.aws/credentials", Line: 1, Key: "AWS_ACCESS_KEY_ID", Value: secrets.NewSecretValue("AKIAABCDEFGHIJ123456")},
	}}
	res, err := pipeline.Assemble(pipeline.Input{
		Settings:     settings,
		CredFiles:    creds,
		Instructions: platform.InstructionScan{Files: []platform.InstructionFile{{Product: "claude-code", ConfigDir: claude, Path: repo + "/AGENTS.md", Shared: true}}},
		Provenance:   graph.Provenance{EngineVersion: "0.10.3-fixture", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt)},
		Salt:         salt,
		RepoRoots:    []string{repo},
		Home:         home,
		Now:          now,
	})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	v1 := report.Build("0.10.3-fixture", nil, nil, 1, 0, 0, 0, nil)
	v2, _ := report.BuildV2(report.V2Input{
		Graph: &res.Graph, GraphSHA256: fixtureSHA, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: []string{repo}, V1: v1, GeneratedAt: now, Salt: salt,
		ClaudeSettings: &settings, Home: home,
	})
	sv, err := report.BuildSpaceView(v2, &res.Graph, home, "0.10.3-fixture")
	if err != nil {
		t.Fatalf("BuildSpaceView: %v", err)
	}
	data, err := report.RenderSpaceViewJSON(sv)
	if err != nil {
		t.Fatalf("RenderSpaceViewJSON: %v", err)
	}
	return data
}

// fixtureSHA is a fixed 64-hex graph hash for the fixture (the report records
// the hash of the graph file; its exact value does not matter here).
const fixtureSHA = "0000000000000000000000000000000000000000000000000000000000000000"
