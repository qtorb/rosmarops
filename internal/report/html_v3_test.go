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
	"github.com/qtorb/rosmarops/internal/rules"
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
		if len(labels) != 4 { // the three steps and §11's section
			t.Errorf("%s: %d labelled sections, want 4", lang, len(labels))
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

// informe HTML v3 §8, headline: with a first finding of each rule, the h1
// is html.headline.<rule>; without findings, html.index.perfect at 100 and
// html.status.<status> otherwise.
func TestRenderHTML_V3Headline(t *testing.T) {
	g := v2Graph(t)
	base := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	h1 := regexp.MustCompile(`<h1 class="hl">([^<]*)</h1>`)
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		for _, r := range rules.AllRules {
			v := cloneV2(base)
			v.Findings[0].Rule = r
			v.Findings[0].Params.Agent = "Agent X"
			key := "html.headline." + r
			if r == rules.RuleCredInGit { // one cred_in_git finding: the singular (v3 §15.4)
				key += ".one"
			}
			want := htmlEscape(cat.Text(key, map[string]string{"agent": "Agent X"}))
			if m := h1.FindStringSubmatch(renderHTML(t, lang, v)); m == nil || m[1] != want {
				t.Errorf("%s %s: h1 %v, want %q", lang, r, m, want)
			}
		}
		v := cloneV2(base)
		v.Findings = []rules.Finding{}
		v.Today = rules.Today{}
		hundred := 100
		v.Index.Agents[0].Value = &hundred
		if m := h1.FindStringSubmatch(renderHTML(t, lang, v)); m == nil || m[1] != htmlEscape(cat.Text("html.index.perfect", nil)) {
			t.Errorf("%s: perfect h1 %v", lang, m)
		}
		v.Index.Agents[0].Value = nil
		v.Index.Agents[0].Unavailable = index.ReasonCoverage
		if m := h1.FindStringSubmatch(renderHTML(t, lang, v)); m == nil || m[1] != htmlEscape(cat.Text("html.status."+v.Status, nil)) {
			t.Errorf("%s: status h1 %v", lang, m)
		}
	}
}

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
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		of := cat.Text("html.index.of", nil)
		if !strings.Contains(page, `<div class="score" role="img" aria-label="`+htmlEscape(strconv.Itoa(low)+of)+`"`) {
			t.Errorf("%s: the big score is not the lowest", lang)
		}
		other := htmlEscape(cat.Text("html.step.1.other", map[string]string{"agent": "Claude Code", "score": strconv.Itoa(*cc.Value), "band": cat.Text("band."+cc.Band, nil)}))
		if !strings.Contains(page, `<br>`+other) {
			t.Errorf("%s: no line %q", lang, other)
		}
		if !strings.Contains(page, `<p class="sagent">Cursor · `) {
			t.Errorf("%s: agent · band line not Cursor's", lang)
		}
		for _, p := range []string{
			cat.Text("html.unknown.body", map[string]string{"agent": "Claude Code"}),
			cat.Text("html.unknown.body.cursor", map[string]string{"agent": "Cursor"}),
		} {
			if !strings.Contains(page, `<p>`+htmlEscape(p)+`</p>`) {
				t.Errorf("%s: no paragraph %q", lang, p)
			}
		}
		if !strings.Contains(page, `<template id="card-txt">`) || !strings.Contains(page, htmlEscape(" · Cursor · "+strconv.Itoa(low)+"/100")) {
			t.Errorf("%s: the card is not the lowest score's", lang)
		}
	}
}
