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

// cursorScenario is the real-shape scenario with Cursor's own inference
// (decision 2026-10-25, Cursor), and files its collector read.
func cursorScenario(t *testing.T, files ...platform.CursorFile) graph.Graph {
	t.Helper()
	var cf platform.CredentialFileScan
	for _, e := range defaultEntries {
		cf.Entries = append(cf.Entries, platform.CredentialEntry{Path: e.path, Line: e.line, Key: e.key, Value: secrets.NewSecretValue(e.value)})
	}
	mcp := platform.MCPScan{Sources: []platform.MCPSource{
		{Platform: "cursor", ConfigPath: cursorMCP, ConfigDir: cursorDir, Status: platform.MCPSourceRead, LoadsMCP: true},
	}}
	settings := platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{{
		Scope: platform.MCPScopeUser, ConfigPath: userJSON, ConfigDir: claudeDir, Status: platform.MCPSourceRead,
	}}}
	f, _, err := graph.FromScansWithCursor(mcp, settings, platform.CursorScan{ConfigDir: cursorDir, Exists: true, Files: files}, cf, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	roots := []string{repoWeb, repoIF, repoNotes, repoWeb2}
	g, err := graph.Infer(graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Version, HostID: graph.HostID(salt)}, now), roots, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.Encode(g); err != nil {
		t.Fatal(err)
	}
	return g
}

func evaluateAll(t *testing.T, g graph.Graph) rules.Result {
	t.Helper()
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	return rules.Evaluate(rules.Input{
		Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: []string{repoWeb, repoIF, repoNotes, repoWeb2},
		Families:  map[string]string{repoWeb: repoWeb, repoWeb2: repoWeb},
	})
}

// cursorFindings are the findings of Cursor's client.
func cursorFindings(g graph.Graph, res rules.Result) []rules.Finding {
	cursor := map[string]bool{}
	for _, n := range g.Nodes {
		cursor[n.ID] = n.Platform == knowledge.CursorProduct && n.Kind == graph.KindAgent
	}
	var out []rules.Finding
	for _, f := range res.Findings {
		if cursor[f.Evidence.Nodes[0]] {
			out = append(out, f)
		}
	}
	return out
}

// §2.4, K2: Cursor reads the Supabase keys through Auto-review's shell and
// sends through Auto-review: cred_read_and_send red, any_project, confidence
// medium (the mode is a premise). Its actions are Cursor's:
// sandbox_read_boundary_workspace (radius only, ~/.cursor/sandbox.json,
// {"readBoundary": "workspace"} without a permissions wrapper) and
// remove_from_disk; none of Claude Code's.
func TestEvaluateCursor_ReadAndSendWithItsActions(t *testing.T) {
	g := cursorScenario(t)
	fs := cursorFindings(g, evaluateAll(t, g))
	var sb *rules.Finding
	for i, f := range fs {
		if f.Rule == rules.RuleCredReadAndSend && f.Params.Service == "supabase" {
			sb = &fs[i]
		}
	}
	if sb == nil {
		t.Fatalf("no Supabase finding for Cursor: %v", fs)
	}
	if sb.Severity != rules.SeverityRed || sb.Radius != rules.RadiusAnyProject || sb.Confidence != rules.ConfidenceMedium || sb.Params.Agent != "Cursor" {
		t.Errorf("finding %s %s %s %s", sb.Severity, sb.Radius, sb.Confidence, sb.Params.Agent)
	}
	codes := map[string]rules.Action{}
	for _, a := range sb.Actions {
		codes[a.Code] = a
	}
	a, ok := codes[rules.ActionSandboxReadBoundary]
	if !ok || len(codes) != 2 || codes[rules.ActionRemoveFromDisk].Code == "" {
		t.Fatalf("actions %v", sb.Actions)
	}
	if a.File != cursorDir+"/sandbox.json" || a.Completeness != rules.CompletenessRadiusOnly || strings.Contains(a.Value, "permissions") ||
		strings.Join(strings.Fields(a.Value), "") != `{"readBoundary":"workspace"}` {
		t.Errorf("read boundary action %+v", a)
	}
	for _, f := range fs {
		for _, x := range f.Actions {
			if x.Code == rules.ActionAskReadRule || x.Code == rules.ActionAskEditRule || x.Code == rules.ActionBlockReadsOutside || x.Code == rules.ActionManualDefaultMode {
				t.Errorf("Claude Code action on a Cursor finding: %+v", x)
			}
		}
	}
}

// Annex 2 point 8: with readBoundary workspace in the user's sandbox.json
// the radius is same_project, and the action is no longer offered.
func TestEvaluateCursor_ReadBoundaryInForce(t *testing.T) {
	sandbox := platform.CursorFile{
		Path: cursorDir + "/sandbox.json", Kind: platform.CursorSandbox, Scope: platform.CursorScopeUser,
		Status: platform.MCPSourceRead, ReadBoundary: "workspace",
	}
	g := cursorScenario(t, sandbox)
	for _, f := range cursorFindings(g, evaluateAll(t, g)) {
		if f.Rule != rules.RuleCredReadAndSend && f.Rule != rules.RuleCredReadable {
			continue
		}
		if f.Radius != rules.RadiusSameProject {
			t.Errorf("%s: radius %s", f.Params.Service, f.Radius)
		}
		for _, a := range f.Actions {
			if a.Code == rules.ActionSandboxReadBoundary {
				t.Errorf("read boundary offered while in force")
			}
		}
	}
}

// Annex 2 point 2: a terminalAllowlist entry that sends to the network
// (curl) is an egress way of its own, without the classifier: its
// capability is in the finding's evidence. Confidence is the union of the
// ways' assumptions (E4), so Auto-review's premise keeps it medium.
func TestEvaluateCursor_AllowlistEgress(t *testing.T) {
	perms := platform.CursorFile{
		Path: cursorDir + "/permissions.json", Kind: platform.CursorPermissions, Scope: platform.CursorScopeUser,
		Status: platform.MCPSourceRead, TerminalAllowlist: []string{"curl"}, HasTerminalAllowlist: true,
	}
	g := cursorScenario(t, perms)
	curl := ""
	for _, n := range g.Nodes {
		if n.Rule == "terminalAllowlist:curl" {
			curl = n.ID
		}
	}
	found := false
	for _, f := range cursorFindings(g, evaluateAll(t, g)) {
		if f.Rule == rules.RuleCredReadAndSend && f.Params.Service == "supabase" {
			found = true
			in := false
			for _, n := range f.Evidence.Nodes {
				in = in || n == curl
			}
			if curl == "" || !in {
				t.Errorf("curl egress way %q not in the evidence %v", curl, f.Evidence.Nodes)
			}
		}
	}
	if !found {
		t.Error("no Supabase finding")
	}
}
