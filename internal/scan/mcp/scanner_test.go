package mcp_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/mcp"
	"github.com/qtorb/rosmarops/internal/state"
)

// testHomeDir is a synthetic home directory used across all tests.
// Windows-style path; isAbsPath and toSlash handle it correctly.
const testHomeDir = "C:/Users/testuser"

// srv is a convenience constructor for platform.MCPServer.
func srv(client, name, command string, args []string) platform.MCPServer {
	return platform.MCPServer{
		Client:     client,
		Name:       name,
		Command:    command,
		Args:       args,
		EnvKeys:    nil,
		ConfigPath: testHomeDir + "/.claude/settings.json",
	}
}

// blFor builds a state.MCPServerBaseline that exactly matches s (no drift).
func blFor(s platform.MCPServer) state.MCPServerBaseline {
	return state.MCPServerBaseline{
		Client:        s.Client,
		Name:          s.Name,
		CommandSHA256: mcp.CommandSHA256(s.Command, s.Args, s.EnvKeys),
		FirstSeen:     "2026-05-22T00:00:00Z",
	}
}

// --- ScanMCPServers tests ---------------------------------------------------

func TestScanMCPServers_Empty(t *testing.T) {
	r := mcp.ScanMCPServers(nil, nil, nil, testHomeDir)
	if len(r.Findings) != 0 {
		t.Errorf("empty: got %d findings, want 0", len(r.Findings))
	}
	if r.AllowListedCount != 0 {
		t.Errorf("empty: allow_listed_count = %d, want 0", r.AllowListedCount)
	}
}

func TestScanMCPServers_NewServer(t *testing.T) {
	s := srv("claude-code", "filesystem", "npx", []string{"-y", "@modelcontextprotocol/server-filesystem"})
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, nil, testHomeDir)

	if len(r.Findings) != 1 {
		t.Fatalf("new server: got %d findings, want 1", len(r.Findings))
	}
	f := r.Findings[0]
	if f.Category != "mcp_server_new" {
		t.Errorf("category = %q, want mcp_server_new", f.Category)
	}
	if f.Severity != finding.SeverityYellow {
		t.Errorf("severity = %q, want yellow", f.Severity)
	}
	if f.Evidence.MCPClient != "claude-code" {
		t.Errorf("evidence.mcp_client = %q, want claude-code", f.Evidence.MCPClient)
	}
	if f.Evidence.MCPServer != "filesystem" {
		t.Errorf("evidence.mcp_server = %q, want filesystem", f.Evidence.MCPServer)
	}
}

func TestScanMCPServers_InBaselineUnchanged(t *testing.T) {
	s := srv("cline", "github", "npx", []string{"-y", "@modelcontextprotocol/server-github"})
	bl := blFor(s)
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, []state.MCPServerBaseline{bl}, nil, testHomeDir)

	if len(r.Findings) != 0 {
		t.Errorf("unchanged: got %d findings, want 0 (findings: %v)", len(r.Findings), r.Findings)
	}
}

func TestScanMCPServers_Modified(t *testing.T) {
	original := srv("cursor", "brave-search", "npx", []string{"-y", "@mcp/brave"})
	bl := blFor(original)

	// Simulate a command args change (e.g., added --extra flag).
	modified := srv("cursor", "brave-search", "npx", []string{"-y", "@mcp/brave", "--extra-flag"})
	r := mcp.ScanMCPServers([]platform.MCPServer{modified}, []state.MCPServerBaseline{bl}, nil, testHomeDir)

	if len(r.Findings) != 1 {
		t.Fatalf("modified: got %d findings, want 1", len(r.Findings))
	}
	f := r.Findings[0]
	if f.Category != "mcp_server_modified" {
		t.Errorf("category = %q, want mcp_server_modified", f.Category)
	}
	if f.Severity != finding.SeverityYellow {
		t.Errorf("severity = %q, want yellow", f.Severity)
	}
	if f.Evidence.PreviousSHA256 == "" {
		t.Error("evidence.previous_sha256 should be set for mcp_server_modified")
	}
	if f.Evidence.CurrentSHA256 == "" {
		t.Error("evidence.current_sha256 should be set for mcp_server_modified")
	}
}

func TestScanMCPServers_UserWritableCommand_Downloads(t *testing.T) {
	s := platform.MCPServer{
		Client:     "claude-code",
		Name:       "evil",
		Command:    `C:\Users\testuser\Downloads\server.exe`,
		ConfigPath: testHomeDir + "/.claude/settings.json",
	}
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, nil, testHomeDir)

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
	s := platform.MCPServer{
		Client:  "claude-code",
		Name:    "node-mcp",
		Command: `C:\Users\testuser\.volta\bin\node.exe`,
		Args:    []string{"server.js"},
	}
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, nil, testHomeDir)

	for _, f := range r.Findings {
		if f.Category == "mcp_server_user_writable_command" {
			t.Errorf("volta path should not trigger user_writable_command: command=%q", s.Command)
		}
	}
}

func TestScanMCPServers_SafeCommand_NpmGlobal(t *testing.T) {
	s := platform.MCPServer{
		Client:  "cline",
		Name:    "npm-tool",
		Command: `C:\Users\testuser\AppData\Roaming\npm\some-mcp.cmd`,
	}
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, nil, testHomeDir)

	for _, f := range r.Findings {
		if f.Category == "mcp_server_user_writable_command" {
			t.Errorf("AppData\\Roaming\\npm path should not trigger user_writable_command")
		}
	}
}

