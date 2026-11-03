package report_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/schemas"
	"github.com/qtorb/rosmarops/internal/schemaval"
)

// TestSpaceViewNewFields (BV-158 part 1): BuildSpaceView fills the five
// optional fields BA-49 added — since, weekly, overview, analysis, card_svg —
// and the view still validates against space-view.v1.
func TestSpaceViewNewFields(t *testing.T) {
	_, sv := buildSpaceViewScenario(t)

	if _, err := report.RenderSpaceViewJSON(sv); err != nil {
		t.Fatalf("space view with the new fields does not validate: %v", err)
	}
	if sv.Since == nil {
		t.Fatal("since is nil")
	}
	// The scenario is a single run, so it is the first and not comparable.
	if !sv.Since.First || sv.Since.Comparable {
		t.Errorf("since: first=%v comparable=%v, want first=true comparable=false", sv.Since.First, sv.Since.Comparable)
	}
	if sv.Weekly == nil {
		t.Fatal("weekly is nil")
	}
	switch sv.Weekly.State {
	case "own", "other", "none":
	default:
		t.Errorf("weekly.state = %q, want own/other/none", sv.Weekly.State)
	}
	if sv.Analysis == nil {
		t.Fatal("analysis is nil")
	}
	if sv.Analysis.KnowledgeReviewedOn == nil || *sv.Analysis.KnowledgeReviewedOn == "" {
		t.Error("analysis.knowledge_reviewed_on is not set")
	}
	// The scenario has an evaluated Claude Code with a score, so there is an
	// overview entry and a card.
	if len(sv.Overview) == 0 {
		t.Error("overview is empty; the scenario has an evaluated agent")
	}
	for _, o := range sv.Overview {
		for _, v := range append(append(append(append([]string{}, o.Nobody...), o.Model...), o.You...), o.Blocked...) {
			switch v {
			case "reads_files", "writes_files", "runs_commands", "sends_out", "changes_other_agents":
			default:
				t.Errorf("overview verb %q is not one of the five", v)
			}
		}
	}
	if sv.CardSVG == nil || !strings.Contains(*sv.CardSVG, "<svg") {
		t.Error("card_svg is not the agent's card SVG")
	}
}

// TestSpaceViewSchemaOptionalAndStrict (BV-158 part 1 / BA-49): a package
// without the new fields still validates; an unknown overview verb or an extra
// property is rejected.
func TestSpaceViewSchemaOptionalAndStrict(t *testing.T) {
	schema, err := schemaval.Compile(schemas.SpaceViewSchema)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	valid := func(doc map[string]any) bool {
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		res, err := schema.Validate(raw)
		if err != nil {
			t.Fatal(err)
		}
		return res.Valid()
	}
	// A minimal package as older versions wrote it: no since/weekly/overview/
	// analysis/card_svg.
	base := func() map[string]any {
		return map[string]any{
			"schema_version":    "space-view.v1",
			"rosmarops_version": "0.7.3",
			"generated_at":      "2026-11-03T10:00:00Z",
			"host_id":           "abcdef0123456789",
			"os":                "windows",
			"agents":            []any{},
			"findings":          []any{},
			"keys":              []any{},
			"changes":           []any{},
			"top_action":        nil,
		}
	}
	if !valid(base()) {
		t.Error("a package without the new fields should validate")
	}
	withOverview := func(verb string) map[string]any {
		d := base()
		d["overview"] = []any{map[string]any{
			"agent_id": "claude-code",
			"nobody":   []any{verb},
			"model":    []any{},
			"you":      []any{},
			"blocked":  []any{},
		}}
		return d
	}
	if !valid(withOverview("reads_files")) {
		t.Error("a known overview verb should validate")
	}
	if valid(withOverview("deletes_everything")) {
		t.Error("an unknown overview verb should be rejected")
	}
	extra := base()
	extra["surprise"] = 1
	if valid(extra) {
		t.Error("an extra root property should be rejected")
	}
	// weekly.time is a strict 24-hour HH:MM (BV-158, aligned with production).
	withTime := func(tm string) map[string]any {
		d := base()
		d["weekly"] = map[string]any{"state": "own", "day": "fri", "time": tm}
		return d
	}
	if !valid(withTime("23:59")) {
		t.Error("weekly.time 23:59 should validate")
	}
	if valid(withTime("24:00")) {
		t.Error("weekly.time 24:00 should be rejected")
	}
	if valid(withTime("9:00")) {
		t.Error("weekly.time 9:00 (no leading zero) should be rejected")
	}
}

