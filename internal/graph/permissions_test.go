package graph_test

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

const (
	userSettings  = "/home/albert/.claude/settings.json"
	permRepo      = "/src/app"
	projSettings  = "/src/app/.claude/settings.json"
	localSettings = "/src/app/.claude/settings.local.json"
)

func settingsFile(scope, scopePath, path string, p platform.ClaudePermissions) platform.ClaudeSettingsFile {
	return platform.ClaudeSettingsFile{
		Scope: scope, ScopePath: scopePath, ConfigPath: path, ConfigDir: claudeDir,
		Status: platform.MCPSourceRead, Permissions: p,
	}
}

func userFile(p platform.ClaudePermissions) platform.ClaudeSettingsFile {
	return settingsFile(platform.MCPScopeUser, "", userSettings, p)
}

func projectFile(p platform.ClaudePermissions) platform.ClaudeSettingsFile {
	return settingsFile(platform.MCPScopeProject, permRepo, projSettings, p)
}

func localFile(p platform.ClaudePermissions) platform.ClaudeSettingsFile {
	return settingsFile(platform.MCPScopeLocal, permRepo, localSettings, p)
}

func fromSettings(files ...platform.ClaudeSettingsFile) (graph.Fragment, graph.PermissionStats) {
	return graph.FromClaudeSettings(platform.ClaudeSettingsScan{Files: files}, scanTime)
}

// capByRule returns the configured capability whose rule is rule.
func capByRule(t *testing.T, f graph.Fragment, rule string) graph.Node {
	t.Helper()
	for _, n := range f.Nodes {
		if n.Kind == graph.KindCapability && n.Basis == graph.BasisConfigured && n.Rule == rule {
			return n
		}
	}
	t.Fatalf("no capability with rule %q", rule)
	return graph.Node{}
}

func capabilities(f graph.Fragment, basis string) []graph.Node {
	var out []graph.Node
	for _, n := range f.Nodes {
		if n.Kind == graph.KindCapability && n.Basis == basis {
			out = append(out, n)
		}
	}
	return out
}

func TestFromClaudeSettings_ToolMapping(t *testing.T) {
	cases := []struct {
		rule, typ, target, reason string
	}{
		{"Bash(npm test *)", graph.CapExecuteShell, "npm test *", ""},
		{"Bash", graph.CapExecuteShell, "*", ""},
		{"PowerShell(Get-ChildItem *)", graph.CapExecuteShell, "Get-ChildItem *", ""},
		{"Read(./.env)", graph.CapReadFiles, "./.env", ""},
		{"Edit(docs/**)", graph.CapWriteFiles, "docs/**", ""},
		{"Write(docs/**)", graph.CapWriteFiles, "docs/**", graph.ReasonRuleNeverMatches},
		{"NotebookEdit(nb/**)", graph.CapWriteFiles, "nb/**", graph.ReasonRuleNeverMatches},
		{"Write", graph.CapWriteFiles, "*", ""},
		{"NotebookEdit", graph.CapWriteFiles, "*", ""},
		{"Glob(src/**)", graph.CapReadFiles, "src/**", graph.ReasonRuleNeverMatches},
		{"Glob", graph.CapReadFiles, "*", ""},
		{"WebFetch(domain:example.com)", graph.CapNetwork, "domain:example.com", ""},
		{"WebSearch", graph.CapNetwork, "*", ""},
		{"mcp__github__get_*", graph.CapInvokeMCPTool, "mcp__github__get_*", ""},
	}
	var rules []string
	for _, c := range cases {
		rules = append(rules, c.rule)
	}
	f, stats := fromSettings(userFile(platform.ClaudePermissions{Allow: rules}))
	for _, c := range cases {
		n := capByRule(t, f, c.rule)
		if n.Type != c.typ || n.TargetScope != c.target || n.Approval != graph.ApprovalAllow || n.AppliesIn != "user" {
			t.Errorf("%s: got type %q target %q approval %q applies_in %q, want %q %q allow user",
				c.rule, n.Type, n.TargetScope, n.Approval, n.AppliesIn, c.typ, c.target)
		}
		if n.InForce != (c.reason == "") || n.NotInForceReason != c.reason {
			t.Errorf("%s: in_force %v reason %q, want reason %q", c.rule, n.InForce, n.NotInForceReason, c.reason)
		}
	}
	if len(stats.Unmodeled) != 0 || stats.Malformed != 0 {
		t.Errorf("modeled rules counted as unmodeled: %+v", stats)
	}
}

