package trust_test

// Contract with the app: the usage packages the program sends
// validate against the app's own copy of usage.v1 (testdata/app, from
// qtorb/rosmarops-app a02a840), and a package with a field the app does not
// know does not. No network: the copy is vendored.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/schemaval"
	"github.com/qtorb/rosmarops/internal/usage"
)

func TestAppUsageContract(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "app", "usage.v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	app, err := schemaval.Compile(raw)
	if err != nil {
		t.Fatal(err)
	}
	score := 36
	inputs := map[string]usage.Input{
		"full": {
			InstallID: "3b2c1d4e-5f60-4a7b-8c9d-0e1f2a3b4c5d", Now: time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC),
			Version: "0.15.0", OS: "windows", Arch: "amd64", Channel: usage.ChannelStore,
			Agents: []string{"claude-code", "cursor", "hermes"}, Score: &score, Class: "C", Status: "red",
			Findings: map[string]int{"cred_read_and_send": 2, "own_config_writable": 1}, SpaceConnected: true, PluginInstalled: true,
		},
		"empty review": {
			InstallID: "3b2c1d4e-5f60-4a7b-8c9d-0e1f2a3b4c5d", Now: time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC),
			Version: "0.15.0", OS: "darwin", Arch: "arm64", Channel: usage.ChannelHomebrew, Status: "green",
		},
	}
	for name, in := range inputs {
		p, err := usage.Build(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if name == "full" && len(p.Agents) != 3 {
			t.Errorf("full: agents %v, want the three", p.Agents)
		}
		body, err := usage.Marshal(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		res, err := app.Validate(body)
		if err != nil || !res.Valid() {
			t.Errorf("%s: the app refuses the program's package %s: %v %v", name, body, err, res.Errors())
		}
		var extra map[string]any
		if err := json.Unmarshal(body, &extra); err != nil {
			t.Fatal(err)
		}
		extra["hostname"] = "x"
		eb, _ := json.Marshal(extra)
		if res, err := app.Validate(eb); err == nil && res.Valid() {
			t.Errorf("%s: the app admits a package with an extra field: %s", name, eb)
		}
	}
}

// Every product the review finds, evaluated or only detected, reaches the
// usage package: the closed list covers what the engine puts in the report
// (the owner's decision, 18-11-2026). R2-todos at factory carries every scored
// agent; Grok Bot and four products only detected are added as present.
func TestUsageAgentsCoverEveryProductFound(t *testing.T) {
	v, _ := appContractReport(t, func(in *pipeline.Input) {
		in.GrokBot = platform.GrokBotScan{Present: true}
		for _, prod := range []string{"claude-desktop", "antigravity", "github-copilot-jetbrains", "cline-cli"} {
			in.MCP.Present = append(in.MCP.Present, platform.AgentPresence{Platform: prod, ConfigDir: fcHome + "/." + prod, Marker: fcHome + "/." + prod})
		}
	})
	found := map[string]bool{}
	in := usage.Input{
		InstallID: "3b2c1d4e-5f60-4a7b-8c9d-0e1f2a3b4c5d", Now: time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC),
		Version: "0.15.0", OS: "windows", Arch: "amd64", Channel: usage.ChannelOther, Status: v.Status,
	}
	for _, o := range v.AgentsOverview {
		found[o.Product] = true
		in.Agents = append(in.Agents, o.Product)
	}
	for _, want := range []string{"grok-bot", "claude-desktop", "antigravity", "github-copilot-jetbrains", "cline-cli"} {
		if !found[want] {
			t.Fatalf("the fixture has no %s: %v", want, found)
		}
	}
	p, err := usage.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]bool{}
	for _, a := range p.Agents {
		kept[a] = true
	}
	for prod := range found {
		if !kept[prod] {
			t.Errorf("product %q found by the review is not in the usage package", prod)
		}
	}
	t.Logf("agents: %v", p.Agents)
}
