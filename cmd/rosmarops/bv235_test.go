package main

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/state"
)

// BV-235 (CTO-48 §2.1, R3): «Lo asumo» is written by the same path as
// «descartar», into ~/.rosmarops/config.json only, and never loses a dismissal.

// bv235Config writes a 0.11.7 configuration with two dismissals into the temp
// home of bv184CmdSetup and points the integrity reference at it.
func bv235Config(t *testing.T) (cfgPath, stPath, reports string, raw []byte) {
	t.Helper()
	cfgPath, stPath, reports = bv184CmdSetup(t)
	raw = []byte(fmt.Sprintf(`{"schema_version": 1, "repo_roots": ["/home/dev/src/webapp"], "report_directory": %q, `+
		`"allow_list": {"dismissed": [{"id": "f_aaaaaaaaaaaa", "date": "2026-11-10"}, {"id": "cred_bbbbbbbbbbbb", "date": "2026-11-12"}]}}`, reports))
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
	return cfgPath, stPath, reports, raw
}

// TestBV235_ConfigOf0117LoadsUnchanged: a 0.11.7 configuration with dismissals
// loads in this version as it is, and reading it writes nothing.
func TestBV235_ConfigOf0117LoadsUnchanged(t *testing.T) {
	cfgPath, _, _, raw := bv235Config(t)
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("0.11.7 config: %v", err)
	}
	if len(cfg.AllowList.Dismissed) != 2 || len(cfg.AllowList.Accepted) != 0 {
		t.Errorf("dismissed %v accepted %v", cfg.AllowList.Dismissed, cfg.AllowList.Accepted)
	}
	if after, _ := os.ReadFile(cfgPath); string(after) != string(raw) {
		t.Error("loading the configuration changed it")
	}
}

// TestBV235_AcceptKeepsDismissed: accepting a finding keeps every other
// dismissal entry by entry, moves an accepted id out of dismissed in the same
// write, re-points the integrity reference, and an id is never in both lists.
func TestBV235_AcceptKeepsDismissed(t *testing.T) {
	cfgPath, stPath, reports, _ := bv235Config(t)
	accepted := []config.AllowEntryAccepted{{ID: "f_cccccccccccc", Date: "2026-11-14"}}
	if err := writeAccepted(cfgPath, reports, stPath, accepted); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	want := []config.AllowEntryDismissed{{ID: "f_aaaaaaaaaaaa", Date: "2026-11-10"}, {ID: "cred_bbbbbbbbbbbb", Date: "2026-11-12"}}
	if !reflect.DeepEqual(cfg.AllowList.Dismissed, want) || !reflect.DeepEqual(cfg.AllowList.Accepted, accepted) {
		t.Fatalf("after accepting: dismissed %v accepted %v", cfg.AllowList.Dismissed, cfg.AllowList.Accepted)
	}
	raw, _ := os.ReadFile(cfgPath)
	st, err := state.LoadState(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if st.Integrity.ConfigSHA256 != hash.Bytes(raw) {
		t.Error("config_sha256 not re-pointed")
	}

	// Accepting a dismissed id moves it: out of dismissed, into accepted.
	accepted = append(accepted, config.AllowEntryAccepted{ID: "f_aaaaaaaaaaaa", Date: "2026-11-14"})
	if err := writeAccepted(cfgPath, reports, stPath, accepted); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.AllowList.Dismissed, want[1:]) || len(cfg.AllowList.Accepted) != 2 {
		t.Fatalf("after moving: dismissed %v accepted %v", cfg.AllowList.Dismissed, cfg.AllowList.Accepted)
	}
	// And dismissing an accepted id moves it back.
	if err := writeDismissed(cfgPath, reports, stPath, want); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.AllowList.Dismissed, want) || !reflect.DeepEqual(cfg.AllowList.Accepted, accepted[:1]) {
		t.Fatalf("after dismissing again: dismissed %v accepted %v", cfg.AllowList.Dismissed, cfg.AllowList.Accepted)
	}

	// Both lists at once are refused by the writer and by the loader.
	if err := writeFindingLists(cfgPath, reports, stPath, want, []config.AllowEntryAccepted{{ID: want[0].ID, Date: "2026-11-14"}}); err == nil {
		t.Error("an id in both lists was written")
	}
	both := strings.Replace(string(raw), `"accepted": [`, `"accepted": [{"id": "f_aaaaaaaaaaaa", "date": "2026-11-14"}, `, 1)
	if both == string(raw) {
		t.Fatal("fixture: no accepted list to extend")
	}
	if err := os.WriteFile(cfgPath, []byte(both), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadConfig(cfgPath); err == nil {
		t.Error("a configuration with an id in both lists loaded")
	}
}

// TestBV235_AcceptNeverWritesOutside: «Lo asumo» never writes a configuration
// outside ~/.rosmarops (CTO-49 v2, invariant 2).
func TestBV235_AcceptNeverWritesOutside(t *testing.T) {
	_, stPath, reports, raw := bv235Config(t)
	outside := t.TempDir() + "/rosmarops.config.json"
	if err := os.WriteFile(outside, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAccepted(outside, reports, stPath, []config.AllowEntryAccepted{{ID: "f_cccccccccccc", Date: "2026-11-14"}}); err == nil {
		t.Error("wrote a configuration outside ~/.rosmarops")
	}
	if after, _ := os.ReadFile(outside); string(after) != string(raw) {
		t.Error("the outside configuration changed")
	}
}
