//go:build windows

package main

import (
	"os"

	"github.com/qtorb/rosmarops/internal/problems"
	"github.com/qtorb/rosmarops/internal/report"
)

// BV-211b: the WinForms scripts are too large to pass on powershell.exe's
// command line (the dismiss script alone is over Windows' ~32 KB limit, so
// CreateProcess fails with «The filename or extension is too long»). Each
// window now writes its script to a short-lived .ps1 in <reports>/data and runs
// it with -File, so the command line is a fixed, tiny size that never grows
// with the number of findings. The window's data still goes down standard
// input as before (so the connect window keeps its live input protocol). The
// file is removed when the window closes.

// windowScriptFile writes script to a uniquely named .ps1 in <reports>/data and
// returns its path, a cleanup that removes it, and whether it succeeded. The
// data directory is an allowed write target (invariant 2); nothing is written
// outside ~/.rosmarops.
func windowScriptFile(script string) (path string, cleanup func(), ok bool) {
	noop := func() {}
	dir, err := report.DefaultReportDir()
	if err != nil {
		return "", noop, false
	}
	dir = report.DataDir(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", noop, false
	}
	f, err := os.CreateTemp(dir, "rosmarops-window-*.ps1")
	if err != nil {
		return "", noop, false
	}
	name := f.Name()
	if _, err := f.WriteString(script); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", noop, false
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", noop, false
	}
	return name, func() { _ = os.Remove(name) }, true
}

// windowArgs are powershell.exe's arguments to run a window's script from a
// file (BV-211b): a fixed, small command line. -ExecutionPolicy Bypass keeps a
// restricted machine policy from blocking the local script, as the old
// -Command invocation was never subject to policy.
func windowArgs(path string) []string {
	return []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-STA", "-File", path}
}

// recordWindowFailure notes, in the problem notebook, that a window could not be
// opened (BV-211b §2), so the next review's problems window surfaces it. It is a
// step_failed in the notify step — both from the closed lists; no new code or
// step. Best effort and local.
func recordWindowFailure() {
	dir, err := report.DefaultReportDir()
	if err != nil {
		return
	}
	_, _ = problems.New(report.DataDir(dir)).Record(problems.Entry{
		Code: problems.StepFailed,
		Step: problems.StepNotify,
	})
}
