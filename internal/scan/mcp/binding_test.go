package mcp_test

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/mcp"
)

// B9 (decision 2026-10-27, blindaje): an allow_list.mcp_servers entry
// applies only while the server points where it pointed when the entry was
// accepted; a later accepted_at accepts the new destination.
func TestAllowListBoundToDestination(t *testing.T) {
	writable := `C:\Users\testuser\Documents\dev\mcp-server.exe`
	s := srv("claude-code", "dev-tool", writable, []string{"--port", "1"})
	allow := []config.AllowEntryID{{ID: "claude-code:dev-tool", Note: "intentional dev setup"}}

	// First run: the destination is recorded, the entry applies and hides
	// the warning, counted with its entry.
	r1 := mcp.ScanMCPServers([]platform.MCPServer{s}, allow, testHomeDir, bind)
	if len(r1.Findings) != 0 || len(r1.Hits) != 1 || r1.Hits[0] != (config.AllowHit{List: config.ListMCPServers, ID: "claude-code:dev-tool", Note: "intentional dev setup"}) {
		t.Fatalf("first run: findings %d, hits %+v", len(r1.Findings), r1.Hits)
	}
	if len(r1.NewBaseline) != 1 || !strings.HasPrefix(r1.NewBaseline[0].DestinationRef, "hmac-sha256:") || len(r1.NewBaseline[0].CommandSHA256) != 64 {
		t.Fatalf("baseline %+v", r1.NewBaseline)
	}
	if strings.Contains(r1.NewBaseline[0].CommandSHA256, "mcp-server") {
		t.Error("command in clear")
	}

	// Same destination: still applies.
	next := bind
	next.Baseline, next.Now = r1.NewBaseline, bind.Now.Add(time.Hour)
	if r := mcp.ScanMCPServers([]platform.MCPServer{s}, allow, testHomeDir, next); len(r.Findings) != 0 || len(r.Hits) != 1 {
		t.Errorf("same destination: findings %d, hits %d", len(r.Findings), len(r.Hits))
	}

	// Another argument: the server points somewhere else; the entry no
	// longer applies, the warning returns, and the accepted destination is
	// kept so that it keeps returning.
	moved := s
	moved.Args = []string{"--port", "2"}
	r2 := mcp.ScanMCPServers([]platform.MCPServer{moved}, allow, testHomeDir, next)
	if len(r2.Findings) != 1 || len(r2.Hits) != 0 || r2.AllowListedCount != 0 {
		t.Errorf("moved: findings %d, hits %d, allow-listed %d", len(r2.Findings), len(r2.Hits), r2.AllowListedCount)
	}
	if len(r2.NewBaseline) != 1 || r2.NewBaseline[0] != r1.NewBaseline[0] {
		t.Errorf("moved: baseline %+v", r2.NewBaseline)
	}
	next.Baseline = r2.NewBaseline
	if r := mcp.ScanMCPServers([]platform.MCPServer{moved}, allow, testHomeDir, next); len(r.Findings) != 1 {
		t.Error("moved, next run: the warning must keep returning")
	}

	// Accepted again, after the recorded destination: the new one applies.
	again := []config.AllowEntryID{{ID: "claude-code:dev-tool", Note: "moved to port 2", AcceptedAt: bind.Now.Add(30 * time.Minute).Format(time.RFC3339)}}
	r3 := mcp.ScanMCPServers([]platform.MCPServer{moved}, again, testHomeDir, next)
	if len(r3.Findings) != 0 || len(r3.NewBaseline) != 1 || r3.NewBaseline[0].DestinationRef == r1.NewBaseline[0].DestinationRef {
		t.Errorf("accepted again: findings %d, baseline %+v", len(r3.Findings), r3.NewBaseline)
	}
	// An accepted_at before the recorded destination changes nothing.
	old := []config.AllowEntryID{{ID: "claude-code:dev-tool", Note: "x", AcceptedAt: bind.Now.Add(-time.Hour).Format(time.RFC3339)}}
	if r := mcp.ScanMCPServers([]platform.MCPServer{moved}, old, testHomeDir, next); len(r.Findings) != 1 {
		t.Error("an older accepted_at must not accept the new destination")
	}
}

// Without the salt nothing can be checked: no entry applies, and the
// recorded destinations are kept.
func TestAllowListWithoutSalt(t *testing.T) {
	s := srv("claude-code", "dev-tool", `C:\Users\testuser\Documents\dev\mcp-server.exe`, nil)
	allow := []config.AllowEntryID{{ID: "claude-code:dev-tool", Note: "intentional dev setup"}}
	first := mcp.ScanMCPServers([]platform.MCPServer{s}, allow, testHomeDir, bind)
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, allow, testHomeDir, mcp.Binding{Baseline: first.NewBaseline})
	if len(r.Findings) != 1 || len(r.Hits) != 0 || len(r.NewBaseline) != 1 {
		t.Errorf("no salt: findings %d, hits %d, baseline %d", len(r.Findings), len(r.Hits), len(r.NewBaseline))
	}
}
