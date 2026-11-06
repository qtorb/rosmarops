package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

// BV-184 point 5 (console): the numbers typed flip those findings; Enter with
// nothing changes nothing and is not saved.
func TestBV184_ApplyConsoleToggles(t *testing.T) {
	cards := []report.FindingCard{
		{ID: "f_a", Severity: "red", Group: report.CardGroupNew},
		{ID: "f_b", Severity: "yellow", Group: report.CardGroupRest},
		{ID: "cred_c", Severity: "red", Group: report.CardGroupDismissed},
	}
	// Mark 1 (dismiss f_a) and un-mark 3 (recover cred_c).
	ids, saved := applyConsoleToggles(cards, "1, 3\n")
	if !saved {
		t.Fatal("not saved after typing numbers")
	}
	got := strings.Join(ids, ",")
	if got != "f_a" {
		t.Errorf("checked ids = %q, want f_a (f_a on, cred_c recovered)", got)
	}
	// Enter with nothing: no change, not saved.
	if ids, saved := applyConsoleToggles(cards, "\n"); saved || ids != nil {
		t.Errorf("empty answer changed state: ids=%v saved=%v", ids, saved)
	}
	// Out-of-range and junk are ignored (not saved).
	if _, saved := applyConsoleToggles(cards, "9, x\n"); saved {
		t.Error("out-of-range/junk counted as a change")
	}
}

// BV-184 point 5: a console line shows the pill, title, subtitle, agent, and
// the dismissed mark for an already-dismissed finding.
func TestBV184_ConsoleFindingLine(t *testing.T) {
	cat, _ := catalog.Load("en")
	newCard := report.FindingCard{Severity: "red", Title: "A leaked key", New: true, Agent: "Claude Code", Group: report.CardGroupNew}
	line := consoleFindingLine(1, newCard, cat)
	for _, want := range []string{"1.", "[Red]", "A leaked key", "New", "Claude Code"} {
		if !strings.Contains(line, want) {
			t.Errorf("new line %q lacks %q", line, want)
		}
	}
	dis := report.FindingCard{Severity: "yellow", Title: "Writable config", Since: "25 Oct", Agent: "Cursor", Group: report.CardGroupDismissed}
	line = consoleFindingLine(2, dis, cat)
	if !strings.Contains(line, cat.Text("findings.console.dismissed", nil)) {
		t.Errorf("dismissed line %q lacks the dismissed mark", line)
	}
	if !strings.Contains(line, "25 Oct") {
		t.Errorf("since line %q lacks the date", line)
	}
}

// BV-184 point 1: recompute keeps dismissed ids that no longer appear, adds the
// newly ticked ones with today's date, keeps an already-dismissed id's date,
// and drops the unticked.
func TestBV184_RecomputeDismissed(t *testing.T) {
	old := []config.AllowEntryDismissed{
		{ID: "f_gone", Date: "2026-01-01"},   // not visible this run → kept
		{ID: "cred_old", Date: "2026-02-02"}, // visible, stays ticked → date kept
		{ID: "f_drop", Date: "2026-03-03"},   // visible, un-ticked → dropped
	}
	cards := []report.FindingCard{
		{ID: "cred_old", Group: report.CardGroupDismissed},
		{ID: "f_drop", Group: report.CardGroupDismissed},
		{ID: "f_new", Group: report.CardGroupNew},
	}
	checked := []string{"cred_old", "f_new"} // keep cred_old, newly dismiss f_new, drop f_drop
	out := recomputeDismissed(old, cards, checked, "2026-11-06")
	got := map[string]string{}
	for _, e := range out {
		got[e.ID] = e.Date
	}
	if got["f_gone"] != "2026-01-01" {
		t.Errorf("absent id not kept with its date: %v", got)
	}
	if got["cred_old"] != "2026-02-02" {
		t.Errorf("kept id lost its date: %v", got)
	}
	if got["f_new"] != "2026-11-06" {
		t.Errorf("new dismissal missing today's date: %v", got)
	}
	if _, ok := got["f_drop"]; ok {
		t.Errorf("un-ticked id not dropped: %v", got)
	}
	if len(out) != 3 {
		t.Errorf("want 3 entries, got %d: %v", len(out), out)
	}
}

