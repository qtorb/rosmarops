package darwin_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/adapters/darwin"
	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/credfiles"
)

var (
	salt = []byte("0123456789abcdef0123456789abcdef")
	now  = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// macHome is a fake macOS $HOME (decision 2026-10-28, macOS, M1a; BV-98):
// Claude Code, Cursor, Hermes Agent in ~/.hermes, a skill with a script in
// ~/.agents/skills, and the four folders LECTURA-101 §2 found wrong: Claude
// Desktop, Cline, VS Code's settings and Antigravity's user folder, under
// ~/Library/Application Support. The environment of the run is cleared of
// the variables that would move them.
type macHome struct {
	home, managed, enterprise string
}

func newMacHome(t *testing.T) macHome {
	t.Helper()
	root := t.TempDir()
	m := macHome{
		home:       filepath.Join(root, "Users", "albert"),
		managed:    filepath.Join(root, "Library", "Application Support", "ClaudeCode"),
		enterprise: filepath.Join(root, "Library", "Application Support", "Cursor", "hooks.json"),
	}
	t.Setenv("HOME", m.home)
	t.Setenv("USERPROFILE", m.home)
	for _, v := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "HERMES_HOME", "XDG_CONFIG_HOME", "GH_CONFIG_DIR"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}
	lib := filepath.Join(m.home, "Library", "Application Support")
	write(t, filepath.Join(m.home, ".claude.json"), `{"mcpServers": {"fs": {"command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem"]}}}`)
	write(t, filepath.Join(m.home, ".claude", "settings.json"), `{"permissions": {"allow": ["Bash"]}}`)
	write(t, filepath.Join(m.home, ".cursor", "mcp.json"), `{"mcpServers": {"github": {"command": "npx"}}}`)
	write(t, filepath.Join(m.home, ".hermes", "config.yaml"), "approvals:\n  mode: smart\nmcp_servers:\n  notes:\n    command: npx\n")
	write(t, filepath.Join(m.home, ".agents", "skills", "demo", "SKILL.md"), "x")
	write(t, filepath.Join(m.home, ".agents", "skills", "demo", "scripts", "run.sh"), "x")
	write(t, filepath.Join(lib, "Claude", "claude_desktop_config.json"), `{"mcpServers": {"desk": {"command": "npx"}}}`)
	write(t, filepath.Join(lib, "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json"), `{"mcpServers": {"browser": {"command": "npx"}}}`)
	write(t, filepath.Join(lib, "Code", "User", "settings.json"), `{"claudeCode.initialPermissionMode": "acceptEdits"}`)
	if err := os.MkdirAll(filepath.Join(lib, "Antigravity", "User"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(m.managed, "managed-settings.json"), `{"permissions": {"deny": ["WebFetch"]}}`)
	write(t, m.enterprise, `{"version": 1, "hooks": {}}`)
	return m
}

func (m macHome) platform() *darwin.DarwinPlatform { return darwin.NewWith(m.managed, m.enterprise) }

// rel is p relative to the fake home, with forward slashes ("~/…").
func (m macHome) rel(p string) string {
	if r, err := filepath.Rel(m.home, p); err == nil && !strings.HasPrefix(r, "..") {
		return "~/" + filepath.ToSlash(r)
	}
	return p
}

// BV-98, item 2: every agent's file at its macOS path.
func TestDarwin_MacOSPaths(t *testing.T) {
	m := newMacHome(t)
	p := m.platform()
	ctx := context.Background()

	scan, err := p.ListMCPServers(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	read := map[string]string{} // client → path read
	for _, s := range scan.Sources {
		if s.Status == platform.MCPSourceRead {
			read[s.Platform] = m.rel(s.ConfigPath)
		}
	}
	for client, want := range map[string]string{
		"claude-code":    "~/.claude.json",
		"cursor":         "~/.cursor/mcp.json",
		"cline":          "~/Library/Application Support/Code/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json",
		"claude-desktop": "~/Library/Application Support/Claude/claude_desktop_config.json",
	} {
		if read[client] != want {
			t.Errorf("%s read %q, want %q", client, read[client], want)
		}
	}
	present := map[string]string{}
	for _, a := range scan.Present {
		present[a.Platform] = m.rel(a.Marker)
	}
	for product, want := range map[string]string{
		agentdirs.Hermes:        "~/.hermes/config.yaml",
		agentdirs.ClaudeDesktop: "~/Library/Application Support/Claude/claude_desktop_config.json",
		agentdirs.Antigravity:   "~/Library/Application Support/Antigravity/User",
	} {
		if present[product] != want {
			t.Errorf("%s detected by %q, want %q", product, present[product], want)
		}
	}

	hermes, err := p.ListHermes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hermes.Configs) != 1 || m.rel(hermes.Configs[0].Path) != "~/.hermes/config.yaml" || hermes.Configs[0].ApprovalMode != "smart" {
		t.Errorf("hermes configs = %+v, want ~/.hermes/config.yaml read", hermes.Configs)
	}

	settings, err := p.ListClaudeSettings(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if settings.VSCode == nil || m.rel(settings.VSCode.Path) != "~/Library/Application Support/Code/User/settings.json" {
		t.Errorf("VS Code settings = %+v, want ~/Library/Application Support/Code/User/settings.json", settings.VSCode)
	}
	var kinds []string
	for _, src := range settings.Managed {
		kinds = append(kinds, src.Kind)
		if src.Kind == platform.ManagedFiles && (len(src.Files) == 0 || src.Files[0].ConfigPath != filepath.Join(m.managed, "managed-settings.json") || src.Files[0].Status != platform.MCPSourceRead) {
			t.Errorf("managed files = %+v, want %s read", src.Files, filepath.Join(m.managed, "managed-settings.json"))
		}
	}
	if strings.Join(kinds, ",") != platform.ManagedRemote+","+platform.ManagedFiles {
		t.Errorf("managed sources = %v, want remote cache and files, no registry", kinds)
	}

	cursor, err := p.ListCursor(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var enterprise bool
	for _, f := range cursor.Files {
		enterprise = enterprise || f.Scope == platform.CursorScopeEnterprise && f.Path == m.enterprise && f.Status == platform.MCPSourceRead
	}
	if !enterprise {
		t.Errorf("Cursor's enterprise hooks %s not read: %+v", m.enterprise, cursor.Files)
	}

	creds := credfiles.CollectOn(agentdirs.MacOSLayout, m.home, nil, nil)
	var vscode bool
	for _, f := range creds.Read {
		vscode = vscode || m.rel(f) == "~/Library/Application Support/Code/User/settings.json"
	}
	if !vscode {
		t.Errorf("credential files read %v, want VS Code's settings at its macOS path", creds.Read)
	}
}

// BV-98, item 3: the skills get the macOS paths from the adapter: the skill
// in ~/.agents/skills, for Cursor (and Codex and Copilot), Hermes Agent's
// folder in ~/.hermes, Cline's under ~/Library.
func TestDarwin_Skills(t *testing.T) {
	m := newMacHome(t)
	sk, err := m.platform().ListSkills(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range sk.Executables {
		got = append(got, f.Product+" "+m.rel(f.Path))
	}
	sort.Strings(got)
	want := []string{
		"codex ~/.agents/skills/demo/scripts/run.sh",
		"copilot ~/.agents/skills/demo/scripts/run.sh",
		"cursor ~/.agents/skills/demo/scripts/run.sh",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("executables = %q, want %q", got, want)
	}
	if len(sk.Failed) != 0 {
		t.Errorf("failed = %+v, want none", sk.Failed)
	}
}

// BV-98, item 1: what has no macOS equivalent yet is «not applicable», not
// a failure.
func TestDarwin_NotApplicable(t *testing.T) {
	p := darwin.New()
	ctx := context.Background()
	_, e1 := p.ListShellProfiles(ctx)
	_, e2 := p.ListScheduledTasks(ctx)
	_, e3 := p.ListDefenderExclusions(ctx)
	_, e4 := p.PythonSitePackages(nil) // BV-100: Windows site-packages layout
	for i, err := range []error{e1, e2, e3, e4} {
		if !errors.Is(err, platform.ErrNotApplicable) {
			t.Errorf("check %d: err = %v, want ErrNotApplicable", i, err)
		}
	}
}

// BV-98: what RosmarOps detects in the fake macOS $HOME, through the graph
// and the rules, as on Windows: the agents' clients and the findings.
func TestDarwin_GraphAndFindings(t *testing.T) {
	m := newMacHome(t)
	p := m.platform()
	ctx := context.Background()
	scan, err := p.ListMCPServers(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	hermes, err := p.ListHermes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	scan.Sources = append(scan.Sources, hermes.Sources...)
	scan.Servers = append(scan.Servers, hermes.Servers...)
	settings, err := p.ListClaudeSettings(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := p.ListCursor(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	ins, err := p.ListAgentInstructions(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	sk, err := p.ListSkills(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	creds := credfiles.CollectOn(agentdirs.MacOSLayout, m.home, nil, nil)
	f, _, err := graph.FromScansWithCursor(scan, settings, cursor, creds, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	f = graph.AddInstructions(f, ins, now)
	f = graph.AddSkills(f, sk, now)
	g, err := graph.Infer(graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt)}, now), nil, m.home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.Encode(g); err != nil {
		t.Fatal(err)
	}
	var clients []string
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient {
			clients = append(clients, n.Platform)
		}
	}
	sort.Strings(clients)
	want := "antigravity,claude-code,claude-desktop,cline,cursor,hermes"
	if strings.Join(clients, ",") != want {
		t.Errorf("ai_clients = %v, want %s", clients, want)
	}
	for _, sf := range g.Coverage.SourcesFailed {
		t.Errorf("source failed: %s %s", m.rel(sf.Path), sf.Reason)
	}

	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	res := rules.Evaluate(rules.Input{
		Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases, Home: m.home, Now: now, ClaudeSettings: &settings,
	})
	var got []string
	for _, fd := range res.Findings {
		target := fd.Params.TargetAgent
		if target == "" {
			target = fd.Params.Agent
		}
		got = append(got, fd.Rule+" "+fd.Severity+" "+fd.Params.Agent+" -> "+target+" "+fd.Params.ResourceType)
	}
	sort.Strings(got)
	// Claude Code has Bash allowed; Cursor's defaults. The skill is
	// Cursor's: Claude Code can rewrite it (instructions and script), and
	// Cursor its own.
	wantFindings := []string{
		"foreign_agent_config_writable red Claude Code -> Claude Desktop agent_config",
		"foreign_agent_config_writable red Claude Code -> Cline agent_config",
		"foreign_agent_config_writable red Claude Code -> Cursor agent_config",
		"foreign_agent_config_writable red Claude Code -> Cursor agent_executable",
		"foreign_agent_config_writable red Claude Code -> Hermes Agent agent_config",
		"foreign_agent_config_writable red Cursor -> Claude Code agent_config",
		"foreign_agent_config_writable red Cursor -> Claude Desktop agent_config",
		"foreign_agent_config_writable red Cursor -> Cline agent_config",
		"foreign_agent_config_writable red Cursor -> Hermes Agent agent_config",
		"foreign_agent_instructions_writable yellow Claude Code -> Cursor agent_instructions",
		"own_config_writable red Claude Code -> Claude Code agent_config",
		"own_config_writable red Cursor -> Cursor agent_config",
		"own_config_writable red Cursor -> Cursor agent_executable",
		"own_instructions_writable yellow Cursor -> Cursor agent_instructions",
	}
	if strings.Join(got, "\n") != strings.Join(wantFindings, "\n") {
		t.Errorf("findings:\n got  %q\n want %q", got, wantFindings)
	}
}
