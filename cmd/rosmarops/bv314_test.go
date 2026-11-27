package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

// BV-314 («Ya la he cambiado»): from the findings window or the console, a key
// saved in git is recorded in allow_list.rotated_credentials of
// ~/.rosmarops/config.json, with the same atomic write and the same integrity
// re-pointing as dismissed and accepted (invariant 2).

func bv314Cards() []report.FindingCard {
	return []report.FindingCard{
		{ID: "f_git", Severity: "red", Title: "Tu clave de Supabase: en git", Group: report.CardGroupRest, CredID: "cred_1111111111111111"},
		{ID: "f_read", Severity: "yellow", Title: "B", Group: report.CardGroupRest},
		{ID: "f_git_dis", Severity: "red", Title: "C", Group: report.CardGroupDismissed, CredID: "cred_2222222222222222"},
		{ID: "f_git_acc", Severity: "red", Title: "D", Group: report.CardGroupAccepted, CredID: "cred_3333333333333333"},
	}
}

// bv314Config is the setup of BV-184 with a configuration that has every kind
// of key: its own settings, a test-data entry, a dismissal and two rotated
// entries an agent wrote by hand (one with a note, of a key no card shows).
func bv314Config(t *testing.T) (cfgPath, stPath, reports string) {
	t.Helper()
	cfgPath, stPath, reports = bv184CmdSetup(t)
	raw := []byte(fmt.Sprintf(`{"schema_version": 1, "repo_roots": ["/home/dev/src/webapp"], "report_directory": %q, "allow_list": {`+
		`"test_fixtures": [{"repo": "/home/dev/src/webapp", "path": "test/**", "note": "fake keys", "expires_on": "2027-10-01"}], `+
		`"dismissed": [{"id": "f_git_dis", "date": "2026-11-10"}], "accepted": [{"id": "f_git_acc", "date": "2026-11-11"}], `+
		`"rotated_credentials": [{"id": "cred_9999999999999999", "date": "2026-11-01", "note": "rotated by my agent"}]}}`, reports))
	if err := os.WriteFile(cfgPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := state.LoadState(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Integrity.ConfigSHA256 = hash.Bytes(raw)
	if err := state.SaveState(stPath, st); err != nil {
		t.Fatal(err)
	}
	return cfgPath, stPath, reports
}

func bv314Save(t *testing.T, cfgPath, stPath, reports string, cards []report.FindingCard, answer, today string) config.Config {
	t.Helper()
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	ch, saved, na := applyConsoleToggles(cards, answer)
	if !saved || len(na) != 0 {
		t.Fatalf("%q: saved %v na %v", answer, saved, na)
	}
	if err := saveFindingChoice(cfgPath, reports, stPath, cfg.AllowList, cards, ch, today); err != nil {
		t.Fatal(err)
	}
	out, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Marking a key «Ya la he cambiado» writes its credential with the day it is
// marked and an empty note, and re-reading the file gives it back; the other
// keys of the file stay as they were; the next review does not ask about the
// change (the integrity reference and the accepted copy are the new file).
func TestBV314_MarkSaveReread(t *testing.T) {
	cfgPath, stPath, reports := bv314Config(t)
	cfg := bv314Save(t, cfgPath, stPath, reports, bv314Cards(), "1c\n", "2026-11-27")
	want := []config.AllowEntryRotated{
		{ID: "cred_1111111111111111", Date: "2026-11-27", Note: ""},
		{ID: "cred_9999999999999999", Date: "2026-11-01", Note: "rotated by my agent"},
	}
	if !reflect.DeepEqual(cfg.AllowList.RotatedCredentials, want) {
		t.Errorf("rotated %v, want %v", cfg.AllowList.RotatedCredentials, want)
	}
	if !reflect.DeepEqual(cfg.AllowList.Dismissed, []config.AllowEntryDismissed{{ID: "f_git_dis", Date: "2026-11-10"}}) ||
		!reflect.DeepEqual(cfg.AllowList.Accepted, []config.AllowEntryAccepted{{ID: "f_git_acc", Date: "2026-11-11"}}) {
		t.Errorf("dismissed %v accepted %v changed", cfg.AllowList.Dismissed, cfg.AllowList.Accepted)
	}
	if len(cfg.AllowList.TestFixtures) != 1 || cfg.AllowList.TestFixtures[0].Note != "fake keys" || len(cfg.RepoRoots) != 1 || cfg.ReportDirectory != reports {
		t.Errorf("the other keys changed: %+v", cfg)
	}
	// The next review does not ask: the configuration's hash is the accepted
	// one, and the accepted copy is the file.
	raw, _ := os.ReadFile(cfgPath)
	st, err := state.LoadState(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if st.Integrity.ConfigSHA256 != hash.Bytes(raw) {
		t.Error("the integrity reference is not the written configuration: the next review would ask")
	}
	if cp, _ := os.ReadFile(filepath.Join(report.DataDir(reports), acceptedCopyName)); string(cp) != string(raw) {
		t.Error("the accepted copy is not the written configuration")
	}
	// The entry as written: id, date and note.
	var m struct {
		AllowList struct {
			Rotated []map[string]any `json:"rotated_credentials"`
		} `json:"allow_list"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if e := m.AllowList.Rotated[0]; e["id"] != "cred_1111111111111111" || e["date"] != "2026-11-27" || e["note"] != "" || len(e) != 3 {
		t.Errorf("entry %v", e)
	}
}

// Unmarking a key recorded as changed and saving removes its entry; an entry
// of a key no card shows (written by an agent or by hand) is kept with its
// date and note.
func TestBV314_Unmark(t *testing.T) {
	cfgPath, stPath, reports := bv314Config(t)
	bv314Save(t, cfgPath, stPath, reports, bv314Cards(), "1c\n", "2026-11-27")
	cards := bv314Cards()
	cards[0].Group = report.CardGroupRotated
	cfg := bv314Save(t, cfgPath, stPath, reports, cards, "1c\n", "2026-11-28")
	if want := []config.AllowEntryRotated{{ID: "cred_9999999999999999", Date: "2026-11-01", Note: "rotated by my agent"}}; !reflect.DeepEqual(cfg.AllowList.RotatedCredentials, want) {
		t.Errorf("rotated %v, want %v", cfg.AllowList.RotatedCredentials, want)
	}
	// Saving with the key left marked keeps its first date.
	bv314Save(t, cfgPath, stPath, reports, bv314Cards(), "1c\n", "2026-11-27")
	cfg = bv314Save(t, cfgPath, stPath, reports, cards, "2\n", "2026-12-01")
	if cfg.AllowList.RotatedCredentials[0] != (config.AllowEntryRotated{ID: "cred_1111111111111111", Date: "2026-11-27", Note: ""}) {
		t.Errorf("a kept mark lost its date: %v", cfg.AllowList.RotatedCredentials)
	}
}

// The three choices exclude each other: marking a dismissed or an accepted key
// «Ya la he cambiado» takes it out of its list, and «Na» on a key recorded as
// changed accepts it and takes its entry out.
func TestBV314_Exclusive(t *testing.T) {
	cfgPath, stPath, reports := bv314Config(t)
	cfg := bv314Save(t, cfgPath, stPath, reports, bv314Cards(), "3c, 4c\n", "2026-11-27")
	if len(cfg.AllowList.Dismissed) != 0 || len(cfg.AllowList.Accepted) != 0 {
		t.Errorf("dismissed %v accepted %v, want both empty", cfg.AllowList.Dismissed, cfg.AllowList.Accepted)
	}
	ids := map[string]bool{}
	for _, e := range cfg.AllowList.RotatedCredentials {
		ids[e.ID] = true
	}
	if !ids["cred_2222222222222222"] || !ids["cred_3333333333333333"] {
		t.Errorf("rotated %v", cfg.AllowList.RotatedCredentials)
	}
	cards := bv314Cards()
	cards[2].Group, cards[3].Group = report.CardGroupRotated, report.CardGroupRotated
	cfg = bv314Save(t, cfgPath, stPath, reports, cards, "3a, 4\n", "2026-11-28")
	if !reflect.DeepEqual(cfg.AllowList.Accepted, []config.AllowEntryAccepted{{ID: "f_git_dis", Date: "2026-11-28"}}) ||
		!reflect.DeepEqual(cfg.AllowList.Dismissed, []config.AllowEntryDismissed{{ID: "f_git_acc", Date: "2026-11-28"}}) {
		t.Errorf("dismissed %v accepted %v", cfg.AllowList.Dismissed, cfg.AllowList.Accepted)
	}
	for _, e := range cfg.AllowList.RotatedCredentials {
		if e.ID == "cred_2222222222222222" || e.ID == "cred_3333333333333333" {
			t.Errorf("%s still recorded as changed", e.ID)
		}
	}
	// A choice the window sends in two lists counts as «Ya la he cambiado».
	ch := findingChoice{Dismissed: []string{"f_git"}, Rotated: []string{"f_git"}}
	if err := saveFindingChoice(cfgPath, reports, stPath, cfg.AllowList, bv314Cards(), ch, "2026-11-29"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = config.LoadConfig(cfgPath)
	for _, d := range cfg.AllowList.Dismissed {
		if d.ID == "f_git" {
			t.Error("f_git both dismissed and changed")
		}
	}
}

// «3c» on a finding that is not a key saved in git changes nothing and is
// named, for findings.console.rotated.na; with nothing else, nothing is saved.
func TestBV314_ConsoleNotAKey(t *testing.T) {
	cards := bv314Cards()
	if _, saved, na := applyConsoleToggles(cards, "2c\n"); saved || !reflect.DeepEqual(na, []int{2}) {
		t.Errorf("2c: saved %v na %v", saved, na)
	}
	ch, saved, na := applyConsoleToggles(cards, "2c, 1c, 2a\n")
	if !saved || !reflect.DeepEqual(na, []int{2}) || !reflect.DeepEqual(ch.Rotated, []string{"f_git"}) || !reflect.DeepEqual(ch.Accepted, []string{"f_read", "f_git_acc"}) {
		t.Errorf("mixed: %+v saved %v na %v", ch, saved, na)
	}
	// Upper case works, as «A» does.
	if ch, _, _ := applyConsoleToggles(cards, "1C\n"); !reflect.DeepEqual(ch.Rotated, []string{"f_git"}) {
		t.Errorf("1C: %+v", ch)
	}
}

// A key recorded as changed shows in the console list with its mark.
func TestBV314_ConsoleMark(t *testing.T) {
	for lang, want := range map[string]string{"en": "(key changed, declared by you)", "es": "(clave cambiada, declarado por ti)"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		c := bv314Cards()[0]
		c.Group = report.CardGroupRotated
		if l := consoleFindingLine(1, c, cat); l[len(l)-len(want):] != want {
			t.Errorf("%s: %q lacks %q", lang, l, want)
		}
	}
}

// A configuration outside ~/.rosmarops is never written, rotated
// credentials included.
func TestBV314_OutsideNotWritten(t *testing.T) {
	_, stPath, reports := bv314Config(t)
	other := filepath.Join(t.TempDir(), "rosmarops.config.json")
	raw := []byte(`{"schema_version": 1, "repo_roots": [], "allow_list": {}}`)
	if err := os.WriteFile(other, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	ch := findingChoice{Rotated: []string{"f_git"}}
	if err := saveFindingChoice(other, reports, stPath, config.AllowList{}, bv314Cards(), ch, "2026-11-27"); err == nil {
		t.Fatal("a configuration outside ~/.rosmarops was written")
	}
	if after, _ := os.ReadFile(other); string(after) != string(raw) {
		t.Error("the outside configuration changed")
	}
}
