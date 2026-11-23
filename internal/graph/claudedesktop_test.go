package graph

import (
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-277: Claude Desktop's MCP tools are assumed to run without asking (its
// «Always allow» approvals are not read). A managed policy that stops
// persistent «Always allow» is read as data and changes nothing; a policy
// file that failed the ownership check is a source not read.
func TestFromClaudeDesktop(t *testing.T) {
	base, err := knowledge.ClaudeDesktop()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	scan := platform.ClaudeDesktopScan{Present: true, ConfigDir: "/home/u/.config/Claude"}
	c, ok := capsOf(FromClaudeDesktop(scan, base, now))[CapInvokeMCPTool+"|*|user"]
	if !ok || c.Approval != ApprovalAllow || c.Basis != BasisProductDefault || c.Rule != "" || c.Source != "kb:claude-desktop/mcp_always_allow_assumed@"+knowledge.ClaudeDesktopVersion {
		t.Fatalf("factory: %+v", c)
	}
	no := false
	scan.Managed = platform.ClaudeDesktopManaged{Path: "/etc/claude-desktop/managed-settings.json", Status: platform.MCPSourceRead, PersistentAlwaysAllow: &no}
	f := FromClaudeDesktop(scan, base, now)
	c = capsOf(f)[CapInvokeMCPTool+"|*|user"]
	if c.Basis != BasisProductDefault || c.Rule != "" || c.Approval != ApprovalAllow {
		t.Fatalf("managed: %+v", c)
	}
	if len(f.Coverage.SourcesRead) != 1 {
		t.Errorf("the managed file is a source read: %v", f.Coverage.SourcesRead)
	}
	scan.Managed = platform.ClaudeDesktopManaged{Path: "/etc/claude-desktop/managed-settings.json", Status: platform.MCPSourceFailed, Reason: "read_error: not owned by an administrator"}
	f = FromClaudeDesktop(scan, base, now)
	if len(f.Coverage.SourcesFailed) != 1 || capsOf(f)[CapInvokeMCPTool+"|*|user"].Basis != BasisProductDefault {
		t.Fatalf("untrusted managed file: %+v", f.Coverage)
	}
	if f := FromClaudeDesktop(platform.ClaudeDesktopScan{}, base, now); len(f.Nodes) != 0 {
		t.Error("absent: no nodes")
	}
}

// isLocalDevMcpEnabled false: the config file's servers stay but load
// nothing; another client's servers are untouched.
func TestClaudeDesktopServers(t *testing.T) {
	servers := []platform.MCPServer{{Client: "claude-desktop", Name: "fs", LoadsMCP: true}, {Client: "cursor", Name: "x", LoadsMCP: true}}
	if got := ClaudeDesktopServers(servers, platform.ClaudeDesktopScan{}); !got[0].LoadsMCP {
		t.Error("no policy: loads")
	}
	no := false
	scan := platform.ClaudeDesktopScan{Managed: platform.ClaudeDesktopManaged{Status: platform.MCPSourceRead, LocalMCP: &no}}
	got := ClaudeDesktopServers(servers, scan)
	if got[0].LoadsMCP || !got[1].LoadsMCP || !servers[0].LoadsMCP {
		t.Errorf("policy off: %+v (input untouched: %v)", got, servers[0].LoadsMCP)
	}
}
