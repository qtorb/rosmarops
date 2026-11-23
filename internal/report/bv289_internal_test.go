package report

import (
	"strconv"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/rules"
)

// startCard renders the «Start here» card of fs alone.
func startCard(t *testing.T, cat catalog.Catalog, fs []rules.Finding, svc map[string]credService, fixes ...string) (string, []issue, htmlCtx) {
	t.Helper()
	v := V2{Findings: fs, credServices: svc, templateFixes: map[string]bool{}}
	for _, id := range fixes {
		v.templateFixes[id] = true
	}
	var sb strings.Builder
	c := htmlCtx{v: v, cat: cat, rows: credRows(v), sb: &sb}
	iss := issuesOf(fs, svc)
	c.renderR3Start(iss)
	return sb.String(), iss, c
}

// bold is the card's sentence as rendered: escaped, its first sentence bold.
func bold(cat catalog.Catalog, key string, p map[string]string) string {
	e := escapeHTML(cat.Text(key, p))
	i := strings.Index(e, ". ")
	return "<b>" + e[:i+1] + "</b>" + e[i+1:]
}

// BV-289 §3.2 and §6.2: the card is the first case that holds, in order. A
// template that fixes urgent findings (A), with the numbers blocks 2 and 3
// show; else the first problem of the table with a key in git whose service
// is known (B); else problem 1 (C). With no problems, no card.
func TestStartCardCases(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		fs, svc := albertCase()

		// A: the template takes two urgent rows away.
		card, iss, c := startCard(t, cat, fs, svc, "c1", "c2")
		urgent, _, _ := c.r3RowCounts(iss)
		k, _ := c.r3TemplateFixes(iss)
		if k != 2 {
			t.Fatalf("k %d, want 2", k)
		}
		want := bold(cat, "html.r3.start.tpl", map[string]string{"k": "2", "n": strconv.Itoa(urgent)})
		for _, w := range []string{
			`<p class="r3-start-e">` + escapeHTML(cat.Text("html.r3.start.eyebrow", nil)) + `</p>`,
			`<p>` + want + `</p>`,
			`<a class="r3-btn" href="#v-pc" data-view="v-pc">` + escapeHTML(cat.Text("html.r3.tpl.button", nil)) + `</a><span>` + escapeHTML(cat.Text("html.r3.tpl.note", nil)) + `</span>`,
		} {
			if !strings.Contains(card, w) {
				t.Errorf("%s A: no %q in %s", lang, w, card)
			}
		}
		// The same numbers as blocks 2 and 3, painted by them.
		var b23 strings.Builder
		c.sb = &b23
		c.renderR3Findings(iss)
		c.renderR3Template(iss)
		if !strings.Contains(b23.String(), escapeHTML(c.r3Count("html.r3.urgent", urgent))) ||
			!strings.Contains(b23.String(), escapeHTML(cat.Text("html.r3.tpl.fixes", map[string]string{"k": "2", "groups": "2"}))) &&
				!strings.Contains(b23.String(), escapeHTML(cat.Text("html.r3.tpl.fixes", map[string]string{"k": "2", "groups": "1"}))) {
			t.Errorf("%s A: blocks 2 and 3 do not show urgent %d and k 2:\n%s", lang, urgent, b23.String())
		}

		// B: nothing fixed by the template; the Supabase key is in git.
		card, iss, c = startCard(t, cat, fs, svc)
		at := 0
		for i, is := range iss {
			if c.issueGitService(is) != "" {
				at = i + 1
				break
			}
		}
		if at == 0 {
			t.Fatal("no problem with a key in git")
		}
		for _, w := range []string{
			`<p>` + bold(cat, "html.r3.start.git", map[string]string{"service": "Supabase"}) + `</p>`,
			`<a class="r3-btn" href="#p-` + strconv.Itoa(at) + `">` + escapeHTML(cat.Text("html.r3.start.steps", nil)) + `</a></div>`,
		} {
			if !strings.Contains(card, w) {
				t.Errorf("%s B: no %q in %s", lang, w, card)
			}
		}
		if strings.Contains(card, "<span>") {
			t.Errorf("%s B: a line under the button: %s", lang, card)
		}

		// B with the service unknown: not B, but C (as the table, §6.2).
		unknown := map[string]credService{}
		for k, s := range svc {
			s.service = "unknown"
			unknown[k] = s
		}
		card, iss, c = startCard(t, cat, fs, unknown)
		first := strings.Replace(bold(cat, "html.r3.start.first", map[string]string{"title": r3Mark}), r3Mark, c.issueTitle(iss[0]), 1)
		for _, w := range []string{`<p>` + first + `</p>`, `<a class="r3-btn" href="#p-1">`} {
			if !strings.Contains(card, w) {
				t.Errorf("%s unknown service: no %q in %s", lang, w, card)
			}
		}

		// C: no key in git and no template fix.
		var noGit []rules.Finding
		for _, f := range fs {
			if f.Rule != rules.RuleCredInGit {
				noGit = append(noGit, f)
			}
		}
		card, iss, c = startCard(t, cat, noGit, svc)
		first = strings.Replace(bold(cat, "html.r3.start.first", map[string]string{"title": r3Mark}), r3Mark, c.issueTitle(iss[0]), 1)
		if !strings.Contains(card, `<p>`+first+`</p>`) || !strings.Contains(card, `href="#p-1"`) {
			t.Errorf("%s C: %s", lang, card)
		}

		// No problems: no card.
		if card, _, _ := startCard(t, cat, nil, svc); card != "" {
			t.Errorf("%s: a card with no problems: %s", lang, card)
		}
	}
}

