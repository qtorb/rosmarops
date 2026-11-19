package report

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/rules"
)

// Albert's case, as findings: a Supabase project's keys (a service_role JWT
// and a database URL, which name the project, and an sb_secret key, which
// does not) within reach of three agents, the sb_secret key also in git in
// 27 projects, and Claude Code able to change the settings of five agents.
func albertCase() ([]rules.Finding, map[string]credService) {
	const proj = "https://abcdefghijklmnopqrst.supabase.co"
	svc := map[string]credService{
		"cred_jwt": {service: "supabase", project: proj},
		"cred_db":  {service: "supabase", project: proj},
		"cred_sb":  {service: "supabase"},
	}
	read := func(id, rule, sev, agent, cred string) rules.Finding {
		return rules.Finding{ID: id, Rule: rule, Severity: sev, Evidence: rules.Evidence{Nodes: []string{agent, "cap_x", cred}}}
	}
	fs := []rules.Finding{
		read("f1", rules.RuleCredReadAndSend, rules.SeverityRed, "agent_cc", "cred_jwt"),
		read("f2", rules.RuleCredReadAndSend, rules.SeverityRed, "agent_cline", "cred_jwt"),
		read("f3", rules.RuleCredReadable, rules.SeverityYellow, "agent_codex", "cred_db"),
		read("f4", rules.RuleCredReadAndSend, rules.SeverityRed, "agent_cc", "cred_sb"),
		{ID: "f5", Rule: rules.RuleCredInGit, Severity: rules.SeverityRed, Params: rules.Params{CredID: "cred_sb", Count: 27}, Evidence: rules.Evidence{Nodes: []string{"cred_sb", "res_git_1"}}},
	}
	for i, target := range []string{"agent_copilot", "agent_cline", "agent_anti", "agent_codex", "agent_hermes"} {
		rule := rules.RuleForeignAgentConfigWritable
		if i == 4 {
			rule = rules.RuleForeignAgentInstructionsWritable
		}
		fs = append(fs, rules.Finding{ID: "c" + string(rune('1'+i)), Rule: rule, Severity: rules.SeverityRed, Evidence: rules.Evidence{Nodes: []string{"agent_cc", target, "cap_w"}}})
	}
	fs = append(fs, rules.Finding{ID: "o1", Rule: rules.RuleOwnConfigWritable, Severity: rules.SeverityYellow, Evidence: rules.Evidence{Nodes: []string{"agent_cc"}}})
	return fs, svc
}

func TestIssues_AlbertCase(t *testing.T) {
	fs, svc := albertCase()
	iss := issuesOf(fs, svc)
	if len(iss) != 3 {
		t.Fatalf("%d problems, want 3: %+v", len(iss), iss)
	}
	// Both urgent with five findings each, first; the yellow one last.
	var sb, cfg issue
	for _, is := range iss[:2] {
		switch is.kind {
		case issueCred:
			sb = is
		case issueConfig:
			cfg = is
		}
	}
	own := iss[2]
	if cfg.kind != issueConfig || len(cfg.findings) != 5 || len(cfg.targets()) != 5 || cfg.agentID != "agent_cc" {
		t.Errorf("config problem %+v", cfg)
	}
	if sb.kind != issueCred || !sb.project || sb.service != "supabase" || len(sb.findings) != 5 {
		t.Errorf("Supabase problem %+v", sb)
	}
	if got := strings.Join(sb.readers(), ","); got != "agent_cc,agent_cline,agent_codex" {
		t.Errorf("readers %s", got)
	}
	if g := sb.inGit(); len(g) != 1 || g[0].Params.Count != 27 {
		t.Errorf("in git %+v", g)
	}
	if own.kind != issueOne || own.red() {
		t.Errorf("the yellow finding alone, last: %+v", own)
	}
}

