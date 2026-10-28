package graph_test

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

func ptrBool(b bool) *bool { return &b }

// --- .mcp.json approval -----------------------------------------------------

const mcpJSONPath = "/src/app/.mcp.json"

// projectServer is a .mcp.json server of permRepo.
func projectServer(name string) platform.MCPServer {
	return platform.MCPServer{
		Client: "claude-code-project", Name: name, Command: "npx", Args: []string{},
		ConfigPath: mcpJSONPath, Pointer: "/mcpServers/" + name, LoadsMCP: true,
		ConfigDir: claudeDir, Scope: platform.MCPScopeProject, ScopePath: permRepo,
	}
}

func mcpScanOf(servers ...platform.MCPServer) platform.MCPScan {
	return platform.MCPScan{
		Servers: servers,
		Sources: []platform.MCPSource{{Platform: "claude-code-project", ConfigPath: mcpJSONPath, ConfigDir: claudeDir, Status: platform.MCPSourceRead, LoadsMCP: true}},
	}
}

// loadsFor returns the approval_state and certainty of the loads edge of
// each server, by server name.
func loadsFor(t *testing.T, f graph.Fragment) map[string][2]string {
	t.Helper()
	names := map[string]string{}
	for _, n := range f.Nodes {
		if n.Type == graph.AgentMCPServer {
			names[n.ID] = n.Name
		}
	}
	out := map[string][2]string{}
	for _, e := range f.Edges {
		if e.Type == graph.EdgeLoads {
			out[names[e.To]] = [2]string{e.ApprovalState, e.Certainty}
		}
	}
	return out
}

func TestFromScan_MCPJSONApproval(t *testing.T) {
	approved := [2]string{graph.ApprovalApproved, graph.CertaintyObserved}
	rejected := [2]string{graph.ApprovalRejected, graph.CertaintyObserved}
	pending := [2]string{graph.ApprovalPending, graph.CertaintyInferred}
	trustedRepo := []platform.ProjectTrust{{Path: permRepo, Accepted: true}}

	cases := []struct {
		name  string
		files []platform.ClaudeSettingsFile
		trust []platform.ProjectTrust
		want  [2]string
	}{
		{"nothing set", nil, trustedRepo, pending},
		{"enabled in local, trusted", []platform.ClaudeSettingsFile{withApprovals(localFile(platform.ClaudePermissions{}), []string{"db"}, nil, nil)}, trustedRepo, approved},
		{"enabled in project, untrusted is ignored", []platform.ClaudeSettingsFile{withApprovals(projectFile(platform.ClaudePermissions{}), []string{"db"}, nil, nil)}, nil, pending},
		{"enabled in local, untrusted is ignored", []platform.ClaudeSettingsFile{withApprovals(localFile(platform.ClaudePermissions{}), []string{"db"}, nil, nil)}, nil, pending},
		{"enabled in user, untrusted", []platform.ClaudeSettingsFile{withApprovals(userFile(platform.ClaudePermissions{}), []string{"db"}, nil, nil)}, nil, approved},
		{"enableAll in user", []platform.ClaudeSettingsFile{withApprovals(userFile(platform.ClaudePermissions{}), nil, nil, ptrBool(true))}, nil, approved},
		{"enableAll false in local overrides user true, trusted", []platform.ClaudeSettingsFile{
			withApprovals(userFile(platform.ClaudePermissions{}), nil, nil, ptrBool(true)),
			withApprovals(localFile(platform.ClaudePermissions{}), nil, nil, ptrBool(false)),
		}, trustedRepo, pending},
		{"enableAll true anywhere honored, untrusted", []platform.ClaudeSettingsFile{
			withApprovals(userFile(platform.ClaudePermissions{}), nil, nil, ptrBool(true)),
			withApprovals(localFile(platform.ClaudePermissions{}), nil, nil, ptrBool(false)),
		}, nil, approved},
		{"rejected in project, even untrusted", []platform.ClaudeSettingsFile{withApprovals(projectFile(platform.ClaudePermissions{}), nil, []string{"db"}, nil)}, nil, rejected},
		{"rejected beats enabled and enableAll", []platform.ClaudeSettingsFile{
			withApprovals(userFile(platform.ClaudePermissions{}), []string{"db"}, nil, ptrBool(true)),
			withApprovals(localFile(platform.ClaudePermissions{}), nil, []string{"db"}, nil),
		}, trustedRepo, rejected},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, _, err := graph.FromScan(mcpScanOf(projectServer("db")), platform.ClaudeSettingsScan{Files: c.files, Trust: c.trust}, testSalt, scanTime)
			if err != nil {
				t.Fatal(err)
			}
			if got := loadsFor(t, f)["db"]; got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
			if _, err := graph.Encode(graph.Build(f, testProvenance, scanTime)); err != nil {
				t.Errorf("graph: %v", err)
			}
		})
	}
}

