package trust_test

// BV-249 §7 (CTO-59 §4.4): the sister of TestExposureClassCoherentWithFactoryBand.
// Same allowed classes (exposure.AllowedClasses, no tolerance, no exception),
// more cases: every measured template per system, Cursor's declared settings
// (factory answers, least controlled, the full C1×C2×C3×C4 grid with answers,
// and «I don't know» in each question), Grok Bot with each B1, and a grid of
// Claude Code's modes with and without its sandbox and read limit. Each case
// is measured as the report measures it: the agent alone for its score and
// band, and its class with R2-todos changed the same way.

import (
	"fmt"
	"maps"
	"testing"

	"github.com/qtorb/rosmarops/internal/declared"
	"github.com/qtorb/rosmarops/internal/exposure"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

type coherenceCase struct {
	name, product string
	v             report.V2
	mod           func(*pipeline.Input)
}

// r2CursorAlone is R2 with Cursor alone and these answers, without its
// template passes (the grid only needs the agent's own figures).
func r2CursorAlone(t *testing.T, answers map[string]string) report.V2 {
	t.Helper()
	scan := platform.CursorScan{ConfigDir: fcHome + "/.cursor", Exists: true, Declared: answers}
	settings := r2Settings()
	in := pipeline.Input{
		Settings: settings, Cursor: scan,
		CredFiles:    platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("cursor", fcHome+"/.cursor")},
	}
	g := r2Assemble(t, in)
	return r2BuildV2(t, &g, &settings, func(v *report.V2Input) {
		v.Cursor = scan
		v.Declared = map[string]map[string]string{declared.Cursor: answers}
	})
}

// r2ClaudeCodeWith is Claude Code alone on R2 with these user permissions,
// and the same change for R2-todos.
func r2ClaudeCodeWith(t *testing.T, perms platform.ClaudePermissions) (report.V2, func(*pipeline.Input)) {
	t.Helper()
	file := platform.ClaudeSettingsFile{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead, Permissions: perms}
	settings := r2Settings(file)
	g := r2Assemble(t, pipeline.Input{
		Settings: settings, CredFiles: platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("claude-code", fcClaudeDir)},
	})
	return r2BuildV2(t, &g, &settings, nil), func(in *pipeline.Input) { in.Settings = r2Settings(file) }
}

func cursorMod(answers map[string]string) func(*pipeline.Input) {
	return func(in *pipeline.Input) { in.Cursor.Declared = answers }
}

func TestCoherenceSister(t *testing.T) {
	prev := graph.HostOS
	defer func() { graph.HostOS = prev }()
	if testing.Short() {
		t.Skip("the full grid is long")
	}
	checked, bad := 0, 0
	check := func(c coherenceCase) {
		t.Helper()
		score, band, _ := r2AgentIndexID(t, c.v, c.product)
		a := classOfSingle(t, c.v, c.product, c.mod)
		checked++
		if ok := a.Coherent(band); ok == nil || !*ok {
			bad++
			t.Errorf("%s: %d (%s) class %s F %v, allowed %v", c.name, score, band, a.Class, ptrF(a.F), exposure.AllowedClasses(band))
		}
	}

	// 1. Every measured template, per system (TestProfileScores's cases).
	for _, sys := range []struct{ goos, name string }{{"windows", "windows"}, {"darwin", "macos"}, {"linux", "linux"}} {
		graph.HostOS = sys.goos
		for _, id := range []string{"P0", "P1", "P2"} {
			if id == "P2" && sys.goos == "windows" {
				continue
			}
			v, mod := r2ClaudeCodeProfile(t, id)
			check(coherenceCase{"claude-code " + id + " " + sys.name, "claude-code", v, mod})
		}
		if sys.goos != "windows" {
			v, mod := r2GrokP2(t)
			check(coherenceCase{"grok-build P2 " + sys.name, "grok-build", v, mod})
		}
	}
	graph.HostOS = "linux"

	// 2. Cursor's declared settings: the full grid with answers, «I don't know»
	// in each question (the others at the factory answer), factory and least
	// controlled.
	qs := declared.CursorQuestions
	var grid func(i int, answers map[string]string)
	grid = func(i int, answers map[string]string) {
		if i == len(qs) {
			a := maps.Clone(answers)
			check(coherenceCase{fmt.Sprintf("cursor %v", a), "cursor", r2CursorAlone(t, a), cursorMod(a)})
			return
		}
		for _, o := range qs[i].Options {
			answers[qs[i].ID] = o
			grid(i+1, answers)
		}
	}
	grid(0, map[string]string{})
	for _, q := range qs {
		a := maps.Clone(declared.Factory(declared.Cursor))
		a[q.ID] = declared.Unknown
		check(coherenceCase{"cursor " + q.ID + "=unknown", "cursor", r2CursorAlone(t, a), cursorMod(a)})
	}
	f := declared.Factory(declared.Cursor)
	check(coherenceCase{"cursor factory", "cursor", r2CursorAlone(t, f), cursorMod(f)})
	check(coherenceCase{"cursor no answer (least controlled)", "cursor", r2CursorAlone(t, nil), cursorMod(nil)})

	// 3. Grok Bot with each B1.
	for _, b1 := range []string{"always", "ask", "never"} {
		v := r2GrokBot(t, b1)
		check(coherenceCase{"grok-bot B1=" + b1, "grok-bot", v, nil})
	}

	// 4. Claude Code's modes, with and without an effective sandbox and the
	// read limit (Linux, where the sandbox exists).
	sb := platform.ClaudeSandbox{Enabled: true, EnabledSet: true, FailIfUnavailable: true, FailIfUnavailableSet: true, AllowUnsandboxedCommands: false, AllowUnsandboxedCommandsSet: true}
	for _, mode := range []string{"", "default", "acceptEdits", "plan", "auto", "bypassPermissions", "dontAsk"} {
		for _, sandbox := range []bool{false, true} {
			for _, block := range []bool{false, true} {
				p := platform.ClaudePermissions{DefaultMode: mode, BlockReadsOutsideWorkingDirectories: block}
				if sandbox {
					p.Sandbox = sb
				}
				v, mod := r2ClaudeCodeWith(t, p)
				check(coherenceCase{fmt.Sprintf("claude-code mode=%q sandbox=%v blockReads=%v", mode, sandbox, block), "claude-code", v, mod})
			}
		}
	}
	t.Logf("%d cases, %d not coherent", checked, bad)
}
