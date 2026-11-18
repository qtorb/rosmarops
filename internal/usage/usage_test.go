package usage_test

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/schemas"
	"github.com/qtorb/rosmarops/internal/usage"
)

func goodInput() usage.Input {
	score := 72
	return usage.Input{
		InstallID: "3b2c1d4e-5f60-4a7b-8c9d-0e1f2a3b4c5d", Now: time.Date(2026, 11, 18, 23, 30, 0, 0, time.FixedZone("CET", 3600)),
		Version: "0.15.0", OS: "linux", Arch: "amd64", Channel: usage.ChannelHomebrew,
		Agents: []string{"cursor", "claude-code", "cursor", "/home/albert/secret-agent", "my-own-bot"},
		Score:  &score, Class: "C", Status: "yellow",
		Findings:       map[string]int{"cred_readable": 2, "acts_without_you": 1, "my_custom_code": 3, "own_config_writable": 0},
		SpaceConnected: true, PluginInstalled: true,
	}
}

// The package has exactly the schema's keys.
func TestBuildHasExactlyTheSchemaKeys(t *testing.T) {
	p, err := usage.Build(goodInput())
	if err != nil {
		t.Fatal(err)
	}
	b, err := usage.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Required   []string       `json:"required"`
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(schemas.UsageSchema, &schema); err != nil {
		t.Fatal(err)
	}
	var keys, want, props []string
	for k := range got {
		keys = append(keys, k)
	}
	want = append(want, schema.Required...)
	for k := range schema.Properties {
		props = append(props, k)
	}
	sort.Strings(keys)
	sort.Strings(want)
	sort.Strings(props)
	if strings.Join(keys, ",") != strings.Join(want, ",") || strings.Join(keys, ",") != strings.Join(props, ",") {
		t.Errorf("keys %v, schema required %v, properties %v", keys, want, props)
	}
}

// Only the known product ids and rule codes leave; the day is UTC.
func TestBuildKeepsOnlyClosedLists(t *testing.T) {
	p, err := usage.Build(goodInput())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.Agents, ",") != "claude-code,cursor" {
		t.Errorf("agents %v", p.Agents)
	}
	if len(p.Findings) != 2 || p.Findings["cred_readable"] != 2 || p.Findings["acts_without_you"] != 1 {
		t.Errorf("findings %v", p.Findings)
	}
	if p.Day != "2026-11-18" || *p.Class != "C" || *p.Score != 72 {
		t.Errorf("day %s class %v score %v", p.Day, p.Class, p.Score)
	}
	// No score and no class: null, as the schema allows.
	in := goodInput()
	in.Score, in.Class = nil, ""
	p, err = usage.Build(in)
	if err != nil || p.Score != nil || p.Class != nil {
		t.Errorf("no score: %+v, %v", p, err)
	}
}

// A value outside its list is not sent: Build fails.
func TestBuildRefusesValuesOutsideTheirList(t *testing.T) {
	for name, mod := range map[string]func(*usage.Input){
		"install id": func(in *usage.Input) { in.InstallID = "not-a-uuid" },
		"version":    func(in *usage.Input) { in.Version = "0.15.0 on albert's laptop" },
		"os":         func(in *usage.Input) { in.OS = "freebsd" },
		"arch":       func(in *usage.Input) { in.Arch = "386" },
		"channel":    func(in *usage.Input) { in.Channel = "scoop" },
		"status":     func(in *usage.Input) { in.Status = "purple" },
		"class":      func(in *usage.Input) { in.Class = "E" },
		"score":      func(in *usage.Input) { s := 101; in.Score = &s },
	} {
		in := goodInput()
		mod(&in)
		if _, err := usage.Build(in); err == nil {
			t.Errorf("%s: built", name)
		}
	}
}

// A random UUID v4, a new one each time.
func TestNewInstallID(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	a, err := usage.NewInstallID()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := usage.NewInstallID()
	if !re.MatchString(a) || !re.MatchString(b) || a == b {
		t.Errorf("ids %q %q", a, b)
	}
}

// The products only detected (not evaluated) are sent too, and every product
// of the closed list fits the schema's pattern.
func TestBuildKeepsDetectedProducts(t *testing.T) {
	in := goodInput()
	in.Agents = []string{"claude-desktop", "antigravity", "github-copilot-jetbrains", "cline-cli", "grok-bot", "continue", "n8n", "not-a-product"}
	p, err := usage.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(p.Agents, ","); got != "antigravity,claude-desktop,cline-cli,continue,github-copilot-jetbrains,grok-bot,n8n" {
		t.Errorf("agents %s", got)
	}
	pat := regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	for _, prod := range usage.KnownProducts() {
		if !pat.MatchString(prod) {
			t.Errorf("product %q does not fit usage.v1's pattern", prod)
		}
	}
}
