package report_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// Decision 2026-10-26 (lanzadores en escritura, L2 amended, BV-65): after
// the rules version changed, a findings-memory.json and an
// index-history.json written with rules_version "1" make the next run not
// comparable: the score says the detection rules changed, with no
// "prev → now", and the memory too.
func TestRulesVersion2_NotComparable(t *testing.T) {
	if rules.Version != "3" {
		t.Fatalf("rules.Version %q", rules.Version)
	}
	g := v2Graph(t)
	base, _ := knowledge.ClaudeCode()
	salt := []byte("0123456789abcdef0123456789abcdef")
	build := func(prev memory.File, history []index.Entry, at time.Time) report.V2 {
		in := report.V2Input{
			Graph: &g, GraphSHA256: strings.Repeat("ab", 32), Base: base, RepoRoots: []string{"/src/a", "/src/b"},
			Families: [][]string{{"/src/a"}, {"/src/b"}}, V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil),
			GeneratedAt: at, Salt: salt, Memory: prev, History: history,
		}
		if len(history) > 0 {
			in.Previous = &history[len(history)-1]
		}
		v, _ := report.BuildV2(in)
		return v
	}
	run1 := build(memory.File{}, nil, v2Now)

	// The files of the run before the update: rules_version "1".
	raw, err := json.Marshal(run1.NextMemory())
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `"rules_version":"3"`, `"rules_version":"1"`, 1))
	var mem memory.File
	if err := json.Unmarshal(raw, &mem); err != nil || mem.LastChecked == nil || mem.LastChecked.RulesVersion != "1" {
		t.Fatalf("memory %+v %v", mem.LastChecked, err)
	}
	entry := index.EntryOf(run1.Index, run1.GeneratedAt[:10])
	if entry.RulesVersion != "3" {
		t.Fatalf("history entry %+v", entry)
	}
	// Control: the same files at rules_version "3" compare directly.
	if c := build(run1.NextMemory(), []index.Entry{entry}, v2Now.Add(24*time.Hour)).Index.Agents[0].Comparison; c.Status != index.CompareDirect {
		t.Fatalf("control: %+v", c)
	}
	entry.RulesVersion = "1"

	run2 := build(mem, []index.Entry{entry}, v2Now.Add(24*time.Hour))
	if c := run2.Index.Agents[0].Comparison; c.Status != index.CompareNotComparableRules {
		t.Errorf("score comparison %+v", c)
	}
	if run2.Memory.Reason != memory.ReasonRulesChanged {
		t.Errorf("memory reason %q", run2.Memory.Reason)
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, run2)
		meta := page[strings.Index(page, `<p class="meta">`):]
		meta = meta[:strings.Index(meta, "</p>")]
		if !strings.Contains(meta, htmlEscape(cat.Text("html.index.not_comparable.rules", nil))) {
			t.Errorf("%s: no not-comparable line in %s", lang, meta)
		}
		if strings.Contains(meta, " → ") {
			t.Errorf("%s: an arrow in %s", lang, meta)
		}
		md := report.RenderV2Markdown(run2, cat, time.UTC)
		reason := cat.Text("memory.not_comparable", map[string]string{"reason": cat.Text("memory.reason."+memory.ReasonRulesChanged, nil)})
		if !strings.Contains(md, reason) || !strings.Contains(page, htmlEscape(reason)) {
			t.Errorf("%s: memory not comparable missing (%q)", lang, reason)
		}
	}
}
