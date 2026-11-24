package report

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/rules"
)

func desktopFinding(agent, sev string, apps ...string) rules.Finding {
	return rules.Finding{
		ID: "cu_" + agent + "_" + sev, Rule: rules.RuleComputerUseAppAllowed, Severity: sev,
		Params:   rules.Params{Agent: agent, Count: len(apps)},
		Evidence: rules.Evidence{Nodes: []string{"agent_" + agent}, Locations: apps},
	}
}

func manyApps(n int) []string {
	var out []string
	for i := 1; i <= n; i++ {
		out = append(out, "com.example.app"+strconv.Itoa(10+i))
	}
	return out
}

// BV-297 §2.1 and §2.6: the desktop titles say the agent and sum up the
// list: any app with «*» (alone or with a terminal), the apps by name up to
// three, how many from four on, urgent or to review.
func TestBV297DesktopTitles(t *testing.T) {
	red, yellow := rules.SeverityRed, rules.SeverityYellow
	cases := []struct {
		f   rules.Finding
		key string
		p   map[string]string
	}{
		{desktopFinding("Codex", red, "*"), "finding.computer_use_app_allowed.red.all", map[string]string{"agent": "Codex"}},
		{desktopFinding("Codex", red, "*", "cmd.exe"), "finding.computer_use_app_allowed.red.all", map[string]string{"agent": "Codex"}},
		{desktopFinding("Codex", red, "cmd.exe"), "finding.computer_use_app_allowed.red", map[string]string{"agent": "Codex", "app": "cmd.exe"}},
		{desktopFinding("Codex", red, "cmd.exe", "pwsh.exe", "wt.exe"), "finding.computer_use_app_allowed.red", map[string]string{"agent": "Codex", "app": "cmd.exe, pwsh.exe, wt.exe"}},
		{desktopFinding("Codex", red, "cmd.exe", "powershell.exe", "pwsh.exe", "wt.exe"), "finding.computer_use_app_allowed.red.many", map[string]string{"agent": "Codex", "n": "4"}},
		{desktopFinding("Codex", yellow, manyApps(3)...), "finding.computer_use_app_allowed.yellow", map[string]string{"agent": "Codex", "apps": strings.Join(manyApps(3), ", ")}},
		{desktopFinding("Codex", yellow, manyApps(4)...), "finding.computer_use_app_allowed.yellow.many", map[string]string{"agent": "Codex", "n": "4"}},
		{desktopFinding("Codex", yellow, manyApps(15)...), "finding.computer_use_app_allowed.yellow.many", map[string]string{"agent": "Codex", "n": "15"}},
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		c, _ := detailCtx(cat, nil, nil)
		for _, tc := range cases {
			want := cat.Text(tc.key, tc.p)
			got := c.titleText(tc.f)
			if got != want {
				t.Errorf("%s %v: title %q, want %q", lang, tc.f.Evidence.Locations, got, want)
			}
			if strings.Contains(got, "*") || strings.Count(got, "com.example.app") > 3 || !strings.Contains(got, "Codex") {
				t.Errorf("%s: title %q has «*», more than three apps, or no agent", lang, got)
			}
			md := RenderV2Markdown(V2{Findings: []rules.Finding{tc.f}}, cat, time.UTC)
			if !strings.Contains(md, want) {
				t.Errorf("%s: the Markdown has not the title %q", lang, want)
			}
		}
	}
}

