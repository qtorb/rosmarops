package report_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/profiles"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// oneAgent is the one-agent report of the golden pages.
func oneAgent(t *testing.T) report.V2 {
	t.Helper()
	g := v2Graph(t)
	return buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
}

// block1 is the first block of a page: from its title to the end of its
// section.
func block1(t *testing.T, page string) string {
	t.Helper()
	_, b, ok := strings.Cut(page, `<section class="r3-card" aria-labelledby="r3-h1">`)
	if !ok {
		t.Fatal("no block 1")
	}
	b, _, _ = strings.Cut(b, `</section>`)
	return b
}

// BV-289 §3.1: the verdict is block 1's first line, after its title, for each
// of the three levels of the score block 1 shows; then the score with its
// sentence, then «Start here». With no score, there is no verdict.
func TestVerdictByLevel(t *testing.T) {
	for _, band := range []string{index.BandExposed, index.BandOpenGaps, index.BandContained} {
		v := oneAgent(t)
		// Block 1 shows the machine's score, or the scored agent's.
		scored := v.Index.Machine.Value != nil
		v.Index.Machine.Band = band
		for i := range v.Index.Agents {
			if v.Index.Agents[i].Value != nil {
				scored = true
				v.Index.Agents[i].Band = band
			}
		}
		if !scored {
			t.Fatal("the one-agent report has no score: the case would be vacuous")
		}
		for _, lang := range []string{"en", "es"} {
			cat, _ := catalog.Load(lang)
			b := block1(t, renderHTML(t, lang, v))
			verdict := `<p class="r3-verdict">` + htmlEscape(cat.Text("html.r3.verdict."+band, nil)) + `</p>`
			iv, is, it, ic := strings.Index(b, verdict), strings.Index(b, `<p class="r3-score`), strings.Index(b, `<p class="r3-text">`), strings.Index(b, `<div class="r3-start">`)
			if !strings.HasPrefix(b[strings.Index(b, "</h2>")+len("</h2>"):], verdict) {
				t.Errorf("%s %s: the verdict is not block 1's first line after its title:\n%.400s", band, lang, b)
			}
			if iv < 0 || is < iv || it < is || ic < it {
				t.Errorf("%s %s: order verdict %d, score %d, sentence %d, card %d", band, lang, iv, is, it, ic)
			}
			for _, other := range []string{index.BandExposed, index.BandOpenGaps, index.BandContained} {
				if other != band && strings.Contains(b, htmlEscape(cat.Text("html.r3.verdict."+other, nil))) {
					t.Errorf("%s %s: the %s verdict too", band, lang, other)
				}
			}
		}
	}
	// No score (no graph, or no agent evaluated): no verdict.
	none := buildV2(t, nil, report.Build("t", nil, []finding.ScanError{{Scope: "global_timeout", Message: "x"}}, 0, 0, 0, 0, nil))
	for _, lang := range []string{"en", "es"} {
		if page := renderHTML(t, lang, none); strings.Contains(page, `class="r3-verdict"`) {
			t.Errorf("%s: a verdict with no score", lang)
		}
	}
}

// §6.2: with a template already in use, case A does not apply, so the card
// does not offer a template; it is case B or C.
func TestStartHereTemplateInUse(t *testing.T) {
	v := withTemplate(t, profiles.P1, "linux")
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		if !strings.Contains(page, htmlEscape(cat.Text("html.r3.tpl.in_use", map[string]string{"template": cat.Text("cc.name."+profiles.P1, nil), "agent": "Claude Code"}))) {
			t.Fatalf("%s: block 3 does not say the template is in use: the case would be vacuous", lang)
		}
		_, card, ok := strings.Cut(block1(t, page), `<div class="r3-start">`)
		if !ok {
			t.Fatalf("%s: no card", lang)
		}
		if strings.Contains(card, `href="#v-pc"`) || strings.Contains(card, htmlEscape(strings.SplitN(cat.Text("html.r3.start.tpl", nil), ".", 2)[0])) {
			t.Errorf("%s: case A with a template in use: %s", lang, card)
		}
		if !strings.Contains(card, `href="#p-`) {
			t.Errorf("%s: neither B nor C: %s", lang, card)
		}
	}
}

