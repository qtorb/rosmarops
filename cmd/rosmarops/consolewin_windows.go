//go:build windows

package main

import (
	"github.com/qtorb/rosmarops/internal/problems"
	"github.com/qtorb/rosmarops/internal/report"
)

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
