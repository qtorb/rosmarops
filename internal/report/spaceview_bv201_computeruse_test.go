package report_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

// TestBV201ComputerUseManyAppsValidates (BV-201): a Codex Computer Use config
// that allows many desktop apps yields one yellow computer_use_app_allowed
// finding whose {apps} list is long. Its title is that list, and the space
// view's findings[].text.*.title is capped at 200 by space-view.v1. Before the
// fix the title overflowed and the WHOLE package failed schema validation
// (findings.0.text.en.title / findings.0.text.es.title) and was silently
// dropped — the 0.9.0 and 0.10.0 symptom. The generator now clips the finding
// text to the schema limit, so the package validates and is sent.
func TestBV201ComputerUseManyAppsValidates(t *testing.T) {
	salt := []byte("0123456789abcdef0123456789abcdef")
	codexDir := "/h/.codex"
	apps := []string{
		"mspaint.exe", "notepad.exe", "calc.exe", "snippingtool.exe", "write.exe",
		"wordpad.exe", "paint.net.exe", "gimp-2.10.exe", "inkscape.exe", "obs64.exe",
		"vlc.exe", "spotify.exe", "slack.exe", "discord.exe", "zoom.exe",
	}
	user := platform.CodexLayer{
		Kind: platform.CodexLayerUser, Path: codexDir + "/config.toml", Status: platform.MCPSourceRead,
		ComputerUse: &platform.CodexComputerUse{Allowed: apps},
	}
	scan := platform.CodexScan{ConfigDir: codexDir, Exists: true, Layers: []platform.CodexLayer{user}}

	mcp := platform.MCPScan{Servers: graph.CodexMCPServers(scan)}
	f, _, err := graph.FromScansWithCodex(mcp, platform.ClaudeSettingsScan{}, platform.CursorScan{}, platform.CredentialFileScan{}, graph.Repos{}, platform.HermesScan{}, scan, salt, v2Now)
	if err != nil {
		t.Fatal(err)
	}
	g, err := graph.Infer(graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(salt)}, v2Now), nil, "/h")
	if err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	v1 := report.Build("t", nil, nil, 0, 0, 0, 0, nil)
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("ab", 32), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		V1: v1, GeneratedAt: v2Now, Home: "/h", Codex: scan,
	})
	v.OS = "windows"

	sv, err := report.BuildSpaceView(v, &g, "/h", "0.10.0")
	if err != nil {
		t.Fatalf("BuildSpaceView: %v", err)
	}
	found := false
	for _, fnd := range sv.Findings {
		if fnd.Rule != "computer_use_app_allowed" {
			continue
		}
		found = true
		for lang, txt := range fnd.Text {
			if n := len([]rune(txt.Title)); n > 200 {
				t.Errorf("%s title not clamped: %d runes", lang, n)
			}
		}
	}
	if !found {
		t.Fatal("no computer_use_app_allowed finding was produced")
	}
	if _, err := report.RenderSpaceViewJSON(sv); err != nil {
		t.Fatalf("space view did not validate (BV-201 regression): %v", err)
	}
}
