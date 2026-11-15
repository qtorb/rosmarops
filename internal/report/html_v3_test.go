package report_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/report"
)

// informe HTML v3 §8, structure: one h1; three step sections, each labelled
// by an element of the page; today's finding is not in the step 3 list; the
// step 3 hint counts the list; without findings, html.none and
// html.step.3.none.
func TestRenderHTML_V3Structure(t *testing.T) {
	g := v2Graph(t)
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	if len(v.Findings) < 2 {
		t.Fatalf("%d findings — test would be vacuous", len(v.Findings))
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		if strings.Count(page, "<h1") != 1 {
			t.Errorf("%s: %d h1", lang, strings.Count(page, "<h1"))
		}
		labels := regexp.MustCompile(`<section[^>]*aria-labelledby="([^"]+)"`).FindAllStringSubmatch(page, -1)
		// BV-236 §1–§3: the sheet's three sections, «Computer profile» and
		// Claude Code's view, steps 2 and 3 and §11's section (the sheet
		// replaces step 1's section).
		if len(labels) != 8 {
			t.Errorf("%s: %d labelled sections, want 8", lang, len(labels))
		}
		for _, l := range labels {
			if !strings.Contains(page, `id="`+l[1]+`"`) {
				t.Errorf("%s: section labelled by missing %s", lang, l[1])
			}
		}
		_, step3, _ := strings.Cut(page, `<section aria-labelledby="step3">`)
		step3, _, _ = strings.Cut(step3, `</section>`)
		rows := strings.Count(step3, `<details class="row">`)
		if rows != len(v.Findings)-1 {
			t.Errorf("%s: %d rows for %d findings and today", lang, rows, len(v.Findings))
		}
		for _, f := range v.Findings {
			if f.ID != v.Today.FindingID {
				continue
			}
			title := htmlEscape(cat.Text("html.f.cred.title", map[string]string{"agent": f.Params.Agent, "cred_name": cat.Text("html.cred.supabase.service_role.name", nil)}))
			if strings.Contains(step3, `<span class="t">`+title+`</span>`) {
				t.Errorf("%s: today's finding in the step 3 list", lang)
			}
		}
		hint := htmlEscape(cat.Plural("html.step.3.hint", rows, map[string]string{"count": strconv.Itoa(rows)}))
		if !strings.Contains(step3, `<span class="hint">`+hint+`</span>`) {
			t.Errorf("%s: no hint %q", lang, hint)
		}
		// Groups: a dot and the group text, no count.
		if !strings.Contains(step3, `<div class="grp"><span class="dot" style="background:var(--ambfill)"></span>`+htmlEscape(cat.Text("html.group.yellow", nil))+`</div>`) {
			t.Errorf("%s: no yellow group label", lang)
		}
	}

	empty := graph.Build(graph.Fragment{}, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: "k", HostID: "h"}, v2Now)
	none := buildV2(t, &empty, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, none)
		for _, k := range []string{"html.none", "html.step.3.none"} {
			if !strings.Contains(page, htmlEscape(cat.Text(k, nil))) {
				t.Errorf("%s: no %s without findings", lang, k)
			}
		}
	}
}

// BV-236 §1: the sheet replaces step 1; the headline (h1.hl, html.headline.*, html.index.perfect, html.status.*) is no longer in the HTML, so TestRenderHTML_V3Headline, whose every assertion was on it, is gone.

// informe HTML v3 §3 point 6: with two evaluated agents, the lowest score
// is the big one (and the card's), the other one a html.step.1.other line,
// and «What we don't know» has a paragraph per agent.
func TestRenderHTML_TwoScores(t *testing.T) {
	g := v2Graph(t)
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	v = cloneV2(v)
	cc := v.Index.Agents[0]
	low := *cc.Value - 5
	cursor := index.AgentIndex{Agent: "Cursor", AgentID: "agent_client_cursor", Score: index.Score{Value: &low, Band: index.BandExposed}}
	v.Index.Agents = append(v.Index.Agents, cursor)
	v.Index.Scope.Evaluated = []string{"Claude Code", "Cursor"}
	v.Index.Scope.NotEvaluated = nil
	// The machine score is the worst agent's (I5), as index.Compute sets it:
	// the sheet's big score (BV-236 §1).
	v.Index.Machine = index.MachineScore{Score: cursor.Score, AgentID: cursor.AgentID}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		if n, _, ok := sheetScore(page); !ok || n != low {
			t.Errorf("%s: the big score is not the lowest", lang)
		}
		other := htmlEscape(cat.Text("html.step.1.other", map[string]string{"agent": "Claude Code", "score": strconv.Itoa(*cc.Value), "band": cat.Text("band."+cc.Band, nil)}))
		if !strings.Contains(page, `<p>`+other+`</p>`) {
			t.Errorf("%s: no line %q", lang, other)
		}
		// BV-236 §1: the agent · band line (.sagent) is gone; the sheet's
		// score label names the agent that sets it.
		if !strings.Contains(sheetLabel(page), "Cursor") {
			t.Errorf("%s: the score's label is not Cursor's: %q", lang, sheetLabel(page))
		}
		// What we don't know left the HTML (v3 §17.1): the .ai.md keeps a
		// paragraph per agent; the HTML has the assumptions' window.
		ai := report.RenderAIExport(v, cat, "/h")
		visible := regexp.MustCompile(`(?s)<template id="ai-export">.*?</template>`).ReplaceAllString(page, "")
		for _, p := range []string{
			cat.Text("html.unknown.body", map[string]string{"agent": "Claude Code"}),
			cat.Text("html.unknown.body.cursor", map[string]string{"agent": "Cursor"}),
		} {
			if !strings.Contains(ai, p) || strings.Contains(visible, htmlEscape(p)) {
				t.Errorf("%s: paragraph %q not in the .ai.md only", lang, p)
			}
		}
		if !strings.Contains(page, `<li>`+htmlEscape(cat.Text("html.assume.claude-code", nil))+`</li>`) {
			t.Errorf("%s: no assumptions line of Claude Code", lang)
		}
		if !strings.Contains(page, `<template id="card-txt">`) || !strings.Contains(page, htmlEscape(" · Cursor · "+strconv.Itoa(low)+"/100")) {
			t.Errorf("%s: the card is not the lowest score's", lang)
		}
	}
}
