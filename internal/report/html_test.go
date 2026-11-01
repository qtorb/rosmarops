package report_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// visibleText is the page without style, script, templates, pre and code:
// what is catalog text.
func visibleText(page string) string {
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?s)<style>.*?</style>`), regexp.MustCompile(`(?s)<script>.*?</script>`),
		regexp.MustCompile(`(?s)<template[^>]*>.*?</template>`), regexp.MustCompile(`(?s)<pre[^>]*>.*?</pre>`),
		regexp.MustCompile(`(?s)<code>.*?</code>`), regexp.MustCompile(`<[^>]+>`),
	} {
		page = re.ReplaceAllString(page, " ")
	}
	return page
}

func renderHTML(t *testing.T, lang string, v report.V2) string {
	t.Helper()
	cat, err := catalog.Load(lang)
	if err != nil {
		t.Fatal(err)
	}
	return report.RenderHTML(v, cat, "/h", time.UTC)
}

// informe HTML v1: every text resolves (no placeholder, no key), in en and
// es; the sections in the order of §3; the card's templates and button.
func TestRenderHTML(t *testing.T) {
	g := v2Graph(t)
	v1 := report.Build("t", []finding.Finding{
		{ID: "st:1", Severity: finding.SeverityRed, Category: "secret_tracked", GroupDetail: "github_pat_classic", Evidence: finding.Evidence{RepoPath: "/src/a", File: "config.py"}},
	}, nil, 0, 0, 0, 0, nil)
	v := buildV2(t, &g, v1)
	for _, lang := range []string{"en", "es"} {
		page := renderHTML(t, lang, v)
		cat, _ := catalog.Load(lang)
		text := visibleText(page)
		if i := strings.Index(text, "{"); i >= 0 {
			t.Errorf("%s: placeholder left: %q", lang, text[max(0, i-40):min(len(text), i+40)])
		}
		if m := regexp.MustCompile(`\[[a-z][a-z0-9_]*(\.[a-z0-9_]+)+\]`).FindString(text); m != "" {
			t.Errorf("%s: key rendered: %s", lang, m)
		}
		if !strings.Contains(page, `<html lang="`+lang+`">`) {
			t.Errorf("%s: html lang", lang)
		}
		last := -1
		// v3: the three steps, then the bottom strip's three columns (§17).
		for _, k := range []string{"html.step.1", "html.index.name", "html.step.2", "html.today.fixes", "html.step.3", "html.since.title", "html.weekly.title", "html.agentfix.title"} {
			i := strings.Index(page, htmlEscape(cat.Text(k, nil)))
			if i <= last {
				t.Errorf("%s: %s out of order (%d after %d)", lang, k, i, last)
			}
			last = i
		}
		// The older checks left the HTML; a pointer to the technical report.
		pointer := htmlEscape(cat.Text("html.legacy.pointer", map[string]string{"file": report.FilenameV2(v1, v) + ".md"}))
		if i := strings.Index(page, pointer); i <= last {
			t.Errorf("%s: legacy pointer %d after %d", lang, i, last)
		}
		// Outside the templates (the AI export has its own «today» heading).
		for _, gone := range []string{"html.legacy.title", "html.legacy.body", "html.today.tag", "html.today.time", "html.status.sub"} {
			if strings.Contains(text, htmlEscape(cat.Text(gone, map[string]string{"count": "1", "groups": "1"}))) {
				t.Errorf("%s: %s in the HTML", lang, gone)
			}
		}
		for _, want := range []string{`<template id="card-svg"><svg xmlns="http://www.w3.org/2000/svg" width="1200" height="630"`, `<template id="card-txt">`, `id="card-button"`, "%USERPROFILE%/.claude/settings.json"} {
			if !strings.Contains(page, want) {
				t.Errorf("%s: missing %s", lang, want)
			}
		}
	}
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;").Replace(s)
}

// J14 (decision 2026-10-26, integridad y revisión semanal, §7): with no
// graph the page cannot look clean — no card; the score unavailable for
// coverage, never not_applicable; step 2 html.today.incomplete and step 3
// html.rest.incomplete instead of «nothing to fix» and «nothing else
// pending»; in en and es. With a graph and no agent, not_applicable stays.
func TestRenderHTML_NoScoreNoFindings(t *testing.T) {
	v := buildV2(t, nil, report.Build("t", nil, []finding.ScanError{{Scope: "global_timeout", Message: "x"}}, 0, 0, 0, 0, nil))
	if v.Coverage.GraphBuilt || v.Index.Machine.Unavailable != index.ReasonCoverage {
		t.Fatalf("no graph: built %v, machine %+v", v.Coverage.GraphBuilt, v.Index.Machine)
	}
	empty := graph.Build(graph.Fragment{}, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: "k", HostID: "h"}, v2Now)
	none := buildV2(t, &empty, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		unavailable := func(reason string) string {
			return htmlEscape(cat.Text("html.index.unavailable", map[string]string{"reason": cat.Text("html.index.reason."+reason, nil)}))
		}
		page := renderHTML(t, lang, v)
		if strings.Contains(page, `id="card-button"`) {
			t.Errorf("%s: no-graph page has a card", lang)
		}
		for _, want := range []string{
			unavailable(index.ReasonCoverage),
			`<p class="none">` + htmlEscape(cat.Text("html.today.incomplete", nil)) + `</p>`,
			`<p class="empty">` + htmlEscape(cat.Text("html.rest.incomplete", nil)) + `</p>`,
		} {
			if !strings.Contains(page, want) {
				t.Errorf("%s: no graph, no %q", lang, want)
			}
		}
		for _, k := range []string{"html.none", "html.step.3.none"} {
			if strings.Contains(page, htmlEscape(cat.Text(k, nil))) {
				t.Errorf("%s: no graph, but %s", lang, k)
			}
		}
		if strings.Contains(page, htmlEscape(cat.Text("html.index.reason.not_applicable", nil))) {
			t.Errorf("%s: no graph, but the not_applicable reason", lang)
		}

		page = renderHTML(t, lang, none)
		if !strings.Contains(page, unavailable(index.ReasonNotApplicable)) {
			t.Errorf("%s: graph with no agent: not applicable line missing", lang)
		}
		for _, k := range []string{"html.none", "html.step.3.none"} {
			if !strings.Contains(page, htmlEscape(cat.Text(k, nil))) {
				t.Errorf("%s: graph with no finding: no %s", lang, k)
			}
		}
		for _, k := range []string{"html.today.incomplete", "html.rest.incomplete"} {
			if strings.Contains(page, htmlEscape(cat.Text(k, nil))) {
				t.Errorf("%s: graph built, but %s", lang, k)
			}
		}
	}
}

// BV-41: today's projection shows before the fix box, only with a score
// that rises and an action that is not partial; the history notice shows
// only when history_reset.
func TestRenderHTML_ProjectionAndHistoryReset(t *testing.T) {
	g := v2Graph(t)
	base := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	a := base.Index.Agents[0]
	if a.Value == nil || a.Projection == nil || a.Projection.Value <= *a.Value {
		t.Fatalf("scenario: %v → %+v, want a rise", a.Value, a.Projection)
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		proj := htmlEscape(cat.Text("html.today.projection", map[string]string{"now": strconv.Itoa(*a.Value), "then": strconv.Itoa(a.Projection.Value)}))
		reset := htmlEscape(cat.Text("html.history.reset", nil))

		page := renderHTML(t, lang, base)
		p, fix := strings.Index(page, proj), strings.Index(page, `class="wide"`)
		if p < 0 || p > fix {
			t.Errorf("%s: projection missing or after the fix box", lang)
		}
		if strings.Contains(page, reset) {
			t.Errorf("%s: history notice without a reset", lang)
		}

		partial := cloneV2(base)
		for i := range partial.Findings[0].Actions {
			if partial.Findings[0].Actions[i].ID == partial.Today.ActionID {
				partial.Findings[0].Actions[i].Completeness = rules.CompletenessPartial
			}
		}
		if strings.Contains(renderHTML(t, lang, partial), proj) {
			t.Errorf("%s: projection shown for a partial action", lang)
		}
		flat := cloneV2(base)
		flat.Index.Agents[0].Projection = &index.Projection{ActionID: flat.Today.ActionID, Value: *a.Value}
		if strings.Contains(renderHTML(t, lang, flat), htmlEscape(cat.Text("html.today.projection", map[string]string{"now": strconv.Itoa(*a.Value), "then": strconv.Itoa(*a.Value)}))) {
			t.Errorf("%s: projection shown without a rise", lang)
		}

		reseted := cloneV2(base)
		reseted.Index.HistoryReset = true
		if !strings.Contains(renderHTML(t, lang, reseted), `<br>`+reset+`</p>`) {
			t.Errorf("%s: history notice missing", lang)
		}
	}
}

// cloneV2 copies the parts of v the test changes.
func cloneV2(v report.V2) report.V2 {
	c := v
	c.Findings = append([]rules.Finding{}, v.Findings...)
	for i := range c.Findings {
		c.Findings[i].Actions = append([]rules.Action{}, v.Findings[i].Actions...)
	}
	c.Index.Agents = append([]index.AgentIndex{}, v.Index.Agents...)
	return c
}

// M12: of the two Supabase keys in one file, the service_role JWT titles
// the finding, with " and 1 more key" (others_count 1).
func TestRenderHTML_TitleOthers(t *testing.T) {
	sbSecret := "sb_secret_" + strings.Repeat("S", 22) + "_" + strings.Repeat("T", 8)
	g := v2GraphWith(t, []platform.CredentialEntry{
		{Path: "/src/a/dashboard/.env.local", Line: 2, Key: "SUPABASE_SECRET_KEY", Value: secrets.NewSecretValue(sbSecret)},
		{Path: "/src/a/dashboard/.env.local", Line: 4, Key: "SUPABASE_SERVICE_ROLE_KEY", Value: secrets.NewSecretValue(serviceRoleJWT)},
	}, nil)
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	if v.Findings[0].Params.Scope != "service_role" || v.Findings[0].OthersCount() != 1 {
		t.Fatalf("title %s, others %d", v.Findings[0].Params.Scope, v.Findings[0].OthersCount())
	}
	// BV-43, v3 §4–§5: the other keys are a pill after today's title in
	// step 2 (after the master «i»), and in a step 3 row's body. Without a
	// today, the finding is a row.
	row := cloneV2(v)
	row.Today = rules.Today{}
	for lang, pill := range map[string]string{
		"en": `<span class="tchip">+1 more key in the same file</span>`,
		"es": `<span class="tchip">+1 clave más en el mismo fichero</span>`,
	} {
		page := renderHTML(t, lang, v)
		_, step2, _ := strings.Cut(page, `<section class="now"`)
		step2, _, _ = strings.Cut(step2, `</section>`)
		if !regexp.MustCompile(`<p class="tf">[^<]*<button type="button" class="i"[^>]*>.*?</button><span class="gl"[^>]*>.*?</span>` + regexp.QuoteMeta(pill) + `</p>`).MatchString(step2) {
			t.Errorf("%s: no pill after today's title and its «i» in step 2", lang)
		}
		if n := strings.Count(page, pill); n != 1 {
			t.Errorf("%s: pill %d times, want 1 (step 2)", lang, n)
		}
		if n := strings.Count(renderHTML(t, lang, row), pill); n != 1 {
			t.Errorf("%s: pill %d times in the rows, want 1", lang, n)
		}
	}
	// No pill without other keys.
	one := v2GraphWith(t, []platform.CredentialEntry{
		{Path: "/src/a/dashboard/.env.local", Line: 4, Key: "SUPABASE_SERVICE_ROLE_KEY", Value: secrets.NewSecretValue(serviceRoleJWT)},
	}, nil)
	w := buildV2(t, &one, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	for _, lang := range []string{"en", "es"} {
		if page := renderHTML(t, lang, w); strings.Contains(page, `<span class="tchip">+`) {
			t.Errorf("%s: pill without other keys", lang)
		}
	}
}

// TestRenderHTML_YourSpaceOnlyWithKey (BV-132): the «Your space» section and
// its app.rosmarops.com URL appear only when this run wrote a closed package
// (a key on this computer); without a key neither is in the HTML, in en and es.
func TestRenderHTML_YourSpaceOnlyWhenConnected(t *testing.T) {
	g := v2Graph(t)
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	for _, lang := range []string{"en", "es"} {
		notConnected := renderHTML(t, lang, v)
		if strings.Contains(notConnected, `class="space"`) || strings.Contains(notConnected, "app.rosmarops.com") {
			t.Errorf("%s: not connected, but the HTML has the «Your space» section or URL", lang)
		}
		// Connected but nothing sent yet: the pending line, no date.
		v.SpaceConnected = true
		v.SpaceSentDate = ""
		pending := renderHTML(t, lang, v)
		if !strings.Contains(pending, `class="space"`) || !strings.Contains(pending, `href="https://app.rosmarops.com/space"`) {
			t.Errorf("%s: connected, but the HTML lacks the «Your space» section or its button URL", lang)
		}
		// Connected and sent: the sent line carries the date.
		v.SpaceSentDate = "2026-10-25"
		sent := renderHTML(t, lang, v)
		if !strings.Contains(sent, "2026-10-25") {
			t.Errorf("%s: the sent line does not carry the date", lang)
		}
		v.SpaceConnected = false
		v.SpaceSentDate = ""
	}
}
