package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/qtorb/rosmarops/internal/agenthooks"
	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

// RosmarOps inside Claude Code (BV-226, CTO-44 §4a-§4b): two commands Claude
// Code runs, never a person, so both are hidden from the help. They check
// integrity like every command, but on a mismatch they never ask and never
// write: they print one line and exit 0 (invariant 8). They never use the
// network nor start a process.

func newSessionCheckCmd() *cli.Command {
	return &cli.Command{Name: "session-check", Hidden: true, Args: 0, Run: runSessionCheck}
}

func newStatuslineCmd() *cli.Command {
	return &cli.Command{Name: "statusline", Hidden: true, Args: 0, Run: runStatusline}
}

// sessionBudget is session-check's own limit (CTO-44 §4a): past it, it says
// and writes nothing. A variable for the tests.
var sessionBudget = time.Second

// sessionInput and sessionOutput are the hook's streams; tests replace them.
var (
	sessionInput  io.Reader = os.Stdin
	sessionOutput io.Writer = os.Stdout
)

// quietIntegrity is invariant 8 for session-check and statusline: the
// comparison every command makes, of the binary and of the configuration
// with the state's references, but it never asks, never writes and never
// aborts. false on a mismatch; true when there is no reference to compare
// with (the next `rosmarops run` handles that, as today).
func quietIntegrity() bool {
	stPath, err := state.StatePath()
	if err != nil {
		return true
	}
	st, err := state.LoadState(stPath)
	if err != nil {
		return true
	}
	if ref := st.Integrity.VigilBinarySHA256; ref != "" && ref != zeroHash {
		self, err := hash.Self()
		if err != nil || self != ref {
			return false
		}
	}
	if ref := st.Integrity.ConfigSHA256; ref != "" && ref != zeroHash {
		if p, err := resolveCfgPath(); err == nil {
			if h, err := hash.File(p); err != nil || h != ref {
				return false
			}
		}
	}
	return true
}

// hooksDataDir is the reports' data folder: the configured report_directory
// (read from the configuration file as is, without the full load), or the
// default one.
func hooksDataDir() (string, error) {
	if p, err := config.ResolveConfigPath(); err == nil {
		if b, err := os.ReadFile(p); err == nil {
			var c struct {
				ReportDirectory string `json:"report_directory"`
			}
			if json.Unmarshal(b, &c) == nil && c.ReportDirectory != "" {
				return report.DataDir(c.ReportDirectory), nil
			}
		}
	}
	dir, err := report.DefaultReportDir()
	if err != nil {
		return "", err
	}
	return report.DataDir(dir), nil
}

// sessionResult is the work session-check did, not yet acted on: the message
// to show and the snapshot write, both left to runSessionCheck so that
// nothing is said or written past the budget.
type sessionResult struct {
	message string
	write   func() (bool, error)
}

// runSessionCheck is `rosmarops session-check` (BV-226 §1). It always exits
// 0 and its only output is one systemMessage JSON object, or nothing.
func runSessionCheck(cli.Invocation) error {
	deadline := time.Now().Add(sessionBudget)
	done := make(chan sessionResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- sessionResult{}
			}
		}()
		done <- sessionWork(sessionInput)
	}()
	var res sessionResult
	select {
	case res = <-done:
	case <-time.After(time.Until(deadline)):
		return nil // over budget: say nothing, write nothing
	}
	if time.Now().After(deadline) {
		return nil
	}
	if res.write != nil {
		if _, err := res.write(); err != nil {
			slog.Debug("session-check: snapshot not written", "err", err)
		}
	}
	if res.message != "" {
		_ = agenthooks.Output(sessionOutput, res.message)
	}
	return nil
}

// sessionWork reads the hook input and does everything but act: a non-Claude
// input or any failure is an empty result.
func sessionWork(in io.Reader) sessionResult {
	raw, err := io.ReadAll(io.LimitReader(in, 1<<16))
	if err != nil {
		return sessionResult{}
	}
	hi, ok := agenthooks.ParseInput(raw)
	if !ok {
		return sessionResult{}
	}
	cat, err := catalog.Load(systemLang())
	if err != nil {
		cat, _ = catalog.Load(catalog.DefaultLang)
	}
	if !quietIntegrity() {
		return sessionResult{message: cat.Text("session.integrity_changed", nil)}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return sessionResult{}
	}
	data, err := hooksDataDir()
	if err != nil {
		return sessionResult{}
	}
	items := agenthooks.Collect(agenthooks.DefaultPaths(home, hi.Cwd))
	snapPath := agenthooks.SnapshotPath(data, hi.Cwd)
	prev, seen := agenthooks.ReadSnapshot(snapPath)
	tell := agenthooks.Tell(items, prev, !seen)
	snap := agenthooks.SnapshotOf(items)
	return sessionResult{
		message: agenthooks.Message(cat, tell, !seen),
		write:   func() (bool, error) { return agenthooks.WriteSnapshot(snapPath, snap) },
	}
}

// statuslineNow is the clock statusline reads; tests replace it.
var statuslineNow = time.Now

// runStatusline is `rosmarops statusline` (BV-226 §2): it checks integrity,
// reads data/statusline.txt and prints the line in the program's language.
// It reads and writes nothing else, and always exits 0.
func runStatusline(cli.Invocation) error {
	cat, err := catalog.Load(systemLang())
	if err != nil {
		cat, _ = catalog.Load(catalog.DefaultLang)
	}
	if !quietIntegrity() {
		fmt.Fprintln(sessionOutput, cat.Text("statusline.integrity_changed", nil))
		return nil
	}
	data, err := hooksDataDir()
	if err != nil {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(data, agenthooks.StatuslineFile))
	if err != nil {
		return nil
	}
	if line := agenthooks.ShowStatusline(raw, cat.Lang(), statuslineNow(), time.Local, cat); line != "" {
		fmt.Fprintln(sessionOutput, line)
	}
	return nil
}

// writeRunStatusline leaves a finished run's line for statusline (BV-226 §2):
// the red and yellow rows as the report counts them, and the machine's score
// (or, without one, the lowest agent score, the report's step 1). Without any
// score there is no line to give, and the old one is removed.
func writeRunStatusline(dataDir string, v report.V2, checked time.Time) {
	path := filepath.Join(dataDir, agenthooks.StatuslineFile)
	score, ok := statuslineScore(v)
	if !ok {
		_ = os.Remove(path)
		return
	}
	red, yellow := report.RowCounts(v)
	lines := map[string]string{}
	for _, l := range catalog.Langs() {
		if c, err := catalog.Load(l); err == nil {
			lines[l] = agenthooks.StatuslineText(c, red, yellow, score)
		}
	}
	if err := agenthooks.WriteStatusline(dataDir, checked, lines); err != nil {
		slog.Warn("statusline not written", "err", err)
	}
}

// writeAbortedStatusline leaves statusline.aborted: the last check did not
// finish.
func writeAbortedStatusline(reportDir string, checked time.Time) {
	lines := map[string]string{}
	for _, l := range catalog.Langs() {
		if c, err := catalog.Load(l); err == nil {
			lines[l] = c.Text("statusline.aborted", nil)
		}
	}
	if err := agenthooks.WriteStatusline(report.DataDir(reportDir), checked, lines); err != nil {
		slog.Warn("statusline not written", "err", err)
	}
}

func statuslineScore(v report.V2) (int, bool) {
	if m := v.Index.Machine.Value; m != nil {
		return *m, true
	}
	best, ok := 0, false
	for _, a := range v.Index.Agents {
		if a.Value != nil && (!ok || *a.Value < best) {
			best, ok = *a.Value, true
		}
	}
	return best, ok
}