func TestFromScan_ManagedApprovalHonoredUntrusted(t *testing.T) {
	doc := managedDoc("/pf/ClaudeCode/managed-settings.json", true)
	doc.EnabledMcpjsonServers = []string{"db"}
	scan := platform.ClaudeSettingsScan{Managed: []platform.ManagedSource{{Kind: platform.ManagedFiles, Files: []platform.ClaudeSettingsFile{doc}}}}
	f, _, err := graph.FromScan(mcpScanOf(projectServer("db"), projectServer("other")), scan, testSalt, scanTime)
	if err != nil {
		t.Fatal(err)
	}
	got := loadsFor(t, f)
	if got["db"][0] != graph.ApprovalApproved || got["other"][0] != graph.ApprovalPending {
		t.Errorf("got %v", got)
	}
}

func withApprovals(f platform.ClaudeSettingsFile, enabled, disabled []string, all *bool) platform.ClaudeSettingsFile {
	f.EnabledMcpjsonServers, f.DisabledMcpjsonServers, f.EnableAllProjectMcpServers = enabled, disabled, all
	return f
}

// --- managed settings -----------------------------------------------------------

const (
	remotePath = "/home/albert/.claude/remote-settings.json"
	hklmPath   = `registry:HKLM\SOFTWARE\Policies\ClaudeCode\Settings`
	hkcuPath   = `registry:HKCU\SOFTWARE\Policies\ClaudeCode\Settings`
	filesMain  = "/pf/ClaudeCode/managed-settings.json"
	filesD10   = "/pf/ClaudeCode/managed-settings.d/10-team.json"
)

func managedDoc(path string, policy bool, allow ...string) platform.ClaudeSettingsFile {
	return platform.ClaudeSettingsFile{
		Scope: platform.MCPScopeManaged, ConfigPath: path, ConfigDir: claudeDir,
		Status: platform.MCPSourceRead, HasPolicyKey: policy,
		Permissions: platform.ClaudePermissions{Allow: allow},
	}
}

func source(kind string, docs ...platform.ClaudeSettingsFile) platform.ManagedSource {
	return platform.ManagedSource{Kind: kind, Files: docs}
}

// managedRules returns the rules of the managed capabilities, sorted.
func managedRules(f graph.Fragment) []string {
	var out []string
	for _, n := range f.Nodes {
		if n.Kind == graph.KindCapability && n.AppliesIn == "managed" {
			out = append(out, n.Rule)
		}
	}
	sort.Strings(out)
	return out
}

func configPaths(f graph.Fragment) []string {
	var out []string
	for _, n := range f.Nodes {
		if n.Type == graph.ResourceAgentConfig {
			out = append(out, n.Path)
		}
	}
	sort.Strings(out)
	return out
}

func managedScan(sources ...platform.ManagedSource) platform.ClaudeSettingsScan {
	return platform.ClaudeSettingsScan{Managed: sources}
}

