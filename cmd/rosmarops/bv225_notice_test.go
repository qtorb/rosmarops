package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/report"
)

// bv225V2 is a finished scheduled review with a scored agent, comparable with
// the last one, with nothing new or changed.
func bv225V2() report.V2 {
	score := 72
	counts := map[string]int{}
	for _, k := range memory.AllVerdicts {
		counts[k] = 0
	}
	counts[memory.VerdictStillObserved] = 3
	var v report.V2
	v.Index.Agents = []index.AgentIndex{{Agent: "Claude Code", AgentID: "claude-code"}}
	v.Index.Agents[0].Value = &score
	v.Memory = memory.Root{Counts: counts}
	return v
}

// TestBV225ScheduledNotice (BV-225 §4): with nothing changed (or only moved
// findings) a scheduled review shows no notification and opens no report,
// whatever Windows allows, and the report it wrote stays as it is; every
// other case notifies as before, or opens the report when notifications are
// off.
func TestBV225ScheduledNotice(t *testing.T) {
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	page := filepath.Join(dir, "report.html")
	if err := os.WriteFile(page, []byte("<html>the report</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	quiet := map[string]func(*report.V2){
		"nothing changed": func(*report.V2) {},
		"only moved":      func(v *report.V2) { v.Memory.Moved = []memory.Moved{{Rule: "credential_reachable"}} },
	}
	for name, mod := range quiet {
		for _, setting := range []string{"Enabled", "DisabledForUser"} {
			log := captureLog(t)
			f := fakeNotifier{probeSetting: setting, showSetting: setting}
			n := f.notifier()
			v := bv225V2()
			mod(&v)
			if !scheduledNotice(context.Background(), n, n.probe(context.Background()), v, cat, page) {
				t.Errorf("%s/%s: reported as not delivered", name, setting)
			}
			if len(f.shows) != 0 || len(f.opens) != 0 {
				t.Errorf("%s/%s: shows %d, opens %d; want none", name, setting, len(f.shows), len(f.opens))
			}
			if !strings.Contains(log.String(), "notify: nothing changed") {
				t.Errorf("%s/%s: the log does not say so:\n%s", name, setting, log.String())
			}
			if raw, err := os.ReadFile(page); err != nil || string(raw) != "<html>the report</html>" {
				t.Errorf("%s/%s: the report changed or is gone: %q %v", name, setting, raw, err)
			}
		}
	}

	loud := []struct {
		name, key string
		mod       func(*report.V2)
	}{
		{"one new", "notify.done.new.one", func(v *report.V2) { v.Memory.Counts[memory.VerdictNew] = 1 }},
		{"several new", "notify.done.new.other", func(v *report.V2) { v.Memory.Counts[memory.VerdictNew] = 2 }},
		{"changed", "notify.done.changed", func(v *report.V2) { v.Memory.Counts[memory.VerdictNoLongerObserved] = 1 }},
		{"mcp changed", "notify.done.changed", func(v *report.V2) {
			v.MCPChanged = []report.MCPServerLine{{Name: "github", Agent: "Claude Code"}}
		}},
		{"not comparable", "notify.done.not_comparable", func(v *report.V2) { v.Memory.Reason = memory.ReasonRulesChanged }},
	}
	for _, c := range loud {
		for _, setting := range []string{"Enabled", "DisabledForUser"} {
			f := fakeNotifier{probeSetting: setting, showSetting: setting}
			n := f.notifier()
			v := bv225V2()
			c.mod(&v)
			scheduledNotice(context.Background(), n, n.probe(context.Background()), v, cat, page)
			if setting == "Enabled" {
				want := strings.SplitN(cat.Text(c.key, nil), "{", 2)[0]
				if len(f.shows) != 1 || len(f.opens) != 0 || !strings.Contains(f.shows[0], want) {
					t.Errorf("%s/%s: shows %v, opens %d; want %s", c.name, setting, f.shows, len(f.opens), c.key)
				}
			} else if len(f.shows) != 0 || len(f.opens) != 1 || f.opens[0] != page {
				t.Errorf("%s/%s: shows %d, opens %v; want the report opened", c.name, setting, len(f.shows), f.opens)
			}
		}
	}

	// The abort and failed-run notices do not go through this rule: they
	// still show, or open the report.
	for _, k := range []string{"notify.aborted", "notify.run_failed"} {
		f := fakeNotifier{probeSetting: "DisabledForUser"}
		f.notifier().announce(context.Background(), cat.Text(k, nil), page)
		if len(f.opens) != 1 {
			t.Errorf("%s: opens %d", k, len(f.opens))
		}
		f = fakeNotifier{probeSetting: "Enabled", showSetting: "Enabled"}
		f.notifier().announce(context.Background(), cat.Text(k, nil), page)
		if len(f.shows) != 1 {
			t.Errorf("%s: shows %d", k, len(f.shows))
		}
	}
}