func TestFromClaudeSettings_UnmodeledAndMalformedAreCounted(t *testing.T) {
	f, stats := fromSettings(userFile(platform.ClaudePermissions{
		Allow: []string{"MultiEdit(docs/**)", "Agent(Explore)", "Cd", "Agent(Plan)", "Bash(npm", "(x)", ""},
		Deny:  []string{"Bash(echo (unclosed)"},
	}))
	want := map[string]int{"MultiEdit": 1, "Agent": 2, "Cd": 1}
	if !reflect.DeepEqual(stats.Unmodeled, want) {
		t.Errorf("unmodeled: got %v, want %v", stats.Unmodeled, want)
	}
	if stats.Malformed != 3 {
		t.Errorf("malformed: got %d, want 3 (\"Bash(npm\", \"(x)\", \"\")", stats.Malformed)
	}
	// "Bash(echo (unclosed)" ends with ")": its specifier is literal.
	if n := capByRule(t, f, "Bash(echo (unclosed)"); n.TargetScope != "echo (unclosed" {
		t.Errorf("nested parentheses: target %q", n.TargetScope)
	}
	if stats.NestedParentheses != 1 {
		t.Errorf("nested parentheses: got %d, want 1", stats.NestedParentheses)
	}
	if got := len(capabilities(f, graph.BasisConfigured)); got != 1 {
		t.Errorf("configured capabilities: got %d, want only the nested-parentheses rule", got)
	}
}

func TestFromClaudeSettings_ListsGiveApproval(t *testing.T) {
	f, _ := fromSettings(userFile(platform.ClaudePermissions{
		Allow: []string{"Bash(git status)"}, Ask: []string{"Bash(git push *)"}, Deny: []string{"Read(./.env)"},
	}))
	for rule, want := range map[string]string{"Bash(git status)": "allow", "Bash(git push *)": "ask", "Read(./.env)": "deny"} {
		n := capByRule(t, f, rule)
		if n.Approval != want {
			t.Errorf("%s: approval %q, want %q", rule, n.Approval, want)
		}
	}
	n := capByRule(t, f, "Bash(git push *)")
	if n.Source != userSettings+"#/permissions/ask/0" {
		t.Errorf("source: %q", n.Source)
	}
}

func TestFromClaudeSettings_WorkspaceTrust(t *testing.T) {
	p := platform.ClaudePermissions{
		Allow:                 []string{"Bash(npm test)"},
		Deny:                  []string{"Bash(rm *)"},
		Ask:                   []string{"Bash(git push *)"},
		AdditionalDirectories: []string{"../shared"},
	}
	build := func(trust []platform.ProjectTrust) graph.Fragment {
		f, _ := graph.FromClaudeSettings(platform.ClaudeSettingsScan{
			Files: []platform.ClaudeSettingsFile{projectFile(p), localFile(platform.ClaudePermissions{Allow: []string{"Bash(make)"}})},
			Trust: trust,
		}, scanTime)
		return f
	}

	untrusted := build([]platform.ProjectTrust{{Path: "/src/other", Accepted: true}, {Path: permRepo, Accepted: false}})
	for rule, wantReason := range map[string]string{
		"Bash(npm test)":                  graph.ReasonWorkspaceUntrusted,
		"additionalDirectories=../shared": graph.ReasonWorkspaceUntrusted,
		"Bash(rm *)":                      "",
		"Bash(git push *)":                "",
		"Bash(make)":                      "", // local allow: not held by project trust
	} {
		n := capByRule(t, untrusted, rule)
		if n.NotInForceReason != wantReason || n.InForce != (wantReason == "") {
			t.Errorf("untrusted %s: in_force %v reason %q, want reason %q", rule, n.InForce, n.NotInForceReason, wantReason)
		}
	}

	// Three entries for the same repository (spellings of one path) with
	// different values: one true is enough.
	trustedFrag := build([]platform.ProjectTrust{
		{Path: permRepo, Accepted: false},
		{Path: permRepo + "/", Accepted: true},
		{Path: permRepo, Accepted: false},
	})
	for _, rule := range []string{"Bash(npm test)", "additionalDirectories=../shared"} {
		if n := capByRule(t, trustedFrag, rule); !n.InForce {
			t.Errorf("trusted %s: not in force (%s)", rule, n.NotInForceReason)
		}
	}
}

