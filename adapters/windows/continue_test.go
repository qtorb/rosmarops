package windows_test

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/platform"
)

// continueEnv is clineEnv with no CONTINUE_GLOBAL_DIR.
func continueEnv(t *testing.T) string {
	t.Helper()
	home, _ := clineEnv(t)
	t.Setenv("CONTINUE_GLOBAL_DIR", "")
	return home
}

func continueServers(t *testing.T, repoRoots ...string) ([]platform.MCPServer, []platform.MCPSource) {
	t.Helper()
	scan, err := windows.New().ListMCPServers(context.Background(), repoRoots)
	if err != nil {
		t.Fatal(err)
	}
	var servers []platform.MCPServer
	for _, s := range scan.Servers {
		if s.Client == "continue" {
			servers = append(servers, s)
		}
	}
	var sources []platform.MCPSource
	for _, s := range scan.Sources {
		if s.Platform == "continue" {
			sources = append(sources, s)
		}
	}
	return servers, sources
}

func continueNames(servers []platform.MCPServer) []string {
	var out []string
	for _, s := range servers {
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}

// The user's config.yaml with comments, a pinned and an unpinned server,
// a template env value, a remote server with a key, and a hub block.
func TestContinue_UserYAML(t *testing.T) {
	home := continueEnv(t)
	writeFile(t, filepath.Join(home, ".continue", "config.yaml"), `name: Local Assistant
version: 1.0.0
schema: v1
# a comment
models:
  - name: x
    prompt: |
      a block scalar Continue allows, outside mcpServers
mcpServers:
  - name: filesystem   # pinned
    command: npx
    args:
      - "-y"
      - "@modelcontextprotocol/server-filesystem@2025.8.21"
    env:
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      LITERAL_KEY: sk-live-abcdefghijklmnop
  - name: unpinned
    command: npx
    args: ["-y", "some-mcp"]
  - name: remote
    type: streamable-http
    url: https://mcp.example.com/mcp
    apiKey: abcd1234efgh5678
  - uses: someone/hub-block
`)
	servers, sources := continueServers(t)
	if got := strings.Join(continueNames(servers), ","); got != "filesystem,remote,unpinned" {
		t.Fatalf("servers = %s (the hub block is skipped)", got)
	}
	if len(sources) != 1 || sources[0].Status != platform.MCPSourceRead || !sources[0].LoadsMCP {
		t.Fatalf("sources = %+v", sources)
	}
	for _, s := range servers {
		if s.ConfigDir != filepath.Join(home, ".continue") || s.Scope != platform.MCPScopeUser {
			t.Errorf("%s: configDir %q scope %q", s.Name, s.ConfigDir, s.Scope)
		}
		switch s.Name {
		case "filesystem":
			if s.Transport != "stdio" || len(s.Args) != 2 || s.Args[1] != "@modelcontextprotocol/server-filesystem@2025.8.21" {
				t.Errorf("filesystem: %+v", s)
			}
			if strings.Join(s.EnvKeys, ",") != "GITHUB_TOKEN,LITERAL_KEY" {
				t.Errorf("filesystem env keys: %v", s.EnvKeys)
			}
			for _, e := range s.Env {
				if e.Key == "GITHUB_TOKEN" && e.Value.Reveal() != "" {
					t.Error("a ${{ secrets.X }} template is a reference, not a value")
				}
				if e.Key == "LITERAL_KEY" && e.Value.Reveal() == "" {
					t.Error("a literal value is a credential on disk")
				}
			}
		case "remote":
			if s.Transport != "http" || s.URL != "https://mcp.example.com/mcp" || len(s.Headers) != 1 || s.Headers[0].Key != "apiKey" {
				t.Errorf("remote: transport %q url %q headers %v", s.Transport, s.URL, s.Headers)
			}
		}
	}
}

// config.json is read only when there is no config.yaml.
func TestContinue_LegacyJSONOnlyWithoutYAML(t *testing.T) {
	home := continueEnv(t)
	writeFile(t, filepath.Join(home, ".continue", "config.json"), `{
  // JSONC comments are allowed
  "experimental": {"modelContextProtocolServers": [
    {"transport": {"type": "stdio", "command": "uvx", "args": ["mcp-server-git"]}}
  ]}
}`)
	servers, _ := continueServers(t)
	if len(servers) != 1 || servers[0].Command != "uvx" || servers[0].Pointer != "/experimental/modelContextProtocolServers/0/transport" {
		t.Fatalf("legacy config.json: %+v", servers)
	}
	writeFile(t, filepath.Join(home, ".continue", "config.yaml"), "name: x\n")
	servers, sources := continueServers(t)
	if len(servers) != 0 || len(sources) != 1 || !strings.HasSuffix(sources[0].ConfigPath, "config.yaml") {
		t.Fatalf("with config.yaml, config.json is not loaded: %+v %+v", servers, sources)
	}
}

// A project's .continue/mcpServers: a YAML block file, the three JSON
// shapes, a file in a subfolder, a folder in the forced exclusions, and a
// file outside the subset (unread, reported, nothing guessed).
func TestContinue_ProjectBlockFiles(t *testing.T) {
	home := continueEnv(t)
	writeFile(t, filepath.Join(home, ".continue", "config.yaml"), "name: x\n")
	repo := t.TempDir()
	dir := filepath.Join(repo, ".continue", "mcpServers")
	writeFile(t, filepath.Join(dir, "block.yaml"), "name: Block\nversion: 0.0.1\nschema: v1\nmcpServers:\n  - name: yaml-server\n    command: node\n    args:\n      - ./server.js\n")
	writeFile(t, filepath.Join(dir, "desktop.json"), `{"mcpServers": {"desk": {"command": "npx", "args": ["-y", "desk-mcp@1.0.0"]}}}`)
	writeFile(t, filepath.Join(dir, "code.json"), `{"projects": {"/x": {"mcpServers": {"proj": {"command": "npx"}}}}, "mcpServers": {"top": {"command": "npx"}}}`)
	writeFile(t, filepath.Join(dir, "sub", "single.json"), `{"command": "docker", "args": ["run", "img"]}`)
	writeFile(t, filepath.Join(dir, "node_modules", "skipped.json"), `{"command": "npx"}`)
	writeFile(t, filepath.Join(dir, "flow.yaml"), "mcpServers:\n  - name: flow\n    command: npx\n    env: {A: b}\n")
	servers, sources := continueServers(t, repo)
	if got := strings.Join(continueNames(servers), ","); got != "desk,proj,single,top,yaml-server" {
		t.Fatalf("project servers = %s", got)
	}
	for _, s := range servers {
		if s.Scope != platform.MCPScopeProject || s.ScopePath != repo {
			t.Errorf("%s: scope %q %q", s.Name, s.Scope, s.ScopePath)
		}
	}
	var failed int
	for _, src := range sources {
		if strings.Contains(src.ConfigPath, "node_modules") {
			t.Error("a forced exclusion was entered")
		}
		if src.Status == platform.MCPSourceFailed {
			failed++
			if !strings.HasSuffix(src.ConfigPath, "flow.yaml") {
				t.Errorf("unexpected failure: %s", src.ConfigPath)
			}
		}
	}
	if failed != 1 {
		t.Errorf("flow.yaml (a flow mapping) is unread and reported: %d failures", failed)
	}
}

// No ~/.continue: nothing is read, and no permissions.yaml is ever a source.
func TestContinue_AbsentAndPermissionsNeverRead(t *testing.T) {
	home := continueEnv(t)
	if servers, sources := continueServers(t); len(servers)+len(sources) != 0 {
		t.Fatalf("absent Continue: %v %v", servers, sources)
	}
	writeFile(t, filepath.Join(home, ".continue", "permissions.yaml"), "allow:\n  - \"*\"\n")
	_, sources := continueServers(t)
	for _, s := range sources {
		if strings.HasSuffix(s.ConfigPath, "permissions.yaml") {
			t.Error("permissions.yaml must never be read")
		}
	}
}

// CONTINUE_GLOBAL_DIR moves the global folder when it is absolute.
func TestContinue_GlobalDirVariable(t *testing.T) {
	continueEnv(t)
	dir := t.TempDir()
	t.Setenv("CONTINUE_GLOBAL_DIR", dir)
	writeFile(t, filepath.Join(dir, "config.yaml"), "mcpServers:\n  - name: moved\n    command: npx\n")
	servers, _ := continueServers(t)
	if len(servers) != 1 || servers[0].Name != "moved" || servers[0].ConfigDir != dir {
		t.Fatalf("CONTINUE_GLOBAL_DIR: %+v", servers)
	}
}
