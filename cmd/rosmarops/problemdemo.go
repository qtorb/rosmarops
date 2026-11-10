//go:build demoproblem

package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/qtorb/rosmarops/internal/problems"
)

// BV-210 test aid, built only into the test candidate (`-tags demoproblem`), so
// Albert can see Windows A and B without touching his real config (CTO-32 C5).
// It writes only to the problem notebook and the run mark in the reports data
// directory (allowed paths), never to any agent or RosmarOps config. The
// release binary compiles problemdemo_off.go instead and never reads the
// variable.

// seedDemoProblemB, when ROSMAROPS_DEMO_PROBLEM=b, writes a fake in-progress run
// mark so the next StartReview reports a previous unfinished review and Window B
// shows at startup.
func seedDemoProblemB(dataDir string, now time.Time) {
	if os.Getenv("ROSMAROPS_DEMO_PROBLEM") != "b" {
		return
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		slog.Warn("demo problem b: mkdir", "err", err)
		return
	}
	path := filepath.Join(dataDir, problems.RunMarkName)
	if err := os.WriteFile(path, []byte(problems.Day(now)+"\n"), 0o600); err != nil {
		slog.Warn("demo problem b: write mark", "err", err)
	}
}

// seedDemoProblemA, when ROSMAROPS_DEMO_PROBLEM=a, records one synthetic unsent
// problem so Window A shows at the end of this review.
func seedDemoProblemA(dataDir string, now time.Time) {
	if os.Getenv("ROSMAROPS_DEMO_PROBLEM") != "a" {
		return
	}
	recordProblems(dataDir, problems.Entry{Code: problems.StepFailed, Step: "mcp", Day: problems.Day(now)})
}
