package windows_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-32 D7: claudeCode.initialPermissionMode of VS Code's user settings is a
// configured permission_mode of layer ide:vscode; the terminal's effective
// mode ignores it.
func TestListClaudeSettings_VSCodeInitialPermissionMode(t *testing.T) {
	e := newIdentityEnv(t)
	settings := filepath.Join(e.appData, "Code", "User", "settings.json")

	cases := []struct {
		name, content string
		mode          string
		failed        bool
	}{
		{"set", `{"editor.fontSize": 14, "claudeCode.initialPermissionMode": "bypassPermissions"}`, "bypassPermissions", false},
		{"unset", `{"editor.fontSize": 14}`, "", false},
		{"comments", "{\n  // VS Code allows comments\n  \"claudeCode.initialPermissionMode\": \"acceptEdits\"\n}", "", true},
	}
	for _, c := range cases {
		writeFile(t, settings, c.content)
		scan, err := windows.New().ListClaudeSettings(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		f, _, err := graph.FromScan(platform.MCPScan{}, scan, testSalt, testTime)
		if err != nil {
			t.Fatal(err)
		}
		var caps []graph.Node
		for _, n := range f.Nodes {
			if n.Kind == graph.KindCapability && n.AppliesIn == "ide:vscode" {
				caps = append(caps, n)
			}
		}
		var failed bool
		for _, sf := range f.Coverage.SourcesFailed {
			failed = failed || (sf.Path == settings && strings.HasPrefix(sf.Reason, graph.ReasonParseError+": "))
		}
		switch {
		case c.mode == "" && len(caps) != 0, c.mode != "" && (len(caps) != 1 || caps[0].TargetScope != c.mode ||
			caps[0].Type != graph.CapPermissionMode || caps[0].Basis != graph.BasisConfigured ||
			caps[0].Rule != "claudeCode.initialPermissionMode="+c.mode):
			t.Errorf("%s: capabilities %+v", c.name, caps)
		case failed != c.failed:
			t.Errorf("%s: coverage %+v", c.name, f.Coverage.SourcesFailed)
		}
		if _, err := graph.Encode(graph.Build(f, graph.Provenance{EngineVersion: "x", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: "unset"}, testTime)); err != nil {
			t.Errorf("%s: Encode: %v", c.name, err)
		}
	}
}
