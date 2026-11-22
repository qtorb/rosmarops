package report

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-274: the two MCP rules render their signed sentence and their own step,
// literally, with {server}, {package} and {scope} sanitized: a hidden
// direction character is shown, not obeyed.
func TestMCPSupplyTexts(t *testing.T) {
	unpinned := rules.Finding{Rule: rules.RuleMCPUnpinnedLaunch, Params: rules.Params{
		Agent: "Claude Code", Server: "git\u202ehub", Launcher: "npx", Package: "@modelcontextprotocol/server-github",
	}}
	auto := rules.Finding{Rule: rules.RuleMCPProjectAutoApprove, Params: rules.Params{Agent: "Claude Code", Scope: "/home/u/.claude/settings.json"}}
	for lang, want := range map[string][4]string{
		"en": {
			"Claude Code starts the MCP server «git⟨U+202E⟩hub» with npx and no fixed version: its code can change every time it starts.",
			"Pin an exact version, for example @modelcontextprotocol/server-github@1.2.3.",
			"Claude Code approves every MCP server a project brings, on its own (/home/u/.claude/settings.json).",
			"Remove enableAllProjectMcpServers and approve each server when Claude Code asks you.",
		},
		"es": {
			"Claude Code arranca el servidor MCP «git⟨U+202E⟩hub» con npx sin versión fija: el código puede cambiar cada vez que se inicia.",
			"Fija una versión exacta, por ejemplo @modelcontextprotocol/server-github@1.2.3.",
			"Claude Code aprueba solo todos los servidores MCP que traiga un proyecto (/home/u/.claude/settings.json).",
			"Quita enableAllProjectMcpServers y aprueba cada servidor cuando Claude Code te lo pregunte.",
		},
	} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		got := [4]string{
			OrchestratorTitle(cat, unpinned), cat.Text(orchestratorFixKey(unpinned), orchestratorParams(unpinned)),
			OrchestratorTitle(cat, auto), cat.Text(orchestratorFixKey(auto), orchestratorParams(auto)),
		}
		if got != want {
			t.Errorf("%s:\n got %q\nwant %q", lang, got, want)
		}
	}
}