// meaningFindings are one finding of each of the eleven rules, and of each
// corrected case of §6.1, on the report's agent.
func meaningFindings(v report.V2) map[string]rules.Finding {
	agent := v.Index.Agents[0].AgentID
	mk := func(id, rule, sev string, p rules.Params, locs ...string) rules.Finding {
		if len(locs) == 0 {
			locs = []string{"/h/.config/" + id}
		}
		return rules.Finding{
			ID: "f_bv289_" + id, Rule: rule, RuleVersion: rules.RuleVersion, Severity: sev, Confidence: rules.ConfidenceHigh, Params: p,
			Evidence: rules.Evidence{Nodes: []string{agent}, Edges: []string{}, Locations: locs, Assumptions: []string{}}, Actions: []rules.Action{},
		}
	}
	red, yellow := rules.SeverityRed, rules.SeverityYellow
	orch := rules.Params{Agent: "Paperclip", TargetAgent: "Claude Code", Version: "0.1.0", Count: 1}
	npm := orch
	npm.Scope = rules.VersionNPMCache
	sub := func(mode string) rules.Params {
		return rules.Params{Agent: "Claude Code", TargetAgent: "reviewer", Scope: mode, Count: 1}
	}
	mcp := rules.Params{Agent: "Claude Code", Server: "files", Launcher: "npx", Package: "files-mcp", Scope: "/h/.claude/settings.json", Count: 1}
	user, project := mk("autoapprove_user", rules.RuleMCPProjectAutoApprove, yellow, mcp), mk("autoapprove_project", rules.RuleMCPProjectAutoApprove, yellow, mcp)
	user.Radius, project.Radius = rules.RadiusAnyProject, rules.RadiusSameProject
	return map[string]rules.Finding{
		"html.meaning.computer_use_app_allowed":                  mk("cu", rules.RuleComputerUseAppAllowed, yellow, rules.Params{Agent: "Codex", Count: 1}, "msedge.exe"),
		"html.meaning.computer_use_app_allowed.all":              mk("cu_all", rules.RuleComputerUseAppAllowed, red, rules.Params{Agent: "Codex", Count: 1}, "*"),
		"html.meaning.orchestrator_skips_approvals":              mk("skips", rules.RuleOrchestratorSkipsApprovals, red, orch),
		"html.meaning.orchestrator_skips_approvals.unknown":      mk("skips_unknown", rules.RuleOrchestratorSkipsApprovals, yellow, orch),
		"html.meaning.orchestrator_vulnerable_version":           mk("vuln", rules.RuleOrchestratorVulnerableVersion, red, orch),
		"html.meaning.orchestrator_vulnerable_version.npm_cache": mk("vuln_npm", rules.RuleOrchestratorVulnerableVersion, red, npm),
		"html.meaning.orchestrator_network_listen":               mk("listen", rules.RuleOrchestratorNetworkListen, yellow, orch),
		"html.meaning.orchestrator_open_signup":                  mk("signup", rules.RuleOrchestratorOpenSignup, yellow, orch),
		"html.meaning.subagent_all_tools":                        mk("all_tools", rules.RuleSubagentAllTools, yellow, sub("")),
		"html.meaning.subagent_permissive_mode":                  mk("mode_red", rules.RuleSubagentPermissiveMode, red, sub("bypassPermissions")),
		"html.meaning.subagent_permissive_mode.yellow":           mk("mode_yellow", rules.RuleSubagentPermissiveMode, yellow, sub("acceptEdits")),
		"html.meaning.subagent_hook_command":                     mk("hook", rules.RuleSubagentHookCommand, yellow, sub("")),
		"html.meaning.subagent_inline_mcp":                       mk("inline", rules.RuleSubagentInlineMCP, yellow, sub("")),
		"html.meaning.mcp_unpinned_launch":                       mk("unpinned", rules.RuleMCPUnpinnedLaunch, yellow, mcp),
		"html.meaning.mcp_project_autoapprove":                   user,
		"html.meaning.mcp_project_autoapprove.project":           project,
	}
}

// meaningParams are the variables of a finding's sentence, as the report
// fills them.
func meaningParams(f rules.Finding) map[string]string {
	if f.Rule == rules.RuleComputerUseAppAllowed {
		apps := strings.Join(f.Evidence.Locations, ", ")
		return map[string]string{"agent": f.Params.Agent, "app": apps, "apps": apps}
	}
	if strings.HasPrefix(f.Rule, "subagent_") {
		return map[string]string{"agent": f.Params.Agent}
	}
	return map[string]string{"orchestrator": f.Params.Agent, "agent": f.Params.TargetAgent}
}

// §3.3, §6.1 and §6.2: each of the eleven rules, in each of its cases, says
// what it means inside its finding in the HTML report, where the other
// findings say their impact; and nowhere else: not in the export for your AI,
// the findings window, the space view or the Markdown.
func TestMeaningInTheHTMLReportOnly(t *testing.T) {
	v := oneAgent(t)
	cases := meaningFindings(v)
	for _, f := range cases {
		v.Findings = append(v.Findings, f)
	}
	if len(cases) != len(report.MeaningKeys) {
		t.Fatalf("%d cases for %d sentences", len(cases), len(report.MeaningKeys))
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		ai := report.RenderAIExport(v, cat, "/h")
		cards, _ := json.Marshal(report.FindingCards(v, cat, "/h"))
		md := report.RenderV2Markdown(v, cat, time.UTC)
		for key, f := range cases {
			text := cat.Text(key, meaningParams(f))
			if strings.Contains(text, "{") {
				t.Errorf("%s %s: a variable left: %q", lang, key, text)
			}
			// Inside the finding's own card, in its first paragraph.
			_, card, ok := strings.Cut(page, `id="`+f.ID+`">`)
			if !ok {
				t.Errorf("%s %s: the finding is not on the page", lang, key)
				continue
			}
			card, _, _ = strings.Cut(card, `</details>`)
			if !strings.Contains(card, `<p>`+htmlEscape(text)) {
				t.Errorf("%s %s: its card does not say %q:\n%.600s", lang, key, text, card)
			}
			for name, out := range map[string]string{"AI export": ai, "findings window": string(cards), "Markdown": md} {
				if strings.Contains(out, text) || strings.Contains(out, htmlEscape(text)) {
					t.Errorf("%s %s: the %s says it", lang, key, name)
				}
			}
		}
	}
	// The space view carries no prose of the report's own language: it never
	// holds these sentences either.
	sv, err := report.BuildSpaceView(v, nil, "/h", "0.20.0")
	if err == nil {
		raw, _ := report.RenderSpaceViewJSON(sv)
		for _, lang := range []string{"en", "es"} {
			cat, _ := catalog.Load(lang)
			for key, f := range cases {
				if text := cat.Text(key, meaningParams(f)); strings.Contains(string(raw), text) {
					t.Errorf("%s %s: the space view says it", lang, key)
				}
			}
		}
	}
}
