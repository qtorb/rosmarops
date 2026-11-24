package report

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/rules"
)

// bigReport is a report with several agents that can change others (one
// with three settings findings), two secrets with no known service and the
// same readers, two «acts without you» findings in different places, two
// alike malicious packages with no place, and more than six problems.
func bigReport() ([]rules.Finding, map[string]credService) {
	var fs []rules.Finding
	foreign := func(id, agent, target, rule, res string) rules.Finding {
		return rules.Finding{
			ID: id, Rule: rule, Severity: rules.SeverityRed, Params: rules.Params{Agent: agent, TargetAgent: target, ResourceType: res},
			Evidence: rules.Evidence{Nodes: []string{agent, target, "cap_w"}, Locations: []string{"/h/" + target + "/" + res}},
		}
	}
	for i, t := range []string{"agent_cline", "agent_codex", "agent_cursor"} {
		fs = append(fs, foreign("cc"+strconv.Itoa(i), "agent_cc", t, rules.RuleForeignAgentConfigWritable, graph.ResourceAgentConfig))
	}
	fs = append(fs, foreign("cci", "agent_cc", "agent_codex", rules.RuleForeignAgentInstructionsWritable, graph.ResourceAgentInstructions))
	fs = append(fs, foreign("cl1", "agent_cline", "agent_cc", rules.RuleForeignAgentConfigWritable, graph.ResourceAgentConfig))
	// Two secrets of no known service, each read by the same two agents.
	for _, cred := range []string{"cred_x", "cred_y"} {
		for _, a := range []string{"agent_cc", "agent_cursor"} {
			fs = append(fs, rules.Finding{
				ID: "s_" + cred + "_" + a, Rule: rules.RuleCredReadAndSend, Severity: rules.SeverityRed, Params: rules.Params{Agent: a},
				Evidence: rules.Evidence{Nodes: []string{a, "cap_r", cred}, Locations: []string{"/h/" + cred + "/.env"}},
			})
		}
	}
	// Acts without you, two agents, two places.
	for _, a := range []string{"agent_cc", "agent_cline"} {
		fs = append(fs, rules.Finding{
			ID: "aw_" + a, Rule: rules.RuleActsWithoutYou, Severity: rules.SeverityYellow, Params: rules.Params{Agent: a},
			Evidence: rules.Evidence{Nodes: []string{a, "src"}, Locations: []string{"/h/" + a + "/hook.json"}},
		})
	}
	// The same malicious package twice, with no place (an MCP server's).
	for _, n := range []string{"srv1", "srv2"} {
		fs = append(fs, rules.Finding{
			ID: "mal_" + n, Rule: rules.RulePackageKnownMalicious, Severity: rules.SeverityRed,
			Params: rules.Params{Package: "evil", Version: "1.0.0", IntelID: "X-1"}, Evidence: rules.Evidence{Nodes: []string{n}},
		})
	}
	// Problems of their own, to pass six.
	for _, a := range []string{"agent_codex", "agent_cursor", "agent_hermes"} {
		fs = append(fs, rules.Finding{
			ID: "own_" + a, Rule: rules.RuleOwnInstructionsWritable, Severity: rules.SeverityYellow, Params: rules.Params{Agent: a},
			Evidence: rules.Evidence{Nodes: []string{a}, Locations: []string{"/h/" + a + "/AGENTS.md"}},
		})
	}
	return fs, map[string]credService{}
}

func detailCtx(cat catalog.Catalog, fs []rules.Finding, svc map[string]credService) (htmlCtx, *strings.Builder) {
	v := V2{Findings: fs, credServices: svc, templateFixes: map[string]bool{}}
	var sb strings.Builder
	return htmlCtx{v: v, cat: cat, rows: credRows(v), sb: &sb, score: map[string]*index.AgentIndex{}, gloss: new(int), exists: func(string) bool { return false }}, &sb
}