func TestFromClaudeSettings_ManagedPrecedence(t *testing.T) {
	cases := []struct {
		name    string
		sources []platform.ManagedSource
		want    []string
	}{
		{"first-wins: HKLM over files", []platform.ManagedSource{
			source(platform.ManagedHKLM, managedDoc(hklmPath, true, "Bash(hklm)")),
			source(platform.ManagedFiles, managedDoc(filesMain, true, "Bash(files)")),
		}, []string{"Bash(hklm)"}},
		{"first-wins: remote cache over HKLM", []platform.ManagedSource{
			source(platform.ManagedRemote, managedDoc(remotePath, true, "Bash(remote)")),
			source(platform.ManagedHKLM, managedDoc(hklmPath, true, "Bash(hklm)")),
		}, []string{"Bash(remote)"}},
		{"empty remote cache does not count", []platform.ManagedSource{
			source(platform.ManagedRemote, managedDoc(remotePath, false)),
			source(platform.ManagedHKLM, managedDoc(hklmPath, true, "Bash(hklm)")),
		}, []string{"Bash(hklm)"}},
		{"files: managed-settings.json and .d combine", []platform.ManagedSource{
			source(platform.ManagedFiles, managedDoc(filesMain, true, "Bash(main)"), managedDoc(filesD10, true, "Bash(d10)")),
		}, []string{"Bash(d10)", "Bash(main)"}},
		{"HKCU alone applies", []platform.ManagedSource{
			source(platform.ManagedFiles),
			source(platform.ManagedHKCU, managedDoc(hkcuPath, true, "Bash(hkcu)")),
		}, []string{"Bash(hkcu)"}},
		{"HKCU ignored under an admin policy", []platform.ManagedSource{
			source(platform.ManagedFiles, managedDoc(filesMain, true, "Bash(files)")),
			source(platform.ManagedHKCU, managedDoc(hkcuPath, true, "Bash(hkcu)")),
		}, []string{"Bash(files)"}},
		{"HKCU ignored under an unreadable admin document", []platform.ManagedSource{
			source(platform.ManagedHKLM, platform.ClaudeSettingsFile{Scope: platform.MCPScopeManaged, ConfigPath: hklmPath, ConfigDir: claudeDir, Status: platform.MCPSourceFailed, Reason: graph.Reason(graph.ReasonReadError, "denied")}),
			source(platform.ManagedHKCU, managedDoc(hkcuPath, true, "Bash(hkcu)")),
		}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, _ := graph.FromClaudeSettings(managedScan(c.sources...), scanTime)
			if got := managedRules(f); !reflect.DeepEqual(got, c.want) {
				t.Errorf("managed rules: got %v, want %v", got, c.want)
			}
			if _, err := graph.Encode(graph.Build(f, testProvenance, scanTime)); err != nil {
				t.Errorf("graph: %v", err)
			}
		})
	}
}

func TestFromClaudeSettings_ManagedReadSourcesAreConfigs(t *testing.T) {
	f, _ := graph.FromClaudeSettings(managedScan(
		source(platform.ManagedHKLM, managedDoc(hklmPath, true, "Bash(hklm)")),
		source(platform.ManagedFiles, managedDoc(filesMain, true, "Bash(files)")),
		source(platform.ManagedHKCU, managedDoc(hkcuPath, true, "Bash(hkcu)")),
	), scanTime)
	want := []string{filesMain, hklmPath, hkcuPath}
	sort.Strings(want)
	if got := configPaths(f); !reflect.DeepEqual(got, want) {
		t.Errorf("agent_config: got %v, want every managed source read, effective or not: %v", got, want)
	}
}

