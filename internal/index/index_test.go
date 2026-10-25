package index

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/rules"
)

// I1, I3: the band of the worst finding; inside it, the score falls with P
// and never reaches the floor; 100 only with P = 0.
func TestScoreBands(t *testing.T) {
	red := []item{{"a", rules.SeverityRed, 0}}
	yellow := []item{{"a", rules.SeverityYellow, 0}}
	for _, c := range []struct {
		items []item
		p     float64
		want  int
		band  string
	}{
		{red, 0, 49, BandExposed},
		{yellow, 0, 79, BandOpenGaps},
		{nil, 0, 100, BandContained},
		{red, 61, 27, BandExposed},
		{red, 47, 30, BandExposed},
		{yellow, 12, 75, BandOpenGaps},
		{red, 1e9, 0, BandExposed},
		{yellow, 1e9, 50, BandOpenGaps},
		{nil, 1e9, 80, BandContained},
	} {
		got, band := score(c.items, c.p)
		if got != c.want || band != c.band {
			t.Errorf("P=%v %v: %d %s, want %d %s", c.p, c.items, got, band, c.want, c.band)
		}
	}
	prev := 101
	for p := 0.0; p < 2000; p += 0.5 {
		v, _ := score(red, p)
		if v > prev {
			t.Fatalf("not monotonic at P=%v", p)
		}
		prev = v
	}
}

func agentInput(fs []rules.Finding) Input {
	return Input{
		GraphBuilt: true, Inference: map[string]bool{"claude-code": true},
		Agents:   []Agent{{ID: "c", Name: "Claude Code", Product: "claude-code", ConfigDir: "/h/.claude"}},
		Findings: fs, Repos: 10, RulesVersion: "1", KnowledgeVersion: "k",
	}
}

func fnd(id, rule, sev, class, radius string, actions ...rules.Action) rules.Finding {
	return rules.Finding{
		ID: id, Rule: rule, Severity: sev, Radius: radius, Params: rules.Params{Class: class},
		Evidence: rules.Evidence{Nodes: []string{"c"}}, Actions: actions,
	}
}

// I5, §2: a failed source that may hide a credential, or one in the agent's
// folder, leaves no score; another does not. No agent: not applicable.
func TestCoverage(t *testing.T) {
	for reason, ok := range map[string]bool{
		graph.Reason(graph.ReasonReadError, "x"):               true,
		graph.Reason(graph.ReasonFileCountLimitExceeded, "/r"): true,
		graph.Reason(graph.ReasonSourceUnsupported, "legacy"):  false,
	} {
		in := agentInput(nil)
		in.SourcesFailed = []graph.SourceFailed{{Path: "/elsewhere/x", Reason: reason}}
		a := Compute(in).Agents[0]
		if (a.Value == nil) != ok {
			t.Errorf("%s: value %v", reason, a.Value)
		}
	}
	in := agentInput(nil)
	in.SourcesFailed = []graph.SourceFailed{{Path: "/h/.claude/settings.json", Reason: graph.Reason(graph.ReasonSourceUnsupported, "x")}}
	if a := Compute(in).Agents[0]; a.Value != nil || a.Unavailable != ReasonCoverage {
		t.Errorf("source in the agent's folder: %+v", a)
	}
	if m := Compute(Input{GraphBuilt: true}).Machine; m.Value != nil || m.Unavailable != ReasonNotApplicable {
		t.Errorf("no agent: %+v", m)
	}
	if m := Compute(agentInput(nil)).Machine; m.Value == nil || *m.Value != 100 || m.Band != BandContained {
		t.Errorf("one clean agent: %+v", m)
	}
}

// I6: full removes, radius_only takes to same_project, partial changes
// nothing; posture weighs 2 per medium credential.
func TestProjection(t *testing.T) {
	full := rules.Action{ID: "a_full", Completeness: rules.CompletenessFull}
	radius := rules.Action{ID: "a_radius", Completeness: rules.CompletenessRadiusOnly}
	partial := rules.Action{ID: "a_partial", Completeness: rules.CompletenessPartial}
	fs := []rules.Finding{
		fnd("f1", rules.RuleCredReadAndSend, rules.SeverityRed, rules.ClassHigh, rules.RadiusAnyProject, full, radius, partial),
		fnd("f2", rules.RuleOwnInstructionsWritable, rules.SeverityYellow, "", ""),
	}
	// P = 30 + 4 = 34: 34; without f1 (yellow, P = 4): 77; f1 same_project
	// (P = 24): 37; partial: unchanged.
	for action, want := range map[string]int{"a_full": 77, "a_radius": 37, "a_partial": 34} {
		in := agentInput(fs)
		in.Today = rules.Today{FindingID: "f1", ActionID: action}
		a := Compute(in).Agents[0]
		if *a.Value != 34 || a.Projection == nil || a.Projection.Value != want {
			t.Errorf("%s: %d → %+v, want 34 → %d", action, *a.Value, a.Projection, want)
		}
	}
	in := agentInput(nil)
	in.MediumReadable = map[string]int{"c": 3}
	if a := Compute(in).Agents[0]; *a.Value != 80+int(20*80/86) {
		t.Errorf("posture: %d", *a.Value)
	}
}

