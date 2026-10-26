package windows_test

import (
	"context"
	"fmt"
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

// T12 (decision 2026-10-25, cómo trabajan tus agentes, cálculo):
// claudeCode.environmentVariables is read as {name, value} pairs, the
// values kept as secrets; a malformed list is left out.
func TestListClaudeSettings_VSCodeEnvironmentVariables(t *testing.T) {
	e := newIdentityEnv(t)
	settings := filepath.Join(e.appData, "Code", "User", "settings.json")
	writeFile(t, settings, `{"claudeCode.environmentVariables": [{"name": "CLAUDE_CODE_USE_BEDROCK", "value": "1"}, {"name": "ANTHROPIC_BASE_URL", "value": "https://gw.example.com"}]}`)
	scan, err := windows.New().ListClaudeSettings(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if scan.VSCode == nil || len(scan.VSCode.Env) != 2 || scan.VSCode.Env[0].Key != "CLAUDE_CODE_USE_BEDROCK" ||
		scan.VSCode.Env[0].Value.Reveal() != "1" || scan.VSCode.Env[1].Key != "ANTHROPIC_BASE_URL" {
		t.Fatalf("env %+v", scan.VSCode)
	}
	if strings.Contains(fmt.Sprintf("%v %+v", scan.VSCode.Env, scan.VSCode.Env), "gw.example.com") {
		t.Error("a value printed in clear")
	}
	writeFile(t, settings, `{"claudeCode.environmentVariables": {"CLAUDE_CODE_USE_BEDROCK": "1"}}`)
	if scan, err = windows.New().ListClaudeSettings(context.Background(), nil); err != nil || scan.VSCode == nil || scan.VSCode.Env != nil {
		t.Errorf("malformed list: %+v %v", scan.VSCode, err)
	}
}
