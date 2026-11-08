package report

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/rules"
)

// TestSchemaErrorFields (BV-201 point 3): the field-path extractor returns only
// the paths of a schema-validation error — the text and any value after ": "
// are dropped, so the sends log's "failed" line never carries a value.
func TestSchemaErrorFields(t *testing.T) {
	err := errors.New("space view fails schema validation:\n" +
		"  findings.0.text.en.title: String length must be less than or equal to 200\n" +
		"  findings.0.paste_block: ghp_FAKE_secret_value_0000 is too long")
	got := SchemaErrorFields(err)
	want := []string{"findings.0.text.en.title", "findings.0.paste_block"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("fields = %v, want %v", got, want)
	}
	for _, f := range got {
		if strings.Contains(f, "ghp_FAKE") || strings.ContainsAny(f, " \t") {
			t.Fatalf("a field path carries a value or space: %q", f)
		}
	}
	if SchemaErrorFields(nil) != nil {
		t.Error("nil error should give no fields")
	}
}

// bv201Slugs are every agent the engine can score or detect, so the space view
// carries them all at once (BV-201 point 4). All match agents[].id's
// ^[a-z0-9-]{1,40}$ pattern.
var bv201Slugs = []string{
	"claude-code", "cursor", "codex", "github-copilot", "gemini-cli",
	"cline", "grok-build", "hermes", "antigravity-cli",
	"claude-desktop", "antigravity", "cline-cli", "copilot-jetbrains",
}