// BV-294 §2.2 and §2.3, with the advisor's answers: problems with the same
// title, rule and level are one when their rows tell themselves apart by
// their place; secrets with no known service and the same readers say how
// many; a malicious package with no place is never joined.
func TestDetailJoinsSameTitles(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		fs, svc := bigReport()
		c, _ := detailCtx(cat, fs, svc)
		iss := c.issues()
		var same []issue
		mal := 0
		for _, is := range iss {
			switch {
			case is.kind == issueSame:
				same = append(same, is)
			case is.kind == issueOne && is.findings[0].Rule == rules.RulePackageKnownMalicious:
				mal++
			}
		}
		if mal != 2 {
			t.Errorf("%s: %d malicious package problems, want 2 (never joined)", lang, mal)
		}
		// BV-297 §2.2: «acts without you» says its agent, so two agents' are
		// no longer the same title; only the secrets are joined.
		if len(same) != 1 {
			t.Fatalf("%s: %d joined problems, want 1 (the secrets)", lang, len(same))
		}
		secrets := escapeHTML(cat.Text("html.r3.group.secret.many", map[string]string{"n": "2", "agents": listNames(cat, []string{"agent_cc", "agent_cursor"})}))
		if got := c.issueTitle(same[0]); got != secrets {
			t.Errorf("%s: title %q, want %q", lang, got, secrets)
		}
		acts := 0
		for _, is := range iss {
			if is.kind == issueOne && is.findings[0].Rule == rules.RuleActsWithoutYou {
				acts++
				want := escapeHTML(cat.Text("finding.acts_without_you.title", map[string]string{"agent": is.findings[0].Params.Agent}))
				if got := c.issueTitle(is); got != want {
					t.Errorf("%s: acts title %q, want %q", lang, got, want)
				}
			}
		}
		if acts != 2 {
			t.Errorf("%s: %d «acts without you» problems, want 2 (two agents: one each)", lang, acts)
		}
		// Each joined finding stays its own row in its section.
		for _, is := range same {
			if got := len(c.issueRows(is)); got != 2 {
				t.Errorf("%s: %s has %d rows, want 2", lang, c.issueTitle(is), got)
			}
		}
	}
}

// Rows that cannot be told apart by their place are not joined.
func TestDetailNoJoinWithoutPlace(t *testing.T) {
	cat, _ := catalog.Load("en")
	var fs []rules.Finding
	for _, a := range []string{"agent_cc", "agent_cline"} {
		fs = append(fs, rules.Finding{
			ID: "aw_" + a, Rule: rules.RuleActsWithoutYou, Severity: rules.SeverityYellow, Params: rules.Params{Agent: a},
			Evidence: rules.Evidence{Nodes: []string{a, "src"}, Locations: []string{"/h/same.json"}},
		})
	}
	c, _ := detailCtx(cat, fs, nil)
	if iss := c.issues(); len(iss) != 2 {
		t.Errorf("%d problems, want 2: two rows with the same place are not joined", len(iss))
	}
}

// §2.1, §2.4 and §6: inside the single «between agents» problem, one folded
// line per agent, the first open, three or more of a kind in one row; the
// first six problems open with their 36 px box and family name, the rest
// folded under their heading with a 28 px box whose text is the family's
// name, their count and level; no «open since» without a saved date.
func TestDetailAgentLinesAndFolds(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		fs, svc := bigReport()
		c, sb := detailCtx(cat, fs, svc)
		iss := c.issues()
		if len(iss) <= detailOpen {
			t.Fatalf("%s: %d problems, want more than %d", lang, len(iss), detailOpen)
		}
		c.renderR3Steps(iss)
		page := sb.String()
		// Two single rows: «between agents» (2 agents) and, since BV-297,
		// the three agents that can rewrite their own instructions; the
		// first line of each is open.
		if got := strings.Count(page, `<details class="r3-ag" open>`); got != 2 {
			t.Errorf("%s: %d open agent lines, want 2 (the first of each single row)", lang, got)
		}
		if got := strings.Count(page, `<details class="r3-ag"`); got != 5 {
			t.Errorf("%s: %d agent lines, want 5 (2 between agents, 3 of their own)", lang, got)
		}
		many := escapeHTML(cat.Text("html.r3.cfg.many.config", map[string]string{"agent": c.agentName("agent_cc"), "n": "3"}))
		if !strings.Contains(page, many) {
			t.Errorf("%s: no joined row %q", lang, many)
		}
		if strings.Contains(page, escapeHTML(cat.Text("html.r3.cfg.many.instructions", map[string]string{"agent": c.agentName("agent_cc"), "n": "1"}))) {
			t.Errorf("%s: one instructions finding joined", lang)
		}
		if strings.Contains(page, "r3-ag-s") {
			t.Errorf("%s: «open since» without a saved date", lang)
		}
		rest := len(iss) - detailOpen
		head := `<h3 class="r3-rest-h">` + escapeHTML(cat.Text(cat.One("html.r3.rest.title", rest), map[string]string{"n": strconv.Itoa(rest)})) + `</h3><p class="r3-rest-s">` + escapeHTML(cat.Text("html.r3.rest.sub", nil)) + `</p>`
		if !strings.Contains(page, head) {
			t.Errorf("%s: no heading %q", lang, head)
		}
		if got := strings.Count(page, `<details class="r3-fold">`); got != rest {
			t.Errorf("%s: %d folded problems, want %d", lang, got, rest)
		}
		withFamily := 0
		for _, is := range iss[:detailOpen] {
			if issueFamily(is) != "" {
				withFamily++
			}
		}
		if got := strings.Count(page, `class="r3-ico red"`) + strings.Count(page, `class="r3-ico amber"`); got != withFamily || withFamily == 0 {
			t.Errorf("%s: %d 36 px boxes, want %d (the open problems with a family)", lang, got, withFamily)
		}
		for i := detailOpen; i < len(iss); i++ {
			if !strings.Contains(page, `<section class="r3-p" id="p-`+strconv.Itoa(i+1)+`">`) {
				t.Errorf("%s: folded problem %d has no #p- anchor inside", lang, i+1)
			}
		}
		if !strings.Contains(page, `role="img" aria-label="`) {
			t.Errorf("%s: folded box without its family's name as text", lang)
		}
	}
}

