package trust_test

// BV-248 figures on the reference computer R2 (CTO-56 §6, CTO-57; Pruebas of
// the brief): Cursor with the factory answers, without answers, its profiles
// measured, and Grok Bot with Always allow, Ask every time and Never allow.

import (
	"slices"
	"testing"

	"github.com/qtorb/rosmarops/internal/declared"
	"github.com/qtorb/rosmarops/internal/exposure"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

// r2CursorWith is R2 with Cursor alone, with answers (nil: none) and, when
// perm, an empty ~/.cursor/permissions.json read.
func r2CursorWith(t *testing.T, answers map[string]string, perm bool) report.V2 {
	t.Helper()
	scan := platform.CursorScan{ConfigDir: fcHome + "/.cursor", Exists: true, Declared: answers}
	if perm {
		scan.Files = []platform.CursorFile{{Path: fcHome + "/.cursor/permissions.json", Kind: platform.CursorPermissions, Scope: platform.CursorScopeUser, Status: platform.MCPSourceRead}}
	}
	settings := r2Settings()
	in := pipeline.Input{
		Settings: settings, Cursor: scan,
		CredFiles:    platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("cursor", fcHome+"/.cursor")},
	}
	g := r2Assemble(t, in)
	var prof []report.ProfileInput
	for _, dp := range pipeline.DeclaredProfiles {
		if dp.Product != declared.Cursor {
			continue
		}
		pin, ok := pipeline.DeclaredProfile(in, dp.Product, dp.ID)
		if !ok {
			t.Fatalf("profile %s not built", dp.ID)
		}
		pg := r2Assemble(t, pin)
		prof = append(prof, report.ProfileInput{ID: dp.ID, Product: dp.Product, Graph: &pg, Settings: pin.Settings, Declared: pin.Declared})
	}
	return r2BuildV2(t, &g, &settings, func(v *report.V2Input) {
		v.Cursor, v.Profiles = scan, prof
		if answers != nil {
			v.Declared = map[string]map[string]string{declared.Cursor: answers}
		}
	})
}

// r2GrokBot is R2 with Grok Bot alone, its settings.json read with the
// local permission value (always, ask, never).
func r2GrokBot(t *testing.T, value string) report.V2 {
	t.Helper()
	scan := platform.GrokBotScan{Present: true, SettingsPath: fcHome + "/.grokbot/settings.json", Status: platform.MCPSourceRead, LocalPermissions: []string{value}, Accounts: 1}
	answers := map[string]string{declared.B3AskFirst: declared.AskFirstNone, declared.B4SendTools: declared.No}
	settings := r2Settings()
	in := pipeline.Input{
		Settings: settings, CredFiles: platform.CredentialFileScan{Entries: r2Creds()},
		GrokBot: scan, Declared: map[string]map[string]string{declared.GrokBot: answers},
	}
	g := r2Assemble(t, in)
	return r2BuildV2(t, &g, &settings, func(v *report.V2Input) { v.GrokBot, v.Declared = scan, in.Declared })
}

// classOfSingle is an agent's class on R2 with the same R2-todos context as
// r2ExposureAgents; mod changes R2-todos the same way as the single build.
func classOfSingle(t *testing.T, single report.V2, product string, mod ...func(*pipeline.Input)) exposure.Agent {
	t.Helper()
	var m func(*pipeline.Input)
	if len(mod) > 0 {
		m = mod[0]
	}
	allV, allG := r2AllReportWith(t, m)
	_, band, sid := r2AgentIndexID(t, single, product)
	_, assume := r2Detail(single, sid)
	return exposure.Compute(exposure.Inputs{
		Product: product, Evaluated: true, Single: overviewByProduct(single, product), SingleAssumptions: assume,
		All: overviewByProduct(allV, product), AllGraph: allG, ClientID: overviewByProduct(allV, product).AgentID, FactoryBand: band,
	})
}

