package rules_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-198 M5 (CTO-23 §0): an agent's own session file — named in its
// knowledge-base session_files list, under its configuration folder — is not a
// finding against that agent, but still is against any other agent that reaches
// it. Cline's secrets.json is new to the list; here Cline owns it and Claude
// Code (bypass, reads the machine) does not.
func TestM5_OwnSessionFileNotCountedAgainstOwner(t *testing.T) {
	secrets := clineData + "/secrets.json"
	claude := []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{DefaultMode: "bypassPermissions"})}
	_, res, cline := clineEvaluate(t, clBase(), nil, claude, []entry{{secrets, 1, "SUPABASE_SERVICE_ROLE_KEY", serviceRole}})

	credRule := func(r string) bool { return r == rules.RuleCredReadAndSend || r == rules.RuleCredReadable }
	var ownerCounts, otherCounts bool
	for _, f := range res.Findings {
		if !credRule(f.Rule) || len(f.Evidence.Nodes) == 0 {
			continue
		}
		hit := false
		for _, loc := range f.Evidence.Locations {
			if strings.Contains(loc, "secrets.json") {
				hit = true
			}
		}
		if !hit {
			continue
		}
		if f.Evidence.Nodes[0] == cline {
			ownerCounts = true
		} else {
			otherCounts = true
		}
	}
	if ownerCounts {
		t.Errorf("Cline's own secrets.json must not count against Cline")
	}
	if !otherCounts {
		t.Errorf("secrets.json should be a finding against another agent that reaches it")
	}
}
