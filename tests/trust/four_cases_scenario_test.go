package trust_test

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-140 (CTO-16 §7): end-to-end regression tests for the four §7 discrepancies
// Albert found by running the real binary. Each builds the full pipeline the
// binary runs (FromScans… → AddInstructions → Build → MarkCodexUntrusted →
// Infer → BuildV2) and asserts the report band and the findings/overview rows,
// so CI catches any future drift between the engine and CTO-16 §7.

const (
	fcHome      = "/home/albert"
	fcRepo      = "/src/webapp"
	fcClaudeDir = fcHome + "/.claude"
	fcCodexDir  = fcHome + "/.codex"
	fcCodexCfg  = fcCodexDir + "/config.toml"
	fcCodexProj = fcRepo + "/.codex/config.toml"
	fcClaudeCr  = fcClaudeDir + "/.credentials.json"
	fcGrokDir   = fcHome + "/.grok"
	fcGrokCfg   = fcGrokDir + "/config.toml"
)

var (
	fcSalt = []byte("0123456789abcdef0123456789abcdef")
	fcNow  = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
)

// codexV2 runs the Codex pipeline end to end and returns the v2 report.
func codexV2(t *testing.T, scan platform.CodexScan, entries []platform.CredentialEntry) report.V2 {
	t.Helper()
	roots := []string{fcRepo}
	settings := platform.ClaudeSettingsScan{
		Files: []platform.ClaudeSettingsFile{
			{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead},
			{Scope: platform.MCPScopeProject, ScopePath: fcRepo, ConfigPath: fcRepo + "/.claude/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead},
		},
		Trust: []platform.ProjectTrust{{Path: fcRepo, Accepted: true}},
	}
	mcp := platform.MCPScan{Servers: graph.CodexMCPServers(scan)}
	frag, _, err := graph.FromScansWithCodex(mcp, settings, platform.CursorScan{}, platform.CredentialFileScan{Entries: entries}, graph.Repos{}, platform.HermesScan{}, scan, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: []platform.InstructionFile{
		{Product: "claude-code", ConfigDir: fcClaudeDir, Path: fcRepo + "/CLAUDE.md"},
		{Product: "codex", ConfigDir: fcCodexDir, Path: fcRepo + "/AGENTS.md"},
	}}, fcNow)
	g := graph.Build(frag, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(fcSalt)}, fcNow)
	graph.MarkCodexUntrusted(&g, scan)
	g, err = graph.Infer(g, roots, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("0", 64), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: roots, V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: fcNow,
		ClaudeSettings: &settings, Home: fcHome, Codex: scan,
	})
	return v
}

// grokV2 runs the Grok Build pipeline end to end and returns the v2 report.
func grokV2(t *testing.T, scan platform.GrokScan, entries []platform.CredentialEntry) report.V2 {
	t.Helper()
	roots := []string{fcRepo}
	settings := platform.ClaudeSettingsScan{
		Files: []platform.ClaudeSettingsFile{{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead}},
		Trust: []platform.ProjectTrust{{Path: fcRepo, Accepted: true}},
	}
	mcp := platform.MCPScan{Servers: graph.GrokMCPServers(scan)}
	frag, _, err := graph.FromScansWithGrok(mcp, settings, platform.CursorScan{}, platform.CredentialFileScan{Entries: entries}, graph.Repos{}, platform.HermesScan{}, platform.CodexScan{}, scan, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	g := graph.Build(frag, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(fcSalt)}, fcNow)
	g, err = graph.Infer(g, roots, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("0", 64), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: roots, V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: fcNow,
		ClaudeSettings: &settings, Home: fcHome, Grok: scan,
	})
	return v
}

// agentRow returns the overview cell of the evaluated agent of the given
// product for the given row.
func agentRow(t *testing.T, v report.V2, product, row string) rules.OverviewCell {
	t.Helper()
	for _, a := range v.AgentsOverview {
		if a.Product != product {
			continue
		}
		if !a.Evaluated {
			t.Fatalf("%s not evaluated: %+v", product, a)
		}
		for _, c := range a.Rows {
			if c.Row == row {
				return c
			}
		}
		t.Fatalf("%s: no %s row", product, row)
	}
	t.Fatalf("%s: not in the overview", product)
	return rules.OverviewCell{}
}