// TestBV201AllListsAtMaxValidate (BV-201 point 4): a space view that carries
// every scored and detected agent at once, with every list driven to its
// schema maximum + 1 and every text past its limit, still builds and validates
// — the generator clips each text and each list to the space-view.v1 limit
// (the same clipRunes/clamp rule throughout), so an oversize run is sent, never
// silently dropped. Before BV-201 findings[].text and changes[].text were not
// clamped and a single long value (a Codex Computer Use {apps} list) failed the
// whole package.
func TestBV201AllListsAtMaxValidate(t *testing.T) {
	over := func(n int) string { return strings.Repeat("x", n) }

	// Findings: text built through the generator's own clamp, from values over
	// every finding_text limit (title 200, what_it_means 600, step 1000,
	// paste_block 16000).
	paste := over(svMaxPaste + 100)
	mkText := func() SVText {
		return clampFindingText(SVText{
			Title: over(svMax200 + 50), WhatItMeans: over(svMax600 + 50),
			Steps: []string{over(svMax1000 + 50), over(svMax1000 + 50)}, PasteBlock: &paste,
		})
	}
	findings := make([]SVFinding, 0, 5)
	for i := 0; i < 5; i++ {
		findings = append(findings, SVFinding{
			ID: "f" + strconv.Itoa(i), Rule: "computer_use_app_allowed", Severity: "yellow",
			Weight: 1, Text: map[string]SVText{"en": mkText(), "es": mkText()},
		})
	}

	// Agents: one per known slug, each with a service carrying valid enums.
	score := 50
	band := "yellow"
	agents := make([]SVAgent, 0, len(bv201Slugs))
	for _, id := range bv201Slugs {
		agents = append(agents, SVAgent{
			ID: id, Name: "Agent", Evaluated: true, Score: &score, Band: &band,
			Services: []SVService{{Service: "svc", Category: "money", Access: "writes", Approval: "nobody"}},
		})
	}

	// Changes: text past the 200 limit, clipped the way svChanges does.
	changes := make([]SVChange, 0, 5)
	for i := 0; i < 5; i++ {
		id := "f" + strconv.Itoa(i)
		changes = append(changes, SVChange{Kind: "new", FindingID: &id, Text: map[string]string{
			"en": clipRunes(over(svMax200+50), svMax200), "es": clipRunes(over(svMax200+50), svMax200),
		}})
	}

	// overview and since.score_changes at the schema maximum (20): the generator
	// caps both at 20 (svOverview/svSince); the cap itself is checked below.
	overview := make([]SVOverview, 0, 20)
	scoreChanges := make([]SVScoreChange, 0, 20)
	for i := 0; i < 20; i++ {
		overview = append(overview, SVOverview{
			AgentID: "a" + strconv.Itoa(i), Nobody: []string{rules.RowReadsFiles},
			Model: []string{rules.RowSendsOut}, You: []string{rules.RowWritesFiles},
			Blocked: []string{rules.RowRunsCommands},
		})
		scoreChanges = append(scoreChanges, SVScoreChange{AgentID: "a" + strconv.Itoa(i), From: 1, To: 2})
	}

	// Report: every list one past its maximum, every text past its limit; clamped
	// by the generator's clampReport.
	big := SVT{EN: over(svMax600 + 50), ES: over(svMax600 + 50)}
	t300 := SVT{EN: over(svMax300 + 50), ES: over(svMax300 + 50)}
	rows := make([]SVRow, 0, svMaxRows+1)
	for i := 0; i < svMaxRows+1; i++ {
		locs := make([]string, 0, svMaxLocs+1)
		for j := 0; j < svMaxLocs+1; j++ {
			locs = append(locs, over(svMax400+50))
		}
		assum := make([]SVT, svMaxAssum+1)
		for j := range assum {
			assum[j] = big
		}
		fix := make([]SVT, svMaxFix+1)
		for j := range fix {
			fix[j] = SVT{EN: over(svMax1000 + 50), ES: over(svMax1000 + 50)}
		}
		verdict, conf, gain := SVT{EN: over(svMax200 + 50), ES: over(svMax200 + 50)}, t300, t300
		rows = append(rows, SVRow{
			FindingID: "f0", Verdict: &verdict, Locations: locs, Confidence: &conf,
			Assumptions: assum, FixSteps: fix, Gain: &gain,
		})
	}
	fill := func(n int) []SVT {
		out := make([]SVT, n)
		for i := range out {
			out[i] = big
		}
		return out
	}
	rep := &SVReport{
		Step1: SVStep1{Headline: t300, Counts: t300, Meta: t300, Warnings: fill(svMaxWarn + 1)},
		Step2: SVStep2{ActionTitle: t300, Lines: fill(svMaxLines + 1)},
		Rows:  rows, HowFixBody: big,
		AIExport: SVT{EN: over(svMax49152 + 50), ES: over(svMax49152 + 50)},
		Coverage: fill(svMaxCov + 1), Signals: make([]SVT, 0, svMaxSignal+1),
	}
	for i := 0; i < svMaxSignal+1; i++ {
		rep.Signals = append(rep.Signals, t300)
	}
	clampReport(rep)

	card := over(40000)
	sv := SpaceView{
		SchemaVersion: "space-view.v1", RosmaropsVersion: "0.10.0",
		GeneratedAt: "2026-11-08T12:00:00Z", HostID: "0123456789abcdef", OS: "windows",
		Agents: agents, Findings: findings, Keys: []SVKey{}, Changes: changes, TopAction: nil,
		Since:    &SVSince{ScoreChanges: scoreChanges},
		Weekly:   &SVWeekly{State: "none"},
		Overview: overview,
		Analysis: &SVAnalysis{TestdataCount: 0},
		CardSVG:  &card, Report: rep,
	}

	if _, err := RenderSpaceViewJSON(sv); err != nil {
		t.Fatalf("maxed space view did not clamp to a valid package: %v", err)
	}

	// The clamps held: every list is within its schema maximum.
	if len(rep.Rows) != svMaxRows || len(rep.Coverage) != svMaxCov || len(rep.Signals) != svMaxSignal ||
		len(rep.Step1.Warnings) != svMaxWarn || len(rep.Step2.Lines) != svMaxLines {
		t.Errorf("report lists not clamped: rows=%d cov=%d sig=%d warn=%d lines=%d",
			len(rep.Rows), len(rep.Coverage), len(rep.Signals), len(rep.Step1.Warnings), len(rep.Step2.Lines))
	}
	for _, row := range rep.Rows {
		if len(row.Locations) > svMaxLocs || len(row.Assumptions) > svMaxAssum || len(row.FixSteps) > svMaxFix {
			t.Fatalf("row lists not clamped: locs=%d assum=%d fix=%d", len(row.Locations), len(row.Assumptions), len(row.FixSteps))
		}
	}
	for _, f := range findings {
		for lang, tx := range f.Text {
			if len([]rune(tx.Title)) > svMax200 || len([]rune(tx.WhatItMeans)) > svMax600 {
				t.Errorf("%s finding text not clamped: title=%d what=%d", lang, len([]rune(tx.Title)), len([]rune(tx.WhatItMeans)))
			}
		}
	}

	// The generator caps overview at 20 even when more agents are present
	// (svSince caps score_changes the same way).
	var many []rules.OverviewAgent
	for i := 0; i < 25; i++ {
		many = append(many, rules.OverviewAgent{
			AgentID: "x" + strconv.Itoa(i), Product: "cline",
			Rows: []rules.OverviewCell{{Row: rules.RowReadsFiles, Decider: rules.DeciderNobody}},
		})
	}
	if got := len(svOverview(V2{AgentsOverview: many})); got != 20 {
		t.Errorf("svOverview did not cap at 20: got %d", got)
	}
}
