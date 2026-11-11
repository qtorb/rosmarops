package darwin_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/adapters/darwin"
	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/credfiles"
)

// TestBV215TodayNotEmpty (BV-214/BV-215): a real macOS user's machine — Cursor
// out of the box, Codex with one MCP server and an OpenAI key in its auth.json,
// and Claude Desktop — read by the darwin adapter and assembled the way
// `rosmarops run` does. Cursor's red write findings have no action in its
// knowledge base; step 2 must not say «Nothing to fix today» while the
// cred_read_and_send finding (the key Cursor can read) has one: it shows that
// finding's action.
func TestBV215TodayNotEmpty(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "Users", "ana")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, v := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "HERMES_HOME", "XDG_CONFIG_HOME", "GH_CONFIG_DIR", "OPENAI_API_KEY"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}
	lib := filepath.Join(home, "Library", "Application Support")
	for _, d := range []string{filepath.Join(home, ".cursor"), filepath.Join(lib, "Cursor", "User")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(home, ".codex", "config.toml"), "[mcp_servers.docs]\ncommand = \"npx\"\nargs = [\"-y\", \"docs-mcp\"]\n")
	write(t, filepath.Join(home, ".codex", "auth.json"), `{"OPENAI_API_KEY": "sk-proj-AAAAbbbbCCCCddddEEEEffffGGGGhhhhIIIIjjjjKKKK"}`)
	write(t, filepath.Join(lib, "Claude", "claude_desktop_config.json"), `{"mcpServers": {}}`)

	p := darwin.NewWith(filepath.Join(root, "Library", "Application Support", "ClaudeCode"), filepath.Join(root, "Library", "Application Support", "Cursor", "hooks.json"))
	ctx := context.Background()
	mcpScan, err := p.ListMCPServers(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	hermes, _ := p.ListHermes(ctx)
	codex, _ := p.ListCodex(ctx, nil)
	grok, _ := p.ListGrok(ctx, nil)
	copilot, _ := p.ListCopilot(ctx, nil)
	anti, _ := p.ListAntigravity(ctx, nil)
	gemini, _ := p.ListGemini(ctx, nil)
	cline, _ := p.ListCline(ctx, nil)
	settings, _ := p.ListClaudeSettings(ctx, nil)
	instr, _ := p.ListAgentInstructions(ctx, nil)
	cursor, _ := p.ListCursor(ctx, nil)
	skills, _ := p.ListSkills(ctx, nil)
	res, err := pipeline.Assemble(pipeline.Input{
		MCP: mcpScan, Settings: settings, Cursor: cursor, Hermes: hermes, Codex: codex, Grok: grok,
		Copilot: copilot, Antigravity: anti, Gemini: gemini, Cline: cline, Instructions: instr, Skills: skills,
		CredFiles:  credfiles.CollectOn(agentdirs.MacOSLayout, home, nil, nil),
		Provenance: graph.Provenance{EngineVersion: "0.11.2-test", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt)},
		Salt:       salt, Home: home, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	g := res.Graph
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	v2, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("0", 64), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		V1: report.Build("0.11.2-test", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: now, Salt: salt,
		ClaudeSettings: &settings, Home: home, Hermes: hermes, Codex: codex, Grok: grok, Cline: cline,
	})

	// The machine is the BV-214 case: red write findings with no action come
	// first; the key finding has actions.
	var cred *rules.Finding
	for i, f := range v2.Findings {
		if f.Rule == rules.RuleCredReadAndSend {
			cred = &v2.Findings[i]
		}
	}
	if cred == nil || len(cred.Actions) == 0 {
		t.Fatalf("fixture has no cred_read_and_send finding with actions: %+v", v2.Findings)
	}
	if len(v2.Findings) == 0 || v2.Findings[0].Rule == rules.RuleCredReadAndSend || len(v2.Findings[0].Actions) != 0 {
		t.Fatalf("fixture's first finding should be a red one with no action, got %+v", v2.Findings[0])
	}

	if v2.Today.FindingID != cred.ID || v2.Today.ActionID == "" {
		t.Fatalf("today = %+v, want the cred_read_and_send finding %s with its action", v2.Today, cred.ID)
	}
	var code string
	for _, a := range cred.Actions {
		if a.ID == v2.Today.ActionID {
			code = a.Code
		}
	}
	cat, _ := catalog.Load("en")
	page, err := report.WriteHTMLPages(v2, "en", home, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(page)
	s := string(raw)
	i := strings.Index(s, `<section class="now"`)
	if i < 0 {
		t.Fatal("no step 2 section")
	}
	j := strings.Index(s[i:], `</section>`)
	step2 := s[i : i+j]
	if strings.Contains(step2, cat.Text("html.none", nil)) {
		t.Errorf("step 2 still says %q:\n%s", cat.Text("html.none", nil), step2)
	}
	title := strings.SplitN(cat.Text("html.action."+code+".title", nil), "{", 2)[0]
	if title == "" || !strings.Contains(step2, title) {
		t.Errorf("step 2 does not show the action %s (%q):\n%s", code, title, step2)
	}
}