func TestFromClaudeSettings_PermissionModes(t *testing.T) {
	f, _ := fromSettings(
		userFile(platform.ClaudePermissions{DefaultMode: "auto", DisableBypassPermissionsMode: "disable", BlockReadsOutsideWorkingDirectories: true}),
		projectFile(platform.ClaudePermissions{DefaultMode: "auto", DisableAutoMode: "disable", DisableAutoModePointer: "/disableAutoMode"}),
		localFile(platform.ClaudePermissions{DefaultMode: "bypassPermissions"}),
	)
	modes := map[string]graph.Node{}
	for _, n := range capabilities(f, graph.BasisConfigured) {
		modes[n.AppliesIn+" "+n.Rule] = n
	}
	check := func(key, typ, target, approval, reason, source string) {
		t.Helper()
		n, ok := modes[key]
		if !ok {
			t.Fatalf("no capability %q in %v", key, modes)
		}
		if n.Type != typ || n.TargetScope != target || n.Approval != approval || n.NotInForceReason != reason || n.InForce != (reason == "") || n.Source != source {
			t.Errorf("%s: got %+v", key, n)
		}
	}
	check("user defaultMode=auto", graph.CapPermissionMode, "auto", "allow", "", userSettings+"#/permissions/defaultMode")
	check("project:/src/app defaultMode=auto", graph.CapPermissionMode, "auto", "allow", graph.ReasonModeNotAllowedInScope, projSettings+"#/permissions/defaultMode")
	check("local:/src/app defaultMode=bypassPermissions", graph.CapPermissionMode, "bypassPermissions", "allow", graph.ReasonModeNotAllowedInScope, localSettings+"#/permissions/defaultMode")
	check("user disableBypassPermissionsMode=disable", graph.CapPermissionMode, "bypassPermissions", "deny", "", userSettings+"#/permissions/disableBypassPermissionsMode")
	check("project:/src/app disableAutoMode=disable", graph.CapPermissionMode, "auto", "deny", "", projSettings+"#/disableAutoMode")
	check("user blockReadsOutsideWorkingDirectories=true", graph.CapReadFiles, "outside_working_directories", "deny", "", userSettings+"#/permissions/blockReadsOutsideWorkingDirectories")
}

func TestFromClaudeSettings_DedupeAndStableIDs(t *testing.T) {
	ids := func(rules ...string) []string {
		f, _ := fromSettings(userFile(platform.ClaudePermissions{Allow: rules}))
		var out []string
		for _, n := range f.Nodes {
			out = append(out, n.ID)
		}
		for _, e := range f.Edges {
			out = append(out, e.ID)
		}
		sort.Strings(out)
		return out
	}
	f, _ := fromSettings(userFile(platform.ClaudePermissions{Allow: []string{" Bash(npm  test) ", "Read(x)", "Bash(npm test)"}}))
	if got := len(capabilities(f, graph.BasisConfigured)); got != 2 {
		t.Errorf("capabilities: got %d, want 2 (the two spellings of Bash(npm test) are one)", got)
	}
	for _, n := range capabilities(f, graph.BasisConfigured) {
		if strings.Contains(n.Rule, "npm") && n.Source != userSettings+"#/permissions/allow/0" {
			t.Errorf("duplicate kept source %q, want the first occurrence", n.Source)
		}
	}
	a := ids("Bash(npm test)", "Read(x)", "Edit(y)")
	if b := ids("Bash(npm test)", "Read(x)", "Edit(y)"); !reflect.DeepEqual(a, b) {
		t.Error("same input, different ids")
	}
	if c := ids("Edit(y)", "Bash(npm test)", "Read(x)"); !reflect.DeepEqual(a, c) {
		t.Error("reordered rules changed the ids")
	}
}

func TestFromClaudeSettings_SecretInRuleIsRedacted(t *testing.T) {
	token := "ghp_" + strings.Repeat("Q", 36)
	rule := `Bash(curl -H "Authorization: Bearer ` + token + `" *)`
	f, _ := fromSettings(localFile(platform.ClaudePermissions{Allow: []string{rule}}))
	g := graph.Build(f, testProvenance, scanTime)
	out, err := graph.Encode(g)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.Contains(string(out), token) || strings.Contains(string(out), strings.Repeat("Q", 12)) {
		t.Fatalf("token reached the graph:\n%s", out)
	}
	n := capabilities(f, graph.BasisConfigured)
	if len(n) != 1 || n[0].Type != graph.CapExecuteShell || !strings.Contains(n[0].Rule, " ... ") {
		t.Errorf("redacted rule: %+v", n)
	}
}