// I7: direct with the same versions and scope; not comparable when the
// rules changed, or the repositories fell more than 10%.
func TestComparison(t *testing.T) {
	v := 20
	prev := &Entry{
		IndexVersion: 1, RulesVersion: "1", KnowledgeVersion: "k",
		Agents: []EntryAgent{{Agent: "Claude Code", Value: &v}}, Scope: EntryScope{Agents: []string{"Claude Code"}, Repos: 10},
	}
	for name, c := range map[string]struct {
		mod  func(in *Input)
		want string
	}{
		"direct":     {func(in *Input) {}, CompareDirect},
		"rules":      {func(in *Input) { in.RulesVersion = "2" }, CompareNotComparableRules},
		"knowledge":  {func(in *Input) { in.KnowledgeVersion = "k2" }, CompareNotComparableRules},
		"repos -20%": {func(in *Input) { in.Repos = 8 }, CompareNotComparableScope},
		"repos -10%": {func(in *Input) { in.Repos = 9 }, CompareDirect},
		"first":      {func(in *Input) { in.Previous = nil }, CompareNone},
	} {
		in := agentInput(nil)
		in.Previous = prev
		c.mod(&in)
		if got := Compute(in).Agents[0].Comparison; got.Status != c.want {
			t.Errorf("%s: %+v, want %s", name, got, c.want)
		}
	}
}

// I9: the history keeps at most 104 entries; an invalid file is set aside
// as .invalid (never over another) and a new history starts.
func TestHistory(t *testing.T) {
	dir := t.TempDir()
	h, reset, err := ReadHistory(dir)
	if err != nil || reset || len(h.Entries) != 0 {
		t.Fatalf("empty: %+v %v %v", h, reset, err)
	}
	idx := Compute(agentInput(nil))
	for i := 0; i < MaxEntries+5; i++ {
		if err := WriteHistory(dir, h, EntryOf(idx, "2026-10-24")); err != nil {
			t.Fatal(err)
		}
		if h, _, err = ReadHistory(dir); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.Entries) != MaxEntries || h.Last() == nil || *h.Last().Agents[0].Value != 100 {
		t.Errorf("entries %d", len(h.Entries))
	}
	raw, _ := os.ReadFile(filepath.Join(dir, HistoryFileName))
	for _, never := range []string{"/h/", "agent_id", "host"} {
		if strings.Contains(string(raw), never) {
			t.Errorf("history holds %q", never)
		}
	}
	for i, want := range []string{HistoryFileName + ".invalid", HistoryFileName + ".1.invalid"} {
		if err := os.WriteFile(filepath.Join(dir, HistoryFileName), []byte(`{"schema_version":1,"entries":[{"date":"x"}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		h, reset, err := ReadHistory(dir)
		if err != nil || !reset || len(h.Entries) != 0 {
			t.Errorf("invalid %d: %v %v", i, reset, err)
		}
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("not set aside as %s", want)
		}
	}
}

// informe HTML v2 §5: the last comparable scores, oldest first, up to 8;
// a run that is not comparable with the next one cuts the series.
func TestSeries(t *testing.T) {
	n := func(v int) *int { return &v }
	e := func(rulesV string, repos int, v *int) Entry {
		return Entry{IndexVersion: Version, RulesVersion: rulesV, KnowledgeVersion: "k", Agents: []EntryAgent{{Agent: "Claude Code", Value: v}}, Scope: EntryScope{Agents: []string{"Claude Code"}, Repos: repos}}
	}
	var hist []Entry
	for i := 0; i < 10; i++ {
		hist = append(hist, e("1", 3, n(10+i)))
	}
	cur := e("1", 3, n(50))
	if got := Series(hist, cur, "Claude Code"); !reflect.DeepEqual(got, []int{13, 14, 15, 16, 17, 18, 19, 50}) {
		t.Errorf("eight: %v", got)
	}
	hist[7] = e("0", 3, n(17)) // other rules: the series starts after it
	if got := Series(hist, cur, "Claude Code"); !reflect.DeepEqual(got, []int{18, 19, 50}) {
		t.Errorf("rules cut: %v", got)
	}
	hist[9] = e("1", 3, nil) // no score: cut
	if got := Series(hist, cur, "Claude Code"); !reflect.DeepEqual(got, []int{50}) {
		t.Errorf("no score: %v", got)
	}
	if got := Series(nil, e("1", 3, nil), "Claude Code"); len(got) != 0 {
		t.Errorf("unavailable: %v", got)
	}
	hist[9] = e("1", 10, n(19)) // 10 repos, then 3: scope cut
	if got := Series(hist, cur, "Claude Code"); !reflect.DeepEqual(got, []int{50}) {
		t.Errorf("scope cut: %v", got)
	}
}