func TestFromClaudeSettings_ManagedMerge(t *testing.T) {
	hklm := managedDoc(hklmPath, true, "Bash(hklm)")
	hklm.ManagedSourcesBehavior = "merge"
	files := managedDoc(filesMain, true, "Bash(files)")
	files.Permissions.DefaultMode = "acceptEdits"
	files.Permissions.DisableBypassPermissionsMode = "disable"
	files.EnableAllProjectMcpServers = ptrBool(true)
	hkcu := managedDoc(hkcuPath, true, "Bash(hkcu)")

	f, _ := graph.FromClaudeSettings(managedScan(
		source(platform.ManagedHKLM, hklm),
		source(platform.ManagedFiles, files),
		source(platform.ManagedHKCU, hkcu),
	), scanTime)
	// Lists and locks combine across admin sources; permissions.defaultMode
	// comes from the highest-ranked source only (unset in HKLM, so none);
	// HKCU never merges.
	want := []string{"Bash(files)", "Bash(hklm)", "disableBypassPermissionsMode=disable"}
	if got := managedRules(f); !reflect.DeepEqual(got, want) {
		t.Errorf("merge: got %v, want %v", got, want)
	}

	// The same sources under first-wins: HKLM alone.
	hklm.ManagedSourcesBehavior = ""
	f, _ = graph.FromClaudeSettings(managedScan(source(platform.ManagedHKLM, hklm), source(platform.ManagedFiles, files)), scanTime)
	if got := managedRules(f); !reflect.DeepEqual(got, []string{"Bash(hklm)"}) {
		t.Errorf("first-wins: got %v", got)
	}
}

func TestFromClaudeSettings_ManagedFilesLaterReplacesSingleValue(t *testing.T) {
	main := managedDoc(filesMain, true)
	main.Permissions.DefaultMode = "plan"
	d10 := managedDoc(filesD10, true)
	d10.Permissions.DefaultMode = "acceptEdits"
	f, _ := graph.FromClaudeSettings(managedScan(source(platform.ManagedFiles, main, d10)), scanTime)
	if got := managedRules(f); !reflect.DeepEqual(got, []string{"defaultMode=acceptEdits"}) {
		t.Errorf("got %v, want the later file's defaultMode", got)
	}
}

func TestFromClaudeSettings_ManagedRulesOnly(t *testing.T) {
	doc := managedDoc(filesMain, true, "Bash(managed)")
	doc.AllowManagedPermissionRulesOnly = true
	scan := managedScan(source(platform.ManagedFiles, doc))
	scan.Files = []platform.ClaudeSettingsFile{userFile(platform.ClaudePermissions{
		Allow: []string{"Bash(npm test)"}, Deny: []string{"Read(./.env)"}, DefaultMode: "acceptEdits",
	})}
	f, _ := graph.FromClaudeSettings(scan, scanTime)
	for rule, wantReason := range map[string]string{
		"Bash(npm test)":          graph.ReasonManagedRulesOnly,
		"Read(./.env)":            graph.ReasonManagedRulesOnly,
		"defaultMode=acceptEdits": "", // not an allow/ask/deny rule
		"Bash(managed)":           "",
	} {
		n := capByRule(t, f, rule)
		if n.NotInForceReason != wantReason || n.InForce != (wantReason == "") {
			t.Errorf("%s: in_force %v reason %q, want %q", rule, n.InForce, n.NotInForceReason, wantReason)
		}
	}
}

func TestFromClaudeSettings_LegacyManagedPathInCoverage(t *testing.T) {
	scan := managedScan()
	scan.LegacyManagedPath = `C:\ProgramData\ClaudeCode\managed-settings.json`
	f, _ := graph.FromClaudeSettings(scan, scanTime)
	if len(f.Coverage.SourcesFailed) != 1 {
		t.Fatalf("sources_failed: %+v", f.Coverage.SourcesFailed)
	}
	sf := f.Coverage.SourcesFailed[0]
	if sf.Path != scan.LegacyManagedPath || sf.Reason != graph.ReasonSourceUnsupported+": "+graph.LegacyManagedReason {
		t.Errorf("got %+v", sf)
	}
	if len(f.Nodes) != 0 {
		t.Errorf("legacy path created nodes: %+v", f.Nodes)
	}
}

// --- env credentials and executables ------------------------------------------

