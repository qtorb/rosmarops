package index

import (
	"reflect"
	"testing"

	"github.com/qtorb/rosmarops/internal/rules"
)

// Decision 2026-10-26 (agentes y MCP nuevos, A3): with Copilot present as an
// extension, the machine score is not available (agents not evaluated) and
// index_version is 4 (AP-10, BV-117/BV-121); the per-agent scores do not
// change, and against an index_version 2 entry the double calculation gives 0
// by method.
func TestExtensionsNotEvaluated(t *testing.T) {
	fs := []rules.Finding{
		fnd("f_r", rules.RuleCredReadAndSend, rules.SeverityRed, rules.ClassHigh, rules.RadiusSameProject),
		fnd("f_y", rules.RuleCredReadable, rules.SeverityYellow, rules.ClassHigh, rules.RadiusAnyProject),
	}
	without := Compute(agentInput(fs))
	in := agentInput(fs)
	in.Agents = append(in.Agents, Agent{ID: "agent_ext_x", Name: "GitHub Copilot", Product: "github-copilot"})
	with := Compute(in)
	if with.IndexVersion != 4 || with.Machine.Unavailable != ReasonAgentsNotEvaluated || without.Machine.Value == nil {
		t.Fatalf("machine %+v (version %d), without Copilot %+v", with.Machine, with.IndexVersion, without.Machine)
	}
	if !reflect.DeepEqual(with.Scope.NotEvaluated, []string{"GitHub Copilot"}) || *with.Agents[0].Value != *without.Agents[0].Value {
		t.Errorf("not evaluated %v, scores %d vs %d", with.Scope.NotEvaluated, *with.Agents[0].Value, *without.Agents[0].Value)
	}
	v := *with.Agents[0].Value
	in.Previous = &Entry{
		IndexVersion: 2, RulesVersion: "1", KnowledgeVersion: "k",
		Agents: []EntryAgent{{Agent: "Claude Code", Value: &v}}, Scope: EntryScope{Agents: []string{"Claude Code"}, Repos: 10},
	}
	c := Compute(in).Agents[0].Comparison
	if c.Status != CompareDirect || c.MethodDelta == nil || *c.MethodDelta != 0 || c.PrevIndexVersion != 2 {
		t.Errorf("comparison with v2: %+v", c)
	}
}

// A3: Cline as an ai_client and as an extension is one «detected, not
// evaluated» entry, joined by product code.
func TestNotEvaluatedJoinedByProduct(t *testing.T) {
	in := agentInput(nil)
	in.Agents = append(in.Agents,
		Agent{ID: "agent_client_cl", Name: "Cline", Product: "cline", ConfigDir: "/h/cline"},
		Agent{ID: "agent_ext_cl", Name: "Cline", Product: "cline"},
	)
	if got := Compute(in).Scope.NotEvaluated; !reflect.DeepEqual(got, []string{"Cline"}) {
		t.Errorf("not evaluated %v", got)
	}
}
