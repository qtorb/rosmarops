package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/state"
)

// TestProblemsAskCommandUnticks is CTO-32 C4.3: `rosmarops problems ask` sets
// send_problems_without_asking to false, so the next review asks again.
func TestProblemsAskCommandUnticks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	stPath, err := state.StatePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(stPath), 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := state.DefaultState("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	st.SendProblemsWithoutAsking = true
	if err := state.SaveState(stPath, st); err != nil {
		t.Fatal(err)
	}

	if err := runProblemsAsk(cli.Invocation{Flags: map[string]string{"lang": "en"}}); err != nil {
		t.Fatalf("problems ask: %v", err)
	}
	got, err := state.LoadState(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.SendProblemsWithoutAsking {
		t.Error("problems ask must untick send_problems_without_asking")
	}
}
