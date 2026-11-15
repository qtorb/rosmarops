package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-241 v2: Hermes's approvals do not cover file writes, so when another
// agent's instructions are the only write reaching Hermes's «changes other
// agents» cell, nobody decides it, in every approval mode, with no premise.
// (With security.protected_instruction_files on, the protected writes are
// not in the graph at all: TestPt6_HermesProtectedInstructions.)
func TestBV241_HermesChangesOtherAgentsNobodyInEveryMode(t *testing.T) {
	for _, mode := range []string{"manual", "smart", "off"} {
		cell := rules.HermesInstructionsChangesCell(mode)
		if cell.Decider != rules.DeciderNobody {
			t.Errorf("%s: decider %q, want %q", mode, cell.Decider, rules.DeciderNobody)
		}
		if cell.Scope != rules.ScopeInstructions {
			t.Errorf("%s: scope %q, want %q", mode, cell.Scope, rules.ScopeInstructions)
		}
		if cell.Assumed {
			t.Errorf("%s: assumed, want no premise", mode)
		}
	}
}