func TestFromScan_SettingsEnvBecomesCredential(t *testing.T) {
	token := "ghp_" + strings.Repeat("E", 36)
	user := userFile(platform.ClaudePermissions{})
	user.Env = []platform.EnvVar{{Key: "GITHUB_TOKEN", Value: secrets.NewSecretValue(token)}}

	// The same token in an MCP server's env: one credential, two locations.
	srv := platform.MCPServer{
		Client: "claude-code", Name: "gh", Command: "npx", Args: []string{}, LoadsMCP: true,
		ConfigPath: "/home/albert/.claude.json", ConfigDir: claudeDir, Scope: platform.MCPScopeUser,
		Env: []platform.EnvVar{{Key: "GH", Value: secrets.NewSecretValue(token)}},
	}
	mcp := platform.MCPScan{Servers: []platform.MCPServer{srv}, Sources: []platform.MCPSource{
		{Platform: "claude-code", ConfigPath: srv.ConfigPath, ConfigDir: claudeDir, Status: platform.MCPSourceRead, LoadsMCP: true},
	}}
	f, _, err := graph.FromScan(mcp, platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{user}}, testSalt, scanTime)
	if err != nil {
		t.Fatal(err)
	}

	var creds []graph.Node
	var client string
	for _, n := range f.Nodes {
		switch {
		case n.Kind == graph.KindCredential:
			creds = append(creds, n)
		case n.Type == graph.AgentAIClient:
			client = n.ID
		}
	}
	if len(creds) != 1 {
		t.Fatalf("credentials: got %d, want 1", len(creds))
	}
	wantLocs := []string{"/home/albert/.claude.json#/mcpServers/gh/env/GH", userSettings + "#/env/GITHUB_TOKEN"}
	if !reflect.DeepEqual(creds[0].Locations, wantLocs) {
		t.Errorf("locations: got %v, want %v", creds[0].Locations, wantLocs)
	}
	var toClient bool
	for _, e := range f.Edges {
		if e.Type == graph.EdgeReceives && e.From == creds[0].ID && e.To == client {
			toClient = e.Source == userSettings+"#/env/GITHUB_TOKEN" && e.Certainty == graph.CertaintyObserved
		}
	}
	if !toClient {
		t.Error("no receives edge from the settings env credential to Claude Code")
	}
	out, err := graph.Encode(graph.Build(f, testProvenance, scanTime))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), token) {
		t.Fatal("token in graph")
	}
}

func TestFromClaudeSettings_ExecutablesAreResources(t *testing.T) {
	user := userFile(platform.ClaudePermissions{})
	user.Executables = []string{"/hooks/PreToolUse/0/hooks/0/command", "/hooks/Stop/0/hooks/1/command", "/apiKeyHelper"}
	f, _ := fromSettings(user)
	var got []string
	for _, n := range f.Nodes {
		if n.Type == graph.ResourceAgentExecutable {
			got = append(got, n.Path)
			if n.Configures == "" {
				t.Errorf("%s configures nothing", n.ID)
			}
		}
	}
	sort.Strings(got)
	want := []string{userSettings + "#/apiKeyHelper", userSettings + "#/hooks/PreToolUse/0/hooks/0/command", userSettings + "#/hooks/Stop/0/hooks/1/command"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("agent_executable: got %v, want %v", got, want)
	}
	if _, err := graph.Encode(graph.Build(f, testProvenance, scanTime)); err != nil {
		t.Errorf("graph: %v", err)
	}
}

func TestFromClaudeSettings_ManagedSingleExecutableFromWinnerOnly(t *testing.T) {
	main := managedDoc(filesMain, true)
	main.Executables = []string{"/apiKeyHelper", "/hooks/Stop/0/hooks/0/command"}
	d10 := managedDoc(filesD10, true)
	d10.Executables = []string{"/apiKeyHelper"}
	f, _ := graph.FromClaudeSettings(managedScan(source(platform.ManagedFiles, main, d10)), scanTime)
	var got []string
	for _, n := range f.Nodes {
		if n.Type == graph.ResourceAgentExecutable {
			got = append(got, n.Path)
		}
	}
	sort.Strings(got)
	want := []string{filesMain + "#/hooks/Stop/0/hooks/0/command", filesD10 + "#/apiKeyHelper"}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want hooks combined and apiKeyHelper from the later file: %v", got, want)
	}
}

