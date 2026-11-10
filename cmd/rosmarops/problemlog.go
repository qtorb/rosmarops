package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/problems"
	"github.com/qtorb/rosmarops/internal/scan"
)

// recordProblems appends the given problems to the local notebook in dataDir.
// Best effort and local (no network): a failure is logged without any value and
// never stops the review.
func recordProblems(dataDir string, entries ...problems.Entry) {
	if len(entries) == 0 {
		return
	}
	if _, err := problems.New(dataDir).Record(entries...); err != nil {
		slog.Warn("problem notebook: record failed", "err", err)
	}
}

// panicDiscardMessage is the ScanError message RecoverStep uses for a step that
// panicked and had its results discarded (internal/scan/runner.go). A
// step_failed problem is recorded only for that case, not for every scan error.
const panicDiscardMessage = "panicked; its results were discarded"

// problemsFromRun turns this review's outcome into notebook entries (BV-210 §1):
// step_failed (discarded panic), time_ran_out (global timeout and per-repo
// scan_timeout), and config_unreadable (an agent config that did not parse).
// run_unfinished is recorded at startup (from the run mark) so Window B can ask
// before the scan; space_rejected and output_invalid are recorded at the send
// path. day is this run's local day.
func problemsFromRun(day string, scanErrors []finding.ScanError, findings []finding.Finding, g *graph.Graph) []problems.Entry {
	var out []problems.Entry

	for _, se := range scanErrors {
		switch {
		case se.Scope == scan.GlobalTimeoutScope:
			// The whole review ran out of time.
			out = append(out, problems.Entry{Code: problems.TimeRanOut, Step: problems.StepReview, Day: day})
		case se.Message == panicDiscardMessage && problems.ValidStep(se.Scope):
			// A part of the review panicked and its results were discarded.
			out = append(out, problems.Entry{Code: problems.StepFailed, Step: se.Scope, Day: day})
		}
	}

	// Per-repo time-outs are yellow findings, not scan errors.
	for _, f := range findings {
		if f.Category == "scan_timeout" {
			out = append(out, problems.Entry{Code: problems.TimeRanOut, Step: "repos", Day: day})
		}
	}

	// A known-format agent config that failed to parse (coverage parse_error).
	// Only the code and the agent's product id are taken — never the path or the
	// parser detail (S2 of BV-202).
	if g != nil {
		agentByPath := map[string]string{}
		for _, n := range g.Nodes {
			if n.Kind == graph.KindAgent && n.ConfigPath != "" && problems.ValidAgent(n.ID) {
				agentByPath[n.ConfigPath] = n.ID
			}
		}
		for _, sf := range g.Coverage.SourcesFailed {
			if !strings.HasPrefix(sf.Reason, graph.ReasonParseError+":") {
				continue
			}
			e := problems.Entry{Code: problems.ConfigUnreadable, Step: problems.StepAgents, Day: day}
			if id, ok := agentByPath[sf.Path]; ok {
				e.Agent = id
			}
			out = append(out, e)
		}
	}

	return out
}

// today is this run's local day in the notebook's format.
func today(now time.Time) string { return problems.Day(now) }

// seedDemoProblemB, when ROSMAROPS_DEMO_PROBLEM=b, writes a fake in-progress run
// mark so the next StartReview reports a previous unfinished review and Window B
// shows at startup. It touches only the run mark in the reports data directory
// (an allowed path), never any real config. A no-op otherwise.
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
// problem so Window A shows at the end of this review. It writes only to the
// problem notebook (an allowed path), never any real config. A no-op otherwise.
func seedDemoProblemA(dataDir string, now time.Time) {
	if os.Getenv("ROSMAROPS_DEMO_PROBLEM") != "a" {
		return
	}
	recordProblems(dataDir, problems.Entry{Code: problems.StepFailed, Step: "mcp", Day: problems.Day(now)})
}
