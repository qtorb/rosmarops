package trust_test

// Contract with the app (BV-256 §A.2): the usage packages the program sends
// validate against the app's own copy of usage.v1 (testdata/app, from
// qtorb/rosmarops-app a02a840), and a package with a field the app does not
// know does not. No network: the copy is vendored.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

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
