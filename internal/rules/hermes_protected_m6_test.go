package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-198 point 6 (CTO-23 §0): Hermes Agent protects its instruction files by
// default (security.protected_instruction_files true, NousResearch hermes
// config_defaults.py @ 8acb7f6): writing AGENTS.md, CLAUDE.md, SOUL.md or
// .cursorrules needs human approval, so it is not a without-asking finding. An
// instruction file outside that set (GEMINI.md) stays writable, and an explicit
// false turns the gate off.

// hermesInstrEval builds a Hermes graph with the given config and instruction
// files, plus a Claude Code client (so AGENTS.md is foreign to Hermes), and
// returns the Hermes client's finding rules.
func hermesInstrEval(t *testing.T, protected *bool, instr []platform.InstructionFile) map[string]bool {
	t.Helper()
	roots := []string{repoWeb}
	claude := []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{})}
	settings := platform.ClaudeSettingsScan{Files: claude, Trust: []platform.ProjectTrust{{Path: repoWeb, Accepted: true}}}
	cfg := platform.HermesConfig{
		Path: hermesDir + "/config.yaml", ConfigDir: hermesDir, Status: platform.MCPSourceRead,
		ApprovalMode: "smart", TerminalBackend: "local", ProtectedInstructionFiles: protected,
	}
	source := platform.MCPSource{Platform: agentdirs.Hermes, ConfigPath: cfg.Path, ConfigDir: cfg.ConfigDir, Status: platform.MCPSourceRead, LoadsMCP: true}
	hermes := platform.HermesScan{Configs: []platform.HermesConfig{cfg}, Sources: []platform.MCPSource{source}}
	mcp := platform.MCPScan{Sources: hermes.Sources}
	f, _, err := graph.FromScansWithHermes(mcp, settings, platform.CursorScan{}, platform.CredentialFileScan{}, graph.Repos{}, hermes, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	f = graph.AddInstructions(f, platform.InstructionScan{Files: instr}, now)
	g := graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(salt)}, now)
	g, err = graph.Infer(g, roots, home)
	if err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	res := rules.Evaluate(rules.Input{Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases, RepoRoots: roots, Home: home, Now: now, ClaudeSettings: &settings, Hermes: hermes})
	var client string
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient && n.Platform == knowledge.HermesProduct {
			client = n.ID
		}
	}
	if client == "" {
		t.Fatal("no Hermes Agent ai_client")
	}
	out := map[string]bool{}
	for _, fd := range res.Findings {
		if len(fd.Evidence.Nodes) > 0 && fd.Evidence.Nodes[0] == client {
			out[fd.Rule] = true
		}
	}
	return out
}

func TestPt6_HermesProtectedInstructions(t *testing.T) {
	soul := hermesDir + "/SOUL.md"
	agents := repoWeb + "/AGENTS.md"
	// SOUL.md is Hermes's own; AGENTS.md is shared by Hermes and Claude Code.
	protectedInstr := []platform.InstructionFile{
		{Product: agentdirs.Hermes, ConfigDir: hermesDir, Path: soul},
		{Product: agentdirs.Hermes, ConfigDir: hermesDir, Path: agents, Shared: true},
		{Product: knowledge.ClaudeCodeProduct, ConfigDir: claudeDir, Path: agents, Shared: true},
	}

	// Default (nil → protected on): neither its own SOUL.md/AGENTS.md nor
	// Claude Code's AGENTS.md is a without-asking write.
	rs := hermesInstrEval(t, nil, protectedInstr)
	if rs[rules.RuleOwnInstructionsWritable] {
		t.Errorf("protected default: SOUL.md/AGENTS.md must not be own_instructions_writable (have %v)", rs)
	}
	if rs[rules.RuleForeignAgentInstructionsWritable] {
		t.Errorf("protected default: Claude Code's AGENTS.md must not be foreign_agent_instructions_writable (have %v)", rs)
	}

	// Explicit false turns the gate off: both fire again.
	off := false
	rs = hermesInstrEval(t, &off, protectedInstr)
	if !rs[rules.RuleOwnInstructionsWritable] {
		t.Errorf("protected off: own_instructions_writable should fire (have %v)", rs)
	}
	if !rs[rules.RuleForeignAgentInstructionsWritable] {
		t.Errorf("protected off: foreign_agent_instructions_writable should fire (have %v)", rs)
	}

	// A non-protected instruction file (GEMINI.md) stays writable even with the
	// gate on.
	gemini := repoWeb + "/GEMINI.md"
	rs = hermesInstrEval(t, nil, []platform.InstructionFile{{Product: agentdirs.Hermes, ConfigDir: hermesDir, Path: gemini}})
	if !rs[rules.RuleOwnInstructionsWritable] {
		t.Errorf("protected default: a non-protected GEMINI.md should stay own_instructions_writable (have %v)", rs)
	}
}
