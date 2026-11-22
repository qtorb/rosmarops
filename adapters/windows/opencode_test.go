package windows

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/platform"
)

const ocSecret = "q8Zr3mV1xT9kLw2pN7cY-OCSECRET"

func TestReadOpenCode(t *testing.T) {
	root := t.TempDir()
	home, repo, xdg := filepath.Join(root, "home"), filepath.Join(root, "repo"), filepath.Join(root, "xdg")
	pd := filepath.Join(root, "ProgramData")
	env := func(k string) (string, bool) {
		switch k {
		case "XDG_CONFIG_HOME":
			return xdg, true
		case "ProgramData":
			return pd, true
		}
		return "", false
	}
	put(t, filepath.Join(xdg, "opencode", "opencode.jsonc"), `{
  // the user's settings
  "$schema": "https://opencode.ai/config.json",
  "permission": { "bash": { "*": "ask", "git status": "allow" }, "edit": "allow", },
  "mcp": {
    "gh": { "type": "local", "command": ["npx", "-y", "@modelcontextprotocol/server-github"], "environment": { "GITHUB_TOKEN": "`+ocSecret+`", "REF_TOKEN": "{env:GH_TOKEN}" } },
    "pinned": { "type": "local", "command": ["npx", "-y", "pkg@1.2.3"] },
    "api": { "type": "remote", "url": "https://mcp.example.com/mcp", "headers": { "Authorization": "Bearer {file:~/.key}" } },
    "old": { "type": "local", "command": ["uvx", "tool"] }
  }
}`)
	put(t, filepath.Join(repo, "opencode.json"), `{"permission": "allow", "mcp": {"old": {"enabled": false}}}`)
	put(t, filepath.Join(repo, ".opencode", "opencode.jsonc"), `{"permission": {"webfetch": "deny"}}`)
	put(t, filepath.Join(pd, "opencode", "opencode.json"), `{"permission": {"bash": "deny"}}`)
	put(t, filepath.Join(xdg, "opencode", "opencode.db"), ocSecret)

	scan := ReadOpenCode(agentdirs.WindowsLayout, home, env, []string{repo}, agentdirs.OpenCode)
	raw, _ := json.Marshal(scan)
	if strings.Contains(string(raw), "OCSECRET") {
		t.Fatalf("a secret value reached the scan: %s", raw)
	}
	if !scan.Present || scan.ConfigDir != filepath.Join(xdg, "opencode") {
		t.Fatalf("scan %+v", scan)
	}
	var kinds []string
	for _, l := range scan.Layers {
		if l.Status != platform.MCPSourceRead {
			t.Errorf("%s: %s %s", l.Path, l.Status, l.Reason)
		}
		kinds = append(kinds, l.Kind+":"+filepath.Base(l.Path))
	}
	if strings.Join(kinds, ",") != "user:opencode.jsonc,project:opencode.json,project:opencode.jsonc,managed:opencode.json" {
		t.Errorf("layers %v (lowest precedence first)", kinds)
	}
	u := scan.Layers[0].Permission
	if u["bash"].Default() != "ask" || u["bash"].Patterns["git status"] != "allow" || u["edit"].Default() != "allow" {
		t.Errorf("user permission %+v", u)
	}
	if p := scan.Layers[1].Permission; p["*"].Default() != "allow" {
		t.Errorf("\"permission\": \"allow\" is {\"*\": \"allow\"}: %+v", p)
	}
	byName := map[string]platform.MCPServer{}
	for _, s := range scan.Servers {
		byName[s.Name] = s
	}
	if _, ok := byName["old"]; ok || len(scan.Servers) != 3 {
		t.Errorf("servers %+v (old is turned off by the project)", byName)
	}
	gh := byName["gh"]
	if gh.Command != "npx" || strings.Join(gh.Args, " ") != "-y @modelcontextprotocol/server-github" || gh.Transport != "stdio" ||
		len(gh.Env) != 1 || gh.Env[0].Key != "GITHUB_TOKEN" || gh.Scope != platform.MCPScopeUser || gh.Pointer != "/mcp/gh" || !gh.LoadsMCP {
		t.Errorf("gh %+v (the {env:} reference is dropped)", gh)
	}
	if api := byName["api"]; api.URL != "https://mcp.example.com/mcp" || api.Transport != "http" || len(api.Headers) != 0 {
		t.Errorf("api %+v", api)
	}
}