// §2.6: the «where» line names every app, with no «and n more», and says
// «*» as all apps; the technical details keep the raw «*». The Markdown has
// the same line.
func TestBV297DesktopWhere(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		f := desktopFinding("Codex", rules.SeverityYellow, manyApps(15)...)
		c, sb := detailCtx(cat, []rules.Finding{f}, nil)
		c.findingWhere(f)
		where := sb.String()
		if n := strings.Count(where, "com.example.app"); n != 15 {
			t.Errorf("%s: «where» names %d apps, want 15: %s", lang, n, where)
		}
		if strings.Contains(where, cat.Text("html.tech.more", map[string]string{"count": "14"})) {
			t.Errorf("%s: «where» says how many more: %s", lang, where)
		}
		all := desktopFinding("Codex", rules.SeverityRed, "*", "cmd.exe")
		c, sb = detailCtx(cat, []rules.Finding{all}, nil)
		c.renderRow(all)
		page := sb.String()
		head, tech, _ := strings.Cut(page, `<details class="tech">`)
		want := `<p class="where">` + escapeHTML(cat.Text("html.where", nil)) + ` ` + escapeHTML(cat.Text("html.where.all_apps", nil)) + `, <code>cmd.exe</code></p>`
		if !strings.Contains(head, want) {
			t.Errorf("%s: no «where» %q in %s", lang, want, head)
		}
		if strings.Contains(head, "*") {
			t.Errorf("%s: «*» outside the technical details: %s", lang, head)
		}
		if !strings.Contains(tech, "<code>*</code>") {
			t.Errorf("%s: the technical details lost the raw «*»: %s", lang, tech)
		}
		md := RenderV2Markdown(V2{Findings: []rules.Finding{all}}, cat, time.UTC)
		if mw := cat.Text("html.where", nil) + " " + cat.Text("html.where.all_apps", nil) + ", `cmd.exe`"; !strings.Contains(md, mw) {
			t.Errorf("%s: the Markdown has no «where» %q", lang, mw)
		}
		// The export for your AI: the finding's places in its data block,
		// as before (the raw list, every app).
		ai := RenderAIExport(V2{Findings: []rules.Finding{f}}, cat, "/h")
		if n := strings.Count(ai, "com.example.app"); n != 15 {
			t.Errorf("%s: the export for your AI has %d apps, want 15 (its data block)", lang, n)
		}
	}
}

func ownFinding(rule, agent, sev, res string) rules.Finding {
	return rules.Finding{
		ID: rule + "_" + agent + "_" + res, Rule: rule, Severity: sev, Params: rules.Params{Agent: agent, ResourceType: res},
		// The agent's id is its name, as agentName gives it with no score.
		Evidence: rules.Evidence{Nodes: []string{agent}, Locations: []string{"/h/" + agent + "/" + res}},
	}
}

