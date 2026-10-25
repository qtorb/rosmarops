package graph_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// Cursor scenario (decision 2026-10-25, Cursor, K4–K8, annex 2): Cursor in
// /home/albert/.cursor, one repository /src/app with a .env (which Cursor's
// file tools ignore by default) and a CLAUDE.md, a credential outside the
// repository, and Claude Code's settings with a hook in the repository.
const (
	cursorEnv      = "/src/app/.env"
	outsideEnv     = "/home/albert/secrets/.env"
	cursorEnvToken = "ghp_CURSORENVCURSORENVCURSORENVCURSORENV"
	outsideEnvTok  = "ghp_OUTSIDEENVOUTSIDEENVOUTSIDEENVOUTSID"
	cursorSandbox  = "/home/albert/.cursor/sandbox.json"
	plainKeys      = "/src/app/config/keys.json"
	plainKeysToken = "ghp_PLAINKEYSPLAINKEYSPLAINKEYSPLAINKEYS"
	cursorPerms    = "/home/albert/.cursor/permissions.json"
)

type cursorFixture struct {
	files    []platform.CursorFile
	servers  []platform.MCPServer
	envFiles map[string]string // path → token
}

func cursorGraph(t *testing.T, fx cursorFixture) graph.Graph {
	t.Helper()
	settings := platform.ClaudeSettingsScan{Trust: trustedRepo, Files: []platform.ClaudeSettingsFile{
		func() platform.ClaudeSettingsFile {
			f := userFile(platform.ClaudePermissions{})
			f.Executables = []string{"/hooks/Stop/0/hooks/0/command"}
			return f
		}(),
		func() platform.ClaudeSettingsFile {
			f := projectFile(platform.ClaudePermissions{})
			f.Executables = []string{"/hooks/PreToolUse/0/hooks/0/command"}
			return f
		}(),
	}}
	creds := platform.CredentialFileScan{
		Entries: []platform.CredentialEntry{
			{Path: cursorEnv, Line: 1, Key: "GITHUB_TOKEN", Value: secrets.NewSecretValue(cursorEnvToken)},
			{Path: outsideEnv, Line: 1, Key: "GITHUB_TOKEN", Value: secrets.NewSecretValue(outsideEnvTok)},
			{Path: plainKeys, Line: 3, Key: "GITHUB_TOKEN", Value: secrets.NewSecretValue(plainKeysToken)},
		},
		Read: []string{cursorEnv, outsideEnv, plainKeys},
	}
	for p, tok := range fx.envFiles {
		creds.Entries = append(creds.Entries, platform.CredentialEntry{Path: p, Line: 1, Key: "API_TOKEN", Value: secrets.NewSecretValue(tok)})
		creds.Read = append(creds.Read, p)
	}
	mcp := platform.MCPScan{Servers: fx.servers, Sources: []platform.MCPSource{
		{Platform: "cursor", ConfigPath: cursorMCPJSON, ConfigDir: cursorDir, Status: platform.MCPSourceRead, LoadsMCP: true},
	}}
	cursor := platform.CursorScan{ConfigDir: cursorDir, Exists: true, Files: fx.files}
	f, _, err := graph.FromScansWithCursor(mcp, settings, cursor, creds, testSalt, scanTime)
	if err != nil {
		t.Fatal(err)
	}
	f = graph.AddInstructions(f, platform.InstructionScan{Files: []platform.InstructionFile{
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
	for _, tok := range []string{cursorEnvToken, outsideEnvTok, plainKeysToken} {
		if strings.Contains(string(out), tok) {
			t.Fatal("token in graph")
		}
	}
	return g
}

// cursorEdge is one of Cursor's inferred edges, by names.
type cursorEdge struct {
	typ, to, via string
	assumptions  []string
}

func cursorEdges(t *testing.T, g graph.Graph) []cursorEdge {
	t.Helper()
	nodes := map[string]graph.Node{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	var out []cursorEdge
	for _, e := range g.Edges {
		if (e.Type != graph.EdgeReaches && e.Type != graph.EdgeWritesConfigOf) || nodes[e.From].Platform != "cursor" {
			continue
		}
		via := nodes[e.DerivedFrom[1]]
		name := via.Rule
		if via.Basis == graph.BasisProductDefault {
			name = strings.SplitN(strings.TrimPrefix(via.Source, "kb:cursor/"), "@", 2)[0]
		}
		to := nodes[e.To]
		target := to.Path
		if to.Kind == graph.KindCredential {
			target = graph.CredentialFiles(to)[0]
		}
		out = append(out, cursorEdge{typ: e.Type, to: target, via: name, assumptions: e.Assumptions})
	}
	return out
}

func findEdges(es []cursorEdge, typ, to, via string) []cursorEdge {
	var out []cursorEdge
	for _, e := range es {
		if e.typ == typ && e.to == to && (via == "" || e.via == via) {
			out = append(out, e)
		}
	}
	return out
}

func hasAll(list []string, want ...string) bool {
	for _, w := range want {
		if !contains(list, w) {
			return false
		}
	}
	return true
}

// §2.1: the .env the file tools ignore by default is reached through
// Auto-review's shell, which reads the whole system (readBoundary system),
// with the mode, classifier, sandbox and team assumptions; the credential
// outside the repository too. The file tools ignore .env* by default
// (annex 3 point 2), and reach a credential file in the repository that is
// not ignored, with no mode or classifier assumption.
func TestInferCursor_EnvReachedByTerminal(t *testing.T) {
	es := cursorEdges(t, cursorGraph(t, cursorFixture{}))
	for _, file := range []string{cursorEnv, outsideEnv} {
		got := findEdges(es, graph.EdgeReaches, file, "run_mode_builtin")
		if len(got) != 1 || !hasAll(got[0].assumptions, graph.AssumeIDERunModeNotReadable, graph.AssumeAutoClassifierPermits,
			graph.AssumeSandboxPlatformUnknown, graph.AssumeTeamPolicyUnknown, assumeUser, assumeVersion) {
			t.Errorf("%s: %+v", file, got)
		}
		if f := findEdges(es, graph.EdgeReaches, file, "file_tools_read"); len(f) != 0 {
			t.Errorf("%s reached by the file tools: %+v", file, f)
		}
	}
	plain := findEdges(es, graph.EdgeReaches, plainKeys, "file_tools_read")
	if len(plain) != 1 || contains(plain[0].assumptions, graph.AssumeAutoClassifierPermits) || !contains(plain[0].assumptions, graph.AssumeTeamPolicyUnknown) {
		t.Errorf("not-ignored file by the file tools: %+v", plain)
	}
}

// K2, annex 2 point 8: readBoundary workspace in the user's sandbox.json
// keeps the shell inside the roots; in only some repository's, it does not.
func TestInferCursor_ReadBoundaryWorkspace(t *testing.T) {
	user := platform.CursorFile{Path: cursorSandbox, Kind: platform.CursorSandbox, Scope: platform.CursorScopeUser, Status: platform.MCPSourceRead, ReadBoundary: "workspace"}
	es := cursorEdges(t, cursorGraph(t, cursorFixture{files: []platform.CursorFile{user}}))
	if got := findEdges(es, graph.EdgeReaches, outsideEnv, "run_mode_builtin"); len(got) != 0 {
		t.Errorf("outside reached with readBoundary workspace: %+v", got)
	}
	if got := findEdges(es, graph.EdgeReaches, cursorEnv, "run_mode_builtin"); len(got) != 1 {
		t.Errorf("inside not reached: %+v", got)
	}
	// Another repository's sandbox.json alone does not cut it.
	other := platform.CursorFile{
		Path: "/src/other/.cursor/sandbox.json", Kind: platform.CursorSandbox, Scope: platform.CursorScopeProject, Root: "/src/other",
		Status: platform.MCPSourceRead, ReadBoundary: "workspace",
	}
	es = cursorEdges(t, cursorGraph(t, cursorFixture{files: []platform.CursorFile{other}}))
	if got := findEdges(es, graph.EdgeReaches, outsideEnv, "run_mode_builtin"); len(got) != 1 {
		t.Errorf("outside not reached when only another repo sets it: %+v", got)
	}
}

// §2.2, annex 2 point 7: the protected paths stop writes inside the
// workspace (.claude/*.json); CLAUDE.md is written by the file tools and the
// shell; outside the workspace the retry writes ~/.claude/settings.json with
// shell_write_unchecked.
func TestInferCursor_WritesAndProtectedPaths(t *testing.T) {
	es := cursorEdges(t, cursorGraph(t, cursorFixture{}))
	if got := findEdges(es, graph.EdgeWritesConfigOf, projSettings, ""); len(got) != 0 {
		t.Errorf("protected .claude/settings.json written: %+v", got)
	}
	fileTools := findEdges(es, graph.EdgeWritesConfigOf, repoClaudeMD, "file_tools_write_workspace")
	if len(fileTools) != 1 || !contains(fileTools[0].assumptions, graph.AssumeConfigFileProtectionUnknown) {
		t.Errorf("CLAUDE.md by the file tools: %+v", fileTools)
	}
	if got := findEdges(es, graph.EdgeWritesConfigOf, repoClaudeMD, "run_mode_builtin"); len(got) != 1 || contains(got[0].assumptions, assumeShellOpen) {
		t.Errorf("CLAUDE.md by the shell: %+v", got)
	}
	out := findEdges(es, graph.EdgeWritesConfigOf, userSettings, "run_mode_builtin")
	if len(out) != 1 || !hasAll(out[0].assumptions, assumeShellOpen, graph.AssumeAutoClassifierPermits, graph.AssumeIDERunModeNotReadable) {
		t.Errorf("~/.claude/settings.json by the retry: %+v", out)
	}
}

// K7: Claude Code's hook is also Cursor's executable (one resource per
// agent), and every write to it rests on third_party_imports_setting_not_readable.
func TestInferCursor_ClaudeHooksAsCursorExecutables(t *testing.T) {
	g := cursorGraph(t, cursorFixture{})
	hook := userSettings + "#/hooks/Stop/0/hooks/0/command"
	var cursorRes, claudeRes string
	clients := map[string]string{}
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent {
			clients[n.ID] = n.Platform
		}
	}
	for _, n := range g.Nodes {
		if n.Type == graph.ResourceAgentExecutable && n.Path == hook {
			switch clients[n.Configures] {
			case "cursor":
				cursorRes = n.ID
			case "claude-code":
				claudeRes = n.ID
			}
		}
	}
	if cursorRes == "" || claudeRes == "" || cursorRes == claudeRes {
		t.Fatalf("hook resources: cursor %q, claude %q", cursorRes, claudeRes)
	}
	writes := 0
	for _, e := range g.Edges {
		if e.Type != graph.EdgeWritesConfigOf {
			continue
		}
		if e.To == cursorRes {
			writes++
			if !contains(e.Assumptions, graph.AssumeThirdPartyImportsNotReadable) {
				t.Errorf("write to Cursor's hook without the imports assumption: %v", e.Assumptions)
			}
		}
		if e.To == claudeRes && contains(e.Assumptions, graph.AssumeThirdPartyImportsNotReadable) {
			t.Errorf("write to Claude Code's own hook with the imports assumption")
		}
	}
	if writes == 0 {
		t.Error("no agent writes Cursor's imported hook — test would be vacuous")
	}
}

// Annex 2 point 2: terminalAllowlist entries are ways without the
// classifier: one that reads files reaches every credential; a launcher
// (a bare "node" covers its inline code) reaches and writes with
// code_launcher_rule.
func TestInferCursor_TerminalAllowlist(t *testing.T) {
	perms := platform.CursorFile{
		Path: cursorPerms, Kind: platform.CursorPermissions, Scope: platform.CursorScopeUser, Status: platform.MCPSourceRead,
		TerminalAllowlist: []string{"cat", "node", "npm:install*"}, HasTerminalAllowlist: true,
	}
	es := cursorEdges(t, cursorGraph(t, cursorFixture{files: []platform.CursorFile{perms}}))
	cat := findEdges(es, graph.EdgeReaches, outsideEnv, "terminalAllowlist:cat")
	if len(cat) != 1 || contains(cat[0].assumptions, graph.AssumeAutoClassifierPermits) || contains(cat[0].assumptions, graph.AssumeIDERunModeNotReadable) {
		t.Errorf("cat: %+v", cat)
	}
	node := findEdges(es, graph.EdgeReaches, cursorEnv, "terminalAllowlist:node")
	if len(node) != 1 || !contains(node[0].assumptions, graph.AssumeCodeLauncherRule) {
		t.Errorf("node reaches: %+v", node)
	}
	if w := findEdges(es, graph.EdgeWritesConfigOf, projSettings, "terminalAllowlist:node"); len(w) != 1 || !hasAll(w[0].assumptions, graph.AssumeCodeLauncherRule, assumeShellOpen) {
		t.Errorf("node writes: %+v", w)
	}
	for _, e := range es {
		if e.via == "terminalAllowlist:npm:install*" {
			t.Errorf("npm install is no way: %+v", e)
		}
	}
}

// K10: an envFile's credential is received by its server; a ${env:NAME}
// value is not resolved and is no credential.
func TestInferCursor_EnvFileAndEnvRefs(t *testing.T) {
	envFile := "/src/app/.cursor/mcp.env"
	srv := platform.MCPServer{
		Client: "cursor", Name: "api", Command: "npx", Args: []string{}, LoadsMCP: true, ConfigPath: cursorMCPJSON, ConfigDir: cursorDir,
		Scope: platform.MCPScopeUser, EnvFile: envFile,
		Env: []platform.EnvVar{{Key: "GITHUB_TOKEN", Value: secrets.NewSecretValue("${env:GITHUB_TOKEN}")}},
	}
	tok := "ghp_ENVFILEENVFILEENVFILEENVFILEENVFILEE"
	g := cursorGraph(t, cursorFixture{servers: []platform.MCPServer{srv}, envFiles: map[string]string{envFile: tok}})
	nodes := map[string]graph.Node{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	received := false
	for _, e := range g.Edges {
		if e.Type == graph.EdgeReceives && nodes[e.To].Name == "api" {
			received = received || strings.HasPrefix(nodes[e.From].Locations[0], envFile)
		}
	}
	if !received {
		t.Error("envFile credential not received by its server")
	}
	for _, n := range g.Nodes {
		for _, l := range n.Locations {
			if strings.HasPrefix(l, cursorMCPJSON) {
				t.Errorf("${env:...} became a credential: %v", n.Locations)
			}
		}
	}
}
