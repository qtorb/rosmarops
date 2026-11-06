package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/state"
)

func TestConnectOfferDue(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"never declined", "", true},
		{"unparseable", "not a time", true},
		{"declined 8 days ago", now.Add(-8 * 24 * time.Hour).Format(time.RFC3339), true},
		{"declined exactly 7 days ago", now.Add(-7 * 24 * time.Hour).Format(time.RFC3339), true},
		{"declined 6 days ago", now.Add(-6 * 24 * time.Hour).Format(time.RFC3339), false},
		{"declined just now", now.Format(time.RFC3339), false},
	}
	for _, c := range cases {
		if got := connectOfferDue(c.in, now); got != c.want {
			t.Errorf("%s: connectOfferDue(%q) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

// writeState writes a minimal valid state.v2 (not connected) and returns its
// path.
func writeState(t *testing.T, declinedAt string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	st := state.State{
		SchemaVersion:          2,
		EngineVersion:          "0.7.4",
		LastRun:                "2026-11-03T10:00:00Z",
		LocalSalt:              "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ConnectOfferDeclinedAt: declinedAt,
		Integrity: state.Integrity{
			VigilBinarySHA256: "0000000000000000000000000000000000000000000000000000000000000000",
			ConfigSHA256:      "0000000000000000000000000000000000000000000000000000000000000000",
		},
	}
	if err := state.SaveState(p, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	return p
}

// TestOfferConnect_PersistsDeclineAndGates: a decline is saved and, within 7
// days, the offer is not shown again; a connection failure saves nothing.
func TestOfferConnect_PersistsDeclineAndGates(t *testing.T) {
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

	// 1) Not connected, never declined, the person says «Not now»: the decline
	// is saved with now.
	stPath := writeState(t, "")
	paths := spacePaths{store: filepath.Join(t.TempDir(), "kit.none"), state: stPath, reports: t.TempDir()}
	calls := 0
	restore := connectOfferHook
	connectOfferHook = func(spacePaths, catalog.Catalog) connectResult { calls++; return connectDeclined }
	defer func() { connectOfferHook = restore }()

	offerConnectAfterReport(paths, cat, stPath, now)
	if calls != 1 {
		t.Fatalf("offer shown %d times, want 1", calls)
	}
	st, err := state.LoadState(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if st.ConnectOfferDeclinedAt != now.UTC().Format(time.RFC3339) {
		t.Errorf("decline not saved: %q", st.ConnectOfferDeclinedAt)
	}

	// 2) Within 7 days of that decline, the offer is not shown again.
	calls = 0
	offerConnectAfterReport(paths, cat, stPath, now.Add(3*24*time.Hour))
	if calls != 0 {
		t.Errorf("offer shown again within 7 days (%d times)", calls)
	}

	// 3) A connection failure saves no decline (so it will be offered again).
	stPath2 := writeState(t, "")
	paths2 := spacePaths{store: filepath.Join(t.TempDir(), "kit.none"), state: stPath2, reports: t.TempDir()}
	connectOfferHook = func(spacePaths, catalog.Catalog) connectResult { return connectFailed }
	offerConnectAfterReport(paths2, cat, stPath2, now)
	st2, err := state.LoadState(stPath2)
	if err != nil {
		t.Fatal(err)
	}
	if st2.ConnectOfferDeclinedAt != "" {
		t.Errorf("a failure saved a decline: %q", st2.ConnectOfferDeclinedAt)
	}
}
