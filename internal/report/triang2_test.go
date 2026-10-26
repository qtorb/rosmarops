package report_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// Informe HTML v3 §15.10 (decision 2026-10-26, triangulación 2).

// §15.1: the rotation's button has the new label; its pre and the .ai.md
// ask whether the old key was revoked before naming the list.
func TestTriang2_Rotation(t *testing.T) {
	v := cto7Report(t)
	for lang, revoked := range map[string]string{"en": "revoked the old one", "es": "revocado la antigua"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		if !strings.Contains(page, `">`+htmlEscape(cat.Text("html.fix.copy.rotated", nil))+`</button>`) {
			t.Errorf("%s: no button %q", lang, cat.Text("html.fix.copy.rotated", nil))
		}
		pre := regexp.MustCompile(`(?s)<pre hidden>(.*?)</pre>`).FindStringSubmatch(page)
		if pre == nil {
			t.Fatalf("%s: no hidden pre", lang)
		}
		ai := report.RenderAIExport(v, cat, "/h")
		for name, s := range map[string]string{"pre": pre[1], "ai.md": ai} {
			i, j := strings.Index(s, revoked), strings.Index(s, "allow_list.rotated_credentials")
			if i < 0 || j < 0 || i > j {
				t.Errorf("%s %s: %q at %d, the list at %d", lang, name, revoked, i, j)
			}
		}
		for _, k := range []string{"html.fix.rotate.step2", "html.after.rotate"} {
			if !strings.Contains(page, htmlEscape(cat.Text(k, nil))) {
				t.Errorf("%s: no %s", lang, k)
			}
		}
	}
}

// §15.3: the score line of each row by its action's completeness; a
// radius_only row has the projection of step 2 for that same action; a
// finding with no action says html.fix.none.
func TestTriang2_GainLine(t *testing.T) {
	g := v2Graph(t)
	base := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	if base.Index.Agents[0].Projection == nil {
		t.Fatal("no projection: test would be vacuous")
	}
	proj := base.Index.Agents[0].Projection
	var today rules.Finding
	for _, f := range base.Findings {
		if f.ID == base.Today.FindingID {
			today = f
		}
	}
	if a, ok := rules.BestAction(today, base.Findings); !ok || a.Completeness != rules.CompletenessRadiusOnly || a.ID != proj.ActionID {
		t.Fatalf("today's action %+v is not radius_only: test would be vacuous", a)
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		now := strconv.Itoa(*base.Index.Agents[0].Value)

		// radius_only: today's finding as a row (no today) carries step 2's
		// projection.
		v := cloneV2(base)
		v.Today = rules.Today{Code: rules.TodayNothingUrgent}
		row := rowFor(renderHTML(t, lang, v), today)
		want := htmlEscape(cat.Text("html.gain", map[string]string{"now": now, "then": strconv.Itoa(proj.Value)}))
		if row == "" || !strings.Contains(row, `<p class="gain">`+want+`</p>`) {
			t.Errorf("%s radius_only: no %q in %s", lang, want, row)
		}

		// partial, full and none: the other rows.
		checked := map[string]int{}
		for _, f := range base.Findings {
			if f.ID == today.ID {
				continue
			}
			without := ""
			for _, w := range base.Index.Agents[0].Weights {
				if w.FindingID == f.ID {
					without = strconv.Itoa(w.Without)
				}
			}
			a, has := rules.BestAction(f, base.Findings)
			key := "html.gain.if_gone"
			if has && a.Completeness == rules.CompletenessFull {
				key = "html.gain"
			}
			if has && a.Completeness == rules.CompletenessRadiusOnly {
				continue
			}
			row := rowFor(renderHTML(t, lang, base), f)
			want := htmlEscape(cat.Text(key, map[string]string{"now": now, "then": without}))
			if row == "" {
				continue // merged into another row
			}
			checked[key]++
			if !strings.Contains(row, `<p class="gain">`+want+`</p>`) {
				t.Errorf("%s %s (%s): no %q", lang, f.Rule, a.Completeness, want)
			}
		}
		if checked["html.gain.if_gone"] == 0 {
			t.Errorf("%s: no partial row checked: %v", lang, checked)
		}
		t.Logf("%s: rows checked %v", lang, checked)

		// full: a row whose actions all remove the finding.
		v = cloneV2(base)
		var full rules.Finding
		for i := range v.Findings {
			if v.Findings[i].ID != today.ID && len(v.Findings[i].Actions) > 0 && rowFor(page0(t, lang, v), v.Findings[i]) != "" {
				for j := range v.Findings[i].Actions {
					v.Findings[i].Actions[j].Completeness = rules.CompletenessFull
				}
				full = v.Findings[i]
				break
			}
		}
		without := ""
		for _, w := range base.Index.Agents[0].Weights {
			if w.FindingID == full.ID {
				without = strconv.Itoa(w.Without)
			}
		}
		if want := htmlEscape(cat.Text("html.gain", map[string]string{"now": now, "then": without})); full.ID == "" || !strings.Contains(rowFor(renderHTML(t, lang, v), full), `<p class="gain">`+want+`</p>`) {
			t.Errorf("%s full: no %q", lang, want)
		}

		// No action: if_gone and html.fix.none.
		v = cloneV2(base)
		var bare rules.Finding
		for i := range v.Findings {
			if v.Findings[i].ID != today.ID {
				v.Findings[i].Actions = nil
				bare = v.Findings[i]
				break
			}
		}
		row = rowFor(renderHTML(t, lang, v), bare)
		none := htmlEscape(cat.Text("html.fix.none", map[string]string{"agent": bare.Params.Agent}))
		if row == "" || !strings.Contains(row, "<p>"+none+"</p>") || !strings.Contains(row, htmlEscape(cat.Text("html.gain.if_gone", map[string]string{"now": now, "then": ""}))[:12]) {
			t.Errorf("%s: no action row %s", lang, row)
		}
	}
}

