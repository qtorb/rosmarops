package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// bv103Graph is a machine where Claude Code (and Cursor), writing machine-wide,
// can change both Cursor's configuration (its mcp.json, an agent_config
// resource) and a program Cursor runs (a skill script under scripts/, an
// agent_executable resource). The two writes_config_of edges give two
// foreign_agent_config_writable findings on the same target, one per resource
// type (BV-103).
func bv103Graph(t *testing.T) report.V2 {
	t.Helper()
	salt := []byte("0123456789abcdef0123456789abcdef")
	mcp := platform.MCPScan{Sources: []platform.MCPSource{
		{Platform: "cursor", ConfigPath: "/h/.cursor/mcp.json", ConfigDir: "/h/.cursor", Status: platform.MCPSourceRead, LoadsMCP: true},
	}}
	settings := platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{{
		Scope: platform.MCPScopeUser, ConfigPath: "/h/.claude/settings.json", ConfigDir: "/h/.claude", Status: platform.MCPSourceRead,
		Permissions: platform.ClaudePermissions{DefaultMode: "bypassPermissions"},
	}}}
	f, _, err := graph.FromScans(mcp, settings, platform.CredentialFileScan{}, salt, v2Now)
	if err != nil {
		t.Fatal(err)
	}
	// A skill script Cursor runs: one agent_executable resource of Cursor.
	f = graph.AddSkills(f, platform.SkillScan{Executables: []platform.InstructionFile{
		{Product: "cursor", ConfigDir: "/h/.cursor", Path: "/h/.cursor/skills/demo/scripts/run.sh", Shared: true},
	}}, v2Now)
	g, err := graph.Infer(graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Version, HostID: graph.HostID(salt)}, v2Now), []string{"/src/a", "/src/b"}, "/h")
	if err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("ab", 32), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: []string{"/src/a", "/src/b"}, Families: [][]string{{"/src/a"}, {"/src/b"}},
		V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: v2Now,
	})
	return v
}

// BV-103: when the resource is an executable, foreign_agent_config_writable and
// own_config_writable paint their ".executable" title (and, for the foreign
// rule, "what"/"impact") instead of the configuration one. A machine with both
// a config and an executable of the same target agent shows two distinct
// titles — not the same text twice.
func TestBV103_ExecutableTitleDiffersFromConfig(t *testing.T) {
	v := bv103Graph(t)

	// The engine produces the two findings on the same target, one per type,
	// with the id/rule/severity unchanged.
	var foreignTypes, ownTypes []string
	for _, f := range v.Findings {
		if f.Rule == rules.RuleForeignAgentConfigWritable && f.Params.Agent == "Claude Code" && f.Params.TargetAgent == "Cursor" {
			foreignTypes = append(foreignTypes, f.Params.ResourceType)
			if f.Severity != rules.SeverityRed {
				t.Errorf("foreign %s severity %s, want red", f.Params.ResourceType, f.Severity)
			}
		}
		if f.Rule == rules.RuleOwnConfigWritable && f.Params.Agent == "Cursor" {
			ownTypes = append(ownTypes, f.Params.ResourceType)
		}
	}
	if !has(foreignTypes, graph.ResourceAgentConfig) || !has(foreignTypes, graph.ResourceAgentExecutable) {
		t.Fatalf("want both a config and an executable foreign finding on Cursor, got %v", foreignTypes)
	}
	if !has(ownTypes, graph.ResourceAgentConfig) || !has(ownTypes, graph.ResourceAgentExecutable) {
		t.Fatalf("want both a config and an executable own finding on Cursor, got %v", ownTypes)
	}

	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		foreign := map[string]string{"agent": "Claude Code", "target_agent": "Cursor"}
		own := map[string]string{"agent": "Cursor"}

		md := report.RenderV2Markdown(v, cat, time.UTC)
		html := renderHTML(t, lang, v)
		ai := report.RenderAIExport(v, cat, "/h")

		// Markdown / AI titles use the "finding.*"/"html.f.*" copies; HTML uses
		// "html.f.*". In every surface the executable title must appear, differ
		// from the configuration title, and be the signed executable text.
		cfgMD := cat.Text("finding.foreign_agent_config_writable.title", foreign)
		exeMD := cat.Text("finding.foreign_agent_config_writable.title.executable", foreign)
		cfgHTML := escapeHTML(cat.Text("html.f.foreign_agent_config_writable.title", foreign))
		exeHTML := escapeHTML(cat.Text("html.f.foreign_agent_config_writable.title.executable", foreign))
		ownCfgHTML := escapeHTML(cat.Text("html.f.own_config_writable.title", own))
		ownExeHTML := escapeHTML(cat.Text("html.f.own_config_writable.title.executable", own))

		if cfgMD == exeMD || cfgHTML == exeHTML || ownCfgHTML == ownExeHTML {
			t.Fatalf("%s: the executable variant must differ from the configuration title", lang)
		}

		// Markdown: both foreign titles present (the §2 findings section).
		if !strings.Contains(md, cfgMD) {
			t.Errorf("%s markdown: missing config title %q", lang, cfgMD)
		}
		if !strings.Contains(md, exeMD) {
			t.Errorf("%s markdown: missing executable title %q", lang, exeMD)
		}
		// HTML: both foreign titles and both own titles present.
		for _, want := range []string{cfgHTML, exeHTML, ownCfgHTML, ownExeHTML} {
			if !strings.Contains(html, want) {
				t.Errorf("%s html: missing %q", lang, want)
			}
		}
		// AI export reuses the HTML title text (html.f.*): the executable
		// variant appears there too.
		if !strings.Contains(ai, cat.Text("html.f.foreign_agent_config_writable.title.executable", foreign)) {
			t.Errorf("%s ai: missing executable title", lang)
		}

		// The foreign executable "impact"/"what" also take their variant.
		if !strings.Contains(html, escapeHTML(cat.Text("html.f.foreign_agent_config_writable.impact.executable", foreign))) {
			t.Errorf("%s html: missing executable impact", lang)
		}
		if !strings.Contains(md, cat.Text("finding.foreign_agent_config_writable.what.executable", foreign)) {
			t.Errorf("%s markdown: missing executable what", lang)
		}
	}
}

func has(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
