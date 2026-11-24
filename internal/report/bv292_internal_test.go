package report

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-292 §3 and §7: every rule's family; package_known_malicious has none.
func TestRuleFamilies(t *testing.T) {
	want := map[string]string{
		rules.RuleCredReadAndSend: familyKey, rules.RuleCredReadable: familyKey, rules.RuleCredInGit: familyKey, rules.RuleN8NKeyWithData: familyKey,
		rules.RuleForeignAgentConfigWritable: familyBetween, rules.RuleForeignAgentInstructionsWritable: familyBetween,
		rules.RuleOwnInstructionsWritable: familyAgent, rules.RuleOwnConfigWritable: familyAgent, rules.RuleActsWithoutYou: familyAgent,
		rules.RuleMCPExternalAction: familyMCP, rules.RuleMCPUnpinnedLaunch: familyMCP, rules.RuleMCPProjectAutoApprove: familyMCP, rules.RuleN8NMCPBridge: familyMCP,
		rules.RuleOrchestratorSkipsApprovals: familyOrch, rules.RuleOrchestratorVulnerableVersion: familyOrch,
		rules.RuleOrchestratorNetworkListen: familyOrch, rules.RuleOrchestratorOpenSignup: familyOrch,
		rules.RuleSubagentAllTools: familySubagent, rules.RuleSubagentPermissiveMode: familySubagent,
		rules.RuleSubagentHookCommand: familySubagent, rules.RuleSubagentInlineMCP: familySubagent,
		rules.RuleProjectHookRunsRepoScript: familyProject, rules.RuleComputerUseAppAllowed: familyDesktop,
		rules.RulePackageKnownMalicious: "",
	}
	for _, r := range rules.AllRules {
		w, ok := want[r]
		if !ok {
			t.Errorf("%s: no family decided", r)
			continue
		}
		if got := ruleFamily[r]; got != w {
			t.Errorf("%s: family %q, want %q", r, got, w)
		}
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		for _, k := range familyKeys() {
			if !cat.HasOwn(k) {
				t.Errorf("%s: no %s", lang, k)
			}
		}
	}
	for f := range familyIcons {
		if familyIcon(f, true) == "" {
			t.Errorf("%s: no icon", f)
		}
	}
	if familyIcon("", true) != "" {
		t.Error("an icon for no family")
	}
}

// twoConfigAgents is albertCase plus a second agent (Cline) that can change
// two others: the two «between agents» problems.
func twoConfigAgents() ([]rules.Finding, map[string]credService) {
	fs, svc := albertCase()
	for i, target := range []string{"agent_cc", "agent_codex"} {
		fs = append(fs, rules.Finding{ID: "k" + string(rune('1'+i)), Rule: rules.RuleForeignAgentConfigWritable, Severity: rules.SeverityRed, Evidence: rules.Evidence{Nodes: []string{"agent_cline", target, "cap_w"}}})
	}
	return fs, svc
}

// §4 and §7: two or more agents that can change others are one row, its
// findings theirs and its parts each agent's problem, in the order they had
// (more findings first); one agent keeps today's row.
func TestConfigManyRow(t *testing.T) {
	fs, svc := albertCase()
	for _, is := range issuesOf(fs, svc) {
		if is.kind == issueConfigMany {
			t.Fatal("one agent that changes others became a single row")
		}
	}
	fs, svc = twoConfigAgents()
	iss := issuesOf(fs, svc)
	var many issue
	configs := 0
	for _, is := range iss {
		switch is.kind {
		case issueConfigMany:
			many = is
		case issueConfig:
			configs++
		}
	}
	if configs != 0 || len(many.parts) != 2 {
		t.Fatalf("%d config problems and a row of %d parts, want one row of 2: %+v", configs, len(many.parts), iss)
	}
	if many.parts[0].agentID != "agent_cc" || many.parts[1].agentID != "agent_cline" {
		t.Errorf("parts %s, %s: want Claude Code (5 findings) before Cline (2)", many.parts[0].agentID, many.parts[1].agentID)
	}
	if len(many.findings) != 7 || len(iss) != 3 {
		t.Errorf("row of %d findings among %d problems, want 7 among 3", len(many.findings), len(iss))
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		v := V2{Findings: fs, credServices: svc, templateFixes: map[string]bool{"c1": true, "k1": true}}
		c := htmlCtx{v: v, cat: cat, rows: credRows(v)}
		names := func(id string) string { return c.agentName(id) }
		full := cat.Text("html.r3.issue.config_many", map[string]string{"n": "2", "agents": names("agent_cc") + ", " + names("agent_cline")})
		if got := c.issueTitle(many) + " " + c.issueDetail(many); got != escapeHTML(full) {
			t.Errorf("%s: row %q, want %q", lang, got, escapeHTML(full))
		}
		// «How to fix it» keeps today's logic over every finding of the row.
		if got := c.issueFix(many, 1); !strings.Contains(got, escapeHTML(cat.Text("html.r3.fix.tpl_some", map[string]string{"k": "2", "n": "7", "rest": "5"}))) {
			t.Errorf("%s: fix %q", lang, got)
		}
	}
}

