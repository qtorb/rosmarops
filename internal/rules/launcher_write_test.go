package rules_test

import (
	"fmt"
	"sort"
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
)

// writeFindings are Claude Code's own_* and foreign_* findings, as
// "rule target confidence", sorted, and whether every one carries
// code_launcher_rule.
func writeFindings(t *testing.T, perms platform.ClaudePermissions) ([]string, bool) {
	t.Helper()
	_, res := ovEvaluate(t, ovFixture{roots: allRoots, claude: []platform.ClaudeSettingsFile{userSettings(perms)}})
	var out []string
	launcher := true
	for _, f := range res.Findings {
		switch f.Rule {
		case rules.RuleForeignAgentConfigWritable, rules.RuleForeignAgentInstructionsWritable, rules.RuleOwnConfigWritable, rules.RuleOwnInstructionsWritable:
		default:
			continue
		}
		if f.Params.Agent != "Claude Code" {
			continue
		}
		out = append(out, fmt.Sprintf("%s %s %s %s", f.Rule, f.Params.TargetAgent, f.Params.ResourceType, f.Confidence))
		has := false
		for _, a := range f.Evidence.Assumptions {
			has = has || a == rules.AssumeCodeLauncherRule
		}
		launcher = launcher && has
	}
	sort.Strings(out)
	return out, launcher
}

// Decision 2026-10-26 (lanzadores en escritura, L1): with a launcher and no
// "shell *", the same own_* and foreign_* findings as with Bash, in default
// and in auto, each carrying code_launcher_rule; in default, confidence
// high (in auto the mode's leg lowers it the same with Bash, E4).
func TestLauncherWrites_SameAsShell(t *testing.T) {
	for _, mode := range []string{"default", ""} {
		got, launcher := writeFindings(t, platform.ClaudePermissions{DefaultMode: mode, Allow: []string{"Bash(npx tsx *)"}})
		want, _ := writeFindings(t, platform.ClaudePermissions{DefaultMode: mode, Allow: []string{"Bash"}})
		if len(got) != 3 || fmt.Sprint(got) != fmt.Sprint(want) || !launcher {
			t.Errorf("mode %q: launcher %v (code_launcher_rule %v), Bash %v", mode, got, launcher, want)
		}
		if mode == "default" {
			for _, f := range got {
				if f[len(f)-4:] != "high" {
					t.Errorf("default: %s", f)
				}
			}
		}
	}
	want := []string{
		"foreign_agent_config_writable Cursor agent_config high",
		"own_config_writable  agent_config high",
		"own_instructions_writable  agent_instructions high",
	}
	if got, _ := writeFindings(t, platform.ClaudePermissions{DefaultMode: "default", Allow: []string{"Bash(npx tsx *)"}}); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("default: %v, want %v", got, want)
	}
}

// A pre-approved command that is no launcher gives no write way.
func TestLauncherWrites_NotALauncher(t *testing.T) {
	if got, _ := writeFindings(t, platform.ClaudePermissions{DefaultMode: "default", Allow: []string{"Bash(npm test)", "Bash(git status)"}}); len(got) != 0 {
		t.Errorf("non-launchers write: %v", got)
	}
	_, res := ovEvaluate(t, ovFixture{roots: allRoots, claude: []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{
		DefaultMode: "default", Allow: []string{"Bash(npm test)"},
	})}})
	if c := ovCell(t, res, "Claude Code", rules.RowWritesFiles); c.Decider == rules.DeciderNobody {
		t.Errorf("writes_files %s with no launcher", c.Decider)
	}
}

// L3: the table inherits it through the probe: with a launcher,
// writes_files and changes_other_agents are nobody.
func TestLauncherWrites_Table(t *testing.T) {
	for _, mode := range []string{"default", ""} {
		_, res := ovEvaluate(t, ovFixture{roots: allRoots, claude: []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{
			DefaultMode: mode, Allow: []string{"Bash(npx tsx *)"},
		})}})
		for _, row := range []string{rules.RowWritesFiles, rules.RowChangesOtherAgents} {
			c := ovCell(t, res, "Claude Code", row)
			if c.Decider != rules.DeciderNobody || c.Origin != rules.OriginConfigured {
				t.Errorf("mode %q %s: %s/%s %s", mode, row, c.Decider, c.Scope, c.Origin)
			}
		}
		if c := ovCell(t, res, "Claude Code", rules.RowWritesFiles); c.Scope != rules.ScopeMachine {
			t.Errorf("mode %q writes_files scope %s", mode, c.Scope)
		}
	}
}