func TestReadKiloCode(t *testing.T) {
	root := t.TempDir()
	home, repo := filepath.Join(root, "home"), filepath.Join(root, "repo")
	env := func(k string) (string, bool) {
		if k == "XDG_CONFIG_HOME" {
			return filepath.Join(home, ".config"), true
		}
		return "", false
	}
	put(t, filepath.Join(home, ".config", "kilo", "kilo.jsonc"), `{"permission": {"*": {"*": "allow"}}, "mcp": {"fs": {"type": "local", "command": ["bunx", "fs-mcp"], "env": {"API_KEY": "`+ocSecret+`"}}}}`)
	put(t, filepath.Join(repo, ".kilo", "kilo.jsonc"), `{"permission": {"bash": "ask"}}`)
	put(t, filepath.Join(repo, ".kilocode", "mcp.json"), `{"mcpServers": {"legacy": {"command": "uvx", "args": ["legacy-tool"]}, "fs": {"command": "x"}, "off": {"command": "y", "disabled": true}}}`)
	put(t, filepath.Join(home, ".config", "Code", "User", "globalStorage", "kilocode.kilo-code", "settings", "mcp_settings.json"), `{"mcpServers": {"ext": {"url": "https://e.example/mcp"}}}`)

	scan := ReadOpenCode(agentdirs.LinuxLayout, home, env, []string{repo}, agentdirs.KiloCode)
	raw, _ := json.Marshal(scan)
	if strings.Contains(string(raw), "OCSECRET") {
		t.Fatalf("a secret value reached the scan: %s", raw)
	}
	if !scan.Present || scan.Product != agentdirs.KiloCode || scan.Layers[0].Permission["*"].Patterns["*"] != "allow" {
		t.Fatalf("scan %+v", scan)
	}
	byName := map[string]platform.MCPServer{}
	for _, s := range scan.Servers {
		byName[s.Name] = s
	}
	if byName["fs"].Command != "bunx" || len(byName["fs"].Env) != 1 {
		t.Errorf("fs %+v (Kilo's env alias; the kilo.jsonc entry wins over the legacy one)", byName["fs"])
	}
	if l := byName["legacy"]; l.Command != "uvx" || l.Scope != platform.MCPScopeProject || l.Pointer != "/mcpServers/legacy" {
		t.Errorf("legacy %+v", l)
	}
	if _, ok := byName["off"]; ok {
		t.Error("a disabled legacy server is loaded")
	}
	if e := byName["ext"]; e.URL != "https://e.example/mcp" || e.Scope != platform.MCPScopeUser {
		t.Errorf("the extension's mcp_settings.json %+v", e)
	}
	if none := ReadOpenCode(agentdirs.LinuxLayout, t.TempDir(), func(string) (string, bool) { return "", false }, nil, agentdirs.OpenCode); none.Present {
		t.Error("no OpenCode")
	}
}

func TestReadOpenCode_BadPermission(t *testing.T) {
	home := t.TempDir()
	put(t, filepath.Join(home, ".config", "opencode", "opencode.json"), `{"permission": {"bash": "sometimes"}}`)
	scan := ReadOpenCode(agentdirs.LinuxLayout, home, func(string) (string, bool) { return "", false }, nil, agentdirs.OpenCode)
	if len(scan.Layers) != 1 || scan.Layers[0].Status != platform.MCPSourceFailed {
		t.Errorf("an unknown action reads as failed, never as clean: %+v", scan.Layers)
	}
}
