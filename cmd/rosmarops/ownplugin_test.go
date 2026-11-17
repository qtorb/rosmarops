package main

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

// BV-253 §B.2: a recognised own plugin is RosmarOps's session check in the
// report's «What RosmarOps has installed» (html.installed.session); without
// it, nothing is said.
func TestAgentHooksOwnPlugin(t *testing.T) {
	for _, own := range []bool{true, false} {
		scan := &platform.ClaudeSettingsScan{OwnPlugin: own}
		a := agentHooksFor(scan, report.V2{}, "/bin/rosmarops")
		got := len(a.Installed) == 1 && a.Installed[0] == platform.OwnHookSessionCheck
		if got != own {
			t.Errorf("OwnPlugin %v: installed %v", own, a.Installed)
		}
	}
}