func TestFromClaudeSettings_FilesBecomeConfigAndCoverage(t *testing.T) {
	failed := settingsFile(platform.MCPScopeProject, permRepo, projSettings, platform.ClaudePermissions{})
	failed.Status, failed.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, "bad")
	missing := settingsFile(platform.MCPScopeLocal, permRepo, localSettings, platform.ClaudePermissions{})
	missing.Status = platform.MCPSourceMissing
	f, _ := fromSettings(userFile(platform.ClaudePermissions{}), failed, missing)

	var clients, configs int
	for _, n := range f.Nodes {
		switch n.Type {
		case graph.AgentAIClient:
			clients++
			if n.Platform != "claude-code" || n.ConfigPath != claudeDir {
				t.Errorf("client: %+v", n)
			}
		case graph.ResourceAgentConfig:
			configs++
			if n.Path != userSettings {
				t.Errorf("agent_config for a file not read: %+v", n)
			}
		}
	}
	if clients != 1 || configs != 1 {
		t.Errorf("clients %d, configs %d; want 1 and 1", clients, configs)
	}
	if !reflect.DeepEqual(f.Coverage.SourcesRead, []string{userSettings}) {
		t.Errorf("sources_read: %v", f.Coverage.SourcesRead)
	}
	if len(f.Coverage.SourcesFailed) != 1 || f.Coverage.SourcesFailed[0].Path != projSettings {
		t.Errorf("sources_failed: %+v", f.Coverage.SourcesFailed)
	}
}

func TestFromScan_ProductDefaultsAndClientWithoutMCP(t *testing.T) {
	settings := platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{
		userFile(platform.ClaudePermissions{Allow: []string{"Bash(npm test)"}, DefaultMode: "acceptEdits"}),
		projectFile(platform.ClaudePermissions{Allow: []string{"Write(x)"}, Deny: []string{"Read(./.env)"}}),
	}}
	f, _, err := graph.FromScan(platform.MCPScan{}, settings, testSalt, scanTime)
	if err != nil {
		t.Fatalf("FromScan: %v", err)
	}
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	defaults := capabilities(f, graph.BasisProductDefault)
	var capEntries []knowledge.Entry
	for _, e := range base.Entries {
		if e.IsCapability() {
			capEntries = append(capEntries, e)
		}
	}
	if len(defaults) != 8 || len(capEntries) != 8 {
		t.Fatalf("product_default capabilities: got %d (of %d capability entries), want 8", len(defaults), len(capEntries))
	}
	sources := map[string]graph.Node{}
	for _, n := range defaults {
		sources[n.Source] = n
	}
	for _, e := range capEntries {
		n, ok := sources["kb:claude-code/"+e.ID+"@"+knowledge.Version]
		if !ok {
			t.Errorf("no product_default for %s", e.ID)
			continue
		}
		if n.Type != e.Type || n.Approval != e.Approval || n.TargetScope != e.TargetScope || !n.InForce || n.Rule != "" || n.AppliesIn != "" {
			t.Errorf("%s: %+v", e.ID, n)
		}
	}

	var client string
	for _, n := range f.Nodes {
		if n.Type == graph.AgentAIClient {
			client = n.ID
		}
	}
	if client == "" {
		t.Fatal("no Claude Code client without MCP sources")
	}
	has := 0
	for _, e := range f.Edges {
		if e.Type == graph.EdgeHas && e.From == client {
			has++
		}
	}
	if want := 8 + len(capabilities(f, graph.BasisConfigured)); has != want {
		t.Errorf("has edges: got %d, want %d", has, want)
	}

	g := graph.Build(f, testProvenance, scanTime)
	if _, err := graph.Encode(g); err != nil {
		t.Errorf("full graph: %v", err)
	}
}

func TestProductDefaults_OnlyForClaudeCodeClients(t *testing.T) {
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	f := graph.ProductDefaults([]graph.Node{{ID: "agent_client_x", Kind: graph.KindAgent, Type: graph.AgentAIClient, Platform: "cursor"}}, base)
	if len(f.Nodes) != 0 {
		t.Errorf("Cursor got Claude Code defaults: %+v", f.Nodes)
	}
}
