package rules_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-140 (CTO-16 §7): OpenAI Codex evaluated. The cells follow its sandbox and
// approval policy; the findings come from the shared rules through the codex
// inference (reaches, writes_config_of) and the MCP pipeline.

const (
	codexDir     = home + "/.codex"
	codexConfig  = codexDir + "/config.toml"
	claudeCred   = claudeDir + "/.credentials.json"
	codexAuth    = codexDir + "/auth.json"
	awsCred      = home + "/.aws/credentials"
	codexProjCfg = repoWeb + "/.codex/config.toml"
)

// codexEvaluate builds, infers and evaluates a Codex scan with the claude
// project settings, CLAUDE.md and AGENTS.md of repoWeb and the given
// credentials.
func codexEvaluate(t *testing.T, scan platform.CodexScan, creds []entry) (graph.Graph, rules.Result, string) {
	t.Helper()
	var cf platform.CredentialFileScan
	for _, e := range creds {
		cf.Entries = append(cf.Entries, platform.CredentialEntry{Path: e.path, Line: e.line, Key: e.key, Value: secrets.NewSecretValue(e.value)})
	}
	roots := []string{repoWeb}
	settings := platform.ClaudeSettingsScan{
		Files: []platform.ClaudeSettingsFile{
			userSettings(platform.ClaudePermissions{}),
			projectSettings(repoWeb, platform.ClaudePermissions{}),
		},
		Trust: []platform.ProjectTrust{{Path: repoWeb, Accepted: true}},
	}
	mcp := platform.MCPScan{Servers: graph.CodexMCPServers(scan)}
	f, _, err := graph.FromScansWithCodex(mcp, settings, platform.CursorScan{}, cf, graph.Repos{}, platform.HermesScan{}, scan, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	f = graph.AddInstructions(f, platform.InstructionScan{Files: []platform.InstructionFile{
		{Product: "claude-code", ConfigDir: claudeDir, Path: repoWeb + "/CLAUDE.md"},
		{Product: "codex", ConfigDir: codexDir, Path: repoWeb + "/AGENTS.md"},
	}}, now)
	g := graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(salt)}, now)
	graph.MarkCodexUntrusted(&g, scan)
	g, err = graph.Infer(g, roots, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.Encode(g); err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	res := rules.Evaluate(rules.Input{
		Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases, RepoRoots: roots,
		Home: home, Now: now, ClaudeSettings: &settings, Codex: scan,
	})
	var client string
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient && n.Platform == knowledge.CodexProduct {
			client = n.ID
		}
	}
	if client == "" {
		t.Fatal("no Codex ai_client in graph")
	}
	return g, res, client
}

// codexLayer is a read Codex config layer for the test scans.
func codexLayer(kind, path, root string) platform.CodexLayer {
	return platform.CodexLayer{Kind: kind, Path: path, Root: root, Status: platform.MCPSourceRead}
}

// codexRules are the rules of the agent's findings.
func codexRules(res rules.Result, client string) map[string]bool {
	out := map[string]bool{}
	for _, f := range res.Findings {
		if len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == client {
			out[f.Rule] = true
		}
	}
	return out
}

func codexOvCell(t *testing.T, res rules.Result, client, row string) rules.OverviewCell {
	t.Helper()
	for _, a := range res.AgentsOverview {
		if a.AgentID != client {
			continue
		}
		if !a.Evaluated {
			t.Fatalf("Codex not evaluated: %+v", a)
		}
		for _, c := range a.Rows {
			if c.Row == row {
				return c
			}
		}
	}
	t.Fatalf("no %s row for Codex", row)
	return rules.OverviewCell{}
}

