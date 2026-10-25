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

// Inference scenario: Claude Code with its configuration folder in
// /home/albert/.claude and one repository, /src/app, that also holds Cursor
// rules. Two credentials: one in the repository's .mcp.json (inside the
// working root), one in ~/.claude.json (outside it).
const (
	inferHome       = "/home/albert"
	claudeJSON      = "/home/albert/.claude.json"
	repoMCPJSON     = "/src/app/.mcp.json"
	cursorMCPJSON   = "/home/albert/.cursor/mcp.json"
	cursorRule      = "/src/app/.cursor/rules/x.mdc"
	repoClaudeMD    = "/src/app/CLAUDE.md"
	insideToken     = "ghp_INSIDEINSIDEINSIDEINSIDEINSIDEINSIDE"
	outsideToken    = "ghp_OUTSIDEOUTSIDEOUTSIDEOUTSIDEOUTSIDEO"
	assumeUser      = graph.AssumeProcessRunsAsUser
	assumeVersion   = graph.AssumeVersionAsDocumented
	assumeHook      = graph.AssumeNoPreToolUseHookBlocks
	assumeBash      = graph.AssumeBashToolAvailable
	assumeAuto      = graph.AssumeAutoClassifierPermits
	assumeReadDeny  = graph.AssumeReadDenyPartial
	assumeShellOpen = graph.AssumeShellWriteUnchecked
)

func envServer(client, path, dir, scope, scopePath, name, token string) platform.MCPServer {
	return platform.MCPServer{
		Client: client, Name: name, Command: "npx", Args: []string{}, LoadsMCP: true,
		ConfigPath: path, ConfigDir: dir, Scope: scope, ScopePath: scopePath,
		Env: []platform.EnvVar{{Key: "GITHUB_TOKEN", Value: secrets.NewSecretValue(token)}},
	}
}

func inferScan() platform.MCPScan {
	src := func(p, path, dir string) platform.MCPSource {
		return platform.MCPSource{Platform: p, ConfigPath: path, ConfigDir: dir, Status: platform.MCPSourceRead, LoadsMCP: true}
	}
	return platform.MCPScan{
		Servers: []platform.MCPServer{
			envServer("claude-code", claudeJSON, claudeDir, platform.MCPScopeUser, "", "outside", outsideToken),
			envServer("claude-code-project", repoMCPJSON, claudeDir, platform.MCPScopeProject, permRepo, "inside", insideToken),
		},
		Sources: []platform.MCPSource{
			src("claude-code", claudeJSON, claudeDir),
			src("claude-code-project", repoMCPJSON, claudeDir),
			src("cursor", cursorMCPJSON, cursorDir),
		},
	}
}

// trustedRepo marks /src/app as a trusted workspace.
var trustedRepo = []platform.ProjectTrust{{Path: permRepo, Accepted: true}}

// inferGraph builds, infers and encodes the scenario with the given
// settings; Encode validates schema and integrity, derived_from included.
func inferGraph(t *testing.T, settings platform.ClaudeSettingsScan) graph.Graph {
	t.Helper()
	f, _, err := graph.FromScan(inferScan(), settings, testSalt, scanTime)
	if err != nil {
		t.Fatal(err)
	}
	f = graph.AddInstructions(f, platform.InstructionScan{Files: []platform.InstructionFile{
		{Product: "cursor", ConfigDir: cursorDir, Path: cursorRule},
		{Product: "claude-code", ConfigDir: claudeDir, Path: repoClaudeMD},
	}}, scanTime)
	g, err := graph.Infer(graph.Build(f, testProvenance, scanTime), []string{permRepo}, inferHome)
	if err != nil {
		t.Fatal(err)
	}
	out, err := graph.Encode(g)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, tok := range []string{insideToken, outsideToken} {
		if strings.Contains(string(out), tok) {
			t.Fatal("token in graph")
		}
	}
	return g
}

func settingsOf(files ...platform.ClaudeSettingsFile) platform.ClaudeSettingsScan {
	return platform.ClaudeSettingsScan{Files: files, Trust: trustedRepo}
}

// inferred is one inferred edge, by readable names.
type inferred struct {
	typ, to, via string
	assumptions  []string
}

