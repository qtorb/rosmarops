package main

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/problems"
	"github.com/qtorb/rosmarops/internal/scan"
)

func TestProblemsFromRunMapsEveryCode(t *testing.T) {
	const day = "2026-11-09"
	scanErrors := []finding.ScanError{
		{Scope: "mcp", Message: panicDiscardMessage},               // step_failed
		{Scope: scan.GlobalTimeoutScope, Message: "repos: ..."},    // time_ran_out (whole)
		{Scope: "intel", Message: "intel source failed to decode"}, // NOT step_failed (no discard)
	}
	repoFindings := []finding.Finding{
		{Category: "scan_timeout"}, // time_ran_out (per repo)
		{Category: "secret_in_repo"},
	}
	g := &graph.Graph{
		Nodes: []graph.Node{
			{ID: "cline", Kind: graph.KindAgent, ConfigPath: "/home/alice/.cline/config.json"},
		},
		Coverage: graph.Coverage{
			SourcesFailed: []graph.SourceFailed{
				{Path: "/home/alice/.cline/config.json", Reason: graph.Reason(graph.ReasonParseError, "/home/alice/.cline/config.json: invalid character")},
				{Path: "/home/alice/.codex/config.toml", Reason: graph.Reason(graph.ReasonParseError, "/home/alice/.codex/config.toml: bad toml")},
				{Path: "/home/alice/.mcp.json", Reason: graph.Reason(graph.ReasonReadError, "permission denied")}, // not a parse error
			},
		},
	}

	got := problemsFromRun(day, "2026-11-02", scanErrors, repoFindings, g)

	want := map[problems.Code]int{}
	for _, e := range got {
		want[e.Code]++
		if e.Day == "" {
			t.Errorf("entry has empty day: %+v", e)
		}
	}
	if want[problems.RunUnfinished] != 1 {
		t.Errorf("run_unfinished: want 1, got %d", want[problems.RunUnfinished])
	}
	if want[problems.StepFailed] != 1 {
		t.Errorf("step_failed: want 1 (only the discard panic), got %d", want[problems.StepFailed])
	}
	if want[problems.TimeRanOut] != 2 {
		t.Errorf("time_ran_out: want 2 (global + per-repo), got %d", want[problems.TimeRanOut])
	}
	if want[problems.ConfigUnreadable] != 2 {
		t.Errorf("config_unreadable: want 2 parse errors, got %d", want[problems.ConfigUnreadable])
	}

	// run_unfinished carries the previous day, not today's.
	for _, e := range got {
		if e.Code == problems.RunUnfinished && e.Day != "2026-11-02" {
			t.Errorf("run_unfinished day = %q, want 2026-11-02", e.Day)
		}
	}
}

func TestConfigUnreadableTakesAgentIDNeverPath(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{ID: "cline", Kind: graph.KindAgent, ConfigPath: "/home/alice/.cline/config.json"},
		},
		Coverage: graph.Coverage{
			SourcesFailed: []graph.SourceFailed{
				{Path: "/home/alice/.cline/config.json", Reason: graph.Reason(graph.ReasonParseError, "/home/alice/.cline/config.json: oops")},
				{Path: "/home/bob/.secret/config.toml", Reason: graph.Reason(graph.ReasonParseError, "/home/bob/.secret/config.toml: oops")},
			},
		},
	}
	got := problemsFromRun("2026-11-09", "", nil, nil, g)

	var withAgent, withoutAgent int
	for _, e := range got {
		if e.Code != problems.ConfigUnreadable {
			continue
		}
		if e.Agent == "cline" {
			withAgent++
		}
		if e.Agent == "" {
			withoutAgent++
		}
		// No field may contain a path, a path separator, or a username.
		for _, v := range []string{string(e.Code), e.Step, e.Agent} {
			if strings.ContainsAny(v, "/\\") || strings.Contains(v, "alice") || strings.Contains(v, "bob") || strings.Contains(v, ".json") || strings.Contains(v, ".toml") {
				t.Fatalf("problem entry leaks path/name: %q in %+v", v, e)
			}
		}
	}
	if withAgent != 1 {
		t.Errorf("want 1 config_unreadable attributed to cline, got %d", withAgent)
	}
	if withoutAgent != 1 {
		t.Errorf("want 1 config_unreadable with no agent (unmatched path), got %d", withoutAgent)
	}
}
