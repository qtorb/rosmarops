package trust_test

// Contract with the app (advisor, signed by Albert 17-11): the space-view
// packages the program sends validate against the app's own copy of
// space-view.v1 (testdata/app, from qtorb/rosmarops-app c411b3d). The test
// fails if the program sends something the app does not admit. No network:
// the copy is vendored.
//
// The packages exercise what the app must accept: the R2-todos computer at
// factory; and R2-todos with Cursor and Grok Bot declared and Hermes Agent's
// gateway on, which carries declared, orders, the always_on group and an
// acts_without_you finding.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/internal/declared"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/schemaval"
)

// appContractReport is R2-todos with mod applied, built as `rosmarops run`
// builds it, with the person's answers and Grok Bot passed to the report.
func appContractReport(t *testing.T, mod func(*pipeline.Input)) (report.V2, graph.Graph) {
	t.Helper()
	in := r2AllInput()
	if mod != nil {
		mod(&in)
	}
	g := r2Assemble(t, in)
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	settings := r2Settings(platform.ClaudeSettingsFile{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead})
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings0(64), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: []string{fcRepo}, V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: fcNow,
		ClaudeSettings: &settings, Home: fcHome,
		Codex: in.Codex, Grok: in.Grok, Cline: in.Cline, Hermes: in.Hermes, FactoryGraph: r2FactoryOf(t),
		Declared: in.Declared, GrokBot: in.GrokBot, Cursor: in.Cursor,
	})
	return v, g
}

func TestAppSpaceViewContract(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "app", "space-view.v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	app, err := schemaval.Compile(raw)
	if err != nil {
		t.Fatalf("the app's space-view.v1 does not compile: %v", err)
	}

	alwaysOn := func(in *pipeline.Input) {
		// Cursor and Grok Bot with the person's answers (factory answers).
		in.Declared = map[string]map[string]string{
			declared.Cursor:  declared.Factory(declared.Cursor),
			declared.GrokBot: declared.Factory(declared.GrokBot),
		}
		in.Cursor.Declared = in.Declared[declared.Cursor]
		in.GrokBot = platform.GrokBotScan{Present: true}
		// Hermes Agent's gateway on: acts_without_you, orders, always_on.
		for i := range in.Hermes.Configs {
			in.Hermes.Configs[i].GatewayPlatforms = []platform.HermesPlatform{{Name: "telegram", Enabled: true, EnabledSet: true}}
		}
	}
	cases := []struct {
		name string
		mod  func(*pipeline.Input)
	}{
		{"R2-todos at factory", nil},
		{"R2-todos with Cursor and Grok Bot declared and Hermes's gateway on", alwaysOn},
	}
	seen := map[string]bool{}
	for _, c := range cases {
		v, g := appContractReport(t, c.mod)
		sv, err := report.BuildSpaceView(v, &g, fcHome, "0.13.0-contract")
		if err != nil {
			t.Fatalf("%s: BuildSpaceView: %v", c.name, err)
		}
		data, err := report.RenderSpaceViewJSON(sv)
		if err != nil {
			t.Fatalf("%s: the program's own space-view.v1 rejects the package: %v", c.name, err)
		}
		res, err := app.Validate(data)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Valid() {
			t.Errorf("%s: the app does not admit the package: %v", c.name, res.Errors())
		}

		// What the package carries, so the contract covers the new fields.
		var doc struct {
			Agents []struct {
				ID       string          `json:"id"`
				Declared *bool           `json:"declared"`
				Orders   json.RawMessage `json:"orders"`
				Groups   []struct {
					Group string `json:"group"`
				} `json:"groups"`
			} `json:"agents"`
			Findings []struct {
				Rule string `json:"rule"`
			} `json:"findings"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		for _, a := range doc.Agents {
			if a.Declared != nil && *a.Declared {
				seen["declared:"+a.ID] = true
			}
			if len(a.Orders) > 0 {
				seen["orders"] = true
			}
			for _, gr := range a.Groups {
				if gr.Group == "always_on" {
					seen["always_on"] = true
				}
			}
		}
		for _, f := range doc.Findings {
			if f.Rule == rules.RuleActsWithoutYou {
				seen["acts_without_you"] = true
			}
		}
	}
	declaredAgents := 0
	for k := range seen {
		if len(k) > 9 && k[:9] == "declared:" {
			declaredAgents++
		}
	}
	if declaredAgents < 2 {
		t.Errorf("the packages carry declared for %d agents, want Cursor and Grok Bot (2)", declaredAgents)
	}
	for _, k := range []string{"orders", "always_on", "acts_without_you"} {
		if !seen[k] {
			t.Errorf("no package carries %s: the contract does not exercise it", k)
		}
	}
}
