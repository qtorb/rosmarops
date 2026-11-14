package index

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-235 R2 (CTO-48 §3.4): per group of the agent's findings, the score
// without the whole group, which is not the sum of the per-finding gains.
func TestGroupsWithout(t *testing.T) {
	fs := []rules.Finding{
		fnd("f1", rules.RuleCredReadAndSend, rules.SeverityRed, "high", rules.RadiusAnyProject),
		fnd("f2", rules.RuleCredReadable, rules.SeverityRed, "high", rules.RadiusAnyProject),
		fnd("f3", rules.RuleOwnInstructionsWritable, rules.SeverityYellow, "", ""),
	}
	idx := Compute(agentInput(fs))
	a := idx.Agents[0]
	if len(a.Groups) != 2 || a.Groups[0].Group != GroupKeys || a.Groups[1].Group != GroupAgentInstructions {
		t.Fatalf("groups %+v, want keys then agent_instructions", a.Groups)
	}
	if got := a.Groups[0].FindingIDs; len(got) != 2 || got[0] != "f1" || got[1] != "f2" {
		t.Errorf("keys finding ids %v", got)
	}
	// Without the keys group only the yellow finding is left.
	want, _ := score([]item{{"f3", rules.SeverityYellow, weightOf(fs[2])}}, 0)
	if a.Groups[0].Without != want {
		t.Errorf("without keys = %d, want %d", a.Groups[0].Without, want)
	}
	// Each red alone leaves the other red: the group gain is not the sum.
	for _, w := range a.Weights[:2] {
		if w.Without >= 50 {
			t.Errorf("without one red only (%s) = %d, still red expected", w.FindingID, w.Without)
		}
	}
	if a.Groups[0].Without < 50 {
		t.Errorf("without the whole keys group = %d, want out of the red band", a.Groups[0].Without)
	}
	for _, r := range rules.AllRules {
		if GroupOf(r) == "" {
			t.Errorf("rule %s is in no group", r)
		}
	}
}
