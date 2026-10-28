package report_test

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// twoAgents is a report of Claude Code and Cursor over the same key, Cursor
// with the lower score, after two earlier runs (so the memory shows each
// agent's evolution); the findings are put in an order where Claude Code's
// come first in each group, so that only v3 §10 can bring Cursor's up.
func twoAgents(t *testing.T) report.V2 {
	t.Helper()
	return twoAgentsAfter(t, 35, 29)
}

// twoAgentsAfter is twoAgents with the previous run's scores prevCC (Claude
// Code) and prevCU (Cursor).
func twoAgentsAfter(t *testing.T, prevCC, prevCU int) report.V2 {
	t.Helper()
	g := v2GraphFull(t, []platform.CredentialEntry{
		{Path: "/src/a/dashboard/.env.local", Line: 4, Key: "SUPABASE_SERVICE_ROLE_KEY", Value: secrets.NewSecretValue(serviceRoleJWT)},
	}, []string{"/src/a/CLAUDE.md"})
	bases, _ := knowledge.All()
	n := func(v int) *int { return &v }
	entry := func(day string, cc, cu int) index.Entry {
		return index.Entry{
			Date: day, IndexVersion: index.Version, RulesVersion: rules.Version, KnowledgeVersion: knowledge.CompositeOf(bases),
			Agents: []index.EntryAgent{{Agent: "Claude Code", Value: n(cc)}, {Agent: "Cursor", Value: n(cu)}},
			Scope:  index.EntryScope{Agents: []string{"Claude Code", "Cursor"}, Repos: 2},
		}
	}
	history := []index.Entry{entry("2026-10-20", 41, 33), entry("2026-10-22", prevCC, prevCU)}
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("ab", 32), Bases: bases, Base: bases[knowledge.ClaudeCodeProduct],
		RepoRoots: []string{"/src/a", "/src/b"}, Families: [][]string{{"/src/a"}, {"/src/b"}},
		V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: v2Now,
		History: history, Previous: &history[1], Memory: memory.File{SchemaVersion: 1, Runs: 2}, Salt: []byte("0123456789abcdef0123456789abcdef"),
	})
	v = cloneV2(v)
	// Claude Code's findings first in each severity (a stable reorder).
	var cc, rest []rules.Finding
	for _, f := range v.Findings {
		if f.Params.Agent == "Claude Code" {
			cc = append(cc, f)
		} else {
			rest = append(rest, f)
		}
	}
	v.Findings = append(cc, rest...)
	sev := map[string]int{rules.SeverityRed: 0, rules.SeverityYellow: 1}
	for i := 1; i < len(v.Findings); i++ {
		for j := i; j > 0 && sev[v.Findings[j].Severity] < sev[v.Findings[j-1].Severity]; j-- {
			v.Findings[j], v.Findings[j-1] = v.Findings[j-1], v.Findings[j]
		}
	}
	return v
}

