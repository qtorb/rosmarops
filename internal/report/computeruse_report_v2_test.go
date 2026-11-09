package report_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

// TestComputerUseFindingReportV2Validates (BV-202, Albert-signed schema change):
// a report.v2 that carries a computer_use_app_allowed finding must validate and
// be written. Before "computer_use_app_allowed" was added to the findings rule
// enum, RenderV2JSON rejected it (findings.N.rule), so a machine with Computer
// Use enabled produced no v2 report at all — the same symptom as an invalid
// graph. This test drives the real path: Codex Computer Use config → finding →
// BuildV2 → RenderV2JSON (the validate-and-write gate).
func TestComputerUseFindingReportV2Validates(t *testing.T) {
	salt := []byte("0123456789abcdef0123456789abcdef")
	codexDir := "/h/.codex"
	user := platform.CodexLayer{
		Kind: platform.CodexLayerUser, Path: codexDir + "/config.toml", Status: platform.MCPSourceRead,
		ComputerUse: &platform.CodexComputerUse{Allowed: []string{"cmd.exe", "mspaint.exe"}},
	}
	scan := platform.CodexScan{ConfigDir: codexDir, Exists: true, Layers: []platform.CodexLayer{user}}

	mcp := platform.MCPScan{Servers: graph.CodexMCPServers(scan)}
	f, _, err := graph.FromScansWithCodex(mcp, platform.ClaudeSettingsScan{}, platform.CursorScan{}, platform.CredentialFileScan{}, graph.Repos{}, platform.HermesScan{}, scan, salt, v2Now)
	if err != nil {
		t.Fatal(err)
	}
	g, err := graph.Infer(graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(salt)}, v2Now), nil, "/h")
	if err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("ab", 32), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: v2Now, Home: "/h", Codex: scan,
	})

	var found bool
	for _, fnd := range v.Findings {
		if fnd.Rule == "computer_use_app_allowed" {
			found = true
		}
	}
	if !found {
		t.Fatal("no computer_use_app_allowed finding was produced — the fixture is not exercising the rule")
	}

	// The gate: the report validates against report.v2.schema.json and is
	// written. Without the enum value this returns a findings.N.rule error.
	if _, err := report.RenderV2JSON(v); err != nil {
		t.Fatalf("report.v2 with a computer_use_app_allowed finding did not validate: %v", err)
	}
}
