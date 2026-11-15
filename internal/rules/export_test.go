package rules

import (
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
)

// CellWay is a cell's winning way, code and capability (§4, annex 2 point 2).
func CellWay(c OverviewCell) (code, capID string) { return c.winCode, c.winCap }

// FindingWays are the ways a finding stands on, as code and capability.
func FindingWays(f Finding) map[[2]string]bool {
	out := map[[2]string]bool{}
	for _, leg := range f.legs {
		for _, w := range leg {
			out[[2]string{w.code, w.cap}] = true
		}
	}
	return out
}

// Stale is stale, for the 90-day test (§6).
func Stale(date string, now time.Time) bool { return stale(date, now) }

// HermesInstructionsChangesCell is Hermes Agent's «changes other agents»
// cell when its only write to another agent is that agent's instructions,
// under the approval mode mode (BV-241 v2): one Hermes node with its
// permission_mode capability, one writes_config_of edge to a Claude Code
// instructions resource, and nothing else competing for the cell.
func HermesInstructionsChangesCell(mode string) OverviewCell {
	hermes := graph.Node{ID: "agent:hermes", Kind: graph.KindAgent, Type: graph.AgentAIClient, Platform: "hermes"}
	instr := graph.Node{ID: "res:claude-agents-md", Type: graph.ResourceAgentInstructions, Configures: "agent:claude"}
	oi := overviewInputs{
		nodes:   map[string]graph.Node{hermes.ID: hermes, instr.ID: instr},
		foreign: map[string][]graph.Edge{hermes.ID: {{From: hermes.ID, To: instr.ID}}},
	}
	v := &agentView{node: hermes, inForce: []graph.Node{{Type: graph.CapPermissionMode, TargetScope: mode}}}
	return oi.hermesChangesCell(v)
}
