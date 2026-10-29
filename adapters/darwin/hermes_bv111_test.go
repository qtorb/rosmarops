package darwin_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/credfiles"
)

// hasRule reports whether rs contains r.
func hasRule(rs []string, r string) bool {
	for _, x := range rs {
		if x == r {
			return true
		}
	}
	return false
}

// hermesFixture is a fake macOS $HOME with Claude Code (evaluated, with a
// config another agent can write) and Hermes Agent (BV-111). backend sets
// Hermes's terminal.backend ("" leaves it absent, i.e. local); withClaudeCreds
// writes Claude Code's account .credentials.json with a synthetic OAuth token.
func hermesFixture(t *testing.T, backend string, withClaudeCreds bool) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "Users", "albert")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, v := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "HERMES_HOME", "XDG_CONFIG_HOME", "GH_CONFIG_DIR"} {
		t.Setenv(v, "")
	}
	write(t, filepath.Join(home, ".claude.json"), `{"mcpServers": {"fs": {"command": "npx"}}}`)
	write(t, filepath.Join(home, ".claude", "settings.json"), `{"permissions": {"allow": ["Bash"]}}`)
	cfg := "approvals:\n  mode: smart\n"
	if backend != "" {
		cfg += "terminal:\n  backend: " + backend + "\n"
	}
	write(t, filepath.Join(home, ".hermes", "config.yaml"), cfg)
	if withClaudeCreds {
		write(t, filepath.Join(home, ".claude", ".credentials.json"),
			`{"claudeAiOauth": {"accessToken": "sk-ant-oat01-fixtureAAAABBBBCCCCDDDD", "refreshToken": "sk-ant-ort01-fixtureEEEEFFFFGGGGHHHH"}}`)
	}
	return home
}

// hermesReport runs the real macOS scan over the fixture and builds the v2
// report, so the overview, findings, score and band are the product's own.
func hermesReport(t *testing.T, home string) report.V2 {
	t.Helper()
	m := macHome{home: home}
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
	creds := credfiles.CollectOn(agentdirs.MacOSLayout, home, nil, nil)
	f, _, err := graph.FromScansWithHermes(scan, settings, cursor, creds, graph.Repos{}, hermes, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	f = graph.AddInstructions(f, ins, now)
	f = graph.AddSkills(f, sk, now)
	g, err := graph.Infer(graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt)}, now), nil, home)
	if err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("0", 64), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: now, ClaudeSettings: &settings, Home: home, Hermes: hermes,
	})
	return v
}

func agentBand(v report.V2, name string) (index.AgentIndex, bool) {
	for _, a := range v.Index.Agents {
		if a.Agent == name {
			return a, true
		}
	}
	return index.AgentIndex{}, false
}

func hermesFindings(v report.V2) []string {
	var out []string
	for _, f := range v.Findings {
		if f.Params.Agent == "Hermes Agent" {
			out = append(out, f.Rule)
		}
	}
	return out
}

// TestBV111_LocalHermesEvaluatedExposed is test (a): Hermes Agent with no
// terminal.backend (local) is evaluated and reaches Claude Code's account
// credential, which shows as cred_read_and_send against Hermes; its band is
// exposed (its red config-write findings).
func TestBV111_LocalHermesEvaluatedExposed(t *testing.T) {
	v := hermesReport(t, hermesFixture(t, "", true))
	a, ok := agentBand(v, "Hermes Agent")
	if !ok {
		t.Fatal("Hermes Agent not in the overview (not evaluated)")
	}
	if a.Band != index.BandExposed {
		t.Errorf("Hermes band = %q, want exposed", a.Band)
	}
	if !hasRule(hermesFindings(v), "cred_read_and_send") {
		t.Errorf("no cred_read_and_send against Hermes; findings: %v", hermesFindings(v))
	}
}

// TestBV111_DockerBackendContained is test (b): with terminal.backend docker,
// Hermes's file, shell and read tools run in the sandbox, so it has no local
// read/write/shell findings and its band is contained.
func TestBV111_DockerBackendContained(t *testing.T) {
	v := hermesReport(t, hermesFixture(t, "docker", true))
	a, ok := agentBand(v, "Hermes Agent")
	if !ok {
		t.Fatal("Hermes Agent not in the overview (not evaluated)")
	}
	for _, r := range hermesFindings(v) {
		switch r {
		case "cred_read_and_send", "cred_readable", "foreign_agent_config_writable", "foreign_agent_instructions_writable":
			t.Errorf("isolated Hermes still has a local finding %q: %v", r, hermesFindings(v))
		}
	}
	if a.Band != index.BandContained {
		t.Errorf("Hermes band = %q, want contained", a.Band)
	}
}

// TestBV111_ClaudeOwnCredentialNoFinding is test (c): Claude Code's own
// .credentials.json is not a finding against Claude Code — its score and
// findings are the same whether or not the file is present (§3). A local
// Hermes Agent still reaches it (covered by test a).
func TestBV111_ClaudeOwnCredentialNoFinding(t *testing.T) {
	without := hermesReport(t, hermesFixture(t, "", false))
	with := hermesReport(t, hermesFixture(t, "", true))
	cw, ok1 := agentBand(without, "Claude Code")
	cc, ok2 := agentBand(with, "Claude Code")
	if !ok1 || !ok2 {
		t.Fatal("Claude Code not in the overview")
	}
	if (cw.Value == nil) != (cc.Value == nil) || (cw.Value != nil && *cw.Value != *cc.Value) || cw.Band != cc.Band {
		t.Errorf("Claude Code score changed with its .credentials.json: without=%v/%s with=%v/%s", cw.Value, cw.Band, cc.Value, cc.Band)
	}
	if cw.Red != cc.Red || cw.Yellow != cc.Yellow {
		t.Errorf("Claude Code finding counts changed: without r%d y%d, with r%d y%d", cw.Red, cw.Yellow, cc.Red, cc.Yellow)
	}
}

// TestBV111_IsolatedActionRendered is test (e): the isolated-backend action's
// text appears, with Hermes's config.yaml path, in the Markdown, the HTML and
// the export for your AI.
func TestBV111_IsolatedActionRendered(t *testing.T) {
	home := hermesFixture(t, "", true)
	v := hermesReport(t, home)
	cat, err := catalog.Load("")
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{
		"markdown": report.RenderV2Markdown(v, cat, now.Location()),
		"html":     report.RenderHTML(v, cat, home, now.Location()),
		"ai":       report.RenderAIExport(v, cat, home),
	}
	const title = "Run Hermes Agent in a container"
	const how = "runs its terminal, file and code tools inside that container"
	for name, out := range outputs {
		// The HTML shows the action title; every surface shows the how line
		// with the config.yaml path.
		if name == "html" && !strings.Contains(out, title) {
			t.Errorf("%s lacks the action title %q", name, title)
		}
		if !strings.Contains(out, how) {
			t.Errorf("%s lacks the action how text", name)
		}
		if !strings.Contains(out, "config.yaml") {
			t.Errorf("%s lacks Hermes's config.yaml path in the action", name)
		}
	}
}
