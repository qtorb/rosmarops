package graph

// BV-208 / CTO-26 §5 (opacity axis): the «not visible at rest» attribute of an
// assumption. An assumption is hidden-at-rest when whether it holds cannot be
// read from the configuration on disk — it depends on the server, the account,
// a launch flag, a UI choice or a model's judgement. The opacity axis counts how
// many distinct hidden-at-rest assumptions a scored agent's edges and in-force
// capabilities carry.
//
// Deliberately excluded (CTO-26 §5):
//   - process_runs_as_user and version_behaves_as_documented: every agent carries
//     them, so they do not distinguish;
//   - bash_tool_available and no_pretooluse_hook_blocks: they depend on the
//     computer, not the product.
//
// This is the signed list of §5, as an attribute of the assumption in code (not
// a document). assumptions_hidden_test.go pins the exact set.
var hiddenAtRest = map[string]bool{
	AssumeLaunchFlagsNotReadable:      true, // launch_flags_not_readable
	AssumeUIModeChoiceUnknown:         true, // ui_mode_choice_unknown
	AssumeIDERunModeNotReadable:       true, // ide_run_mode_not_readable
	AssumeTeamPolicyUnknown:           true, // team_policy_unknown
	AssumeAutoClassifierPermits:       true, // auto_classifier_permits
	AssumeReviewerModelPermits:        true, // reviewer_model_permits
	AssumeConfigFileProtectionUnknown: true, // config_file_protection_scope_unknown
	AssumeSandboxPlatformUnknown:      true, // sandbox_platform_unknown
	AssumeAutoModeAvailable:           true, // auto_mode_available
	// Defined in internal/rules (which imports this package, so referenced here
	// by their stable enum string to avoid an import cycle):
	"aux_model_permits":                 true,
	"windows_sandbox_unknown":           true,
	"agent_model_judges_command_safety": true,
}

// AssumptionHiddenAtRest reports whether an assumption code is «not visible at
// rest» and so counts toward the opacity axis (CTO-26 §5).
func AssumptionHiddenAtRest(code string) bool { return hiddenAtRest[code] }