// agentFindings returns the set of finding rules whose first evidence node is
// the evaluated agent of the given product.
func agentFindings(v report.V2, product string) map[string]bool {
	var client string
	for _, a := range v.AgentsOverview {
		if a.Product == product {
			client = a.AgentID
		}
	}
	out := map[string]bool{}
	if client == "" {
		return out
	}
	for _, f := range v.Findings {
		if len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == client {
			out[f.Rule] = true
		}
	}
	return out
}

func fcCred(path string, line int, key, value string) platform.CredentialEntry {
	return platform.CredentialEntry{Path: path, Line: line, Key: key, Value: secrets.NewSecretValue(value)}
}

func fcContains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// 1. GRAVE — Grok T5 (auto mode): a classifier decides writes, so Grok's own
// config is writable (own_config_writable, red). The writes row decides model
// with auto_classifier_permits. Band red.
func TestScenario_GrokT5_AutoMode(t *testing.T) {
	scan := platform.GrokScan{ConfigDir: fcGrokDir, Exists: true, Layers: []platform.GrokLayer{{
		Kind: platform.GrokLayerUser, Path: fcGrokCfg, Status: platform.MCPSourceRead,
		PermissionMode: "auto", HasPermissionMode: true,
	}}}
	v := grokV2(t, scan, nil)

	if v.Status != "red" {
		t.Errorf("T5 band: want red, got %q", v.Status)
	}
	rs := agentFindings(v, knowledge.GrokBuildProduct)
	if !rs[rules.RuleOwnConfigWritable] {
		t.Errorf("T5: want own_config_writable (have %v)", rs)
	}
	w := agentRow(t, v, knowledge.GrokBuildProduct, rules.RowWritesFiles)
	if w.Decider != rules.DeciderModel || !w.Assumed {
		t.Errorf("T5 writes: want model/assumed, got %+v", w)
	}
	if !fcContains(w.Data.Assumptions, graph.AssumeAutoClassifierPermits) {
		t.Errorf("T5 writes: missing auto_classifier_permits: %+v", w.Data.Assumptions)
	}
}

// 2. GRAVE — Codex E (explicitly untrusted project): AGENTS.md does not load
// (no own_instructions finding), Codex writes nothing in the project (no
// foreign config finding), and commands ask (runs_commands you/none with
// exec_rules_not_evaluated).
func TestScenario_CodexE_Untrusted(t *testing.T) {
	user := platform.CodexLayer{Kind: platform.CodexLayerUser, Path: fcCodexCfg, Status: platform.MCPSourceRead}
	user.Projects = map[string]string{graph.CanonicalPath(fcRepo): "untrusted"}
	proj := platform.CodexLayer{Kind: platform.CodexLayerProject, Path: fcCodexProj, Root: fcRepo, Status: platform.MCPSourceRead}
	proj.ApprovalPolicy, proj.HasApprovalPolicy = "never", true
	proj.TrustLevel = "untrusted"
	proj.MCP = []platform.CodexMCPServer{{ID: "x", Command: "evil.exe"}}
	scan := platform.CodexScan{ConfigDir: fcCodexDir, Exists: true, Layers: []platform.CodexLayer{user, proj}}

	v := codexV2(t, scan, []platform.CredentialEntry{fcCred(fcClaudeCr, 1, "SUPABASE_SECRET_KEY", "sb_secret_FAKEFAKEFAKE0000")})

	// The high-privilege credential is readable at rest, so the band is red.
	if v.Status != "red" {
		t.Errorf("E untrusted band: want red, got %q", v.Status)
	}
	rs := agentFindings(v, knowledge.CodexProduct)
	if rs[rules.RuleOwnInstructionsWritable] {
		t.Errorf("E untrusted: AGENTS.md must not load, so no own_instructions_writable: %v", rs)
	}
	if rs[rules.RuleForeignAgentConfigWritable] {
		t.Errorf("E untrusted: Codex is read-only in the project, so no foreign_agent_config_writable: %v", rs)
	}
	c := agentRow(t, v, knowledge.CodexProduct, rules.RowRunsCommands)
	if c.Decider != rules.DeciderYou || c.Scope != rules.ScopeNone {
		t.Errorf("E untrusted runs: want you/none, got %+v", c)
	}
	if !fcContains(c.Data.Assumptions, rules.AssumeExecRulesNotEvaluated) {
		t.Errorf("E untrusted runs: missing exec_rules_not_evaluated: %+v", c.Data.Assumptions)
	}
}