// A. Default, trusted repo (CTO-16 §7 A): reads machine/nobody, writes
// same_project/nobody, runs sandbox/nobody, sends you; foreign config and own
// instructions writable; the Claude credential readable; its own auth.json not
// a finding.
func TestCodex_CaseA_DefaultTrusted(t *testing.T) {
	scan := platform.CodexScan{
		ConfigDir: codexDir, Exists: true,
		Layers: []platform.CodexLayer{
			codexLayer(platform.CodexLayerUser, codexConfig, ""),
			func() platform.CodexLayer {
				l := codexLayer(platform.CodexLayerProject, codexProjCfg, repoWeb)
				l.Trusted = true
				return l
			}(),
		},
	}
	_, res, client := codexEvaluate(t, scan, []entry{
		{claudeCred, 1, "SUPABASE_SECRET_KEY", sbSecret},
		{codexAuth, 1, "OPENAI_API_KEY", openAIKey},
	})

	if c := codexOvCell(t, res, client, rules.RowReadsFiles); c.Decider != rules.DeciderNobody || c.Scope != rules.ScopeMachine {
		t.Errorf("reads: %+v", c)
	}
	if c := codexOvCell(t, res, client, rules.RowWritesFiles); c.Decider != rules.DeciderNobody || c.Scope != rules.ScopeSameProject {
		t.Errorf("writes: %+v", c)
	}
	if c := codexOvCell(t, res, client, rules.RowRunsCommands); c.Decider != rules.DeciderNobody || c.Scope != rules.ScopeSandbox {
		t.Errorf("runs: %+v", c)
	}
	if c := codexOvCell(t, res, client, rules.RowSendsOut); c.Decider != rules.DeciderYou {
		t.Errorf("sends: %+v", c)
	}
	rs := codexRules(res, client)
	for _, want := range []string{rules.RuleForeignAgentConfigWritable, rules.RuleForeignAgentInstructionsWritable, rules.RuleOwnInstructionsWritable, rules.RuleCredReadable} {
		if !rs[want] {
			t.Errorf("case A: missing finding %q (have %v)", want, rs)
		}
	}
	if rs[rules.RuleCredReadAndSend] {
		t.Errorf("case A: network is off, cred_read_and_send should not fire")
	}
	if rs[rules.RuleOwnConfigWritable] {
		t.Errorf("case A: workspace-write must not write Codex's own config outside the workspace")
	}
	// Every Codex cell rests on launch_flags_not_readable.
	c := codexOvCell(t, res, client, rules.RowReadsFiles)
	if !contains(c.Data.Assumptions, rules.AssumeLaunchFlagsNotReadable) {
		t.Errorf("reads cell missing launch_flags_not_readable: %+v", c.Data.Assumptions)
	}
}

// B. Approve-everything at rest (CTO-16 §7 B): every row machine/nobody,
// cred_read_and_send, own_config_writable on CODEX_HOME/config.toml.
func TestCodex_CaseB_DangerNever(t *testing.T) {
	net := false
	user := codexLayer(platform.CodexLayerUser, codexConfig, "")
	user.SandboxMode = "danger-full-access"
	user.ApprovalPolicy, user.HasApprovalPolicy = "never", true
	user.NetworkAccess = &net
	proj := codexLayer(platform.CodexLayerProject, codexProjCfg, repoWeb)
	proj.Trusted = true
	scan := platform.CodexScan{ConfigDir: codexDir, Exists: true, Layers: []platform.CodexLayer{user, proj}}

	_, res, client := codexEvaluate(t, scan, []entry{
		{claudeCred, 1, "SUPABASE_SECRET_KEY", sbSecret},
		{codexAuth, 1, "OPENAI_API_KEY", openAIKey},
		{awsCred, 1, "SUPABASE_SERVICE_ROLE_KEY", serviceRole},
	})

	for _, row := range []string{rules.RowReadsFiles, rules.RowWritesFiles, rules.RowRunsCommands} {
		if c := codexOvCell(t, res, client, row); c.Decider != rules.DeciderNobody || c.Scope != rules.ScopeMachine {
			t.Errorf("%s: want machine/nobody, got %+v", row, c)
		}
	}
	if c := codexOvCell(t, res, client, rules.RowSendsOut); c.Decider != rules.DeciderNobody || c.Scope != rules.ScopeAnyDestination {
		t.Errorf("sends: want any_destination/nobody, got %+v", c)
	}
	rs := codexRules(res, client)
	for _, want := range []string{rules.RuleCredReadAndSend, rules.RuleOwnConfigWritable, rules.RuleForeignAgentConfigWritable} {
		if !rs[want] {
			t.Errorf("case B: missing finding %q (have %v)", want, rs)
		}
	}
	// Both Codex actions are connected: reverting the sandbox, and marking the
	// trusted project untrusted.
	acts := map[string]bool{}
	for _, f := range res.Findings {
		if len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == client {
			for _, a := range f.Actions {
				acts[a.Code] = true
			}
		}
	}
	for _, want := range []string{rules.ActionCodexWorkspaceSandbox, rules.ActionCodexUntrustedProject} {
		if !acts[want] {
			t.Errorf("case B: action %q not connected (have %v)", want, acts)
		}
	}
}