func page0(t *testing.T, lang string, v report.V2) string { t.Helper(); return renderHTML(t, lang, v) }

// rowFor is the step 3 row of f: the one whose «Where» names f's first
// location.
func rowFor(page string, f rules.Finding) string {
	if len(f.Evidence.Locations) == 0 {
		return ""
	}
	loc := htmlEscape(f.Evidence.Locations[0])
	for _, r := range regexp.MustCompile(`(?s)<details class="row">.*?</details></div></details>`).FindAllString(page, -1) {
		if strings.Contains(r, loc) {
			return r
		}
	}
	return ""
}

// §15.4: the counts line is .multi only with two evaluated agents.
func TestTriang2_Step1(t *testing.T) {
	g := v2Graph(t)
	one := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	two := twoAgents(t)
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		multi := htmlEscape(cat.Text("html.step.1.counts.multi", map[string]string{"red": "", "yellow": ""}))
		multi = multi[strings.LastIndex(multi, " · ")+3:]
		if strings.Contains(renderHTML(t, lang, one), multi) {
			t.Errorf("%s: .multi with one agent", lang)
		}
		red, yellow := report.RowCounts(two)
		want := htmlEscape(cat.Text("html.step.1.counts.multi", map[string]string{"red": strconv.Itoa(red), "yellow": strconv.Itoa(yellow)}))
		if !strings.Contains(renderHTML(t, lang, two), want) {
			t.Errorf("%s: no %q with two agents", lang, want)
		}
	}
}

// §15.8–§15.9: ai.limits in the HTML; the .ai.md's no-secrets line, the
// total with two agents, and today's finding not repeated.
func TestTriang2_AIExportAndLimits(t *testing.T) {
	g := v2Graph(t)
	one := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	two := twoAgents(t)
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, one)
		more := page[strings.Index(page, `<div class="more">`):]
		if !strings.Contains(more, "<p>"+htmlEscape(cat.Text("ai.limits", nil))+"</p>") {
			t.Errorf("%s: no ai.limits in «What we don't know»", lang)
		}
		ai := report.RenderAIExport(one, cat, "/h")
		if !strings.HasPrefix(ai, cat.Text("ai.instruction", nil)+"\n\n"+cat.Text("ai.no_secrets", nil)+"\n\n") {
			t.Errorf("%s: no ai.no_secrets after the instruction", lang)
		}
		if strings.Contains(ai, cat.Text("ai.counts.total", map[string]string{"red": "", "yellow": ""})[:6]) {
			t.Errorf("%s: ai.counts.total with one agent", lang)
		}
		_, findings, _ := strings.Cut(ai, "## "+cat.Text("ai.findings", nil))
		if !strings.Contains(findings, cat.Text("ai.same_as_today", nil)) {
			t.Errorf("%s: no ai.same_as_today", lang)
		}
		// The action's instructions appear once, in «today».
		var todayAction rules.Action
		for _, f := range one.Findings {
			if f.ID == one.Today.FindingID {
				todayAction, _ = rules.BestAction(f, one.Findings)
			}
		}
		if n := strings.Count(ai, todayAction.Value); todayAction.Value == "" || n != 1 {
			t.Errorf("%s: today's instructions %d times", lang, n)
		}
		red, yellow := report.RowCounts(two)
		total := cat.Text("ai.counts.total", map[string]string{"red": strconv.Itoa(red), "yellow": strconv.Itoa(yellow)})
		if !strings.Contains(report.RenderAIExport(two, cat, "/h"), "\n\n"+total+"\n\n") {
			t.Errorf("%s: no %q with two agents", lang, total)
		}
	}
}
