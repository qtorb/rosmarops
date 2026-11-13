package report_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/report"
)

// bv225Base is a comparable run with a scored agent and nothing new, gone or
// changed: today it would be notify.done.same.
func bv225Base(t *testing.T) report.V2 {
	t.Helper()
	g := v2Graph(t)
	v := buildV2(t, &g, report.Report{})
	counts := map[string]int{}
	for _, k := range memory.AllVerdicts {
		counts[k] = 0
	}
	counts[memory.VerdictStillObserved] = 2
	v.Memory = memory.Root{Counts: counts}
	v.NewAgents, v.NewMCP, v.MCPChanged = nil, nil, nil
	return v
}

// prefixOf is a catalog text up to its first parameter, to recognise it.
func prefixOf(cat catalog.Catalog, key string) string {
	return strings.SplitN(cat.Text(key, nil), "{", 2)[0]
}

// TestBV225Notification (BV-225 §4): an MCP server that now points elsewhere
// is a change (A); moved findings alone, and nothing at all, show nothing
// (B); the other cases keep their texts.
func TestBV225Notification(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		// Nothing changed: no notification.
		v := bv225Base(t)
		if !report.NothingChanged(v) {
			t.Fatalf("%s: a run with nothing new is not «nothing changed»", lang)
		}
		// Only moved findings: still nothing.
		v = bv225Base(t)
		v.Memory.Moved = []memory.Moved{{Rule: "credential_reachable"}}
		if !report.NothingChanged(v) {
			t.Errorf("%s: moved findings alone are a change", lang)
		}
		// Only an MCP server with another destination: changed (A).
		v = bv225Base(t)
		v.MCPChanged = []report.MCPServerLine{{Name: "github", Agent: "Claude Code", Remote: "https://evil.example"}}
		if report.NothingChanged(v) {
			t.Errorf("%s: an MCP server with another destination is «nothing changed»", lang)
		}
		if n, ok := report.NotificationText(v, cat); !ok || n != cat.Text("notify.done.changed", map[string]string{"agent": "Claude Code", "score": scoreOf(t, v)}) {
			t.Errorf("%s: MCP changed: %q", lang, n)
		}

		// The cases that still show, each with its text.
		cases := []struct {
			name, key string
			mod       func(*report.V2)
		}{
			{"one new finding", "notify.done.new.one", func(v *report.V2) { v.Memory.Counts[memory.VerdictNew] = 1 }},
			{"several new findings", "notify.done.new.other", func(v *report.V2) { v.Memory.Counts[memory.VerdictObservedAgain] = 3 }},
			{"no longer observed", "notify.done.changed", func(v *report.V2) { v.Memory.Counts[memory.VerdictNoLongerObserved] = 1 }},
			{"new agent", "notify.done.changed", func(v *report.V2) { v.NewAgents = []string{"Cursor"} }},
			{"not comparable", "notify.done.not_comparable", func(v *report.V2) { v.Memory.Reason = memory.ReasonRulesChanged }},
		}
		for _, c := range cases {
			v := bv225Base(t)
			c.mod(&v)
			if report.NothingChanged(v) {
				t.Errorf("%s: %s is «nothing changed»", lang, c.name)
			}
			n, ok := report.NotificationText(v, cat)
			if !ok || !strings.HasPrefix(n, prefixOf(cat, c.key)) {
				t.Errorf("%s: %s: %q, want %s", lang, c.name, n, c.key)
			}
		}
		// The abort and failed-run notices are separate texts, unchanged and
		// still in the catalog; notify.done.same stays there too.
		for _, k := range []string{"notify.aborted", "notify.run_failed", "notify.done.same"} {
			if cat.Text(k, nil) == k {
				t.Errorf("%s: %s missing from the catalog", lang, k)
			}
		}
	}
	// No scored agent is not «nothing changed»: the report still opens.
	if report.NothingChanged(report.V2{}) {
		t.Error("a run without a scored agent is «nothing changed»")
	}
}

func scoreOf(t *testing.T, v report.V2) string {
	t.Helper()
	for _, a := range v.Index.Agents {
		if a.Value != nil && a.Agent == "Claude Code" {
			return strconv.Itoa(*a.Value)
		}
	}
	t.Fatal("no scored Claude Code")
	return ""
}