// C. Automatic reviewer (CTO-16 §7 C): on-request + auto_review turns the
// escalation (sending out) over to the reviewer model, a premise.
func TestCodex_CaseC_AutoReview(t *testing.T) {
	user := codexLayer(platform.CodexLayerUser, codexConfig, "")
	user.ApprovalPolicy, user.HasApprovalPolicy = "on-request", true
	user.ApprovalsReviewer = "auto_review"
	scan := platform.CodexScan{ConfigDir: codexDir, Exists: true, Layers: []platform.CodexLayer{user}}

	_, res, client := codexEvaluate(t, scan, []entry{{claudeCred, 1, "SUPABASE_SECRET_KEY", sbSecret}})
	c := codexOvCell(t, res, client, rules.RowSendsOut)
	if c.Decider != rules.DeciderModel || !c.Assumed {
		t.Errorf("sends under auto_review: want model/assumed, got %+v", c)
	}
	if !contains(c.Data.Assumptions, rules.AssumeReviewerModelPermits) {
		t.Errorf("sends cell missing reviewer_model_permits: %+v", c.Data.Assumptions)
	}
	// Writing is nobody inside the project, and the reviewer model decides the
	// escalation outside it (CTO-16 §7 C: «escribir fuera» → model).
	w := codexOvCell(t, res, client, rules.RowWritesFiles)
	if w.Decider != rules.DeciderNobody || w.Scope != rules.ScopeSameProject {
		t.Errorf("writes under auto_review: want same_project/nobody primary, got %+v", w)
	}
	if w.Also == nil || w.Also.Decider != rules.DeciderModel || w.Also.Scope != rules.ScopeMachine {
		t.Errorf("writes under auto_review: want Also model/machine (outside-project escalation), got %+v", w.Also)
	}
	if !w.Assumed || !contains(w.Data.Assumptions, rules.AssumeReviewerModelPermits) {
		t.Errorf("writes cell missing reviewer_model_permits/assumed: %+v", w)
	}
}

// D. Secret in an MCP server (CTO-16 §7 D): the config.toml credential is
// kept by fingerprint only (preview "ghp_ ... 1234", never the value); the
// github mcp_server loads from Codex.
func TestCodex_CaseD_MCPSecret(t *testing.T) {
	user := codexLayer(platform.CodexLayerUser, codexConfig, "")
	user.MCP = []platform.CodexMCPServer{{
		ID: "github", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-github"},
		EnvKeys: []string{"GITHUB_PERSONAL_ACCESS_TOKEN"},
		Env:     []platform.EnvVar{{Key: "GITHUB_PERSONAL_ACCESS_TOKEN", Value: secrets.NewSecretValue("ghp_FAKEFAKEFAKEFAKE1234")}},
	}}
	scan := platform.CodexScan{ConfigDir: codexDir, Exists: true, Layers: []platform.CodexLayer{user}}
	g, _, client := codexEvaluate(t, scan, nil)

	var server, loads bool
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentMCPServer && n.Name == "github" {
			server = true
		}
	}
	for _, e := range g.Edges {
		if e.Type == graph.EdgeLoads && e.From == client {
			loads = true
		}
	}
	if !server || !loads {
		t.Errorf("github mcp_server/loads: server=%v loads=%v", server, loads)
	}
	// The token is never in clear anywhere in the encoded graph; its preview is.
	enc, err := graph.Encode(g)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(enc), "FAKEFAKE") {
		t.Errorf("the MCP token value leaked into the graph")
	}
}

// mcpLoaded reports an mcp_server node of the given name in the graph.
func mcpLoaded(g graph.Graph, name string) bool {
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentMCPServer && n.Name == name {
			return true
		}
	}
	return false
}

