package trust_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-235 (CTO-48 §3, R0′–R5): the 0.12.0 engine on the reference computer.

// r2IndexOf is the index entry of the evaluated agent of product in v.
func r2IndexOf(t *testing.T, v report.V2, product string) index.AgentIndex {
	t.Helper()
	_, _, id := r2AgentIndexID(t, v, product)
	for _, a := range v.Index.Agents {
		if a.AgentID == id {
			return a
		}
	}
	t.Fatalf("%s: no index entry", product)
	return index.AgentIndex{}
}

// TestBV235FactoryScoreIsR2: on R2, each agent's factory score (the second
// pass, every agent at its product defaults) is the score r2Cases pins.
func TestBV235FactoryScoreIsR2(t *testing.T) {
	t.Parallel()
	for _, c := range r2Cases() {
		a := r2IndexOf(t, c.build(t), c.product)
		if a.Factory.Value == nil || *a.Factory.Value != c.score || a.Factory.Band == nil || *a.Factory.Band != c.band {
			t.Errorf("%s: factory %v %v, want %d %s (r2Cases)", c.product, a.Factory.Value, a.Factory.Band, c.score, c.band)
		}
		if a.Value == nil || *a.Value != c.score {
			t.Errorf("%s: score %v, want %d (unchanged)", c.product, a.Value, c.score)
		}
		if a.Letter.Class == nil || a.Factory.Letter.Class == nil || a.Letter.Rule != index.ClassRule {
			t.Errorf("%s: letter %+v, factory letter %+v: want a class from the six axes", c.product, a.Letter, a.Factory.Letter)
		}
	}
}

// TestBV235FactoryCutsPersonalSettings: the person's settings change the score
// of this computer but not the factory score, which stays R2's.
func TestBV235FactoryCutsPersonalSettings(t *testing.T) {
	t.Parallel()
	// Claude Code with every command allowed and bypassPermissions.
	settings := r2Settings(platform.ClaudeSettingsFile{
		Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead,
		Permissions: platform.ClaudePermissions{Allow: []string{"Bash", "WebFetch"}, DefaultMode: "bypassPermissions"},
	})
	g := r2Assemble(t, pipeline.Input{
		Settings:     settings,
		CredFiles:    platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("claude-code", fcClaudeDir)},
	})
	claude := r2IndexOf(t, r2BuildV2(t, &g, &settings, nil), "claude-code")
	if claude.Factory.Value == nil || *claude.Factory.Value != 31 {
		t.Errorf("claude-code with personal settings: factory %v, want 31", claude.Factory.Value)
	}

	// Codex with danger-full-access and approval never, in t2 (its own test, as
	// r2Assemble keeps one factory graph per test).
	t.Run("codex", func(t *testing.T) {
		scan := platform.CodexScan{
			ConfigDir: fcCodexDir, Exists: true,
			Layers: []platform.CodexLayer{
				{
					Kind: platform.CodexLayerUser, Path: fcCodexCfg, Status: platform.MCPSourceRead,
					SandboxMode: "danger-full-access", ApprovalPolicy: "never", HasApprovalPolicy: true,
				},
				{Kind: platform.CodexLayerProject, Path: fcCodexProj, Root: fcRepo, Status: platform.MCPSourceRead, Trusted: true},
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
		codex := r2IndexOf(t, r2BuildV2(t, &g, &settings, func(in *report.V2Input) { in.Codex = scan }), "codex")
		if codex.Value == nil || *codex.Value >= 73 {
			t.Errorf("codex danger-full-access + never: score %v, want below 73", codex.Value)
		}
		if codex.Factory.Value == nil || *codex.Factory.Value != 73 {
			t.Errorf("codex with personal settings: factory %v, want 73", codex.Factory.Value)
		}
	})
	if claude.Value == nil || *claude.Value > 31 {
		t.Errorf("claude-code bypass + Bash: score %v, want at most 31", claude.Value)
	}
}

// TestBV235R2ClassesUnchanged: opacity now also counts the assumptions of the
// overview cells (CTO-48 §4.2); R2's classes are those of the v0.11.7 map.
func TestBV235R2ClassesUnchanged(t *testing.T) {
	want := map[string]string{
		"claude-code": "D", "cursor": "D", "codex": "C", "github-copilot": "B", "gemini-cli": "B",
		"cline": "D", "grok-build": "C", "hermes": "D", "antigravity-cli": "B",
	}
	got := r2ExposureAgents(t)
	if len(got) != len(want) {
		t.Fatalf("%d agents, want %d", len(got), len(want))
	}
	for _, a := range got {
		if a.Class != want[a.Product] {
			t.Errorf("%s: class %q, want %q (exposure-potential.json of v0.11.7)", a.Product, a.Class, want[a.Product])
		}
		if a.F == nil {
			t.Errorf("%s: no F", a.Product)
		}
	}
}

// TestBV235FactoryPassWritesNothing: the factory pass (pipeline.Factory, a
// second Assemble, and BuildV2 with it) writes no file: not the graph file,
// not the memory, not the history. Run in an empty home and working folder.
func TestBV235FactoryPassWritesNothing(t *testing.T) {
	root := t.TempDir()
	home, work := filepath.Join(root, "home"), filepath.Join(root, "work")
	for _, d := range []string{home, work} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("TMPDIR", filepath.Join(root, "tmp"))
	t.Chdir(work)
	before := snapshot(t, root)
	for _, c := range r2Cases() {
		v := c.build(t)
		if _, err := report.RenderV2JSON(v); err != nil {
			t.Fatalf("%s: %v", c.product, err)
		}
	}
	v, _ := r2AllReport(t)
	if _, err := report.RenderV2JSON(v); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, root)
	if len(before) != len(after) {
		t.Fatalf("the factory pass wrote: %d entries before, %d after", len(before), len(after))
	}
	for p, e := range before {
		if after[p] != e {
			t.Errorf("%s changed", p)
		}
	}
}

