package main

import (
	"log/slog"
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
// run_unfinished (from the run mark), step_failed (discarded panic),
// time_ran_out (global timeout and per-repo scan_timeout), and config_unreadable
// (an agent config that did not parse). space_rejected and output_invalid are
// recorded at the send path, not here. day is this run's local day;
// prevUnfinishedDay, when non-empty, is the day a previous review was found
// unfinished at startup.
func problemsFromRun(day, prevUnfinishedDay string, scanErrors []finding.ScanError, findings []finding.Finding, g *graph.Graph) []problems.Entry {
	var out []problems.Entry

	if prevUnfinishedDay != "" {
		out = append(out, problems.Entry{Code: problems.RunUnfinished, Step: problems.StepReview, Day: prevUnfinishedDay})
	}

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
