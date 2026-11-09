package report_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// TestReportV2EveryNotEvaluatedReasonValidates (BV-202, CTO-28): a report.v2
// whose agents_overview carries an agent for every not_evaluated reason must
// validate and be written. Before "antigravity_app" and "copilot_jetbrains"
// were in the schema enum, a machine with Antigravity App or Copilot in
// JetBrains produced those reasons and RenderV2JSON rejected the whole report
// (no nota).
func TestReportV2EveryNotEvaluatedReasonValidates(t *testing.T) {
	salt := []byte("0123456789abcdef0123456789abcdef")
	codexDir := "/h/.codex"
	scan := platform.CodexScan{ConfigDir: codexDir, Exists: true, Layers: []platform.CodexLayer{
		{Kind: platform.CodexLayerUser, Path: codexDir + "/config.toml", Status: platform.MCPSourceRead},
	}}
	f, _, err := graph.FromScansWithCodex(platform.MCPScan{Servers: graph.CodexMCPServers(scan)}, platform.ClaudeSettingsScan{}, platform.CursorScan{}, platform.CredentialFileScan{}, graph.Repos{}, platform.HermesScan{}, scan, salt, v2Now)
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
	if len(v.AgentsOverview) == 0 {
		t.Fatal("no agent overview to use as a template")
	}

	// One agent per reason, cloned from a real overview entry (valid knowledge
	// and rows) so only the reason varies.
	tmpl := v.AgentsOverview[0]
	var agents []rules.OverviewAgent
	for _, reason := range rules.AllNotEvaluatedReasons {
		r := reason
		a := tmpl
		a.Evaluated = false
		a.NotEvaluatedReason = &r
		a.Rows = rules.NotEvaluatedRows()
		a.Services = nil
		agents = append(agents, a)
	}
	v.AgentsOverview = agents

	out, err := report.RenderV2JSON(v)
	if err != nil {
		t.Fatalf("report.v2 with every not_evaluated reason did not validate: %v", err)
	}
	for _, reason := range rules.AllNotEvaluatedReasons {
		if !strings.Contains(string(out), `"not_evaluated_reason": "`+reason+`"`) {
			t.Errorf("written report lacks not_evaluated_reason %q", reason)
		}
	}
}