// bv184CmdSetup lays out a temp config, state and last report (the valid
// fixture) and returns their paths. The config points its reports at the temp
// reports directory, so the dismiss flow writes only inside it.
func bv184CmdSetup(t *testing.T) (cfgPath, stPath, reports string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath = filepath.Join(dir, "config.json")
	stPath = filepath.Join(dir, "state.json")
	reports = filepath.Join(dir, "reports")
	cfg := fmt.Sprintf(`{"schema_version": 1, "repo_roots": ["/home/dev/src/webapp"], "report_directory": %q, "allow_list": {}}`, reports)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := state.DefaultState("0.1.0-dev")
	if err != nil {
		t.Fatal(err)
	}
	st.Integrity.ConfigSHA256 = hash.Bytes([]byte(cfg))
	if err := writeAcceptedCopy(reports, []byte(cfg)); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveState(stPath, st); err != nil {
		t.Fatal(err)
	}
	// The last review: the valid v2 fixture.
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "schemaval", "testdata", "valid", "report.v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(report.DataDir(reports), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(report.DataDir(reports), report.V2FileName), src, 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath, stPath, reports
}

// BV-184 points 1-3: saving a dismissal writes allow_list.dismissed into
// config.json and re-points the integrity reference, so the next review does
// not ask «configuration changed».
func TestBV184_OfferDismissWritesBackAndIntegrityHolds(t *testing.T) {
	cfgPath, stPath, reports := bv184CmdSetup(t)
	var dismissedID string
	prev := dismissUIHook
	dismissUIHook = func(cards []report.FindingCard, _ catalog.Catalog) ([]string, bool) {
		if len(cards) == 0 {
			t.Fatal("no cards shown")
		}
		dismissedID = cards[0].ID
		return []string{cards[0].ID}, true
	}
	defer func() { dismissUIHook = prev }()

	offerDismissAfterReport("en", cfgPath, stPath, false, time.Now())

	// config.json now carries the dismissal.
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	al, _ := m["allow_list"].(map[string]any)
	dl, _ := al["dismissed"].([]any)
	if len(dl) != 1 {
		t.Fatalf("allow_list.dismissed = %v, want one entry", al["dismissed"])
	}
	if e, _ := dl[0].(map[string]any); e["id"] != dismissedID {
		t.Errorf("dismissed id = %v, want %s", e["id"], dismissedID)
	}
	// The config is still schema-valid.
	if _, err := config.LoadConfig(cfgPath); err != nil {
		t.Fatalf("written config is invalid: %v", err)
	}
	// Integrity re-pointed: the state hash and the accepted copy match the new
	// config, so the next review does not ask about the change.
	st, err := state.LoadState(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if st.Integrity.ConfigSHA256 != hash.Bytes(raw) {
		t.Error("state config_sha256 does not match the written config")
	}
	accepted, err := os.ReadFile(filepath.Join(report.DataDir(reports), acceptedCopyName))
	if err != nil {
		t.Fatal(err)
	}
	if string(accepted) != string(raw) {
		t.Error("accepted copy does not match the written config")
	}
}

// BV-184 point 3: «Not now» (the UI returns not-saved) changes nothing.
func TestBV184_NotNowChangesNothing(t *testing.T) {
	cfgPath, stPath, _ := bv184CmdSetup(t)
	before, _ := os.ReadFile(cfgPath)
	prev := dismissUIHook
	dismissUIHook = func([]report.FindingCard, catalog.Catalog) ([]string, bool) { return nil, false }
	defer func() { dismissUIHook = prev }()
	offerDismissAfterReport("en", cfgPath, stPath, false, time.Now())
	after, _ := os.ReadFile(cfgPath)
	if string(before) != string(after) {
		t.Error("«Not now» changed config.json")
	}
}

// BV-184 point 3: the automatic offer is gated on a new finding.
func TestBV184_GateOnNew(t *testing.T) {
	if anyNewCard([]report.FindingCard{{New: false}, {New: false}}) {
		t.Error("anyNewCard true with no new finding")
	}
	if !anyNewCard([]report.FindingCard{{New: false}, {New: true}}) {
		t.Error("anyNewCard false with a new finding")
	}
	// onlyIfNew with a report whose findings include new ones (the fixture) calls
	// the UI; with a stub that fails the test if called, a non-new gate holds.
	cfgPath, stPath, _ := bv184CmdSetup(t)
	prev := dismissUIHook
	called := false
	dismissUIHook = func([]report.FindingCard, catalog.Catalog) ([]string, bool) { called = true; return nil, false }
	defer func() { dismissUIHook = prev }()
	offerDismissAfterReport("en", cfgPath, stPath, true, time.Now())
	if !called {
		t.Error("onlyIfNew did not show the UI for a report with new findings")
	}
}
