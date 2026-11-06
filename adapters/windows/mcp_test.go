package windows_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/platform"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

const twoServers = `{"mcpServers": {
  "github":   {"command": "npx", "args": ["-y", "@modelcontextprotocol/server-github"], "env": {"GITHUB_TOKEN": "x"}},
  "postgres": {"command": "npx"}
}}`

func TestReadMCPFile_Classification(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name        string
		content     *string // nil = file absent
		wantStatus  platform.MCPSourceStatus
		wantServers int
	}{
		{"with servers", ptr(twoServers), platform.MCPSourceRead, 2},
		{"no mcpServers block", ptr(`{"theme": "dark"}`), platform.MCPSourceRead, 0},
		{"empty mcpServers", ptr(`{"mcpServers": {}}`), platform.MCPSourceRead, 0},
		{"empty file", ptr(""), platform.MCPSourceRead, 0},
		{"missing", nil, platform.MCPSourceMissing, 0},
		{"malformed", ptr(`{"mcpServers": `), platform.MCPSourceFailed, 0},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, string(rune('a'+i))+".json")
			if tc.content != nil {
				writeFile(t, path, *tc.content)
			}
			servers, src := windows.ReadMCPFile(path, "cursor", true)
			if src.Status != tc.wantStatus {
				t.Errorf("status: got %q, want %q", src.Status, tc.wantStatus)
			}
			if len(servers) != tc.wantServers {
				t.Errorf("servers: got %d, want %d", len(servers), tc.wantServers)
			}
			if src.Platform != "cursor" || src.ConfigPath != path {
				t.Errorf("source identity: got %q %q, want cursor %q", src.Platform, src.ConfigPath, path)
			}
			if (src.Status == platform.MCPSourceFailed) != (src.Reason != "") {
				t.Errorf("reason %q: must be set exactly when status is failed", src.Reason)
			}
		})
	}
}

func TestReadMCPFile_ServersUnchangedForV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	writeFile(t, path, twoServers)
	servers, _ := windows.ReadMCPFile(path, "cursor", true)
	byName := map[string]platform.MCPServer{}
	for _, s := range servers {
		byName[s.Name] = s
	}
	gh := byName["github"]
	if gh.Command != "npx" || len(gh.Args) != 2 || len(gh.EnvKeys) != 1 || gh.EnvKeys[0] != "GITHUB_TOKEN" {
		t.Errorf("github server: got %+v", gh)
	}
	if pg := byName["postgres"]; pg.Args == nil || len(pg.EnvKeys) != 0 {
		t.Errorf("postgres: args must be non-nil empty and no env keys, got %+v", pg)
	}
	for _, s := range servers {
		if s.Client != "cursor" || s.ConfigPath != path {
			t.Errorf("server %s: got client %q path %q", s.Name, s.Client, s.ConfigPath)
		}
	}
}

// BV-140 (CTO-16): a server with "disabled": true is not loaded, so it never
// becomes a server node. Before, a disabled Antigravity server counted as
// loaded, claiming more than is true. (Antigravity case C.)
func TestReadMCPFile_SkipsDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_config.json")
	writeFile(t, path, `{"mcpServers": {
  "api": {"serverUrl": "https://api.example.com/mcp/", "headers": {"Authorization": "Bearer abcd1234efgh5678"}},
  "off": {"command": "node", "args": ["x.js"], "disabled": true}
}}`)
	servers, src := windows.ReadMCPFile(path, "antigravity", true)
	if src.Status != platform.MCPSourceRead {
		t.Fatalf("status: got %q", src.Status)
	}
	if len(servers) != 1 {
		t.Fatalf("got %d servers, want 1 (the disabled one is skipped)", len(servers))
	}
	if servers[0].Name != "api" {
		t.Errorf("remaining server: got %q, want api", servers[0].Name)
	}
}

func TestListMCPServers_ReportsEverySource(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))

	// Claude Code global: present, no MCP servers.
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), `{"theme": "dark"}`)
	// Claude Code project: committed file absent, local file malformed.
	writeFile(t, filepath.Join(repo, ".claude", "settings.local.json"), `{not json`)
	// Cline: absent. Cursor: two servers.
	writeFile(t, filepath.Join(home, ".cursor", "mcp.json"), twoServers)

	scan, err := windows.New().ListMCPServers(context.Background(), []string{repo})
	if err != nil {
		t.Fatalf("ListMCPServers: %v", err)
	}

	want := []struct {
		platform string
		suffix   string
		status   platform.MCPSourceStatus
	}{
		{"claude-code", filepath.Join(".claude", "settings.json"), platform.MCPSourceRead},
		{"claude-code", ".claude.json", platform.MCPSourceMissing},
		{"claude-code-project", filepath.Join(".claude", "settings.json"), platform.MCPSourceMissing},
		{"claude-code-project", filepath.Join(".claude", "settings.local.json"), platform.MCPSourceFailed},
		{"claude-code-project", ".mcp.json", platform.MCPSourceMissing},
		{"cline", "cline_mcp_settings.json", platform.MCPSourceMissing},
		{"cursor", filepath.Join(".cursor", "mcp.json"), platform.MCPSourceRead},
		{"cursor", filepath.Join(".cursor", "mcp.json"), platform.MCPSourceMissing}, // the repo's (K10)
	}
	if runtime.GOOS == "windows" {
		// BV-91 (D2): managed-mcp.json, read only on Windows (New sets the
		// managed dir there), right after the user ~/.claude.json source.
		managed := struct {
			platform string
			suffix   string
			status   platform.MCPSourceStatus
		}{"claude-code", "managed-mcp.json", platform.MCPSourceMissing}
		want = append(want[:2], append([]struct {
			platform string
			suffix   string
			status   platform.MCPSourceStatus
		}{managed}, want[2:]...)...)
	}
	if len(scan.Sources) != len(want) {
		t.Fatalf("sources: got %d %+v, want %d", len(scan.Sources), scan.Sources, len(want))
	}
	for i, w := range want {
		s := scan.Sources[i]
		if s.Platform != w.platform || !strings.HasSuffix(s.ConfigPath, w.suffix) || s.Status != w.status {
			t.Errorf("source %d: got %s %s %s, want %s …%s %s", i, s.Platform, s.ConfigPath, s.Status, w.platform, w.suffix, w.status)
		}
	}
	if len(scan.Servers) != 2 {
		t.Errorf("servers: got %d, want 2 (Cursor)", len(scan.Servers))
	}
}

func ptr(s string) *string { return &s }
