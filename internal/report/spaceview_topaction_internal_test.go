package report

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-203 point 9: svTopAction resolves today's agent from the finding, not from
// v.Today.ActionID (a content hash that does not identify an agent). With two
// scored agents whose today's action has the same content (so the same
// ActionID), the space must show the before/after of the agent today's finding
// belongs to — the same figure the report shows — not the other agent's.
func TestSvTopActionResolvesAgentByFinding(t *testing.T) {
	val := func(n int) *int { return &n }

	agentA := index.AgentIndex{Agent: "Agent A", AgentID: "a_A"}
	agentA.Value = val(21)
	agentA.Projection = &index.Projection{ActionID: "a_shared", Value: 25}

	agentB := index.AgentIndex{Agent: "Agent B", AgentID: "a_B"}
	agentB.Value = val(8)
	agentB.Projection = &index.Projection{ActionID: "a_shared", Value: 9}

	// Today's finding belongs to agent B (its first evidence node is a_B).
	v := V2{
		Today:    rules.Today{FindingID: "f_today", ActionID: "a_shared"},
		Findings: []rules.Finding{{ID: "f_today", Evidence: rules.Evidence{Nodes: []string{"a_B"}}}},
		Index:    index.Index{Agents: []index.AgentIndex{agentA, agentB}},
	}

	got := svTopAction(v)
	if got == nil {
		t.Fatal("svTopAction returned nil")
	}
	// Matching by ActionID returns agent A (first with Projection.ActionID ==
	// a_shared): 21->25. Resolving by the finding returns agent B: 8->9.
	if got.ScoreFrom != 8 || got.ScoreTo != 9 {
		t.Errorf("svTopAction = %d->%d, want 8->9 (agent B, today's finding's agent)", got.ScoreFrom, got.ScoreTo)
	}

	// The report resolves the same way (html.go agentScore): the agent of
	// today's finding is its first evidence node. The space and the report
	// therefore show the same two figures.
	reportAgent := agentB // Evidence.Nodes[0] == a_B
	if got.ScoreFrom != *reportAgent.Value || got.ScoreTo != reportAgent.Projection.Value {
		t.Errorf("space figures %d->%d differ from the report's %d->%d",
			got.ScoreFrom, got.ScoreTo, *reportAgent.Value, reportAgent.Projection.Value)
	}
}

// TestSvTopActionMachineFinding: a machine finding (cred_in_git) is in every
// agent's score; the space shows the lowest-scored agent's, as the report does.
func TestSvTopActionMachineFinding(t *testing.T) {
	val := func(n int) *int { return &n }
	a := index.AgentIndex{Agent: "Agent A", AgentID: "a_A"}
	a.Value = val(40)
	a.Projection = &index.Projection{ActionID: "a_rotate", Value: 55}
	v := V2{
		Today:    rules.Today{FindingID: "f_git", ActionID: "a_rotate"},
		Findings: []rules.Finding{{ID: "f_git", Rule: rules.RuleCredInGit}},
		Index:    index.Index{Agents: []index.AgentIndex{a}},
	}
	got := svTopAction(v)
	if got == nil || got.ScoreFrom != 40 || got.ScoreTo != 55 {
		t.Fatalf("machine-finding top action = %+v, want 40->55", got)
	}
}

// TestSvMachineFindingUsesLowestScoredAgent is Albert's real case (CTO-30 A1/A2).
// A cred_in_git key in git is readable by five agents, so the finding is in
// every agent's score. The index lists them in the order the engine produced —
// Codex 21 first, Claude Code 8 third — not sorted. The report resolves a
// machine finding to the lowest-scored agent (Claude Code, 8), ties by name;
// the space view must show the same agent and the same figures, both in the
// top action (the space figure) and in findings[] (score_after_fix).
//
// Before A1, svAgentOfToday took the first agent in index order with a score
// (Codex, 21->25) and findingWeightAndAfter's predecessor took the last agent's
// row, so the space said «21->25» / the wrong score_after_fix where the report
// said «8->9». This test fails at 5d4e5e0 and passes with A1.
func TestSvMachineFindingUsesLowestScoredAgent(t *testing.T) {
	val := func(n int) *int { return &n }
	// Every agent carries the machine finding in its weights (its score without
	// the key) and a projection for today's rotate action.
	mk := func(name, id string, score, without, proj int) index.AgentIndex {
		return index.AgentIndex{
			Agent: name, AgentID: id, Score: index.Score{Value: val(score)},
			Weights:    []index.Weight{{FindingID: "f_git", Weight: 4, Without: without}},
			Projection: &index.Projection{ActionID: "a_rotate", Value: proj},
		}
	}
	// Index order as the engine emitted it — NOT sorted by score.
	agents := []index.AgentIndex{
		mk("Codex", "a_codex", 21, 25, 25),
		mk("Cline", "a_cline", 24, 28, 28),
		mk("Claude Code", "a_claude", 8, 9, 9), // the lowest: the report's agent
		mk("Cursor", "a_cursor", 31, 33, 33),
		mk("Gemini CLI", "a_gemini", 76, 80, 80),
	}
	v := V2{
		Today:    rules.Today{FindingID: "f_git", ActionID: "a_rotate"},
		Findings: []rules.Finding{{ID: "f_git", Rule: rules.RuleCredInGit}},
		Index:    index.Index{Agents: agents},
	}

	// The report's single resolution function: the lowest-scored agent.
	reportAgent, ok := agentOfFinding(v, v.Findings[0])
	if !ok || reportAgent.Agent != "Claude Code" {
		t.Fatalf("report resolves the machine finding to %v, want Claude Code", reportAgent)
	}

	// 1) The space figure (top action) equals the report's: 8->9.
	top := svTopAction(v)
	if top == nil {
		t.Fatal("svTopAction returned nil")
	}
	if top.ScoreFrom != 8 || top.ScoreTo != 9 {
		t.Errorf("space top action = %d->%d, want 8->9 (Claude Code, the lowest-scored agent)", top.ScoreFrom, top.ScoreTo)
	}
	if top.ScoreFrom != *reportAgent.Value || top.ScoreTo != reportAgent.Projection.Value {
		t.Errorf("space top action %d->%d differs from the report's %d->%d",
			top.ScoreFrom, top.ScoreTo, *reportAgent.Value, reportAgent.Projection.Value)
	}

	// 2) The findings[] figure (score_after_fix) equals the report agent's: 9.
	_, after := findingWeightAndAfter(v, v.Findings[0])
	if after == nil {
		t.Fatal("findings[] score_after_fix is nil")
	}
	if *after != 9 {
		t.Errorf("findings[] score_after_fix = %d, want 9 (Claude Code's score without the key)", *after)
	}
	want := reportAgent.Weights[0].Without
	if *after != want {
		t.Errorf("findings[] score_after_fix %d differs from the report agent's %d", *after, want)
	}
}
