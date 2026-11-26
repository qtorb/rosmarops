package trust_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// Two writes the score did not count (ADR 0042, block 200):
//   - Hermes Agent with an isolated terminal backend still writes its memory
//     and its skills on the computer: its memory and skill_manage tools do not
//     go through the backend;
//   - Claude Code in Manual mode (default) writes its auto memory without
//     asking, unless autoMemoryEnabled is false.
//
// The fixtures are the reference computer's (R2) single agent, with the
// memory and skills added. The scores before the change were measured on the
// same fixtures (vigil docs/research/2026-11-26-bv-309-impacto-en-la-nota.md).

const (
	bv310HermesDir = fcHome + "/.hermes"
	bv310ClaudeMem = fcClaudeDir + "/projects/-src-webapp/memory"
)

// bv310Hermes is R2's Hermes Agent with the given terminal backend, its
// memories folder when memory, and a skill with a script when skill.
func bv310Hermes(t *testing.T, backend string, memory, skill bool) report.V2 {
	hd := bv310HermesDir
	cfg := platform.HermesConfig{Path: hd + "/config.yaml", ConfigDir: hd, Status: platform.MCPSourceRead, ApprovalMode: "smart", TerminalBackend: backend}
	source := platform.MCPSource{Platform: "hermes", ConfigPath: cfg.Path, ConfigDir: cfg.ConfigDir, Status: platform.MCPSourceRead, LoadsMCP: true}
	hermes := platform.HermesScan{Configs: []platform.HermesConfig{cfg}, Sources: []platform.MCPSource{source}}
	in := pipeline.Input{
		MCP:          platform.MCPScan{Sources: hermes.Sources},
		Settings:     r2Settings(),
		Hermes:       hermes,
		CredFiles:    platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("hermes", hd)},
	}
	if memory {
		in.AlwaysOn.Memory = []platform.AgentMemory{{Product: knowledge.HermesProduct, ConfigDir: hd, Path: hd + "/memories", Files: 2, Bytes: 200}}
	}
	if skill {
		in.Skills = platform.SkillScan{
			Instructions: []platform.InstructionFile{{Product: knowledge.HermesProduct, ConfigDir: hd, Path: hd + "/skills/notes/SKILL.md"}},
			Executables:  []platform.InstructionFile{{Product: knowledge.HermesProduct, ConfigDir: hd, Path: hd + "/skills/notes/scripts/run.py"}},
		}
	}
	settings := in.Settings
	g := r2Assemble(t, in)
	return r2BuildV2(t, &g, &settings, func(v *report.V2Input) { v.Hermes = hermes })
}

// bv310Claude is R2's Claude Code with defaultMode mode in its user settings
// ("" for the factory mode) and an auto memory folder, off when off.
func bv310Claude(t *testing.T, mode string, off bool) report.V2 {
	user := platform.ClaudeSettingsFile{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead}
	user.Permissions.DefaultMode = mode
	settings := r2Settings(user)
	g := r2Assemble(t, pipeline.Input{
		Settings:     settings,
		CredFiles:    platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("claude-code", fcClaudeDir)},
		AlwaysOn: platform.AlwaysOnScan{Memory: []platform.AgentMemory{{
			Product: knowledge.ClaudeCodeProduct, ConfigDir: fcClaudeDir, Path: bv310ClaudeMem, Files: 3, Bytes: 300, AutoMemoryOff: off,
		}}},
	})
	return r2BuildV2(t, &g, &settings, nil)
}

type bv310Want struct {
	score       int
	band, class string
	findings    []fsFinding
}

func bv310Check(t *testing.T, name string, v report.V2, product string, want bv310Want) {
	t.Helper()
	score, band, id := r2AgentIndexID(t, v, product)
	class := ""
	if l := v.Index.Machine.Letter; l != nil && l.Class != nil {
		class = *l.Class
	}
	findings, _ := r2Detail(v, id)
	t.Logf("%s: %d %s %s %v", name, score, band, class, findings)
	if score != want.score || band != want.band || class != want.class {
		t.Errorf("%s: %d %s class %s, want %d %s class %s", name, score, band, class, want.score, want.band, want.class)
	}
	if len(findings) != len(want.findings) {
		t.Errorf("%s: findings %v, want %v", name, findings, want.findings)
		return
	}
	for i := range findings {
		if findings[i] != want.findings[i] {
			t.Errorf("%s: findings %v, want %v", name, findings, want.findings)
			return
		}
	}
}

