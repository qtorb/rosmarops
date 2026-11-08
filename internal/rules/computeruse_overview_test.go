package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-193 (CTO-22 Computer Use §6): OpenAI Codex / ChatGPT desktop can be let
// operate desktop apps without asking. One operate_app capability per allowed
// app becomes one computer_use_app_allowed finding per colour: red for a
// terminal/interpreter or default_app_access = allow (same as running
// commands), yellow for any other app. The feature off (requirements
// computer_use = false) leaves the capability not in force and emits no
// finding.

// computerUseFindings returns the severities of the Codex client's
// computer_use_app_allowed findings and, per severity, the app ids carried in
// Evidence.Locations.
func computerUseResult(res rules.Result, client string) map[string][]string {
	out := map[string][]string{}
	for _, f := range res.Findings {
		if f.Rule != rules.RuleComputerUseAppAllowed {
			continue
		}
		if len(f.Evidence.Nodes) == 0 || f.Evidence.Nodes[0] != client {
			continue
		}
		out[f.Severity] = f.Evidence.Locations
	}
	return out
}

// cuScan builds a Codex scan with a single user layer whose [computer_use]
// section is the given config, plus the requirement toggles.
func cuScan(cu *platform.CodexComputerUse, featureOff, persistentOff bool) platform.CodexScan {
	l := codexLayer(platform.CodexLayerUser, codexConfig, "")
	l.ComputerUse = cu
	l.CUFeatureDisabled = featureOff
	l.CUPersistentApprovalDisabled = persistentOff
	return platform.CodexScan{ConfigDir: codexDir, Exists: true, Layers: []platform.CodexLayer{l}}
}

// U1: an ordinary app on always_allowed_app_ids (mspaint) is yellow, not red.
func TestComputerUse_U1_OrdinaryAppYellow(t *testing.T) {
	scan := cuScan(&platform.CodexComputerUse{Allowed: []string{"mspaint.exe"}}, false, false)
	_, res, client := codexEvaluate(t, scan, nil)
	cu := computerUseResult(res, client)
	if len(cu[rules.SeverityRed]) != 0 {
		t.Errorf("U1: unexpected red %v", cu[rules.SeverityRed])
	}
	if got := cu[rules.SeverityYellow]; len(got) != 1 || got[0] != "mspaint.exe" {
		t.Errorf("U1: yellow apps %v, want [mspaint.exe]", got)
	}
}

// U2: a Windows Terminal AUMID allowed is red — operating a terminal is the
// same as running commands.
func TestComputerUse_U2_TerminalRed(t *testing.T) {
	scan := cuScan(&platform.CodexComputerUse{Allowed: []string{"MICROSOFT.WINDOWSTERMINAL_8wekyb3d8bbwe!App"}}, false, false)
	_, res, client := codexEvaluate(t, scan, nil)
	cu := computerUseResult(res, client)
	if len(cu[rules.SeverityRed]) != 1 {
		t.Errorf("U2: red apps %v, want one terminal", cu[rules.SeverityRed])
	}
	if len(cu[rules.SeverityYellow]) != 0 {
		t.Errorf("U2: unexpected yellow %v", cu[rules.SeverityYellow])
	}
}

// U3: default_app_access = allow is red for every app ("*").
func TestComputerUse_U3_DefaultAllowRed(t *testing.T) {
	scan := cuScan(&platform.CodexComputerUse{DefaultAppAccess: "allow"}, false, false)
	_, res, client := codexEvaluate(t, scan, nil)
	cu := computerUseResult(res, client)
	if got := cu[rules.SeverityRed]; len(got) != 1 || got[0] != "*" {
		t.Errorf("U3: red apps %v, want [*]", got)
	}
}

// U4: U2 with the feature turned off at rest (requirements computer_use =
// false) leaves the capability not in force and emits no finding.
func TestComputerUse_U4_FeatureOffNoFinding(t *testing.T) {
	scan := cuScan(&platform.CodexComputerUse{Allowed: []string{"MICROSOFT.WINDOWSTERMINAL_8wekyb3d8bbwe!App"}}, true, false)
	_, res, client := codexEvaluate(t, scan, nil)
	cu := computerUseResult(res, client)
	if len(cu) != 0 {
		t.Errorf("U4: feature off must emit no finding, got %v", cu)
	}
}

// U5: the legacy $CODEX_HOME/computer-use/config.toml source behaves like the
// current one — an ordinary allowed app is yellow.
func TestComputerUse_U5_LegacySource(t *testing.T) {
	scan := cuScan(&platform.CodexComputerUse{Allowed: []string{"mspaint.exe"}, Legacy: true}, false, false)
	_, res, client := codexEvaluate(t, scan, nil)
	cu := computerUseResult(res, client)
	if len(cu[rules.SeverityRed]) != 0 {
		t.Errorf("U5: unexpected red %v", cu[rules.SeverityRed])
	}
	if got := cu[rules.SeverityYellow]; len(got) != 1 || got[0] != "mspaint.exe" {
		t.Errorf("U5: yellow apps %v, want [mspaint.exe]", got)
	}
}