// TestBV158_ReportStructure: not connected, the report has a folded «More
// information» and «How to fix it», and step 1's meta is just two lines;
// connected, «More information» and the overview/assumptions dialogs are gone
// (they live in the space), but «Your space» stays.
func TestBV158_ReportStructure(t *testing.T) {
	g := v2Graph(t)
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))

	un := renderHTML(t, "en", v)
	if !strings.Contains(un, `<details class="moreinfo">`) {
		t.Error("not connected: no «More information»")
	}
	if !strings.Contains(un, "How to fix it") {
		t.Error("the agent-fix block is not retitled «How to fix it»")
	}
	meta := un[strings.Index(un, `<p class="meta">`):]
	meta = meta[:strings.Index(meta, "</p>")]
	if n := strings.Count(meta, "<br>"); n != 1 {
		t.Errorf("step 1 meta has %d <br>, want 1 (counts + projects line):\n%s", n, meta)
	}
	if !strings.Contains(meta, "projects checked") {
		t.Errorf("step 1 meta lacks the projects line:\n%s", meta)
	}

	v.SpaceConnected = true
	con := renderHTML(t, "en", v)
	for _, s := range []string{`class="moreinfo"`, `id="ov-dialog"`, `id="as-dialog"`} {
		if strings.Contains(con, s) {
			t.Errorf("connected: the report still has %q", s)
		}
	}
	if !strings.Contains(con, `class="space"`) {
		t.Error("connected: no «Your space» section")
	}
}

// TestBV158_SilencedWarningsInStep1 (BV-158, the pre-merge fix): the silenced
// counts are not dropped. With test data silenced and entries hidden by the
// configuration, step 1 shows both warnings — html.silenced (test data) and
// html.silenced.config — in en and es; with nothing silenced, neither appears.
func TestBV158_SilencedWarningsInStep1(t *testing.T) {
	g := v2Graph(t)
	// One hidden-by-configuration entry (HiddenCount == 1) and two silenced as
	// test data (Silenced.Count == 2).
	v := bv94Build(t, &g, func(in *report.V2Input) {
		in.AllowHits = []config.AllowHit{{List: config.ListGitHooks, ID: "pre-commit", Note: "x"}}
	})
	v.Silenced.Count = 2
	if v.HiddenCount() != 1 {
		t.Fatalf("hidden %d, want 1", v.HiddenCount())
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		_, step1, _ := strings.Cut(page, `<section class="s1" aria-labelledby="step1">`)
		step1, _, _ = strings.Cut(step1, `</section>`)
		testdata := escapeHTML(cat.Plural("html.silenced", 2, map[string]string{"count": "2"}))
		hidden := escapeHTML(cat.Plural("html.silenced.config", 1, map[string]string{"count": "1"}))
		if !strings.Contains(step1, `<p class="meta warn">`+testdata+`</p>`) {
			t.Errorf("%s: step 1 lacks the test-data silenced warning", lang)
		}
		if !strings.Contains(step1, `<p class="meta warn">`+hidden+`</p>`) {
			t.Errorf("%s: step 1 lacks the configuration-hidden warning", lang)
		}
	}

	// With nothing silenced, neither warning appears.
	none := bv94Build(t, &g, nil)
	if none.Silenced.Count != 0 || none.HiddenCount() != 0 {
		t.Fatalf("baseline not clean: silenced=%d hidden=%d", none.Silenced.Count, none.HiddenCount())
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, none)
		for _, key := range []string{"html.silenced", "html.silenced.config"} {
			if strings.Contains(page, escapeHTML(cat.Plural(key, 2, map[string]string{"count": "2"}))) {
				t.Errorf("%s: %s warning present with nothing silenced", lang, key)
			}
		}
	}
}