// hasUntrustedCap reports a not-in-force capability with reason
// workspace_untrusted.
func hasUntrustedCap(g graph.Graph) bool {
	for _, n := range g.Nodes {
		if n.Kind == graph.KindCapability && n.NotInForceReason == graph.ReasonWorkspaceUntrusted {
			return true
		}
	}
	return false
}

// codexEProject builds the §7 E scan with the repo's trap config at the given
// trust_level ("" = no [projects] entry, "untrusted", "trusted").
func codexEProject(trustLevel string) platform.CodexScan {
	proj := codexLayer(platform.CodexLayerProject, codexProjCfg, repoWeb)
	proj.ApprovalPolicy, proj.HasApprovalPolicy = "never", true
	proj.MCP = []platform.CodexMCPServer{{ID: "x", Command: "evil.exe"}}
	proj.TrustLevel = trustLevel
	proj.Trusted = trustLevel == "trusted"
	user := codexLayer(platform.CodexLayerUser, codexConfig, "")
	if trustLevel != "" {
		user.Projects = map[string]string{graph.CanonicalPath(repoWeb): trustLevel}
	}
	return platform.CodexScan{
		ConfigDir: codexDir, Exists: true,
		Layers: []platform.CodexLayer{user, proj},
	}
}

// E. Untrusted project with a trap (CTO-16 §7 E), the three states:
//   - no [projects] entry: the project MCP does not load; its capabilities read
//     workspace_untrusted; but AGENTS.md still loads (own_instructions), and
//     commands keep the user default (sandbox/nobody);
//   - trust_level = "untrusted": the project MCP does not load; AGENTS.md does
//     not load (no own_instructions) and Codex writes nothing in the project
//     (no foreign config either); commands ask (you, exec_rules_not_evaluated);
//   - trust_level = "trusted": the project MCP loads.
func TestCodex_CaseE_ProjectTrustStates(t *testing.T) {
	creds := []entry{{claudeCred, 1, "SUPABASE_SECRET_KEY", sbSecret}}

	// No entry: AGENTS.md loads, commands sandbox/nobody.
	g, res, client := codexEvaluate(t, codexEProject(""), creds)
	if mcpLoaded(g, "x") {
		t.Errorf("no-entry: the project MCP must not load")
	}
	if !hasUntrustedCap(g) {
		t.Errorf("no-entry: no workspace_untrusted capability")
	}
	rs := codexRules(res, client)
	if !rs[rules.RuleOwnInstructionsWritable] {
		t.Errorf("no-entry: AGENTS.md should load and be own-instructions-writable: %v", rs)
	}
	if c := codexOvCell(t, res, client, rules.RowRunsCommands); c.Decider != rules.DeciderNobody || c.Scope != rules.ScopeSandbox {
		t.Errorf("no-entry: runs should be sandbox/nobody: %+v", c)
	}

	// Explicit untrusted: AGENTS.md does not load, commands ask.
	g, res, client = codexEvaluate(t, codexEProject("untrusted"), creds)
	if mcpLoaded(g, "x") {
		t.Errorf("untrusted: the project MCP must not load")
	}
	rs = codexRules(res, client)
	if rs[rules.RuleOwnInstructionsWritable] {
		t.Errorf("untrusted: AGENTS.md must not load, so no own_instructions: %v", rs)
	}
	if rs[rules.RuleForeignAgentConfigWritable] {
		t.Errorf("untrusted: Codex is read-only in the project, so no foreign config write: %v", rs)
	}
	c := codexOvCell(t, res, client, rules.RowRunsCommands)
	if c.Decider != rules.DeciderYou || c.Scope != rules.ScopeNone {
		t.Errorf("untrusted: runs should be you/none: %+v", c)
	}
	if !contains(c.Data.Assumptions, rules.AssumeExecRulesNotEvaluated) {
		t.Errorf("untrusted: runs cell missing exec_rules_not_evaluated: %+v", c.Data.Assumptions)
	}

	// Trusted: the project MCP loads.
	g, _, _ = codexEvaluate(t, codexEProject("trusted"), creds)
	if !mcpLoaded(g, "x") {
		t.Errorf("trusted: the project MCP should load")
	}
}