// inferredEdges lists the inferred edges of g: to is the credential's
// location file or the resource path, via the rule (configured) or the
// knowledge-base entry (product_default) of the capability.
func inferredEdges(t *testing.T, g graph.Graph) []inferred {
	t.Helper()
	nodes := map[string]graph.Node{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	var out []inferred
	for _, e := range g.Edges {
		if e.Type != graph.EdgeReaches && e.Type != graph.EdgeWritesConfigOf {
			continue
		}
		from := nodes[e.From]
		if from.Platform == "cursor" {
			continue // Cursor's edges (BV-48) have their tests: infer_cursor_test.go
		}
		if from.Platform != "claude-code" || e.Certainty != graph.CertaintyInferred {
			t.Errorf("edge %s from %s, certainty %s", e.ID, from.Platform, e.Certainty)
		}
		via := nodes[e.DerivedFrom[1]]
		if e.Type == graph.EdgeReaches && e.Via != via.ID {
			t.Errorf("edge %s: via %s, derived_from[1] %s", e.ID, e.Via, via.ID)
		}
		if e.DerivedFrom[0] != e.From || e.DerivedFrom[2] != e.To || e.Source != via.Source {
			t.Errorf("edge %s: derived_from %v, source %s", e.ID, e.DerivedFrom, e.Source)
		}
		name := via.Rule
		if via.Basis == graph.BasisProductDefault {
			name = strings.SplitN(strings.TrimPrefix(via.Source, "kb:claude-code/"), "@", 2)[0]
		}
		to := nodes[e.To]
		target := to.Path
		if to.Kind == graph.KindCredential {
			target = strings.SplitN(to.Locations[0], "#", 2)[0]
		}
		out = append(out, inferred{e.Type, target, name, e.Assumptions})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.typ != b.typ {
			return a.typ < b.typ
		}
		if a.to != b.to {
			return a.to < b.to
		}
		return a.via < b.via
	})
	return out
}

func edgesTo(es []inferred, typ, to string) []inferred {
	var out []inferred
	for _, e := range es {
		if e.typ == typ && e.to == to {
			out = append(out, e)
		}
	}
	return out
}

func vias(es []inferred) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.via)
	}
	return out
}

var (
	readsBash  = []string{assumeUser, assumeVersion, assumeBash, assumeHook}
	readsFiles = []string{assumeUser, assumeVersion, assumeHook}
	writesBase = []string{assumeUser, assumeVersion, assumeHook}
)

// With no settings: the read-only Bash commands reach both credentials, the
// file tools the one inside the working root; nothing writes.
// In Manual mode (defaultMode "default"): the read-only Bash commands reach
// both credentials, the file tools the one inside the working root; nothing
// writes.
func TestInfer_DefaultReaches(t *testing.T) {
	es := inferredEdges(t, inferGraph(t, settingsOf(userFile(platform.ClaudePermissions{DefaultMode: "default"}))))
	want := []inferred{
		{graph.EdgeReaches, claudeJSON, "shell_readonly_bash_commands", readsBash},
		{graph.EdgeReaches, repoMCPJSON, "read_in_working_dirs", readsFiles},
		{graph.EdgeReaches, repoMCPJSON, "shell_readonly_bash_commands", readsBash},
	}
	if !reflect.DeepEqual(es, want) {
		t.Errorf("got %+v\nwant %+v", es, want)
	}
}

func TestInfer_BlockReadsOutsideWorkingDirectories(t *testing.T) {
	es := inferredEdges(t, inferGraph(t, settingsOf(userFile(platform.ClaudePermissions{DefaultMode: "default", BlockReadsOutsideWorkingDirectories: true}))))
	if got := edgesTo(es, graph.EdgeReaches, claudeJSON); len(got) != 0 {
		t.Errorf("outside credential still reached: %+v", got)
	}
	if got := vias(edgesTo(es, graph.EdgeReaches, repoMCPJSON)); !reflect.DeepEqual(got, []string{"read_in_working_dirs", "shell_readonly_bash_commands"}) {
		t.Errorf("inside credential: %v", got)
	}
}