// v3 §10: with Cursor's score the lower, the score is followed by «Cursor ·
// band», the headline is Cursor's first finding, the projection pill names
// the agent of today's finding, the memory has one series per agent in step
// 1's order, and the first row of each group in step 3 is Cursor's.
func TestRenderHTML_MultiAgentReading(t *testing.T) {
	v := twoAgents(t)
	var cursor, claude *index.AgentIndex
	for i := range v.Index.Agents {
		switch v.Index.Agents[i].Agent {
		case "Cursor":
			cursor = &v.Index.Agents[i]
		case "Claude Code":
			claude = &v.Index.Agents[i]
		}
	}
	if cursor == nil || claude == nil || *cursor.Value >= *claude.Value {
		t.Fatalf("scores: %+v %+v", cursor, claude)
	}
	if v.Findings[0].Params.Agent != "Claude Code" {
		t.Fatal("the report does not start with Claude Code's finding: test would be vacuous")
	}
	var today rules.Finding
	for _, f := range v.Findings {
		if f.ID == v.Today.FindingID {
			today = f
		}
	}
	var firstCursor rules.Finding
	for _, f := range v.Findings {
		if f.Params.Agent == "Cursor" {
			firstCursor = f
			break
		}
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		of := cat.Text("html.index.of", nil)
		score := `<div class="score" role="img" aria-label="` + htmlEscape(strconv.Itoa(*cursor.Value)+of) + `"`
		i := strings.Index(page, score)
		if i < 0 {
			t.Fatalf("%s: the score is not Cursor's", lang)
		}
		after := page[i:]
		after = after[strings.Index(after, "</div>")+len("</div>"):]
		if !strings.HasPrefix(after, `<p class="sagent">Cursor · `+htmlEscape(cat.Text("band."+cursor.Band, nil))+`<button type="button" class="i"`) {
			t.Errorf("%s: the score is not followed by Cursor's line: %.120s", lang, after)
		}
		h1 := regexp.MustCompile(`<h1 class="hl">([^<]*)</h1>`).FindStringSubmatch(page)
		if want := htmlEscape(cat.Text("html.headline."+firstCursor.Rule, map[string]string{"agent": "Cursor"})); h1 == nil || h1[1] != want {
			t.Errorf("%s: headline %v, want %q", lang, h1, want)
		}
		s := claude
		if today.Params.Agent == "Cursor" {
			s = cursor
		}
		if s.Projection != nil && s.Projection.Value > *s.Value {
			pill := htmlEscape(cat.Text("html.today.projection.agent", map[string]string{"agent": today.Params.Agent, "now": strconv.Itoa(*s.Value), "then": strconv.Itoa(s.Projection.Value)}))
			if !strings.Contains(page, `<div class="proj">`+pill+`</div>`) {
				t.Errorf("%s: no pill %q", lang, pill)
			}
		} else {
			t.Errorf("%s: today's finding has no projection: test would be vacuous", lang)
		}
		// v3 §17.2: a line per agent whose score changed, in step 1's order;
		// no series.
		ci := strings.Index(page, htmlEscape(cat.Text("html.since.score", map[string]string{"agent": "Cursor", "prev": "29", "now": strconv.Itoa(*cursor.Value)})))
		cl := strings.Index(page, htmlEscape(cat.Text("html.since.score", map[string]string{"agent": "Claude Code", "prev": "35", "now": strconv.Itoa(*claude.Value)})))
		if ci < 0 || cl < 0 || ci > cl || strings.Contains(page, "33 → 29") {
			t.Errorf("%s: score lines Cursor %d, Claude Code %d", lang, ci, cl)
		}
		_, step3, _ := strings.Cut(page, `<section aria-labelledby="step3">`)
		groups := strings.Split(step3, `<div class="grp">`)[1:]
		if len(groups) != 2 {
			t.Fatalf("%s: %d groups in step 3", lang, len(groups))
		}
		for _, grp := range groups {
			row := regexp.MustCompile(`<span class="t">([^<]*)</span>`).FindStringSubmatch(grp)
			if row == nil || !strings.Contains(row[1], "Cursor") {
				t.Errorf("%s: first row of a group is not Cursor's: %v", lang, row)
			}
		}
	}
}

// BV-51 point 7: with one agent, the HTML is the golden page (rendered at
// BV-56, after the signed wording of v3 §13, at BV-59 the card's method
// v3, at BV-61 with v3 §11's section, checked to be the only change, and at
// BV-62 the launchers' one/other line, at BV-63 with the changes of v3 §15,
// at BV-64 with the launcher's write way (own_config_writable, score 27),
// at BV-66 with §11 folded into a summary and a window (v3 §16), at BV-68
// with the bottom strip of v3 §17 (checked: nothing else changes), at
// BV-69 with the plan B pitch and the agent note's style (checked), at
// BV-75 with the header's link to the other language (v3 §19, checked), at
// BV-94 with ai.untrusted_data and the export's ```text location blocks
// (blindaje B7, checked: nothing else changes);
// before, at 7b41e35 with the changes of BV-51 and BV-54). A change to it
// is a change to review.
func TestRenderHTML_OneAgentUnchanged(t *testing.T) {
	g := v2Graph(t)
	v1 := report.Build("t", nil, nil, 0, 0, 0, 0, nil)
	v1.GeneratedAt = "2026-10-25T12:00:00Z" // the day the golden page was rendered (its report name)
	v := buildV2(t, &g, v1)
	for _, lang := range []string{"en", "es"} {
		raw, err := os.ReadFile("testdata/html_one_agent_bv56." + lang + ".html")
		if err != nil {
			t.Fatal(err)
		}
		want := string(raw)
		if got := renderHTML(t, lang, v); got != want {
			i := 0
			for i < len(got) && i < len(want) && got[i] == want[i] {
				i++
			}
			t.Errorf("%s: differs at %d:\ngot  %.200s\nwant %.200s", lang, i, got[i:], want[i:])
		}
	}
}