// «Open since» is the oldest saved date of an agent's findings.
func TestDetailOpenSince(t *testing.T) {
	cat, _ := catalog.Load("en")
	fs, svc := bigReport()
	for i := range fs {
		if fs[i].ID == "cc1" {
			fs[i].Memory = &rules.FindingMemory{Verdict: "still_observed", OpenSince: "2026-11-01"}
		}
		if fs[i].ID == "cc2" {
			fs[i].Memory = &rules.FindingMemory{Verdict: "still_observed", OpenSince: "2026-10-20"}
		}
	}
	c, sb := detailCtx(cat, fs, svc)
	c.v.memoryPrev = true
	c.renderR3Steps(c.issues())
	want := `<span class="r3-ag-s">` + escapeHTML(cat.Text("html.verdict.still", map[string]string{"date": cat.DayMonthLabel(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))})) + `</span>`
	if !strings.Contains(sb.String(), want) {
		t.Errorf("no %q: the oldest saved date of the agent's findings", want)
	}
}

// «I accept it» stays per finding: an accepted finding of a folded problem
// leaves it, and the problem is drawn without it.
func TestDetailAcceptedInFolded(t *testing.T) {
	cat, _ := catalog.Load("en")
	fs, svc := bigReport()
	for i := range fs {
		if fs[i].ID == "own_agent_hermes" {
			fs[i].Accepted = &rules.Accepted{Date: "2026-11-24"}
		}
	}
	c, sb := detailCtx(cat, fs, svc)
	c.renderR3Steps(c.issues())
	if strings.Contains(sb.String(), `id="own_agent_hermes"`) {
		t.Error("an accepted finding is still drawn among the problems")
	}
}

// The script that opens a folded problem on a link is local: a click on a
// #p- link, hashchange and load; no network or storage.
func TestFoldJS(t *testing.T) {
	for _, w := range []string{`addEventListener('hashchange'`, `a[href^="#p-"]`, `location.hash`, `.open=true`} {
		if !strings.Contains(foldJS, w) {
			t.Errorf("foldJS lacks %q", w)
		}
	}
	for _, bad := range []string{"fetch(", "XMLHttpRequest", "localStorage", "sessionStorage", "http"} {
		if strings.Contains(foldJS, bad) {
			t.Errorf("foldJS has %q", bad)
		}
	}
}

// iconos.md, points 2 and 3: the «Start here» card shows the icon of what
// it proposes; the orchestrators' header, the neutral orchestrator box.
func TestStartCardAndFleetIcons(t *testing.T) {
	cat, _ := catalog.Load("en")
	fs, svc := albertCase()
	// C: no template fix, no known key in git → problem 1's family.
	card, iss, _ := startCard(t, cat, fs, map[string]credService{"cred_jwt": {service: "unknown"}, "cred_db": {service: "unknown"}, "cred_sb": {service: "unknown"}})
	if !strings.Contains(card, familyIcon(issueFamily(iss[0]), iss[0].red())) {
		t.Errorf("C: no box of problem 1's family: %s", card)
	}
	// B: the key.
	card, iss, c := startCard(t, cat, fs, svc)
	at := 0
	for i, is := range iss {
		if c.issueGitService(is) != "" {
			at = i
			break
		}
	}
	if !strings.Contains(card, familyIcon(familyKey, iss[at].red())) {
		t.Errorf("B: no key box: %s", card)
	}
	// A: the family of the first problem the template fixes.
	card, iss, c = startCard(t, cat, fs, svc, "c1", "c2")
	_, groups := c.r3TemplateFixes(iss)
	g, _ := strconv.Atoi(groups[0])
	if !strings.Contains(card, familyIcon(issueFamily(iss[g-1]), iss[g-1].red())) {
		t.Errorf("A: no box of the fixed problem's family: %s", card)
	}
	if !strings.Contains(familyIconNeutral(familyOrch), `class="r3-ico neutral"`) {
		t.Error("the orchestrators' box is not neutral")
	}
}
