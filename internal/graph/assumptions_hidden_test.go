package graph

import (
	"sort"
	"strings"
	"testing"
)

// TestHiddenAtRestSet pins the «not visible at rest» assumption set to the
// signed list of CTO-26 §5, and checks the deliberately excluded assumptions
// are not in it.
func TestHiddenAtRestSet(t *testing.T) {
	want := []string{
		"agent_model_judges_command_safety",
		"auto_classifier_permits",
		"auto_mode_available",
		"aux_model_permits",
		"config_file_protection_scope_unknown",
		"ide_run_mode_not_readable",
		"launch_flags_not_readable",
		"reviewer_model_permits",
		"sandbox_platform_unknown",
		"team_policy_unknown",
		"ui_mode_choice_unknown",
		"windows_sandbox_unknown",
	}
	var got []string
	for k, v := range hiddenAtRest {
		if v {
			got = append(got, k)
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("hidden-at-rest set = %v, want %v", got, want)
	}
	for _, excluded := range []string{
		AssumeProcessRunsAsUser, AssumeVersionAsDocumented, AssumeBashToolAvailable, AssumeNoPreToolUseHookBlocks,
	} {
		if AssumptionHiddenAtRest(excluded) {
			t.Errorf("%s must not be hidden-at-rest (every agent / computer-dependent)", excluded)
		}
	}
}
