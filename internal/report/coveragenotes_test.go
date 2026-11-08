package report

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
)

// BV-193 D3 (CTO-22 Claude Desktop §2, §7 D3): when Claude Desktop loads MCP
// servers from two configuration files whose server sets differ (the standard
// install and the Microsoft Store/MSIX install), the report carries the
// claude_desktop_two_configs coverage note; two files with the same servers,
// or only one file, do not.
func TestClaudeDesktopTwoConfigs(t *testing.T) {
	const (
		installer = `C:\Users\a\AppData\Roaming\Claude\claude_desktop_config.json`
		msix      = `C:\Users\a\AppData\Local\Packages\Claude_x\LocalCache\Roaming\Claude\claude_desktop_config.json`
	)
	mcp := func(id, name, cfg, platform string) graph.Node {
		return graph.Node{ID: id, Kind: graph.KindAgent, Type: graph.AgentMCPServer, Name: name, ConfigPath: cfg, Platform: platform}
	}
	cases := []struct {
		name  string
		nodes []graph.Node
		want  bool
	}{
		{"two files, different servers", []graph.Node{
			mcp("s1", "brave", installer, "claude-desktop"),
			mcp("s2", "github", msix, "claude-desktop"),
		}, true},
		{"two files, same servers", []graph.Node{
			mcp("s1", "brave", installer, "claude-desktop"),
			mcp("s2", "brave", msix, "claude-desktop"),
		}, false},
		{"one file only", []graph.Node{
			mcp("s1", "brave", installer, "claude-desktop"),
		}, false},
		{"other agent's two files ignored", []graph.Node{
			mcp("c1", "brave", installer, "cursor"),
			mcp("c2", "github", msix, "cursor"),
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := graph.Graph{Nodes: tc.nodes}
			if got := claudeDesktopTwoConfigs(g); got != tc.want {
				t.Errorf("claudeDesktopTwoConfigs = %v, want %v", got, tc.want)
			}
			notes := coverageNotes(g)
			has := len(notes) == 1 && notes[0] == CoverageNoteClaudeDesktopTwoConfigs
			if has != tc.want {
				t.Errorf("coverageNotes = %v, want note present = %v", notes, tc.want)
			}
		})
	}
}