// §6.2: with several problems with a key in git, the card names the first of
// the table, and takes its service.
func TestStartCardFirstKeyInGit(t *testing.T) {
	cat, _ := catalog.Load("en")
	svc := map[string]credService{"cred_a": {service: "github"}, "cred_b": {service: "supabase"}}
	fs := []rules.Finding{
		{ID: "a", Rule: rules.RuleCredInGit, Severity: rules.SeverityRed, Params: rules.Params{CredID: "cred_a", Count: 1}, Evidence: rules.Evidence{Nodes: []string{"cred_a"}}},
		{ID: "b", Rule: rules.RuleCredInGit, Severity: rules.SeverityRed, Params: rules.Params{CredID: "cred_b", Count: 1}, Evidence: rules.Evidence{Nodes: []string{"cred_b"}}},
	}
	card, iss, c := startCard(t, cat, fs, svc)
	if len(iss) != 2 {
		t.Fatalf("%d problems", len(iss))
	}
	first := c.issueGitService(iss[0])
	if !strings.Contains(card, `href="#p-1"`) || !strings.Contains(card, bold(cat, "html.r3.start.git", map[string]string{"service": serviceDisplay(first)})) {
		t.Errorf("not the table's first key in git (%s): %s", first, card)
	}
}

// §3.3 and §6.1: each rule says what it means in the sentence that fits its
// case, with its own variables.
func TestMeaningKey(t *testing.T) {
	red, yellow := rules.SeverityRed, rules.SeverityYellow
	for _, tc := range []struct {
		f    rules.Finding
		want string
	}{
		{rules.Finding{Rule: rules.RuleComputerUseAppAllowed, Severity: yellow, Evidence: rules.Evidence{Locations: []string{"msedge.exe"}}}, "html.meaning.computer_use_app_allowed"},
		{rules.Finding{Rule: rules.RuleComputerUseAppAllowed, Severity: red, Evidence: rules.Evidence{Locations: []string{"cmd.exe"}}}, "html.meaning.computer_use_app_allowed"},
		{rules.Finding{Rule: rules.RuleComputerUseAppAllowed, Severity: red, Evidence: rules.Evidence{Locations: []string{"*", "cmd.exe"}}}, "html.meaning.computer_use_app_allowed.all"},
		{rules.Finding{Rule: rules.RuleOrchestratorSkipsApprovals, Severity: red}, "html.meaning.orchestrator_skips_approvals"},
		{rules.Finding{Rule: rules.RuleOrchestratorSkipsApprovals, Severity: yellow}, "html.meaning.orchestrator_skips_approvals.unknown"},
		{rules.Finding{Rule: rules.RuleOrchestratorVulnerableVersion, Severity: red, Params: rules.Params{Scope: rules.VersionInstalled}}, "html.meaning.orchestrator_vulnerable_version"},
		{rules.Finding{Rule: rules.RuleOrchestratorVulnerableVersion, Severity: red, Params: rules.Params{Scope: rules.VersionNPMCache}}, "html.meaning.orchestrator_vulnerable_version.npm_cache"},
		{rules.Finding{Rule: rules.RuleOrchestratorNetworkListen, Severity: yellow}, "html.meaning.orchestrator_network_listen"},
		{rules.Finding{Rule: rules.RuleOrchestratorOpenSignup, Severity: red}, "html.meaning.orchestrator_open_signup"},
		{rules.Finding{Rule: rules.RuleSubagentAllTools, Severity: yellow}, "html.meaning.subagent_all_tools"},
		{rules.Finding{Rule: rules.RuleSubagentPermissiveMode, Severity: red, Params: rules.Params{Scope: "bypassPermissions"}}, "html.meaning.subagent_permissive_mode"},
		{rules.Finding{Rule: rules.RuleSubagentPermissiveMode, Severity: yellow, Params: rules.Params{Scope: "acceptEdits"}}, "html.meaning.subagent_permissive_mode.yellow"},
		{rules.Finding{Rule: rules.RuleSubagentHookCommand, Severity: yellow}, "html.meaning.subagent_hook_command"},
		{rules.Finding{Rule: rules.RuleSubagentInlineMCP, Severity: yellow}, "html.meaning.subagent_inline_mcp"},
		{rules.Finding{Rule: rules.RuleMCPUnpinnedLaunch, Severity: yellow}, "html.meaning.mcp_unpinned_launch"},
		{rules.Finding{Rule: rules.RuleMCPProjectAutoApprove, Severity: yellow, Radius: rules.RadiusAnyProject}, "html.meaning.mcp_project_autoapprove"},
		{rules.Finding{Rule: rules.RuleMCPProjectAutoApprove, Severity: yellow, Radius: rules.RadiusSameProject}, "html.meaning.mcp_project_autoapprove.project"},
		{rules.Finding{Rule: rules.RuleCredInGit, Severity: red}, ""},
	} {
		if got := meaningKey(tc.f); got != tc.want {
			t.Errorf("%s %s %v: key %q, want %q", tc.f.Rule, tc.f.Severity, tc.f.Evidence.Locations, got, tc.want)
		}
	}
	// Every key has its text in both languages, with no variable left.
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		for _, k := range MeaningKeys {
			if !cat.HasOwn(k) {
				t.Errorf("%s: no %s", lang, k)
			}
		}
		f := rules.Finding{Rule: rules.RuleOrchestratorSkipsApprovals, Severity: yellow, Params: rules.Params{Agent: "Paperclip", TargetAgent: "Claude Code"}}
		got := htmlCtx{cat: cat}.meaningText(f)
		if !strings.Contains(got, "Paperclip") || !strings.Contains(got, "Claude Code") || strings.Contains(got, "{") {
			t.Errorf("%s: %q", lang, got)
		}
		cu := rules.Finding{Rule: rules.RuleComputerUseAppAllowed, Severity: yellow, Params: rules.Params{Agent: "Codex"}, Evidence: rules.Evidence{Locations: []string{"msedge.exe"}}}
		if got := (htmlCtx{cat: cat}).meaningText(cu); !strings.Contains(got, "msedge.exe") || !strings.Contains(got, "Codex") {
			t.Errorf("%s: %q", lang, got)
		}
	}
}
