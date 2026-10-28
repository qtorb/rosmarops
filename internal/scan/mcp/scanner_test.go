package mcp_test

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/mcp"
)

// testHomeDir is a synthetic home directory used across all tests.
// Windows-style path; isAbsPath and toSlash handle it correctly.
const testHomeDir = "C:/Users/testuser"

// bind is a first run's binding: every allow-listed server records its
// destination and the entry applies (B9).
var bind = mcp.Binding{Salt: []byte("0123456789abcdef0123456789abcdef"), Now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}

// srv is a convenience constructor for platform.MCPServer: a server of a
// file its client loads MCP servers from.
func srv(client, name, command string, args []string) platform.MCPServer {
	return platform.MCPServer{
		Client:     client,
		Name:       name,
		Command:    command,
		Args:       args,
		EnvKeys:    nil,
		ConfigPath: testHomeDir + "/.claude.json",
		LoadsMCP:   true,
	}
}

// --- ScanMCPServers tests ---------------------------------------------------

func TestScanMCPServers_Empty(t *testing.T) {
	r := mcp.ScanMCPServers(nil, nil, testHomeDir, bind)
	if len(r.Findings) != 0 {
		t.Errorf("empty: got %d findings, want 0", len(r.Findings))
	}
	if r.AllowListedCount != 0 {
		t.Errorf("empty: allow_listed_count = %d, want 0", r.AllowListedCount)
	}
}

// Decision 2026-10-26 (agentes y MCP nuevos, A4): mcp_server_new and
// mcp_server_modified are retired; a server, new or changed, with a safe
// command gives no finding.
func TestScanMCPServers_NewAndModifiedRetired(t *testing.T) {
	s := srv("claude-code", "filesystem", "npx", []string{"-y", "@modelcontextprotocol/server-filesystem"})
	desk := srv("claude-code", "suspicious", `C:\Users\testuser\Desktop\server.exe`, nil)
	r := mcp.ScanMCPServers([]platform.MCPServer{s, desk}, nil, testHomeDir, bind)
	for _, f := range r.Findings {
		if f.Category == "mcp_server_new" || f.Category == "mcp_server_modified" {
			t.Errorf("retired category %s", f.Category)
		}
	}
	if len(r.Findings) != 1 || r.Findings[0].Category != "mcp_server_user_writable_command" {
		t.Errorf("findings %+v, want only the user-writable command", r.Findings)
	}
}

// A4: mcp_server_user_writable_command only on the servers of a file the
// client loads MCP servers from; not on ~/.claude/settings.json's.
func TestScanMCPServers_UserWritableCommand_LoadedOnly(t *testing.T) {
	loaded := srv("claude-code", "evil", `C:\Users\testuser\Downloads\server.exe`, nil)
	notLoaded := loaded
	notLoaded.ConfigPath, notLoaded.LoadsMCP = testHomeDir+"/.claude/settings.json", false
	if r := mcp.ScanMCPServers([]platform.MCPServer{notLoaded}, nil, testHomeDir, bind); len(r.Findings) != 0 {
		t.Errorf("not loaded: %d findings", len(r.Findings))
	}
	if r := mcp.ScanMCPServers([]platform.MCPServer{loaded}, nil, testHomeDir, bind); len(r.Findings) != 1 {
		t.Errorf("loaded: %d findings", len(r.Findings))
	}
}

func TestScanMCPServers_UserWritableCommand_Downloads(t *testing.T) {
	s := srv("claude-code", "evil", `C:\Users\testuser\Downloads\server.exe`, nil)
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, testHomeDir, bind)

	var found bool
	for _, f := range r.Findings {
		if f.Category == "mcp_server_user_writable_command" {
			found = true
			if f.Severity != finding.SeverityRed {
				t.Errorf("severity = %q, want red", f.Severity)
			}
			if f.Evidence.Command == "" {
				t.Error("evidence.command should be set")
			}
		}
	}
	if !found {
		t.Error("expected mcp_server_user_writable_command finding, got none")
	}
}

func TestScanMCPServers_SafeCommand_Volta(t *testing.T) {
	s := srv("claude-code", "node-mcp", `C:\Users\testuser\.volta\bin\node.exe`, []string{"server.js"})
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, testHomeDir, bind)

	for _, f := range r.Findings {
		if f.Category == "mcp_server_user_writable_command" {
			t.Errorf("volta path should not trigger user_writable_command: command=%q", s.Command)
		}
	}
}

func TestScanMCPServers_SafeCommand_NpmGlobal(t *testing.T) {
	s := srv("cline", "npm-tool", `C:\Users\testuser\AppData\Roaming\npm\some-mcp.cmd`, nil)
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, testHomeDir, bind)

	for _, f := range r.Findings {
		if f.Category == "mcp_server_user_writable_command" {
			t.Errorf("AppData\\Roaming\\npm path should not trigger user_writable_command")
		}
	}
}

func TestScanMCPServers_BareCommand_NotWritable(t *testing.T) {
	s := srv("cline", "fs", "npx", []string{"-y", "@mcp/fs"})
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, testHomeDir, bind)

	for _, f := range r.Findings {
		if f.Category == "mcp_server_user_writable_command" {
			t.Error("bare command 'npx' must not trigger mcp_server_user_writable_command")
		}
	}
}

func TestScanMCPServers_AllowListed_SuppressesAll(t *testing.T) {
	s := srv("claude-code", "dev-tool", `C:\Users\testuser\Documents\dev\mcp-server.exe`, nil) // user-writable
	allow := []config.AllowEntryID{
		{ID: "claude-code:dev-tool", Note: "intentional dev setup"},
	}
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, allow, testHomeDir, bind)

	if len(r.Findings) != 0 {
		t.Errorf("allow-listed: got %d findings, want 0", len(r.Findings))
	}
	if r.AllowListedCount != 1 {
		t.Errorf("allow_listed_count = %d, want 1", r.AllowListedCount)
	}
}

func TestScanMCPServers_AllowList_CaseInsensitive(t *testing.T) {
	s := srv("Claude-Code", "FileSystem", "npx", nil)
	allow := []config.AllowEntryID{
		{ID: "claude-code:filesystem", Note: "test"},
	}
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, allow, testHomeDir, bind)

	if r.AllowListedCount != 1 {
		t.Errorf("allow-list must be case-insensitive: allow_listed_count = %d, want 1", r.AllowListedCount)
	}
}

func TestScanMCPServers_FindingID_DeterministicAndPrefixed(t *testing.T) {
	s := srv("claude-code", "evil", `C:\Users\testuser\Desktop\server.exe`, nil)
	r1 := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, testHomeDir, bind)
	r2 := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, testHomeDir, bind)

	if len(r1.Findings) == 0 {
		t.Fatal("no findings for determinism test")
	}
	if r1.Findings[0].ID != r2.Findings[0].ID {
		t.Errorf("non-deterministic ID: %q vs %q", r1.Findings[0].ID, r2.Findings[0].ID)
	}
	if !strings.HasPrefix(r1.Findings[0].ID, "mcp_server_user_writable_command:") {
		t.Errorf("finding ID prefix: got %q", r1.Findings[0].ID)
	}
}