// TestBV235RunsBoundedListedCommands (§3.7): Claude Code in default mode asks
// for commands but runs its read-only list without asking: bounded, with the
// listed_commands scope. In auto (R2's factory mode) the classifier decides.
func TestBV235RunsBoundedListedCommands(t *testing.T) {
	t.Parallel()
	kind := func(v report.V2) rules.RiskKindCell {
		for _, a := range v.AgentsOverview {
			if a.Product != "claude-code" {
				continue
			}
			for _, k := range a.Kinds {
				if k.Kind == rules.KindRunsCommands {
					return k
				}
			}
		}
		t.Fatal("no runs_commands kind")
		return rules.RiskKindCell{}
	}
	if k := kind(r2ClaudeCode(t)); k.State != rules.StateModel {
		t.Errorf("auto: runs %+v, want model", k)
	}
	t.Run("default", func(t *testing.T) {
		settings := r2Settings(platform.ClaudeSettingsFile{
			Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead,
			Permissions: platform.ClaudePermissions{DefaultMode: "default"},
		})
		g := r2Assemble(t, pipeline.Input{
			Settings:     settings,
			CredFiles:    platform.CredentialFileScan{Entries: r2Creds()},
			Instructions: platform.InstructionScan{Files: r2Instr("claude-code", fcClaudeDir)},
		})
		if k := kind(r2BuildV2(t, &g, &settings, nil)); k.State != rules.StateBounded || k.Scope != rules.KindScopeListedCommands {
			t.Errorf("default: runs %+v, want bounded listed_commands", k)
		}
	})
}

// TestBV235MachineLetter (point 2): the machine's letter is the class of the
// agent that gives its score; another agent with a worse class is named.
// coherent is against the band of the same review.
func TestBV235MachineLetter(t *testing.T) {
	t.Parallel()
	v, _ := r2AllReport(t)
	m := v.Index.Machine
	if m.Value == nil || m.Letter == nil || m.FactoryValue == nil || m.FactoryLetter == nil {
		t.Fatalf("machine %+v: want a value, a letter and the factory figures", m)
	}
	rank := func(c *string) int {
		if c == nil {
			return 0
		}
		return int((*c)[0]-'A') + 1
	}
	var giver *index.AgentIndex
	worst := 0
	for i, a := range v.Index.Agents {
		if a.Value != nil && *a.Value == *m.Value && giver == nil {
			giver = &v.Index.Agents[i]
		}
		if r := rank(a.Letter.Class); r > worst {
			worst = r
		}
		if a.Letter.Class != nil && a.Band != "" {
			if a.Letter.Coherent == nil {
				t.Errorf("%s: no coherent with class %s and band %s", a.Agent, *a.Letter.Class, a.Band)
			}
		}
	}
	if giver == nil || *giver.Letter.Class != *m.Letter.Class {
		t.Fatalf("machine letter %v is not the class of the agent giving %d", m.Letter.Class, *m.Value)
	}
	if (worst > rank(m.Letter.Class)) != (m.WorstLetterAgentID != "") {
		t.Errorf("worst class rank %d vs machine %d: worst_letter_agent_id %q", worst, rank(m.Letter.Class), m.WorstLetterAgentID)
	}
}
