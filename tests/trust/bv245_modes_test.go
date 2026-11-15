package trust_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/exposure"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

// BV-245 (CTO-55 option b): Antigravity CLI with agentMode auto and GitHub
// Copilot with chat.defaultConfiguration assisted, on R2. Their terminal runs on
// the whole computer with a reviewer model deciding, so it reaches every
// credential (b-i), and their «sends out» row gives them a way out (b-ii): the
// note is red and the class D, coherent with the band.

func r2AntigravityAuto(t *testing.T) report.V2 {
	scan := platform.AntigravityScan{ConfigDir: fcGemini, Present: true, Settings: platform.AntigravitySettings{
		Path: fcGemini + "/antigravity-cli/settings.json", Status: platform.MCPSourceRead, AgentMode: "auto",
	}}
	settings := r2Settings()
	g := r2Assemble(t, pipeline.Input{
		Settings:     settings,
		Antigravity:  scan,
		CredFiles:    platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("antigravity-cli", fcGemini)},
	})
	return r2BuildV2(t, &g, &settings, func(in *report.V2Input) { in.Antigravity = scan })
}

func r2CopilotAssisted(t *testing.T) report.V2 {
	scan := platform.CopilotScan{ConfigDir: fcCopilotDir, Present: true, Layers: []platform.CopilotLayer{{
		Kind: platform.CopilotLayerUser, Path: fcCopilotDir + "/settings.json", Status: platform.MCPSourceRead, Approvals: "assisted",
	}}}
	settings := r2Settings()
	g := r2Assemble(t, pipeline.Input{
		MCP:          platform.MCPScan{Servers: graph.CopilotMCPServers(scan)},
		Settings:     settings,
		Copilot:      scan,
		Extensions:   []platform.VSCodeExtension{{ID: "github.copilot"}},
		CredFiles:    platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("github-copilot", fcCopilotDir)},
	})
	return r2BuildV2(t, &g, &settings, nil)
}

func TestBV245_ReviewerModeIsRedAndD(t *testing.T) {
	allV, allG := r2AllReport(t)
	for _, c := range []struct {
		product string
		build   func(*testing.T) report.V2
		score   int
	}{
		{"antigravity-cli", r2AntigravityAuto, 31},
		{"github-copilot", r2CopilotAssisted, 31},
	} {
		single := c.build(t)
		score, band, sid := r2AgentIndexID(t, single, c.product)
		_, singleAssume := r2Detail(single, sid)
		a := exposure.Compute(exposure.Inputs{
			Product: c.product, Evaluated: true,
			Single: overviewByProduct(single, c.product), SingleAssumptions: singleAssume,
			All: overviewByProduct(allV, c.product), AllGraph: allG,
			ClientID: overviewByProduct(allV, c.product).AgentID, FactoryBand: band,
		})
		f := 0.0
		if a.F != nil {
			f = *a.F
		}
		t.Logf("%s: score %d, band %s, class %s (F %.3f)", c.product, score, band, a.Class, f)
		if score != c.score {
			t.Errorf("%s: score %d, want %d", c.product, score, c.score)
		}
		if band != "exposed" {
			t.Errorf("%s: band %q, want exposed (red)", c.product, band)
		}
		if a.Class != "D" {
			t.Errorf("%s: class %s, want D", c.product, a.Class)
		}
		if !a.CoherentWithBand() {
			t.Errorf("%s: class %s not coherent with band %s", c.product, a.Class, band)
		}
	}
}