func TestBV248_CursorFigures(t *testing.T) {
	cases := []struct {
		name    string
		answers map[string]string
		perm    bool
		score   int
		band    string
	}{
		{"factory answers", declared.Factory(declared.Cursor), false, 31, "exposed"},
		// With a permissions.json, Auto-review's shell writes it too, as today.
		{"factory answers, permissions.json", declared.Factory(declared.Cursor), true, 27, "exposed"},
		// CTO-56 measured «no answer» with a permissions.json present: the
		// file is a configuration Run Everything writes (own_config_writable).
		{"no answer, permissions.json", nil, true, 27, "exposed"},
		{"no answer, no Cursor file", nil, false, 31, "exposed"},
	}
	for _, c := range cases {
		v := r2CursorWith(t, c.answers, c.perm)
		got, band, id := r2AgentIndexID(t, v, "cursor")
		findings, assume := r2Detail(v, id)
		t.Logf("%s: %d (%s) %v %v", c.name, got, band, findings, assume)
		if got != c.score || band != c.band {
			t.Errorf("%s: %d (%s), want %d (%s)", c.name, got, band, c.score, c.band)
		}
		if c.answers != nil && (slices.Contains(assume, "ide_run_mode_not_readable") || !slices.Contains(assume, "declared_by_you")) {
			t.Errorf("%s: assumptions %v, want declared_by_you in place of ide_run_mode_not_readable", c.name, assume)
		}
		if c.answers == nil && !slices.Contains(assume, "not_declared_least_controlled") {
			t.Errorf("%s: assumptions %v, want not_declared_least_controlled", c.name, assume)
		}
	}
	// The factory answers keep today's class (CTO-56 §6).
	if a := classOfSingle(t, r2CursorWith(t, declared.Factory(declared.Cursor), false), "cursor"); a.Class != "D" {
		t.Errorf("cursor class with the factory answers %s, want D", a.Class)
	}
}

// CTO-56 §3: Cursor with P0's answers, its class as r2ExposureAgents gives it
// (R2-todos with the same answers): 77 yellow C (0.500).
func TestBV248_CursorP0InContext(t *testing.T) {
	p0 := pipeline.DeclaredProfiles[0].Answers
	single := r2CursorWith(t, p0, false)
	got, band, _ := r2AgentIndexID(t, single, "cursor")
	a := classOfSingle(t, single, "cursor", func(in *pipeline.Input) { in.Cursor.Declared = p0 })
	if got != 77 || band != "open_gaps" || a.Class != "C" || a.F == nil || *a.F != 0.5 {
		t.Errorf("Cursor with P0: %d (%s) class %s F %v, want 77 open_gaps C 0.5", got, band, a.Class, a.F)
	}
}

// CTO-56 §6: R2-todos is 13 with Cursor's factory answers; without an answer,
// 13 with no Cursor file and 12 with an empty permissions.json (CTO-56's
// measurement).
func TestBV248_R2AllFigures(t *testing.T) {
	perm := []platform.CursorFile{{Path: fcHome + "/.cursor/permissions.json", Kind: platform.CursorPermissions, Scope: platform.CursorScopeUser, Status: platform.MCPSourceRead}}
	for _, c := range []struct {
		name string
		mod  func(*pipeline.Input)
		want int
	}{
		{"factory answers", nil, 13},
		{"no answer", func(in *pipeline.Input) { in.Cursor.Declared = nil }, 13},
		{"no answer, permissions.json", func(in *pipeline.Input) { in.Cursor.Declared, in.Cursor.Files = nil, perm }, 12},
	} {
		v, _ := r2AllReportWith(t, c.mod)
		if v.Index.Machine.Value == nil || *v.Index.Machine.Value != c.want {
			t.Errorf("R2-todos %s: %v, want %d", c.name, v.Index.Machine.Value, c.want)
		}
	}
}

// CTO-56 §3: Cursor's profiles measured on the declared settings, 77 yellow
// with P0 and P1.
func TestBV248_CursorProfiles(t *testing.T) {
	v := r2CursorWith(t, declared.Factory(declared.Cursor), false)
	for _, a := range v.Index.Agents {
		if a.Agent != "Cursor" {
			continue
		}
		if len(a.Profiles) != 2 {
			t.Fatalf("profiles %+v", a.Profiles)
		}
		for _, p := range a.Profiles {
			c := ""
			if p.Letter.Class != nil {
				c = *p.Letter.Class
			}
			t.Logf("Cursor %s: %d %s", p.Profile, p.Value, c)
			if p.Value != 77 {
				t.Errorf("Cursor %s: %d %s, want 77", p.Profile, p.Value, c)
			}
		}
	}
}

// CTO-57: Grok Bot with its local permission read; the figures go in the PR.
func TestBV248_GrokBotFigures(t *testing.T) {
	want := map[string]int{"always": 32, "ask": 100, "never": 100}
	for _, value := range []string{"always", "ask", "never"} {
		v := r2GrokBot(t, value)
		got, band, id := r2AgentIndexID(t, v, "grok-bot")
		findings, assume := r2Detail(v, id)
		a := classOfSingle(t, v, "grok-bot")
		t.Logf("Grok Bot %s: %d (%s) class %s %v %v", value, got, band, a.Class, findings, assume)
		if got != want[value] {
			t.Errorf("Grok Bot %s: %d, want %d", value, got, want[value])
		}
		if value == "always" && !slices.Contains(assume, "measured_local_setting") {
			t.Errorf("Grok Bot always: %v, want measured_local_setting", assume)
		}
	}
}