func TestInfer_ReadDenyPartial(t *testing.T) {
	g := inferGraph(t, settingsOf(projectFile(platform.ClaudePermissions{Deny: []string{"Read(./.mcp.json)"}})))
	var deny string
	for _, n := range g.Nodes {
		if n.Rule == "Read(./.mcp.json)" {
			deny = n.ID
		}
	}
	cursor := map[string]bool{}
	for _, n := range g.Nodes {
		cursor[n.ID] = n.Platform == "cursor"
	}
	for _, e := range g.Edges {
		if e.Type != graph.EdgeReaches || cursor[e.From] {
			continue // Claude Code's read deny; Cursor's ways are its own (BV-48)
		}
		inside := false
		for _, n := range g.Nodes {
			if n.ID == e.To {
				inside = strings.HasPrefix(n.Locations[0], repoMCPJSON)
			}
		}
		hasDeny := contains(e.Assumptions, assumeReadDeny) && contains(e.DerivedFrom, deny)
		if inside != hasDeny {
			t.Errorf("edge %s (inside %v): assumptions %v, derived_from %v", e.ID, inside, e.Assumptions, e.DerivedFrom)
		}
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// The agent resources of the scenario, by path.
var (
	resUserSettings = userSettings
	allResources    = []string{claudeJSON, cursorMCPJSON, repoMCPJSON, cursorRule, repoClaudeMD, resUserSettings}
	unprotected     = []string{cursorMCPJSON, cursorRule, repoClaudeMD}
)

func writeTargets(es []inferred, via string) []string {
	var out []string
	for _, e := range es {
		if e.typ == graph.EdgeWritesConfigOf && e.via == via {
			out = append(out, e.to)
		}
	}
	sort.Strings(out)
	return out
}

func sorted(s []string) []string {
	out := append([]string{}, s...)
	sort.Strings(out)
	return out
}

// The star case: Claude Code in acceptEdits rewrites, without asking, the
// Cursor rules of a repository it works in.
func TestInfer_AcceptEditsWritesCursorRules(t *testing.T) {
	files := []platform.ClaudeSettingsFile{
		userFile(platform.ClaudePermissions{DefaultMode: "acceptEdits"}),
		projectFile(platform.ClaudePermissions{}),
	}
	g := inferGraph(t, settingsOf(files...))
	es := inferredEdges(t, g)
	// Inside the working root and not protected: the Cursor rule and
	// CLAUDE.md. Not .mcp.json or .claude/settings.json (protected), nor
	// anything outside /src/app.
	if got := writeTargets(es, "defaultMode=acceptEdits"); !reflect.DeepEqual(got, sorted([]string{cursorRule, repoClaudeMD})) {
		t.Fatalf("acceptEdits writes: %v", got)
	}
	var cursorRes, cursorClient string
	for _, n := range g.Nodes {
		if n.Path == cursorRule {
			cursorRes, cursorClient = n.ID, n.Configures
		}
	}
	for _, n := range g.Nodes {
		if n.ID == cursorClient && n.Platform != "cursor" {
			t.Fatalf("the rule configures %s, not Cursor", n.Platform)
		}
	}
	for _, e := range edgesTo(es, graph.EdgeWritesConfigOf, cursorRule) {
		if !reflect.DeepEqual(e.assumptions, append(append([]string{}, writesBase...), graph.AssumeUIModeChoiceUnknown)) {
			t.Errorf("assumptions %v", e.assumptions)
		}
	}
	if cursorRes == "" {
		t.Fatal("no Cursor rule resource")
	}
}

func TestInfer_BypassWritesEverythingAndReaches(t *testing.T) {
	es := inferredEdges(t, inferGraph(t, settingsOf(userFile(platform.ClaudePermissions{DefaultMode: "bypassPermissions"}))))
	if got := writeTargets(es, "defaultMode=bypassPermissions"); !reflect.DeepEqual(got, sorted(allResources)) {
		t.Errorf("bypass writes: %v", got)
	}
	for _, cred := range []string{claudeJSON, repoMCPJSON} {
		var mode []inferred
		for _, e := range edgesTo(es, graph.EdgeReaches, cred) {
			if e.via == "defaultMode=bypassPermissions" {
				mode = append(mode, e)
			}
		}
		if len(mode) != 1 || !reflect.DeepEqual(mode[0].assumptions, []string{assumeUser, assumeVersion, graph.AssumeUIModeChoiceUnknown}) {
			t.Errorf("%s: mode reaches %+v", cred, mode)
		}
	}
}

func TestInfer_AutoSkipsProtectedAndAssumesClassifier(t *testing.T) {
	es := inferredEdges(t, inferGraph(t, settingsOf(userFile(platform.ClaudePermissions{DefaultMode: "auto"}))))
	if got := writeTargets(es, "defaultMode=auto"); !reflect.DeepEqual(got, sorted(unprotected)) {
		t.Errorf("auto writes: %v", got)
	}
	for _, e := range es {
		if e.via == "defaultMode=auto" && !contains(e.assumptions, assumeAuto) {
			t.Errorf("auto edge without %s: %+v", assumeAuto, e)
		}
	}
}

// An Edit allow writes what its pattern covers, never a protected path.
func TestInfer_EditAllow(t *testing.T) {
	user := userFile(platform.ClaudePermissions{Allow: []string{"Edit(//home/albert/.cursor/**)", "Edit(~/.claude/**)", "Edit(**/*.mdc)"}})
	es := inferredEdges(t, inferGraph(t, settingsOf(user)))
	if got := writeTargets(es, "Edit(//home/albert/.cursor/**)"); !reflect.DeepEqual(got, []string{cursorMCPJSON}) {
		t.Errorf("Edit(//home/albert/.cursor/**): %v", got)
	}
	if got := writeTargets(es, "Edit(~/.claude/**)"); len(got) != 0 {
		t.Errorf("Edit(~/.claude/**) reached protected paths: %v", got)
	}
	if got := writeTargets(es, "Edit(**/*.mdc)"); !reflect.DeepEqual(got, []string{cursorRule}) {
		t.Errorf("Edit(**/*.mdc): %v", got)
	}
}

func TestInfer_ShellAllowWritesEverything(t *testing.T) {
	user := userFile(platform.ClaudePermissions{Allow: []string{"Bash", "PowerShell(*)", "Bash(npm test)"}})
	es := inferredEdges(t, inferGraph(t, settingsOf(user)))
	for _, rule := range []string{"Bash", "PowerShell(*)"} {
		if got := writeTargets(es, rule); !reflect.DeepEqual(got, sorted(allResources)) {
			t.Errorf("%s writes: %v", rule, got)
		}
	}
	if got := writeTargets(es, "Bash(npm test)"); len(got) != 0 {
		t.Errorf("a scoped Bash allow writes: %v", got)
	}
	for _, e := range es {
		if e.via == "Bash" && !contains(e.assumptions, assumeShellOpen) {
			t.Errorf("shell edge without %s: %+v", assumeShellOpen, e)
		}
	}
}

func TestInfer_AskNeverYieldsEdges(t *testing.T) {
	user := userFile(platform.ClaudePermissions{DefaultMode: "default", Ask: []string{"Bash", "Edit(**)", "Read(**)"}})
	for _, e := range inferredEdges(t, inferGraph(t, settingsOf(user))) {
		if strings.HasPrefix(e.via, "Bash") || strings.HasPrefix(e.via, "Edit") || contains(e.assumptions, assumeReadDeny) {
			t.Errorf("edge from an ask rule: %+v", e)
		}
	}
}

// Capabilities not in force yield nothing: a project allow in an untrusted
// workspace, and a project bypassPermissions (not allowed in that scope).
func TestInfer_NotInForceYieldsNothing(t *testing.T) {
	scan := platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{
		userFile(platform.ClaudePermissions{DefaultMode: "default"}),
		projectFile(platform.ClaudePermissions{Allow: []string{"Bash"}}),
		localFile(platform.ClaudePermissions{DefaultMode: "bypassPermissions"}),
	}}
	for _, e := range inferredEdges(t, inferGraph(t, scan)) {
		if e.typ == graph.EdgeWritesConfigOf {
			t.Errorf("write edge from a capability not in force: %+v", e)
		}
	}
}

func TestInfer_EffectiveMode(t *testing.T) {
	// A disabled mode is not effective.
	user := userFile(platform.ClaudePermissions{DefaultMode: "bypassPermissions", DisableBypassPermissionsMode: "disable"})
	if es := inferredEdges(t, inferGraph(t, settingsOf(user))); len(writeTargets(es, "defaultMode=bypassPermissions")) != 0 {
		t.Error("disabled bypassPermissions is effective")
	}
	// Managed wins over user.
	managed := platform.ClaudeSettingsFile{
		Scope: platform.MCPScopeManaged, ConfigPath: "/etc/claude-code/managed-settings.json", ConfigDir: claudeDir,
		Status: platform.MCPSourceRead, HasPolicyKey: true, Permissions: platform.ClaudePermissions{DefaultMode: "acceptEdits"},
	}
	scan := settingsOf(userFile(platform.ClaudePermissions{DefaultMode: "bypassPermissions"}))
	scan.Managed = []platform.ManagedSource{{Kind: platform.ManagedFiles, Files: []platform.ClaudeSettingsFile{managed}}}
	es := inferredEdges(t, inferGraph(t, scan))
	if len(writeTargets(es, "defaultMode=bypassPermissions")) != 0 || len(writeTargets(es, "defaultMode=acceptEdits")) == 0 {
		t.Errorf("managed acceptEdits does not win over user bypass: %+v", es)
	}
	// Project and local modes do not set the global mode.
	es = inferredEdges(t, inferGraph(t, settingsOf(projectFile(platform.ClaudePermissions{DefaultMode: "acceptEdits"}))))
	if len(writeTargets(es, "defaultMode=acceptEdits")) != 0 {
		t.Error("project acceptEdits is the global mode")
	}
}

// additionalDirectories extend the working roots: acceptEdits then writes
// Cursor's ~/.cursor/mcp.json, and the file tools reach ~/.claude.json.
func TestInfer_AdditionalDirectoriesAreWorkingRoots(t *testing.T) {
	user := userFile(platform.ClaudePermissions{DefaultMode: "acceptEdits", AdditionalDirectories: []string{"~/.cursor", "/home/albert"}})
	es := inferredEdges(t, inferGraph(t, settingsOf(user)))
	if !contains(writeTargets(es, "defaultMode=acceptEdits"), cursorMCPJSON) {
		t.Errorf("acceptEdits writes: %v", writeTargets(es, "defaultMode=acceptEdits"))
	}
	if !contains(vias(edgesTo(es, graph.EdgeReaches, claudeJSON)), "read_in_working_dirs") {
		t.Errorf("reaches %s: %v", claudeJSON, vias(edgesTo(es, graph.EdgeReaches, claudeJSON)))
	}
}

// A registry resource has no file: no write edge, even in bypass.
func TestInfer_RegistryResourceHasNoWriteEdge(t *testing.T) {
	hklm := platform.ClaudeSettingsFile{
		Scope: platform.MCPScopeManaged, ConfigPath: `registry:HKLM\SOFTWARE\Policies\ClaudeCode\Settings`, ConfigDir: claudeDir,
		Status: platform.MCPSourceRead, HasPolicyKey: true, Permissions: platform.ClaudePermissions{DefaultMode: "bypassPermissions"},
	}
	scan := settingsOf()
	scan.Managed = []platform.ManagedSource{{Kind: platform.ManagedHKLM, Files: []platform.ClaudeSettingsFile{hklm}}}
	for _, e := range inferredEdges(t, inferGraph(t, scan)) {
		if strings.HasPrefix(e.to, "registry:") {
			t.Errorf("edge to a registry value: %+v", e)
		}
	}
}

// Inferred edge ids are keyed by type, from, to and via: stable across runs.
func TestInfer_StableIDs(t *testing.T) {
	scan := settingsOf(userFile(platform.ClaudePermissions{DefaultMode: "bypassPermissions", Allow: []string{"Bash"}}))
	ids := func() []string {
		var out []string
		for _, e := range inferGraph(t, scan).Edges {
			out = append(out, e.ID)
		}
		return out
	}
	if a, b := ids(), ids(); !reflect.DeepEqual(a, b) {
		t.Error("edge ids differ between runs")
	}
}

// capID returns the id of the configured capability with rule.
func capID(t *testing.T, g graph.Graph, rule string) string {
	t.Helper()
	for _, n := range g.Nodes {
		if n.Kind == graph.KindCapability && n.Rule == rule {
			return n.ID
		}
	}
	t.Fatalf("no capability %q", rule)
	return ""
}

// edgeWith returns the inferred edge of type typ to the node at path (or
// credential location file) through the capability with rule.
func edgeWith(t *testing.T, g graph.Graph, typ, path, viaRule string) (graph.Edge, bool) {
	t.Helper()
	via := capID(t, g, viaRule)
	for _, e := range g.Edges {
		if e.Type != typ || e.DerivedFrom[1] != via {
			continue
		}
		for _, n := range g.Nodes {
			if n.ID == e.To && (n.Path == path || (len(n.Locations) > 0 && strings.HasPrefix(n.Locations[0], path+"#"))) {
				return e, true
			}
		}
	}
	return graph.Edge{}, false
}

// BV-29 D1, the star case's variant: an Edit deny or ask on .cursor/**
// removes the acceptEdits edge to the Cursor rule.
func TestInfer_EditDenyOrAskBlocksAcceptEdits(t *testing.T) {
	for _, list := range []string{"deny", "ask"} {
		p := platform.ClaudePermissions{DefaultMode: "acceptEdits"}
		proj := platform.ClaudePermissions{}
		if list == "deny" {
			proj.Deny = []string{"Edit(.cursor/**)"}
		} else {
			proj.Ask = []string{"Edit(.cursor/**)"}
		}
		es := inferredEdges(t, inferGraph(t, settingsOf(userFile(p), projectFile(proj))))
		if got := writeTargets(es, "defaultMode=acceptEdits"); !reflect.DeepEqual(got, []string{repoClaudeMD}) {
			t.Errorf("%s Edit(.cursor/**): acceptEdits writes %v, want only CLAUDE.md", list, got)
		}
	}
}

// A Read deny also blocks the Edit tools on the path: no acceptEdits or
// Edit allow edge to it.
func TestInfer_ReadDenyBlocksEdit(t *testing.T) {
	user := userFile(platform.ClaudePermissions{DefaultMode: "acceptEdits", Allow: []string{"Edit(**/CLAUDE.md)"}})
	proj := projectFile(platform.ClaudePermissions{Deny: []string{"Read(./CLAUDE.md)"}})
	es := inferredEdges(t, inferGraph(t, settingsOf(user, proj)))
	if got := edgesTo(es, graph.EdgeWritesConfigOf, repoClaudeMD); len(got) != 0 {
		t.Errorf("writes to a Read-denied path: %+v", got)
	}
	if got := writeTargets(es, "defaultMode=acceptEdits"); !reflect.DeepEqual(got, []string{cursorRule}) {
		t.Errorf("acceptEdits writes %v", got)
	}
}

// B: a Bash deny or ask without specifier removes the Bash way of reaches.
func TestInfer_BashDenyOrAskRemovesBashWay(t *testing.T) {
	for _, p := range []platform.ClaudePermissions{{Deny: []string{"Bash"}}, {Ask: []string{"Bash(*)"}}} {
		es := inferredEdges(t, inferGraph(t, settingsOf(userFile(p))))
		for _, e := range es {
			if e.via == "shell_readonly_bash_commands" {
				t.Errorf("%+v: Bash way still present: %+v", p, e)
			}
		}
		if !contains(vias(edgesTo(es, graph.EdgeReaches, repoMCPJSON)), "read_in_working_dirs") {
			t.Errorf("%+v: file tools way lost", p)
		}
	}
	// A scoped Bash deny is not B.
	es := inferredEdges(t, inferGraph(t, settingsOf(userFile(platform.ClaudePermissions{Deny: []string{"Bash(rm *)"}}))))
	if !contains(vias(edgesTo(es, graph.EdgeReaches, claudeJSON)), "shell_readonly_bash_commands") {
		t.Error("a scoped Bash deny removed the Bash way")
	}
}

// S: an ask on the shell tool without specifier removes that tool's shell
// way of writes_config_of, not the other tool's.
func TestInfer_ShellDenyOrAskRemovesShellWay(t *testing.T) {
	user := userFile(platform.ClaudePermissions{Allow: []string{"Bash", "PowerShell"}})
	proj := projectFile(platform.ClaudePermissions{Ask: []string{"Bash"}})
	es := inferredEdges(t, inferGraph(t, settingsOf(user, proj)))
	if got := writeTargets(es, "Bash"); len(got) != 0 {
		t.Errorf("Bash shell way despite S: %v", got)
	}
	if got := writeTargets(es, "PowerShell"); len(got) == 0 {
		t.Error("PowerShell shell way lost")
	}
}

// R(f): the file tools way is gone; the Bash and mode ways keep the edge
// with read_deny_partial and the rule (deny or ask) in derived_from.
func TestInfer_ReadRulesOnReaches(t *testing.T) {
	for _, list := range []string{"deny", "ask"} {
		p := platform.ClaudePermissions{}
		if list == "deny" {
			p.Deny = []string{"Read(./.mcp.json)"}
		} else {
			p.Ask = []string{"Read(./.mcp.json)"}
		}
		user := userFile(platform.ClaudePermissions{DefaultMode: "bypassPermissions"})
		g := inferGraph(t, settingsOf(user, projectFile(p)))
		rule := capID(t, g, "Read(./.mcp.json)")
		es := inferredEdges(t, g)
		if got := vias(edgesTo(es, graph.EdgeReaches, repoMCPJSON)); contains(got, "read_in_working_dirs") {
			t.Errorf("%s: file tools way despite R: %v", list, got)
		}
		for _, v := range []string{"shell_readonly_bash_commands", "defaultMode=bypassPermissions"} {
			var e graph.Edge
			var ok bool
			for _, x := range g.Edges {
				if x.Type != graph.EdgeReaches {
					continue
				}
				for _, n := range g.Nodes {
					if n.ID == x.DerivedFrom[1] && (n.Rule == v || strings.Contains(n.Source, "/"+v+"@")) {
						for _, c := range g.Nodes {
							if c.ID == x.To && strings.HasPrefix(c.Locations[0], repoMCPJSON) {
								e, ok = x, true
							}
						}
					}
				}
			}
			if !ok || !contains(e.Assumptions, assumeReadDeny) || !contains(e.DerivedFrom, rule) {
				t.Errorf("%s via %s: %+v", list, v, e)
			}
		}
	}
}

// blockReads also governs the bypassPermissions way of reaches.
func TestInfer_BlockReadsInBypass(t *testing.T) {
	user := userFile(platform.ClaudePermissions{DefaultMode: "bypassPermissions", BlockReadsOutsideWorkingDirectories: true})
	g := inferGraph(t, settingsOf(user))
	if _, ok := edgeWith(t, g, graph.EdgeReaches, claudeJSON, "defaultMode=bypassPermissions"); ok {
		t.Error("bypass reaches a credential outside the roots under blockReads")
	}
	if _, ok := edgeWith(t, g, graph.EdgeReaches, repoMCPJSON, "defaultMode=bypassPermissions"); !ok {
		t.Error("bypass does not reach the credential inside the roots")
	}
}

// auto reaches only inside the working roots.
func TestInfer_AutoReachesOnlyInsideRoots(t *testing.T) {
	g := inferGraph(t, settingsOf(userFile(platform.ClaudePermissions{DefaultMode: "auto"})))
	if _, ok := edgeWith(t, g, graph.EdgeReaches, claudeJSON, "defaultMode=auto"); ok {
		t.Error("auto reaches a credential outside the roots")
	}
	e, ok := edgeWith(t, g, graph.EdgeReaches, repoMCPJSON, "defaultMode=auto")
	if !ok || !contains(e.Assumptions, assumeAuto) {
		t.Errorf("auto inside the roots: %+v", e)
	}
}

// E(p) keeps the bypassPermissions, auto and shell edges, with
// edit_rule_partial and the rule in derived_from.
func TestInfer_EditRulePartial(t *testing.T) {
	for _, mode := range []string{"bypassPermissions", "auto"} {
		user := userFile(platform.ClaudePermissions{DefaultMode: mode, Allow: []string{"Bash"}})
		proj := projectFile(platform.ClaudePermissions{Deny: []string{"Edit(.cursor/**)"}})
		g := inferGraph(t, settingsOf(user, proj))
		rule := capID(t, g, "Edit(.cursor/**)")
		for _, v := range []string{"defaultMode=" + mode, "Bash"} {
			e, ok := edgeWith(t, g, graph.EdgeWritesConfigOf, cursorRule, v)
			if !ok || !contains(e.Assumptions, graph.AssumeEditRulePartial) || !contains(e.DerivedFrom, rule) {
				t.Errorf("%s via %s: %+v", mode, v, e)
			}
			if e, ok := edgeWith(t, g, graph.EdgeWritesConfigOf, repoClaudeMD, v); !ok || contains(e.Assumptions, graph.AssumeEditRulePartial) {
				t.Errorf("%s via %s, uncovered CLAUDE.md: %+v", mode, v, e)
			}
		}
	}
}

// BV-32 D5/D6: with no defaultMode, the built-in default mode (auto) is the
// effective mode, with auto_mode_available and ui_mode_choice_unknown.
func TestInfer_BuiltinAutoMode(t *testing.T) {
	es := inferredEdges(t, inferGraph(t, settingsOf()))
	builtin := writeTargets(es, "builtin_default_mode")
	if !reflect.DeepEqual(builtin, sorted(unprotected)) {
		t.Errorf("built-in auto writes %v, want %v", builtin, sorted(unprotected))
	}
	reach := edgesTo(es, graph.EdgeReaches, repoMCPJSON)
	if !contains(vias(reach), "builtin_default_mode") {
		t.Errorf("reaches inside the roots: %v", vias(reach))
	}
	if got := vias(edgesTo(es, graph.EdgeReaches, claudeJSON)); contains(got, "builtin_default_mode") {
		t.Errorf("built-in auto reaches outside the roots: %v", got)
	}
	for _, e := range es {
		if e.via != "builtin_default_mode" {
			continue
		}
		for _, a := range []string{assumeAuto, graph.AssumeUIModeChoiceUnknown, graph.AssumeAutoModeAvailable} {
			if !contains(e.assumptions, a) {
				t.Errorf("%+v lacks %s", e, a)
			}
		}
	}
}

// disableAutoMode "disable" in force: the built-in mode capability is not
// in force (mode_disabled), and the mode is Manual.
func TestInfer_BuiltinAutoModeDisabled(t *testing.T) {
	for _, f := range []platform.ClaudeSettingsFile{
		userFile(platform.ClaudePermissions{DisableAutoMode: "disable", DisableAutoModePointer: "/disableAutoMode"}),
		projectFile(platform.ClaudePermissions{DisableAutoMode: "disable", DisableAutoModePointer: "/permissions/disableAutoMode"}),
	} {
		g := inferGraph(t, settingsOf(f))
		var builtin []graph.Node
		for _, n := range g.Nodes {
			// Claude Code's built-in mode (Cursor's run mode is its own, BV-48).
			if n.Basis == graph.BasisProductDefault && n.Type == graph.CapPermissionMode && strings.HasPrefix(n.Source, "kb:claude-code/") {
				builtin = append(builtin, n)
			}
		}
		if len(builtin) != 1 || builtin[0].InForce || builtin[0].NotInForceReason != graph.ReasonModeDisabled {
			t.Errorf("built-in mode while disabled: %+v, want in_force false, mode_disabled", builtin)
		}
		if got := writeTargets(inferredEdges(t, g), "builtin_default_mode"); len(got) != 0 {
			t.Errorf("writes through the disabled built-in mode: %v", got)
		}
	}
}

// A configured mode (managed > user) wins over the built-in one, and
// carries ui_mode_choice_unknown but not auto_mode_available.
func TestInfer_ConfiguredModeWinsOverBuiltin(t *testing.T) {
	es := inferredEdges(t, inferGraph(t, settingsOf(userFile(platform.ClaudePermissions{DefaultMode: "auto"}))))
	if got := writeTargets(es, "builtin_default_mode"); len(got) != 0 {
		t.Errorf("built-in mode used under a configured one: %v", got)
	}
	for _, e := range es {
		if e.via == "defaultMode=auto" && (!contains(e.assumptions, graph.AssumeUIModeChoiceUnknown) || contains(e.assumptions, graph.AssumeAutoModeAvailable)) {
			t.Errorf("configured auto edge: %+v", e)
		}
	}
}

// BV-32 D7: the VS Code extension's starting mode (ide:vscode) does not set
// the terminal's effective mode.
func TestInfer_VSCodeModeIgnored(t *testing.T) {
	scan := settingsOf(userFile(platform.ClaudePermissions{DefaultMode: "default"}))
	scan.VSCode = &platform.VSCodeModeSetting{Path: "/home/albert/AppData/Roaming/Code/User/settings.json", ConfigDir: claudeDir, Mode: "bypassPermissions", Status: platform.MCPSourceRead}
	for _, e := range inferredEdges(t, inferGraph(t, scan)) {
		if strings.HasPrefix(e.via, "claudeCode.initialPermissionMode") {
			t.Errorf("edge through the VS Code mode: %+v", e)
		}
	}
}

// Without disableAutoMode the built-in mode is in force.
func TestInfer_BuiltinAutoModeInForce(t *testing.T) {
	for _, n := range inferGraph(t, settingsOf()).Nodes {
		if n.Basis == graph.BasisProductDefault && n.Type == graph.CapPermissionMode && (!n.InForce || n.NotInForceReason != "") {
			t.Errorf("built-in mode without disableAutoMode: %+v", n)
		}
	}
}
