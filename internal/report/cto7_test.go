package report_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// cto7Report is a report of the second cut of CTO-7: a Stripe key committed
// in /src/a with a remote (today's finding: cred_in_git), a fake key in a
// test file of /src/b silenced as test data, two red git hooks with network
// and an antivirus exclusion (other signals), a new MCP server and a new
// Cline extension (new agents).
func cto7Report(t *testing.T) report.V2 {
	t.Helper()
	salt := []byte("0123456789abcdef0123456789abcdef")
	id := graph.Identifier(salt)
	stripe := "sk_live_" + strings.Repeat("R", 24)
	fake := "sk_live_" + strings.Repeat("F", 24)
	v1Findings := []finding.Finding{
		{
			ID: "st:1", Severity: finding.SeverityRed, Category: "secret_tracked", Evidence: finding.Evidence{RepoPath: "/src/a", File: "billing/keys.ts"},
			Committed: []finding.CommittedSecret{{PatternID: "stripe_secret_key", Ref: id("stripe_secret_key", stripe), Repo: "/src/a", Presence: finding.PresenceTracked, File: "billing/keys.ts", Line: 3, HasRemote: true}},
		},
		{
			ID: "st:2", Severity: finding.SeverityRed, Category: "secret_tracked", Evidence: finding.Evidence{RepoPath: "/src/b", File: "pay/pay_test.go"},
			Committed: []finding.CommittedSecret{{PatternID: "stripe_secret_key", Ref: id("stripe_secret_key", fake), Repo: "/src/b", Presence: finding.PresenceTracked, File: "pay/pay_test.go", Line: 9}},
		},
		{ID: "hk:1", Severity: finding.SeverityRed, Category: "git_hook_with_network", GroupDetail: "pre-commit", Evidence: finding.Evidence{RepoPath: "/src/a"}},
		{ID: "hk:2", Severity: finding.SeverityRed, Category: "git_hook_with_network", GroupDetail: "pre-push", Evidence: finding.Evidence{RepoPath: "/src/b"}},
		{ID: "av:1", Severity: finding.SeverityRed, Category: "av_exclusion_new", Evidence: finding.Evidence{Extra: map[string]string{"exclusion_kind": "path", "exclusion_value": `C:\x`}}},
		{ID: "mcp:1", Severity: finding.SeverityYellow, Category: "mcp_server_new", Evidence: finding.Evidence{MCPServer: "browser", MCPClient: "claude-code"}},
		{ID: "ext:1", Severity: finding.SeverityYellow, Category: "vscode_extension_new", Evidence: finding.Evidence{ExtensionID: "saoudrizwan.claude-dev"}},
	}
	v1 := report.Build("t", v1Findings, nil, 0, 0, 0, 0, nil)
	mcp := platform.MCPScan{Sources: []platform.MCPSource{{Platform: "claude-code", ConfigPath: "/h/.claude.json", ConfigDir: "/h/.claude", Status: platform.MCPSourceRead, LoadsMCP: true}}}
	settings := platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{{Scope: platform.MCPScopeUser, ConfigPath: "/h/.claude/settings.json", ConfigDir: "/h/.claude", Status: platform.MCPSourceRead}}}
	f, _, err := graph.FromScansWithRepos(mcp, settings, platform.CursorScan{}, platform.CredentialFileScan{}, graph.Repos{Findings: v1Findings}, salt, v2Now)
	if err != nil {
		t.Fatal(err)
	}
	g, err := graph.Infer(graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Version, HostID: graph.HostID(salt)}, v2Now), []string{"/src/a", "/src/b"}, "/h")
	if err != nil {
		t.Fatal(err)
	}
	g = claudeCodeOnly(g)
	base, _ := knowledge.ClaudeCode()
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("ab", 32), Base: base, RepoRoots: []string{"/src/a", "/src/b"},
		Families: [][]string{{"/src/a"}, {"/src/b"}}, V1: v1, GeneratedAt: v2Now,
		Fixtures:   []config.AllowEntryTestFixture{{Repo: "/src/b", Path: "**/*_test.go", Note: "fake keys in tests", ExpiresOn: "2027-10-01"}},
		ConfigFile: "/h/.rosmarops/config.json",
	})
	return v
}

