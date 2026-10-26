package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/state"
)

// slowAnswer is a person who answers «y» after delay, and notes when.
type slowAnswer struct {
	delay    time.Duration
	answered time.Time
	done     bool
}

func (s *slowAnswer) Read(p []byte) (int, error) {
	if s.done {
		return 0, nil
	}
	time.Sleep(s.delay)
	s.answered, s.done = time.Now(), true
	return copy(p, "y\n"), nil
}

// J13 (decision 2026-10-26, integridad y revisión semanal, §7): the
// integrity question comes before the scan's context, so an answer slower
// than the global cap does not consume it: the scan finishes and there is a
// graph. The cap is shrunk to budget and the answer takes longer.
func TestIntegrityAnswerOutsideScanBudget(t *testing.T) {
	const budget = 2 * time.Second
	root := t.TempDir()
	home := filepath.Join(root, "home")
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		dir := filepath.Join(root, strings.ToLower(name))
		if name == "HOME" || name == "USERPROFILE" {
			dir = home
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(name, dir)
	}
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)

	// A configuration accepted by init and a binary reference that is not
	// this binary's: the run asks whether you installed this version.
	dir := filepath.Join(home, ".rosmarops")
	reports := filepath.Join(dir, "reports")
	cfgPath, stPath := filepath.Join(dir, "config.json"), filepath.Join(dir, "state.json")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteConfig(cfgPath, config.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(cfgPath)
	st, err := state.DefaultState("0.0.9")
	if err != nil {
		t.Fatal(err)
	}
	st.Integrity.VigilBinarySHA256 = strings.Repeat("a", 64)
	st.Integrity.ConfigSHA256 = hash.Bytes(raw)
	if err := state.SaveState(stPath, st); err != nil {
		t.Fatal(err)
	}
	if err := writeAcceptedCopy(reports, raw); err != nil {
		t.Fatal(err)
	}

	answer := &slowAnswer{delay: budget + budget/2}
	var created time.Time
	prevIn, prevAsk, prevCtx := runInput, runCanAsk, newScanContext
	t.Cleanup(func() { runInput, runCanAsk, newScanContext = prevIn, prevAsk, prevCtx })
	runInput, runCanAsk = answer, func() bool { return true }
	newScanContext = func() (context.Context, context.CancelFunc) {
		created = time.Now()
		return context.WithTimeout(context.Background(), budget)
	}

	if err := runRun(cli.Invocation{Path: []string{"run"}, Flags: map[string]string{"lang": "en"}}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !answer.done || created.IsZero() || created.Before(answer.answered) {
		t.Errorf("the scan's context was created at %v, before the answer at %v", created, answer.answered)
	}
	var v2 struct {
		Coverage struct {
			GraphBuilt bool     `json:"graph_built"`
			ScanErrors []string `json:"scan_errors"`
		} `json:"coverage"`
	}
	b, err := os.ReadFile(filepath.Join(reports, "report.v2.json"))
	if err == nil {
		err = json.Unmarshal(b, &v2)
	}
	if err != nil {
		t.Fatal(err)
	}
	if !v2.Coverage.GraphBuilt {
		t.Errorf("no graph: %v", v2.Coverage.ScanErrors)
	}
	for _, e := range v2.Coverage.ScanErrors {
		if strings.Contains(e, "global_timeout") {
			t.Errorf("the answer consumed the scan's budget: %s", e)
		}
	}
	if _, err := os.Stat(filepath.Join(reports, "graph.v2.json")); err != nil {
		t.Error(err)
	}
	if saved, _ := state.LoadState(stPath); saved.Integrity.VigilBinarySHA256 == strings.Repeat("a", 64) {
		t.Error("the accepted binary was not recorded")
	}
}