func TestFromScan_ManagedEnvAcrossAdminSources(t *testing.T) {
	hklm := managedDoc(hklmPath, true)
	hklm.Env = []platform.EnvVar{{Key: "A_TOKEN", Value: secrets.NewSecretValue("ghp_" + strings.Repeat("H", 36))}}
	files := managedDoc(filesMain, true)
	files.Env = []platform.EnvVar{
		{Key: "A_TOKEN", Value: secrets.NewSecretValue("ghp_" + strings.Repeat("F", 36))},
		{Key: "B_TOKEN", Value: secrets.NewSecretValue("ghp_" + strings.Repeat("G", 36))},
	}
	scan := managedScan(source(platform.ManagedHKLM, hklm), source(platform.ManagedFiles, files))
	f, _, err := graph.FromScan(platform.MCPScan{}, scan, testSalt, scanTime)
	if err != nil {
		t.Fatal(err)
	}
	var locs []string
	for _, n := range f.Nodes {
		if n.Kind == graph.KindCredential {
			locs = append(locs, n.Locations...)
		}
	}
	sort.Strings(locs)
	// A_TOKEN from HKLM (higher), B_TOKEN filled in from the files: env
	// merges per variable across admin sources even under first-wins.
	want := []string{filesMain + "#/env/B_TOKEN", hklmPath + "#/env/A_TOKEN"}
	sort.Strings(want)
	if !reflect.DeepEqual(locs, want) {
		t.Errorf("got %v, want %v", locs, want)
	}
}

// B10 (decision 2026-10-27, blindaje): Claude Code's .mcp.json approvals are
// not Cursor's. A project server of <repo>/.cursor/mcp.json with the same
// name keeps approval_state unknown and certainty inferred; the Claude Code
// server beside it is still approved.
func TestFromScan_CursorProjectServerIgnoresClaudeApprovals(t *testing.T) {
	cursorDir := "/home/me/.cursor"
	cursorSrv := platform.MCPServer{
		Client: "cursor", Name: "db", Command: "npx", Args: []string{},
		ConfigPath: permRepo + "/.cursor/mcp.json", Pointer: "/mcpServers/db", LoadsMCP: true,
		ConfigDir: cursorDir, Scope: platform.MCPScopeProject, ScopePath: permRepo,
	}
	scan := mcpScanOf(projectServer("db"), cursorSrv)
	scan.Sources = append(scan.Sources, platform.MCPSource{Platform: "cursor", ConfigPath: cursorSrv.ConfigPath, ConfigDir: cursorDir, Status: platform.MCPSourceRead, LoadsMCP: true})
	files := []platform.ClaudeSettingsFile{withApprovals(userFile(platform.ClaudePermissions{}), []string{"db"}, nil, ptrBool(true))}
	f, _, err := graph.FromScan(scan, platform.ClaudeSettingsScan{Files: files}, testSalt, scanTime)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][2]string{}
	for _, e := range f.Edges {
		if e.Type != graph.EdgeLoads {
			continue
		}
		for _, n := range f.Nodes {
			if n.ID == e.From {
				got[n.Platform] = [2]string{e.ApprovalState, e.Certainty}
			}
		}
	}
	if want := [2]string{graph.ApprovalUnknown, graph.CertaintyInferred}; got["cursor"] != want {
		t.Errorf("cursor: %v, want %v", got["cursor"], want)
	}
	if want := [2]string{graph.ApprovalApproved, graph.CertaintyObserved}; got["claude-code"] != want {
		t.Errorf("claude-code: %v, want %v", got["claude-code"], want)
	}
	if _, err := graph.Encode(graph.Build(f, testProvenance, scanTime)); err != nil {
		t.Errorf("graph: %v", err)
	}
}
