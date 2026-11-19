package main

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
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

	got := problemsFromRun(day, scanErrors, repoFindings, g)

	want := map[problems.Code]int{}
	for _, e := range got {
		want[e.Code]++
		if e.Day == "" {
			t.Errorf("entry has empty day: %+v", e)
		}
	}
	// run_unfinished is recorded at startup, not here.
	if want[problems.RunUnfinished] != 0 {
		t.Errorf("run_unfinished must not come from problemsFromRun, got %d", want[problems.RunUnfinished])
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
}

func TestConfigUnreadableTakesAgentIDNeverPath(t *testing.T) {
	// A real agent node, as the graph builds it: its ID is a hash, its product
	// is in Platform and its ConfigPath is its folder, not the file.
	frag := graph.FromCursor(platform.CursorScan{
		Exists: true, ConfigDir: "/home/alice/.cursor",
		Files: []platform.CursorFile{{
			Path: "/home/alice/.cursor/permissions.json", Kind: platform.CursorPermissions, Scope: platform.CursorScopeUser,
			Status: platform.MCPSourceFailed, Reason: graph.Reason(graph.ReasonParseError, "/home/alice/.cursor/permissions.json: oops"),
		}},
	}, platform.ClaudeSettingsScan{}, time.Now())
	g := &graph.Graph{Nodes: frag.Nodes, Coverage: frag.Coverage}
	g.Coverage.SourcesFailed = append(g.Coverage.SourcesFailed,
		graph.SourceFailed{Path: "/home/bob/.secret/config.toml", Reason: graph.Reason(graph.ReasonParseError, "/home/bob/.secret/config.toml: oops")})
	if len(g.Nodes) == 0 || strings.Contains(g.Nodes[0].ID, "cursor") {
		t.Fatalf("not a real agent node: %+v", g.Nodes)
	}
	got := problemsFromRun("2026-11-09", nil, nil, g)

	var withAgent, withoutAgent int
	for _, e := range got {
		if e.Code != problems.ConfigUnreadable {
			continue
		}
		if e.Agent == "cursor" {
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
		t.Errorf("want 1 config_unreadable attributed to cursor, got %d (%+v)", withAgent, got)
	}
	if withoutAgent != 1 {
		t.Errorf("want 1 config_unreadable with no agent (unmatched path), got %d", withoutAgent)
	}
	// A folder that only shares a prefix with the agent's is not its folder.
	g.Coverage.SourcesFailed = []graph.SourceFailed{{Path: "/home/alice/.cursor-old/mcp.json", Reason: graph.Reason(graph.ReasonParseError, "x")}}
	if got := problemsFromRun("2026-11-09", nil, nil, g); len(got) != 1 || got[0].Agent != "" {
		t.Errorf("a sibling folder was taken for the agent's: %+v", got)
	}
}

// A config_unreadable problem with no known agent says «one of your agents»,
// never a placeholder; with one, the product's name.
func TestSummaryConfigUnreadableWithoutAgent(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		none := summaryOf(cat, problems.Entry{Code: problems.ConfigUnreadable})
		if none != cat.Text("problems.summary.config_unreadable.no_agent", nil) || strings.Contains(none, "{") {
			t.Errorf("%s: %q", lang, none)
		}
		named := summaryOf(cat, problems.Entry{Code: problems.ConfigUnreadable, Agent: "cursor"})
		if !strings.Contains(named, "Cursor") || strings.Contains(named, "{") {
			t.Errorf("%s: %q", lang, named)
		}
	}
}