// 3. Menor — Grok T3 (trusted project, wildcard allow): the wildcard covers
// the shell too (CTO-16 Grok §4), so commands run without asking
// (runs_commands nobody). Band red (approve-everything at rest).
func TestScenario_GrokT3_TrustedWildcard(t *testing.T) {
	proj := platform.GrokLayer{
		Kind: platform.GrokLayerProject, Path: fcRepo + "/.grok/config.toml", Root: fcRepo, Status: platform.MCPSourceRead,
		Allow: []string{"*"}, Trusted: true,
		MCP: []platform.GrokMCPServer{{ID: "x", Command: "node", Args: []string{"x.js"}}},
	}
	scan := platform.GrokScan{
		ConfigDir: fcGrokDir, Exists: true, Trusted: []string{graph.CanonicalPath(fcRepo)},
		Layers: []platform.GrokLayer{{Kind: platform.GrokLayerUser, Path: fcGrokCfg, Status: platform.MCPSourceRead}, proj},
	}
	v := grokV2(t, scan, nil)

	if v.Status != "red" {
		t.Errorf("T3 band: want red, got %q", v.Status)
	}
	if c := agentRow(t, v, knowledge.GrokBuildProduct, rules.RowRunsCommands); c.Decider != rules.DeciderNobody {
		t.Errorf("T3 trusted wildcard runs: want nobody, got %+v", c)
	}
	if c := agentRow(t, v, knowledge.GrokBuildProduct, rules.RowWritesFiles); c.Decider != rules.DeciderNobody {
		t.Errorf("T3 trusted wildcard writes: want nobody, got %+v", c)
	}
}

// 4. Menor — Codex C (automatic reviewer): on-request + auto_review hands the
// escalations to the reviewer model. Writing is nobody inside the project and
// the model decides outside it; sending out is model. Both carry
// reviewer_model_permits.
func TestScenario_CodexC_AutoReview(t *testing.T) {
	user := platform.CodexLayer{Kind: platform.CodexLayerUser, Path: fcCodexCfg, Status: platform.MCPSourceRead}
	user.ApprovalPolicy, user.HasApprovalPolicy = "on-request", true
	user.ApprovalsReviewer = "auto_review"
	scan := platform.CodexScan{ConfigDir: fcCodexDir, Exists: true, Layers: []platform.CodexLayer{user}}

	v := codexV2(t, scan, []platform.CredentialEntry{fcCred(fcClaudeCr, 1, "SUPABASE_SECRET_KEY", "sb_secret_FAKEFAKEFAKE0000")})

	// The reviewer model can write the config and send out, so the band is red.
	if v.Status != "red" {
		t.Errorf("C auto_review band: want red, got %q", v.Status)
	}
	s := agentRow(t, v, knowledge.CodexProduct, rules.RowSendsOut)
	if s.Decider != rules.DeciderModel || !s.Assumed {
		t.Errorf("C sends: want model/assumed, got %+v", s)
	}
	if !fcContains(s.Data.Assumptions, rules.AssumeReviewerModelPermits) {
		t.Errorf("C sends: missing reviewer_model_permits: %+v", s.Data.Assumptions)
	}
	w := agentRow(t, v, knowledge.CodexProduct, rules.RowWritesFiles)
	if w.Decider != rules.DeciderNobody || w.Scope != rules.ScopeSameProject {
		t.Errorf("C writes primary: want same_project/nobody, got %+v", w)
	}
	if w.Also == nil || w.Also.Decider != rules.DeciderModel || w.Also.Scope != rules.ScopeMachine {
		t.Errorf("C writes Also (outside-project escalation): want model/machine, got %+v", w.Also)
	}
	if !w.Assumed || !fcContains(w.Data.Assumptions, rules.AssumeReviewerModelPermits) {
		t.Errorf("C writes: missing reviewer_model_permits/assumed: %+v", w)
	}
}