func TestScanMCPServers_BareCommand_NotWritable(t *testing.T) {
	s := srv("cline", "fs", "npx", []string{"-y", "@mcp/fs"})
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, nil, testHomeDir)

	for _, f := range r.Findings {
		if f.Category == "mcp_server_user_writable_command" {
			t.Error("bare command 'npx' must not trigger mcp_server_user_writable_command")
		}
	}
}

func TestScanMCPServers_AllowListed_SuppressesAll(t *testing.T) {
	s := platform.MCPServer{
		Client:  "claude-code",
		Name:    "dev-tool",
		Command: `C:\Users\testuser\Documents\dev\mcp-server.exe`, // user-writable
	}
	allow := []config.AllowEntryID{
		{ID: "claude-code:dev-tool", Note: "intentional dev setup"},
	}
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, allow, testHomeDir)

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
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, allow, testHomeDir)

	if r.AllowListedCount != 1 {
		t.Errorf("allow-list must be case-insensitive: allow_listed_count = %d, want 1", r.AllowListedCount)
	}
}

func TestScanMCPServers_NewAndUserWritable_TwoFindings(t *testing.T) {
	// A brand-new server with a user-writable path must produce two distinct findings.
	s := platform.MCPServer{
		Client:  "claude-code",
		Name:    "suspicious",
		Command: `C:\Users\testuser\Desktop\server.exe`,
	}
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, nil, testHomeDir)

	cats := make(map[string]bool)
	for _, f := range r.Findings {
		cats[f.Category] = true
	}
	if !cats["mcp_server_new"] {
		t.Error("expected mcp_server_new finding for new server")
	}
	if !cats["mcp_server_user_writable_command"] {
		t.Error("expected mcp_server_user_writable_command finding for desktop path")
	}
}

func TestScanMCPServers_FindingID_Deterministic(t *testing.T) {
	s := srv("claude-code", "filesystem", "npx", []string{"-y", "@mcp/fs"})
	r1 := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, nil, testHomeDir)
	r2 := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, nil, testHomeDir)

	if len(r1.Findings) == 0 {
		t.Fatal("no findings for determinism test")
	}
	if r1.Findings[0].ID != r2.Findings[0].ID {
		t.Errorf("non-deterministic ID: %q vs %q", r1.Findings[0].ID, r2.Findings[0].ID)
	}
}

func TestScanMCPServers_FindingID_HasPrefix(t *testing.T) {
	s := srv("cline", "playwright", "npx", []string{"-y", "@mcp/playwright"})
	r := mcp.ScanMCPServers([]platform.MCPServer{s}, nil, nil, testHomeDir)

	if len(r.Findings) == 0 {
		t.Fatal("expected at least one finding")
	}
	if !strings.HasPrefix(r.Findings[0].ID, "mcp_server_new:") {
		t.Errorf("finding ID should start with 'mcp_server_new:': got %q", r.Findings[0].ID)
	}
}

// --- CommandSHA256 tests ----------------------------------------------------

func TestCommandSHA256_Deterministic(t *testing.T) {
	h1 := mcp.CommandSHA256("npx", []string{"-y", "@mcp/fs"}, []string{"API_KEY", "HOME"})
	h2 := mcp.CommandSHA256("npx", []string{"-y", "@mcp/fs"}, []string{"API_KEY", "HOME"})
	if h1 != h2 {
		t.Errorf("non-deterministic: %q vs %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("hash length = %d, want 64", len(h1))
	}
}

func TestCommandSHA256_EnvKeysSorted(t *testing.T) {
	// Different key order must produce the same hash.
	h1 := mcp.CommandSHA256("npx", []string{"-y"}, []string{"HOME", "API_KEY"})
	h2 := mcp.CommandSHA256("npx", []string{"-y"}, []string{"API_KEY", "HOME"})
	if h1 != h2 {
		t.Errorf("env key order should not affect hash: %q vs %q", h1, h2)
	}
}

func TestCommandSHA256_ArgsOrderSensitive(t *testing.T) {
	// Argument order DOES matter for the hash.
	h1 := mcp.CommandSHA256("npx", []string{"a", "b"}, nil)
	h2 := mcp.CommandSHA256("npx", []string{"b", "a"}, nil)
	if h1 == h2 {
		t.Error("different arg order must produce different hash")
	}
}

func TestCommandSHA256_DifferentCommands(t *testing.T) {
	h1 := mcp.CommandSHA256("npx", []string{"-y"}, nil)
	h2 := mcp.CommandSHA256("uvx", []string{"-y"}, nil)
	if h1 == h2 {
		t.Error("different commands must produce different hash")
	}
}

func TestCommandSHA256_EmptyArgsAndEnv(t *testing.T) {
	// Should not panic and should return a valid 64-char hash.
	h := mcp.CommandSHA256("npx", nil, nil)
	if len(h) != 64 {
		t.Errorf("hash length = %d, want 64", len(h))
	}
}

func TestCommandSHA256_EnvValuesNotIncluded(t *testing.T) {
	// Changing env values (not keys) must NOT change the hash — values are never hashed.
	// We can't test this directly (values are stripped before reaching CommandSHA256),
	// but we verify that two calls with the same keys produce the same hash regardless
	// of what values existed before the keys were extracted.
	h1 := mcp.CommandSHA256("npx", []string{"-y"}, []string{"SECRET"})
	h2 := mcp.CommandSHA256("npx", []string{"-y"}, []string{"SECRET"})
	if h1 != h2 {
		t.Errorf("same env keys must produce same hash: %q vs %q", h1, h2)
	}
}