// H2 and H3: with an isolated backend, Hermes keeps its write edges to its
// own memory, SKILL.md and skill scripts; every other write and read stays in
// the sandbox. Before: 100 contained in the three cases.
func TestBV310HermesIsolatedKeepsMemoryAndSkills(t *testing.T) {
	ownConfig := fsFinding{Code: rules.RuleOwnConfigWritable, Weight: 15}
	ownInstr := fsFinding{Code: rules.RuleOwnInstructionsWritable, Weight: 4}
	for _, backend := range []string{"docker", "ssh", "modal", "daytona", "vercel_sandbox"} {
		bv310Check(t, backend+" + memory + skill with script", bv310Hermes(t, backend, true, true), knowledge.HermesProduct,
			bv310Want{39, "exposed", "B", []fsFinding{ownConfig, ownInstr}})
		bv310Check(t, backend+" + memory", bv310Hermes(t, backend, true, false), knowledge.HermesProduct,
			bv310Want{77, "open_gaps", "B", []fsFinding{ownInstr}})
		bv310Check(t, backend+" alone", bv310Hermes(t, backend, false, false), knowledge.HermesProduct,
			bv310Want{100, "contained", "B", []fsFinding{}})
	}
}

// H5: the gain the report promises for «Run Hermes Agent in a container»
// («If you fix this: {now} → {then}») is the score without the finding the
// action is offered on. The action is offered only on what the container cuts
// (reads, «any_reach»), never on a memory or skill write, so every promised
// score keeps the writes the container does not cover: none is above the
// score Hermes gets in the container, and with memory and skills none is 100.
func TestBV310HermesContainerGain(t *testing.T) {
	for _, c := range []struct {
		name          string
		memory, skill bool
	}{
		{"memory + skill with script", true, true},
		{"memory", true, false},
		{"nothing the container leaves", false, false},
	} {
		inContainer, _, _ := r2AgentIndexID(t, bv310Hermes(t, "docker", c.memory, c.skill), knowledge.HermesProduct)
		v := bv310Hermes(t, "local", c.memory, c.skill)
		_, _, id := r2AgentIndexID(t, v, knowledge.HermesProduct)
		without := map[string]int{}
		for _, a := range v.Index.Agents {
			if a.AgentID == id {
				for _, w := range a.Weights {
					without[w.FindingID] = w.Without
				}
			}
		}
		offered := 0
		for _, f := range v.Findings {
			for _, a := range f.Actions {
				if a.Code != rules.ActionHermesIsolatedBackend {
					continue
				}
				offered++
				if f.Rule == rules.RuleOwnInstructionsWritable || f.Rule == rules.RuleOwnConfigWritable {
					t.Errorf("%s: the container action is offered on %s, which the container does not cut", c.name, f.Rule)
				}
				then, ok := without[f.ID]
				t.Logf("%s: %s → %d (in the container: %d)", c.name, f.Rule, then, inContainer)
				if !ok || then > inContainer || c.memory && then == 100 {
					t.Errorf("%s: %s promises %d, in the container the score is %d", c.name, f.Rule, then, inContainer)
				}
			}
		}
		if offered == 0 {
			t.Errorf("%s: no finding offers the container action", c.name)
		}
	}
}

// C1 and C2: in Manual mode (default) Claude Code writes its auto memory
// without asking: one more location of own_instructions_writable. With
// autoMemoryEnabled false, no edge. The other modes do not change.
func TestBV310ClaudeAutoMemory(t *testing.T) {
	ownInstr := fsFinding{Code: rules.RuleOwnInstructionsWritable, Weight: 4}
	credReadable := fsFinding{Code: rules.RuleCredReadable, Weight: 12}
	// Before: 74 open_gaps B, no finding.
	bv310Check(t, "default + memory", bv310Claude(t, "default", false), knowledge.ClaudeCodeProduct,
		bv310Want{73, "open_gaps", "B", []fsFinding{credReadable, ownInstr}})
	off := bv310Claude(t, "default", true)
	bv310Check(t, "default + memory, autoMemoryEnabled false", off, knowledge.ClaudeCodeProduct,
		bv310Want{74, "open_gaps", "B", []fsFinding{credReadable}})
	// plan is not documented: unchanged.
	bv310Check(t, "plan + memory", bv310Claude(t, "plan", false), knowledge.ClaudeCodeProduct,
		bv310Want{74, "open_gaps", "B", []fsFinding{credReadable}})
	// The factory mode (auto) is unchanged: 31 exposed, and the memory adds no
	// location (only Manual mode is documented).
	auto := bv310Claude(t, "", false)
	score, band, id := r2AgentIndexID(t, auto, knowledge.ClaudeCodeProduct)
	if score != 31 || band != "exposed" {
		t.Errorf("auto + memory: %d %s, want 31 exposed", score, band)
	}
	for _, f := range auto.Findings {
		if f.Rule == rules.RuleOwnInstructionsWritable && f.Evidence.Nodes[0] == id && len(f.Evidence.Locations) != 1 {
			t.Errorf("auto + memory: %d locations, want 1 (AGENTS.md): %v", len(f.Evidence.Locations), f.Evidence.Locations)
		}
	}
}
