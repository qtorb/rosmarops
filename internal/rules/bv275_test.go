package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/declared"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-275: Cursor's CLI settings, networkPolicy and team hooks feed the rules
// that exist (signed by Albert, 22-11).

// cursorScanScenario is cursorScenario with the whole Cursor scan given.
func cursorScanScenario(t *testing.T, scan platform.CursorScan) graph.Graph {
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
	scan.ConfigDir, scan.Exists = cursorDir, true
	if scan.Declared == nil {
		scan.Declared = declared.Factory(declared.Cursor)
	}
	f, _, err := graph.FromScansWithCursor(mcp, settings, scan, cf, salt, now)
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

func cliConfig(mode, sandbox string) platform.CursorFile {
	return platform.CursorFile{
		Path: cursorDir + "/cli-config.json", Kind: platform.CursorCLI, Scope: platform.CursorScopeUser,
		Status: platform.MCPSourceRead, CLIApprovalMode: mode, CLISandboxMode: sandbox,
	}
}

// capOfRule is the in-force capability of Cursor with this rule, or nil.
func capOfRule(g graph.Graph, rule string) *graph.Node {
	for i, n := range g.Nodes {
		if n.Kind == graph.KindCapability && n.Rule == rule {
			return &g.Nodes[i]
		}
	}
	return nil
}

// hasWay reports a Cursor finding standing on the way code through cap.
func hasWay(t *testing.T, g graph.Graph, code, capID string) bool {
	t.Helper()
	for _, f := range cursorFindings(g, evaluateAll(t, g)) {
		if rules.FindingWays(f)[[2]string{code, capID}] {
			return true
		}
	}
	return false
}

// approvalMode unrestricted in the CLI is Run Everything for Cursor, even
// with the IDE in Auto-review (the least controlled counts).
func TestBV275_CLIUnrestrictedIsRunEverything(t *testing.T) {
	g := cursorScanScenario(t, platform.CursorScan{Files: []platform.CursorFile{cliConfig("unrestricted", "")}})
	c := capOfRule(g, "approvalMode=unrestricted")
	if c == nil || c.AppliesIn != "cli:user" || c.TargetScope != graph.CursorScopeRunEverything || !graph.IsCursorMode(*c) {
		t.Fatalf("CLI mode capability: %+v", c)
	}
	if !hasWay(t, g, "egress_run_everything", c.ID) {
		t.Error("no egress_run_everything way through the CLI's mode")
	}
}

// allowlist and auto-review in the CLI change nothing.
func TestBV275_CLIOtherModesChangeNothing(t *testing.T) {
	for _, mode := range []string{"allowlist", "auto-review", ""} {
		g := cursorScanScenario(t, platform.CursorScan{Files: []platform.CursorFile{cliConfig(mode, "")}})
		for _, n := range g.Nodes {
			if n.Kind == graph.KindCapability && n.AppliesIn == "cli:user" && n.Type == graph.CapPermissionMode {
				t.Errorf("%q: a CLI mode capability %+v", mode, n)
			}
		}
	}
}

// The least controlled mode counts in the rules view too.
func TestBV275_LeastControlledRank(t *testing.T) {
	order := []string{declared.ModeAskEveryTime, declared.ModeAllowlist, declared.ModeAllowlistSandbox, declared.ModeAutoReview, declared.ModeRunEverything}
	for i := 1; i < len(order); i++ {
		if graph.CursorModeRank(order[i]) <= graph.CursorModeRank(order[i-1]) {
			t.Errorf("%s does not rank above %s", order[i], order[i-1])
		}
	}
	if graph.CursorModeRank(declared.ModeAutoReviewSandbox) != graph.CursorModeRank(declared.ModeAutoReview) {
		t.Error("the two Auto-review modes rank differently")
	}
}

// sandbox.mode disabled is no sandbox for the CLI.
func TestBV275_CLISandboxDisabled(t *testing.T) {
	g := cursorScanScenario(t, platform.CursorScan{Files: []platform.CursorFile{cliConfig("", "disabled")}})
	c := capOfRule(g, graph.CursorCLISandboxOffRule)
	if c == nil || c.TargetScope != graph.ScopeSandboxNone || !graph.IsCursorNoSandbox(*c) {
		t.Fatalf("CLI no-sandbox capability: %+v", c)
	}
	if c := capOfRule(cursorScanScenario(t, platform.CursorScan{Files: []platform.CursorFile{cliConfig("", "enabled")}}), graph.CursorCLISandboxOffRule); c != nil {
		t.Errorf("sandbox enabled read as disabled: %+v", c)
	}
}

// networkPolicy.default allow: with the sandbox on and a command running in
// it without a person, it is a way out; under the other modes, nothing new.
func TestBV275_NetworkPolicyAllow(t *testing.T) {
	sandbox := platform.CursorFile{
		Path: cursorDir + "/sandbox.json", Kind: platform.CursorSandbox, Scope: platform.CursorScopeUser,
		Status: platform.MCPSourceRead, NetworkDefault: "allow",
	}
	for mode, want := range map[string]bool{
		declared.ModeAutoReviewSandbox: true,
		declared.ModeAllowlistSandbox:  true,
		declared.ModeAllowlist:         false,
		declared.ModeAutoReview:        false,
	} {
		ans := declared.Factory(declared.Cursor)
		ans[declared.C1RunMode] = mode
		// A command in the list that reads files, so that Cursor has a way
		// to read a key under Allowlist too.
		perms := platform.CursorFile{
			Path: cursorDir + "/permissions.json", Kind: platform.CursorPermissions, Scope: platform.CursorScopeUser,
			Status: platform.MCPSourceRead, TerminalAllowlist: []string{"cat"}, HasTerminalAllowlist: true,
		}
		g := cursorScanScenario(t, platform.CursorScan{Files: []platform.CursorFile{sandbox, perms}, Declared: ans})
		c := capOfRule(g, graph.CursorNetworkAllowRule)
		if c == nil || c.TargetScope != graph.ScopeSandboxNetworkAll {
			t.Fatalf("%s: network capability %+v", mode, c)
		}
		if got := hasWay(t, g, "egress_sandbox_network", c.ID); got != want {
			t.Errorf("%s: egress_sandbox_network %v, want %v", mode, got, want)
		}
	}
	// "deny" (the default) adds nothing.
	sandbox.NetworkDefault = "deny"
	if c := capOfRule(cursorScanScenario(t, platform.CursorScan{Files: []platform.CursorFile{sandbox}}), graph.CursorNetworkAllowRule); c != nil {
		t.Errorf("deny read as allow: %+v", c)
	}
}

// Team hooks present: Cursor's executables, and team_policy_unknown stays on
// its ways. Absent: no team executable.
func TestBV275_TeamHooks(t *testing.T) {
	hook := cursorDir + "/managed/team_42/hooks/audit.sh"
	for _, present := range []bool{true, false} {
		scan := platform.CursorScan{}
		if present {
			scan.TeamHooks = []string{hook}
		}
		g := cursorScanScenario(t, scan)
		found := false
		for _, n := range g.Nodes {
			if n.Type == graph.ResourceAgentExecutable && n.Path == hook {
				found = true
			}
		}
		if found != present {
			t.Errorf("present %v: executable found %v", present, found)
		}
		kept := false
		for _, f := range cursorFindings(g, evaluateAll(t, g)) {
			for _, a := range f.Evidence.Assumptions {
				kept = kept || a == graph.AssumeTeamPolicyUnknown
			}
		}
		if !kept {
			t.Errorf("present %v: team_policy_unknown gone", present)
		}
	}
}
