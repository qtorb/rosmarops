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
// agent's score; the space shows the first scored agent's, as the report does.
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
