package trust_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

// r2CodexWith is the reference computer's Codex (r2Codex) with the user
// config's sandbox_mode set to mode ("" leaves it unset).
func r2CodexWith(t *testing.T, mode string) report.V2 {
	scan := platform.CodexScan{
		ConfigDir: fcCodexDir, Exists: true,
		Layers: []platform.CodexLayer{
			{Kind: platform.CodexLayerUser, Path: fcCodexCfg, Status: platform.MCPSourceRead, SandboxMode: mode},
			func() platform.CodexLayer {
				l := platform.CodexLayer{Kind: platform.CodexLayerProject, Path: fcCodexProj, Root: fcRepo, Status: platform.MCPSourceRead}
				l.Trusted = true
				return l
			}(),
		},
	}
	settings := r2Settings()
	g := r2Assemble(t, pipeline.Input{
		MCP:          platform.MCPScan{Servers: graph.CodexMCPServers(scan)},
		Settings:     settings,
		Codex:        scan,
		CredFiles:    platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("codex", fcCodexDir)},
	})
	return r2BuildV2(t, &g, &settings, func(in *report.V2Input) { in.Codex = scan })
}

// With sandbox_mode = "read-only" in the user config, Codex does not edit
// files without approval (BV-293): the factory write to the workspace is no
// longer in force, so the write to AGENTS.md (own_instructions_writable)
// goes. Reading, commands and network do not change. The factory score is
// the same either way: the factory pass drops the setting.
func TestBV293_CodexReadOnly(t *testing.T) {
	without := r2CodexWith(t, "")
	with := r2CodexWith(t, "read-only")
	s0, b0, id0 := r2AgentIndexID(t, without, "codex")
	s1, b1, id1 := r2AgentIndexID(t, with, "codex")
	f0, _ := r2Detail(without, id0)
	f1, _ := r2Detail(with, id1)
	t.Logf("without: %d %s %v", s0, b0, f0)
	t.Logf("read-only: %d %s %v", s1, b1, f1)
	if s0 != 73 || b0 != "open_gaps" {
		t.Errorf("without read-only: %d %s, want 73 open_gaps (the pinned factory score)", s0, b0)
	}
	has := func(fs []fsFinding, rule string) bool {
		for _, f := range fs {
			if f.Code == rule {
				return true
			}
		}
		return false
	}
	if !has(f0, "own_instructions_writable") {
		t.Errorf("without read-only: no own_instructions_writable: %v", f0)
	}
	if has(f1, "own_instructions_writable") {
		t.Errorf("read-only: Codex still writes its instructions: %v", f1)
	}
	if !has(f1, "cred_readable") {
		t.Errorf("read-only: reading must not change, cred_readable gone: %v", f1)
	}
	for _, a := range with.Index.Agents {
		if a.AgentID == id1 && (a.Factory.Value == nil || *a.Factory.Value != 73) {
			t.Errorf("read-only: the factory score must stay 73: %+v", a.Factory)
		}
	}
}
