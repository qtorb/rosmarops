package integration_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

// BV-237 (CTO-50 §1, §3): the profiles of Claude Code and Grok Build applied,
// as written, to the full test computer (testdata/bv235), on Windows and on
// Linux: the agent's score and class.

type bv237Profile struct {
	agent, name string
	apply       func(in *pipeline.Input)
}

// claudeProfile merges a profile into the user settings file, as Claude Code
// merges a pasted block: the lists add, the single values replace.
func claudeProfile(p platform.ClaudePermissions) func(in *pipeline.Input) {
	return func(in *pipeline.Input) {
		f := &in.Settings.Files[0]
		q := &f.Permissions
		q.Allow, q.Ask, q.Deny = append(q.Allow, p.Allow...), append(q.Ask, p.Ask...), append(q.Deny, p.Deny...)
		q.DefaultMode, q.DisableAutoMode, q.DisableAutoModePointer = p.DefaultMode, p.DisableAutoMode, "/permissions/disableAutoMode"
		q.DisableBypassPermissionsMode, q.BlockReadsOutsideWorkingDirectories = p.DisableBypassPermissionsMode, p.BlockReadsOutsideWorkingDirectories
	}
}

// grokProfile writes config.toml's [ui], [permission] rules, [sandbox] and
// [compat] into the user layer and adds requirements.toml.
func grokProfile(rules []platform.GrokRule, profile string) func(in *pipeline.Input) {
	return func(in *pipeline.Input) {
		l := &in.Grok.Layers[0]
		l.PermissionMode, l.HasPermissionMode = "ask", true
		l.Rules, l.SandboxProfile = rules, profile
		if l.Compat == nil {
			l.Compat = map[string]bool{}
		}
		l.Compat["claude.hooks"], l.Compat["cursor.hooks"] = false, false
		in.Grok.Layers = append(in.Grok.Layers, platform.GrokLayer{
			Kind: platform.GrokLayerRequirements, Path: fmGrok + `\requirements.toml`, Status: platform.MCPSourceRead, DisableBypass: true,
		})
	}
}

func bv237Profiles() []bv237Profile {
	deny := func(tool string) platform.GrokRule { return platform.GrokRule{Action: "deny", Tool: tool} }
	allow := func(p string) platform.GrokRule { return platform.GrokRule{Action: "allow", Tool: "bash", Pattern: p} }
	return []bv237Profile{
		{"claude-code", "P0", claudeProfile(platform.ClaudePermissions{
			DefaultMode: "plan", DisableAutoMode: "disable", DisableBypassPermissionsMode: "disable", BlockReadsOutsideWorkingDirectories: true,
			Deny: []string{"Edit", "Bash", "WebFetch", "WebSearch", "mcp__*"},
		})},
		{"claude-code", "P1", claudeProfile(platform.ClaudePermissions{
			DefaultMode: "acceptEdits", DisableAutoMode: "disable", DisableBypassPermissionsMode: "disable", BlockReadsOutsideWorkingDirectories: true,
			Allow: []string{"Bash(npm run lint)", "Bash(npm run test *)", "Bash(git status)", "Bash(git diff)"},
			Ask: []string{
				"Edit(~/.codex/**)", "Edit(**/.codex/**)", "Edit(~/.cursor/**)", "Edit(**/.cursor/**)",
				"Edit(~/.cline/**)", "Edit(**/.clinerules/**)", "Edit(~/.gemini/**)", "Edit(**/.gemini/**)",
				"Edit(~/.grok/**)", "Edit(**/.grok/**)", "Edit(~/.hermes/**)", "Edit(~/AppData/Roaming/**)",
				"Edit(**/AGENTS.md)", "Edit(**/CLAUDE.md)", "Edit(**/GEMINI.md)", "Edit(**/.github/copilot-instructions.md)",
			},
			Deny: []string{"WebFetch", "WebSearch", "Bash(curl *)", "Bash(wget *)"},
		})},
		{"grok-build", "P0", grokProfile([]platform.GrokRule{deny("edit"), deny("bash"), deny("webfetch"), deny("mcp")}, "read-only")},
		{"grok-build", "P1", grokProfile([]platform.GrokRule{
			allow("npm run lint"), allow("npm run test *"), allow("git status"), allow("git diff"), deny("webfetch"),
		}, "rosmarops-p1")},
	}
}

// bv237Score is the agent's score and class on the full test computer, with
// the profile applied when apply is set.
func bv237Score(t *testing.T, product string, apply func(*pipeline.Input)) (int, string) {
	t.Helper()
	in, scans := fullMachineInput(true)
	if apply != nil {
		apply(&in)
		scans.Settings, scans.Grok = in.Settings, in.Grok
	}
	res, err := pipeline.Assemble(in)
	if err != nil {
		t.Fatal(err)
	}
	fres, err := pipeline.Assemble(pipeline.Factory(in))
	if err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	g := res.Graph
	v2, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: graphSHA(res.Encoded), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: in.RepoRoots, V1: report.Build("t", nil, nil, len(in.RepoRoots), 0, 0, 0, nil), GeneratedAt: fmNow, Salt: fmSalt,
		ClaudeSettings: &scans.Settings, Home: fmHome,
		Hermes: scans.Hermes, Codex: scans.Codex, Grok: scans.Grok, Cline: scans.Cline,
		FactoryGraph: &fres.Graph,
	})
	for _, o := range v2.AgentsOverview {
		if o.Product != product || !o.Evaluated {
			continue
		}
		for _, a := range v2.Index.Agents {
			if a.AgentID == o.AgentID && a.Value != nil {
				class := "—"
				if a.Letter.Class != nil {
					class = *a.Letter.Class
				}
				return *a.Value, class
			}
		}
	}
	t.Fatalf("%s: no score", product)
	return 0, ""
}

// TestBV237Profiles applies each profile on Windows and on Linux. Applying a
// profile never lowers the agent's score; the table is written when
// ROSMAROPS_BV237_TABLE is set.
func TestBV237Profiles(t *testing.T) {
	prev := graph.HostOS
	defer func() { graph.HostOS = prev }()
	var table string
	for _, osName := range []string{"windows", "linux"} {
		graph.HostOS = osName
		for _, p := range bv237Profiles() {
			baseScore, baseClass := bv237Score(t, p.agent, nil)
			score, class := bv237Score(t, p.agent, p.apply)
			table += fmt.Sprintf("| %s | %s | %s | %d %s | %d %s |\n", p.agent, p.name, osName, baseScore, baseClass, score, class)
			if score < baseScore {
				t.Errorf("%s %s on %s: score %d below %d without the profile", p.agent, p.name, osName, score, baseScore)
			}
		}
	}
	if out := os.Getenv("ROSMAROPS_BV237_TABLE"); out != "" {
		if err := os.WriteFile(out, []byte(table), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