// v3 §9.2–§9.5 and the Markdown of §9.2–§9.3: the report of the second cut,
// in en and es, every text resolved.
func TestCTO7Report(t *testing.T) {
	v := cto7Report(t)
	if _, err := report.RenderV2JSON(v); err != nil {
		t.Fatal(err)
	}
	if len(v.Findings) == 0 || v.Findings[0].Rule != rules.RuleCredInGit || v.Today.FindingID != v.Findings[0].ID {
		t.Fatalf("today is not cred_in_git: %+v", v.Today)
	}
	if v.Silenced.Count != 1 || len(v.Signals) != 2 || v.Signals[0].Category != "git_hook_with_network" || v.Signals[0].Count != 2 {
		t.Fatalf("silenced %+v, signals %+v", v.Silenced, v.Signals)
	}
	if strings.Join(v.NewAgents, ",") != "Cline,browser" {
		t.Errorf("new agents %v", v.NewAgents)
	}
	credID := v.Findings[0].Params.CredID
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		if m := regexp.MustCompile(`\[(html|band|memory|unknown)\.[a-z0-9_.]+\]`).FindString(page); m != "" {
			t.Errorf("%s: key rendered %s", lang, m)
		}
		for _, want := range []string{
			`<h1 class="hl">` + htmlEscape(cat.Text("html.headline.cred_in_git", nil)) + `</h1>`,
			`<h2 id="today-title">` + htmlEscape(cat.Text("html.action.rotate_credential.title", nil)) + `</h2>`,
			htmlEscape(cat.Text("html.f.cred_in_git.title", map[string]string{"cred_name": cat.Text("html.cred.stripe.name", nil)})),
			htmlEscape(cat.Text("html.f.cred_in_git.impact.remote", nil)),
			`<ol class="rot"><li>` + htmlEscape(cat.Text("html.fix.rotate.step1", nil)) + `</li>`,
			htmlEscape(cat.Text("html.agent.prompt.rotated", map[string]string{"file": "%USERPROFILE%/.rosmarops/config.json"})),
			credID, `&#34;note&#34;: &#34;&#34;`,
			htmlEscape(cat.Text("html.fix.copy.generic", nil)),
			htmlEscape(cat.Plural("html.silenced", 1, map[string]string{"count": "1"})),
			`<h3>` + htmlEscape(cat.Text("html.signals.title", nil)) + `</h3>`,
			htmlEscape(cat.Text("html.signal.git_hook_with_network", nil)) + ` <span class="n">· 2</span>`,
			htmlEscape(cat.Text("html.signal.av_exclusion_new", nil)) + `</li>`,
			htmlEscape(cat.Text("html.memory.new_agent", map[string]string{"agent": "Cline"})),
			htmlEscape(cat.Text("html.card.silenced", map[string]string{"count": "1"})),
		} {
			if !strings.Contains(page, want) {
				t.Errorf("%s: no %q", lang, want)
			}
		}
		// Step 2 with rotate_credential has no effort hint.
		_, step2, _ := strings.Cut(page, `<section class="now"`)
		step2, _, _ = strings.Cut(step2, `</section>`)
		if strings.Contains(step2, `class="hint"`) {
			t.Errorf("%s: rotate_credential with a hint", lang)
		}
	}
	// The signals do not count: no v2 finding comes from them, and the
	// step 1 counts are the v2 findings'.
	for _, f := range v.Findings {
		if f.Rule != rules.RuleCredInGit && f.Rule != rules.RuleCredReadAndSend && f.Rule != rules.RuleCredReadable &&
			!strings.HasSuffix(f.Rule, "_writable") {
			t.Errorf("unexpected rule %s", f.Rule)
		}
	}
	cat, _ := catalog.Load("en")
	md := report.RenderV2Markdown(v, cat, time.UTC)
	assertRenderedTexts(t, md)
	for _, want := range []string{
		cat.Text("finding.cred_in_git.why.remote", nil),
		cat.Text("unknown.remote_visibility_unknown", nil),
		cat.Plural("report.silenced", 1, map[string]string{"count": "1"}),
		cat.Text("report.silenced.entry", map[string]string{"repo": "/src/b", "path": "**/*_test.go", "note": "fake keys in tests", "expires_on": "2027-10-01"}),
		cat.Text("memory.new_agent", map[string]string{"agent": "browser"}),
		credID,
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q", want)
		}
	}
	// The silenced key is nowhere a finding.
	for _, f := range v.Findings {
		for _, l := range f.Evidence.Locations {
			if strings.Contains(l, "pay_test.go") {
				t.Errorf("silenced key reported: %s", f.ID)
			}
		}
	}
}

// I11 with M11: the card with a silenced count is the same bytes on two
// machines (other agent ids, homes and repositories), and the count shows
// only when there is one.
func TestCardSilencedSameBytes(t *testing.T) {
	cat, _ := catalog.Load("en")
	v := 41
	day := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	a := index.AgentIndex{Agent: "Claude Code", AgentID: "agent_client_aaaaaaaaaaaaaaaa", Score: index.Score{Value: &v, Band: index.BandExposed}, Red: 2, Yellow: 1}
	b := a
	b.AgentID = "agent_client_bbbbbbbbbbbbbbbb"
	b.Weights = []index.Weight{{FindingID: "f_x", Weight: 20, Without: 60}}
	s1, t1, _ := index.Card(a, 3, day, cat)
	s2, t2, _ := index.Card(b, 3, day, cat)
	if s1 != s2 || t1 != t2 {
		t.Error("card differs between machines")
	}
	if !strings.Contains(s1, cat.Text("html.card.silenced", map[string]string{"count": "3"})) || !strings.Contains(t1, "3 silenced") {
		t.Errorf("no silenced count: %s", t1)
	}
	s0, t0, _ := index.Card(a, 0, day, cat)
	if strings.Contains(s0, "tspan") || strings.Contains(t0, "silenced") {
		t.Errorf("silenced count with none: %s", t0)
	}
}