// Two Supabase projects never mix: each its own problem, and the key that
// names neither stands alone.
func TestIssues_TwoProjects(t *testing.T) {
	svc := map[string]credService{
		"cred_a":  {service: "supabase", project: "https://aaaaaaaaaaaaaaaaaaaa.supabase.co"},
		"cred_b":  {service: "supabase", project: "https://bbbbbbbbbbbbbbbbbbbb.supabase.co"},
		"cred_sb": {service: "supabase"},
	}
	read := func(id, cred string) rules.Finding {
		return rules.Finding{ID: id, Rule: rules.RuleCredReadAndSend, Severity: rules.SeverityRed, Evidence: rules.Evidence{Nodes: []string{"agent_cc", "cap", cred}}}
	}
	iss := issuesOf([]rules.Finding{read("a", "cred_a"), read("b", "cred_b"), read("s", "cred_sb")}, svc)
	if len(iss) != 3 {
		t.Fatalf("%d problems, want 3", len(iss))
	}
	kinds := map[string]int{}
	for _, is := range iss {
		kinds[is.kind]++
		if len(is.findings) != 1 {
			t.Errorf("a problem mixes findings: %+v", is)
		}
	}
	if kinds[issueCred] != 2 || kinds[issueOne] != 1 {
		t.Errorf("kinds %v, want two projects and the unnamed key alone", kinds)
	}
}

// A key of an unknown service, and a known key no agent reads (only in
// git), have no signed group text: one finding each.
func TestIssues_UnknownAndGitOnly(t *testing.T) {
	svc := map[string]credService{"cred_u": {service: "unknown"}, "cred_g": {service: "github"}}
	fs := []rules.Finding{
		{ID: "u1", Rule: rules.RuleCredReadAndSend, Severity: rules.SeverityRed, Evidence: rules.Evidence{Nodes: []string{"agent_cc", "cap", "cred_u"}}},
		{ID: "u2", Rule: rules.RuleCredReadAndSend, Severity: rules.SeverityRed, Evidence: rules.Evidence{Nodes: []string{"agent_cursor", "cap", "cred_u"}}},
		{ID: "g1", Rule: rules.RuleCredInGit, Severity: rules.SeverityRed, Params: rules.Params{CredID: "cred_g", Count: 2}, Evidence: rules.Evidence{Nodes: []string{"cred_g"}}},
	}
	for _, is := range issuesOf(fs, svc) {
		if is.kind != issueOne || len(is.findings) != 1 {
			t.Errorf("problem %+v, want one finding alone", is)
		}
	}
}

// «How to fix it» (the signed rules): a credential problem with a key in
// git is rotated, and the template is added when it takes some of its rows
// away; a problem every row of which the template takes away is fixed by
// it; some, how many; none, the steps.
func TestIssues_HowToFix(t *testing.T) {
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	fs, svc := albertCase()
	v := V2{Findings: fs, credServices: svc}
	c := htmlCtx{v: v, cat: cat, rows: credRows(v)}
	iss := issuesOf(fs, svc)
	find := func(kind string) issue {
		for _, is := range iss {
			if is.kind == kind {
				return is
			}
		}
		t.Fatalf("no %s problem", kind)
		return issue{}
	}
	cred, cfg := find(issueCred), find(issueConfig)
	cases := []struct {
		name  string
		fixes []string
		is    issue
		want  string
	}{
		{"key in git, the template takes nothing", nil, cred, cat.Text("html.r3.fix.rotate", map[string]string{"service": "Supabase"})},
		{"key in git, the template takes a row", []string{"f3"}, cred, cat.Text("html.r3.fix.rotate_tpl", nil)},
		{"every row", []string{"c1", "c2", "c3", "c4", "c5"}, cfg, cat.Text("html.r3.fix.tpl_all", nil)},
		{"some rows", []string{"c1", "c2"}, cfg, cat.Text("html.r3.fix.tpl_some", map[string]string{"k": "2", "n": "5", "rest": "3"})},
		{"no row", nil, cfg, cat.Text("html.r3.fix.steps", nil)},
	}
	for _, tc := range cases {
		c.v.templateFixes = map[string]bool{}
		for _, id := range tc.fixes {
			c.v.templateFixes[id] = true
		}
		if got := c.issueFix(tc.is, 1); !strings.Contains(got, escapeHTML(tc.want)) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func escapeHTML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;").Replace(s)
}
