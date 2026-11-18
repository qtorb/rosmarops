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

// The page's structure: one h1; every labelled section labelled by an
// element of the page, the first screen's three blocks among them; every
// finding in its problem's steps, once; without findings, html.none.
func TestRenderHTML_V3Structure(t *testing.T) {
	g := v2Graph(t)
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	if len(v.Findings) < 2 {
		t.Fatalf("%d findings — test would be vacuous", len(v.Findings))
	}
	for _, lang := range []string{"en", "es"} {
		page := renderHTML(t, lang, v)
		if strings.Count(page, "<h1") != 1 {
			t.Errorf("%s: %d h1", lang, strings.Count(page, "<h1"))
		}
		labels := regexp.MustCompile(`<section[^>]*aria-labelledby="([^"]+)"`).FindAllStringSubmatch(page, -1)
		blocks := 0
		for _, l := range labels {
			if !strings.Contains(page, `id="`+l[1]+`"`) {
				t.Errorf("%s: section labelled by missing %s", lang, l[1])
			}
			if strings.HasPrefix(l[1], "r3-h") {
				blocks++
			}
		}
		if blocks != 3 {
			t.Errorf("%s: %d of the first screen's blocks labelled, want 3", lang, blocks)
		}
		_, steps, _ := strings.Cut(page, `<div class="r3-steps">`)
		steps, _, _ = strings.Cut(steps, `<section aria-labelledby="dismissed">`)
		if rows := strings.Count(steps, `<details class="row">`); rows != len(v.Findings) {
			t.Errorf("%s: %d rows in the problems' steps for %d findings", lang, rows, len(v.Findings))
		}
	}

	empty := graph.Build(graph.Fragment{}, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: "k", HostID: "h"}, v2Now)
	none := buildV2(t, &empty, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		if page := renderHTML(t, lang, none); !strings.Contains(page, htmlEscape(cat.Text("html.none", nil))) {
			t.Errorf("%s: no html.none without findings", lang)
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
