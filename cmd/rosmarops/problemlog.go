package main

import (
	"log/slog"
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
		for _, sf := range g.Coverage.SourcesFailed {
			if !strings.HasPrefix(sf.Reason, graph.ReasonParseError+":") {
				continue
			}
			e := problems.Entry{Code: problems.ConfigUnreadable, Step: problems.StepAgents, Day: day}
			e.Agent = agentOfFile(g, sf.Path)
			out = append(out, e)
		}
	}

	return out
}

// agentOfFile is the product id of the agent whose configuration folder holds
// the file at path: an agent node's ConfigPath is its folder and its product is
// in Platform (its ID is a hash). The deepest folder wins; "" when no known
// agent's folder holds it.
func agentOfFile(g *graph.Graph, path string) string {
	file := filepath.Clean(path)
	best, bestLen := "", -1
	for _, n := range g.Nodes {
		if n.Kind != graph.KindAgent || n.ConfigPath == "" || !problems.ValidAgent(n.Platform) {
			continue
		}
		dir := filepath.Clean(n.ConfigPath)
		if (file == dir || strings.HasPrefix(file, dir+string(filepath.Separator))) && len(dir) > bestLen {
			best, bestLen = n.Platform, len(dir)
		}
	}
	return best
}

// today is this run's local day in the notebook's format.
func today(now time.Time) string { return problems.Day(now) }

// seedDemoProblemA and seedDemoProblemB are the BV-210 test aid. They read
// ROSMAROPS_DEMO_PROBLEM only when the binary is built with the `demoproblem`
// tag (the test candidate); the release binary compiles the no-op variants and
// never reads the variable (CTO-32 C5). See problemdemo.go / problemdemo_off.go.
