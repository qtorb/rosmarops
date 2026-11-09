package trust_test

import (
	"encoding/base64"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-140 (CTO-16 Antigravity §7): end-to-end tests for the Antigravity CLI
// through the full report pipeline (FromScans → FromAntigravityCLI → Infer →
// BuildV2), asserting band, findings and the overview rows.

const fcGemini = fcHome + "/.gemini"

// fcServiceRole is a Supabase service_role JWT (high-privilege), like the
// rules tests' jwt("service_role").
func fcJWT(role string) string {
	seg := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	return seg(`{"alg":"HS256","typ":"JWT"}`) + "." +
		seg(`{"iss":"supabase","ref":"abcdefghijklmnopqrst","role":"`+role+`"}`) + ".ZmFrZS1zaWduYXR1cmUtZmlsbGVy"
}

var fcServiceRole = fcJWT("service_role")

// antigravityV2 runs the Antigravity CLI pipeline end to end.
func antigravityV2(t *testing.T, scan platform.AntigravityScan, claude []platform.ClaudeSettingsFile, entries []platform.CredentialEntry) report.V2 {
	t.Helper()
	roots := []string{fcRepo}
	if claude == nil {
		claude = []platform.ClaudeSettingsFile{{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead}}
	}
	settings := platform.ClaudeSettingsScan{Files: claude, Trust: []platform.ProjectTrust{{Path: fcRepo, Accepted: true}}}
	frag, _, err := graph.FromScans(platform.MCPScan{}, settings, platform.CredentialFileScan{Entries: entries}, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.Merge(frag, graph.FromAntigravityCLI(scan, fcNow))
	g, err := pipeline.BuildGraph(frag, pipeline.GraphOpts{
		Provenance: graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(fcSalt)},
		RepoRoots:  roots, Home: fcHome, Now: fcNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings0(64), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: roots, V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: fcNow,
		ClaudeSettings: &settings, Home: fcHome,
	})
	return v
}

// Antigravity CLI approve-all (CTO-16 Antigravity §7 A): always-proceed +
// allowNonWorkspaceAccess. Band red; own config writable, a foreign global
// config writable and cred_read_and_send all fire.
func TestScenario_Antigravity_ApproveAll(t *testing.T) {
	scan := platform.AntigravityScan{ConfigDir: fcGemini, Present: true, Settings: platform.AntigravitySettings{
		Path: fcGemini + "/antigravity-cli/settings.json", Status: platform.MCPSourceRead,
		ToolPermission: "always-proceed", AllowNonWorkspaceAccess: true,
	}}
	entries := []platform.CredentialEntry{{Path: fcRepo + "/.env", Line: 1, Key: "SUPABASE_SERVICE_ROLE_KEY", Value: secrets.NewSecretValue(fcServiceRole)}}
	v := antigravityV2(t, scan, nil, entries)

	if v.Status != "red" {
		t.Errorf("approve-all band: want red, got %q", v.Status)
	}
	rs := agentFindings(v, knowledge.AntigravityCLIProduct)
	for _, want := range []string{rules.RuleOwnConfigWritable, rules.RuleForeignAgentConfigWritable, rules.RuleCredReadAndSend} {
		if !rs[want] {
			t.Errorf("approve-all: missing %q (have %v)", want, rs)
		}
	}
	if c := agentRow(t, v, knowledge.AntigravityCLIProduct, rules.RowWritesFiles); c.Decider != rules.DeciderNobody {
		t.Errorf("approve-all writes: want nobody, got %+v", c)
	}
}

// Antigravity CLI agentMode auto (BV-140 decision 6): a reviewer model decides,
// with reviewer_model_permits.
func TestScenario_Antigravity_AutoMode(t *testing.T) {
	scan := platform.AntigravityScan{ConfigDir: fcGemini, Present: true, Settings: platform.AntigravitySettings{
		Path: fcGemini + "/antigravity-cli/settings.json", Status: platform.MCPSourceRead, AgentMode: "auto",
	}}
	v := antigravityV2(t, scan, nil, nil)

	runs := agentRow(t, v, knowledge.AntigravityCLIProduct, rules.RowRunsCommands)
	if runs.Decider != rules.DeciderModel || !runs.Assumed {
		t.Errorf("auto runs: want model/assumed, got %+v", runs)
	}
	if !fcContains(runs.Data.Assumptions, rules.AssumeReviewerModelPermits) {
		t.Errorf("auto runs: missing reviewer_model_permits: %+v", runs.Data.Assumptions)
	}
}