// §8: the table row and the detail's header have the family's icon box,
// coloured by the level, and its name above the title; the findings inside
// a problem and the agents' parts of the single row have none (an icon is
// not repeated on every line); the «Key» tag is no longer in the table or
// the detail.
func TestFamilyBoxInTableAndDetail(t *testing.T) {
	cat, _ := catalog.Load("en")
	fs, svc := twoConfigAgents()
	fs = append(fs, rules.Finding{ID: "m1", Rule: rules.RuleMCPUnpinnedLaunch, Severity: rules.SeverityYellow, Evidence: rules.Evidence{Nodes: []string{"agent_cc", "srv"}}})
	v := V2{Findings: fs, credServices: svc, templateFixes: map[string]bool{}}
	var sb strings.Builder
	c := htmlCtx{v: v, cat: cat, rows: credRows(v), sb: &sb, score: map[string]*index.AgentIndex{}, gloss: new(int), exists: func(string) bool { return false }}
	iss := issuesOf(fs, svc)
	c.renderR3Findings(iss)
	table := sb.String()
	sb.Reset()
	c.renderR3Steps(iss)
	detail := sb.String()
	for _, w := range []string{
		familyIcon(familyBetween, true) + `<div class="r3-pt">`,
		familyIcon(familyKey, true),
		familyIcon(familyMCP, false),
		`<span class="r3-fl">` + escapeHTML(cat.Text("html.family.between_agents", nil)) + `</span>`,
		`<span class="r3-fl">MCP</span>`,
	} {
		if !strings.Contains(table, w) {
			t.Errorf("table lacks %q", w)
		}
	}
	for _, w := range []string{
		`<span class="r3-n" aria-hidden="true">1</span>` + familyIcon(familyBetween, true) + `<span class="r3-pt"><span class="r3-fl">`,
		familyIcon(familyMCP, false),
		// BV-294 §2.1: each agent is a folded line with its name.
		`<span class="r3-ag-n">` + escapeHTML(c.agentName("agent_cline")) + `</span>`,
	} {
		if !strings.Contains(detail, w) {
			t.Errorf("detail lacks %q", w)
		}
	}
	if got := strings.Count(detail, `class="r3-ico `); got != len(iss) {
		t.Errorf("%d icon boxes in the detail, want one per problem (%d)", got, len(iss))
	}
	if strings.Contains(table+detail, "r3-tag") {
		t.Error("the «Key» tag is still in the table or the detail")
	}
	if strings.Count(detail, `<details class="r3-ag"`) != 2 {
		t.Errorf("%d agent lines in the single row's section, want 2", strings.Count(detail, `<details class="r3-ag"`))
	}
}

// «Lo asumo» and «No me afecta» stay per finding: an agent whose findings
// the person accepted leaves the row, and with one agent left, the row is
// today's.
func TestConfigManyAcceptedPerAgent(t *testing.T) {
	cat, _ := catalog.Load("en")
	fs, svc := twoConfigAgents()
	for i := range fs {
		if fs[i].ID == "k1" || fs[i].ID == "k2" {
			fs[i].Accepted = &rules.Accepted{Date: "2026-11-24"}
		}
	}
	v := V2{Findings: fs, credServices: svc}
	c := htmlCtx{v: v, cat: cat, rows: credRows(v)}
	for _, is := range c.issues() {
		if is.kind == issueConfigMany {
			t.Fatal("a single row with one agent left")
		}
		for _, f := range is.findings {
			if f.ID == "k1" || f.ID == "k2" {
				t.Errorf("accepted finding %s still counted", f.ID)
			}
		}
	}
}