// §2.2, §2.3 and §2.6: «acts without you» says its agent; three agents or
// more with the same problem of their own and the same level are one row,
// a folded line per agent, with no «can change» line; with one or two
// agents, a problem each; a program the agent runs stays apart; levels
// apart stay in rows apart.
func TestBV297OwnRows(t *testing.T) {
	yellow, red := rules.SeverityYellow, rules.SeverityRed
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		var fs []rules.Finding
		three := []string{"Cline", "Codex", "Cursor"}
		for _, a := range three {
			fs = append(fs, ownFinding(rules.RuleOwnInstructionsWritable, a, yellow, graph.ResourceAgentInstructions))
			fs = append(fs, ownFinding(rules.RuleOwnConfigWritable, a, yellow, graph.ResourceAgentConfig))
			fs = append(fs, ownFinding(rules.RuleActsWithoutYou, a, yellow, "hook"))
		}
		// The same three agents can change a program they run: never in
		// the settings row.
		for _, a := range three {
			fs = append(fs, ownFinding(rules.RuleOwnConfigWritable, a, yellow, graph.ResourceAgentExecutable))
		}
		// Settings of a fourth agent, urgent: a level apart, its own problem.
		fs = append(fs, ownFinding(rules.RuleOwnConfigWritable, "Gemini", red, graph.ResourceAgentConfig))
		// Two agents that act without you on another level: one each.
		for _, a := range []string{"Hermes", "Kilo"} {
			fs = append(fs, ownFinding(rules.RuleActsWithoutYou, a, red, "hook"))
		}
		c, sb := detailCtx(cat, fs, nil)
		iss := c.issues()
		rows := map[string]issue{}
		single := map[string]int{}
		for _, is := range iss {
			switch is.kind {
			case issueOwnMany:
				rows[is.findings[0].Rule] = is
			case issueOne:
				single[is.findings[0].Rule+"/"+is.findings[0].Severity+"/"+is.findings[0].Params.ResourceType]++
			}
		}
		agents := joinNames(three) // as the «between agents» row lists them
		for rule, key := range ownManyKey {
			is, ok := rows[rule]
			if !ok {
				t.Errorf("%s: no single row for %s", lang, rule)
				continue
			}
			if len(is.parts) != 3 || len(is.findings) != 3 {
				t.Errorf("%s: %s row has %d parts and %d findings, want 3 and 3", lang, rule, len(is.parts), len(is.findings))
			}
			want := escapeHTML(cat.Text(key, map[string]string{"n": "3", "agents": agents}))
			if got := c.issueTitle(is) + " " + c.issueDetail(is); got != want {
				t.Errorf("%s: %s row says %q, want %q", lang, rule, got, want)
			}
		}
		if got := single[rules.RuleOwnConfigWritable+"/"+yellow+"/"+graph.ResourceAgentExecutable]; got != 3 {
			t.Errorf("%s: %d «program it runs» problems, want 3 apart", lang, got)
		}
		if got := single[rules.RuleOwnConfigWritable+"/"+red+"/"+graph.ResourceAgentConfig]; got != 1 {
			t.Errorf("%s: %d urgent settings problems, want 1 apart", lang, got)
		}
		if got := single[rules.RuleActsWithoutYou+"/"+red+"/hook"]; got != 2 {
			t.Errorf("%s: %d «acts without you» problems of two agents, want 2", lang, got)
		}
		for _, is := range iss {
			if is.kind == issueOne && is.findings[0].Rule == rules.RuleActsWithoutYou {
				want := escapeHTML(cat.Text("finding.acts_without_you.title", map[string]string{"agent": is.findings[0].Params.Agent}))
				if got := c.issueTitle(is); got != want {
					t.Errorf("%s: acts title %q, want %q", lang, got, want)
				}
			}
		}
		c.renderR3Steps(iss)
		page := sb.String()
		if got := strings.Count(page, `<details class="r3-ag"`); got != 9 {
			t.Errorf("%s: %d agent lines, want 9 (three rows of three)", lang, got)
		}
		if strings.Contains(page, `class="r3-ag-t"`) {
			t.Errorf("%s: a row of their own says whom they can change", lang)
		}
	}
}

// §2.4: the Spanish title of a key in git does not depend on the name's
// gender; the English one does not change.
func TestBV297KeyInGitTitle(t *testing.T) {
	es, _ := catalog.Load("es")
	en, _ := catalog.Load("en")
	for k, want := range map[string]string{
		"html.f.cred_in_git.title":         "{cred_name}: en git",
		"html.f.cred_in_git.title.history": "{cred_name}: en tu historial de git",
	} {
		if got := es.Text(k, map[string]string{"cred_name": "{cred_name}"}); got != want {
			t.Errorf("es %s = %q, want %q", k, got, want)
		}
	}
	if got := es.Text("html.f.cred_in_git.title", map[string]string{"cred_name": es.Text("html.cred.generic.name", nil)}); got != "Un secreto: en git" {
		t.Errorf("es: %q, want «Un secreto: en git»", got)
	}
	for k, want := range map[string]string{
		"html.f.cred_in_git.title":         "{cred_name} is saved in git",
		"html.f.cred_in_git.title.history": "{cred_name} is in your git history",
	} {
		if got := en.Text(k, map[string]string{"cred_name": "{cred_name}"}); got != want {
			t.Errorf("en %s = %q, want %q", k, got, want)
		}
	}
}
