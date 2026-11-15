package rules_test

import (
	"slices"
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-244 (CTO-54): Antigravity CLI changed its default agentMode to auto in
// 1.5.7 and RosmarOps cannot read the installed version. With agentMode not
// written, the mode stays request-review and the rows whose decider depends on
// that default (writing files, running commands, sending out) carry
// default_mode_depends_on_version, with the asterisk. With agentMode written
// (default or auto), or a level set by toolPermission, they do not.
func TestBV244_AntigravityDefaultModeDependsOnVersion(t *testing.T) {
	marked := []string{rules.RowWritesFiles, rules.RowRunsCommands, rules.RowSendsOut}
	others := []string{rules.RowReadsFiles, rules.RowChangesOtherAgents, rules.RowModelProvider}
	settings := func(agentMode, toolPermission string) platform.AntigravityScan {
		return platform.AntigravityScan{ConfigDir: agGemini, Present: true, Settings: platform.AntigravitySettings{
			Path: agSettings, Status: platform.MCPSourceRead, AgentMode: agentMode, ToolPermission: toolPermission,
		}}
	}
	has := func(c rules.OverviewCell) bool {
		return slices.Contains(c.Data.Assumptions, rules.AssumeDefaultModeDependsOnVersion)
	}

	// agentMode not written: the assumption and the asterisk on the three rows;
	// the deciders stay those of request-review.
	_, res, client := antigravityEvaluate(t, settings("", ""), nil, nil, nil, nil)
	for _, row := range marked {
		c := agCell(t, res, client, row)
		if !has(c) || !c.Assumed {
			t.Errorf("agentMode not written, %s: want %s and the asterisk, got %+v", row, rules.AssumeDefaultModeDependsOnVersion, c)
		}
	}
	for _, row := range others {
		if c := agCell(t, res, client, row); has(c) {
			t.Errorf("agentMode not written, %s: must not carry %s, got %+v", row, rules.AssumeDefaultModeDependsOnVersion, c)
		}
	}
	want := map[string]string{rules.RowWritesFiles: rules.DeciderNobody, rules.RowRunsCommands: rules.DeciderYou, rules.RowSendsOut: rules.DeciderYou}
	for row, dec := range want {
		if c := agCell(t, res, client, row); c.Decider != dec {
			t.Errorf("agentMode not written, %s: decider %q, want %q (request-review unchanged)", row, c.Decider, dec)
		}
	}

	// agentMode written (default or auto), or always-proceed: no assumption.
	for _, s := range []struct{ name, agentMode, toolPermission string }{
		{"agentMode default", "default", ""},
		{"agentMode auto", "auto", ""},
		{"toolPermission always-proceed", "", "always-proceed"},
	} {
		_, res, client := antigravityEvaluate(t, settings(s.agentMode, s.toolPermission), nil, nil, nil, nil)
		for _, row := range append(append([]string{}, marked...), others...) {
			if c := agCell(t, res, client, row); has(c) {
				t.Errorf("%s, %s: must not carry %s, got %+v", s.name, row, rules.AssumeDefaultModeDependsOnVersion, c)
			}
		}
	}
	// agentMode default: no asterisk on the three rows either.
	_, res, client = antigravityEvaluate(t, settings("default", ""), nil, nil, nil, nil)
	for _, row := range marked {
		if c := agCell(t, res, client, row); c.Assumed {
			t.Errorf("agentMode default, %s: no asterisk wanted, got %+v", row, c)
		}
	}
}
