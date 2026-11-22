package graph

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-274: every launcher with and without an exact version; Docker with a
// tag and with a digest.
func TestUnpinnedLaunchOf(t *testing.T) {
	for _, c := range []struct {
		cmd      string
		args     string
		launcher string // "" when pinned or not a launch
		pkg      string
	}{
		{"npx", "-y @modelcontextprotocol/server-github", "npx", "@modelcontextprotocol/server-github"},
		{"npx", "-y @modelcontextprotocol/server-github@1.2.3", "", ""},
		{"npx", "-y @scope/pkg@^1.2.0", "npx", "@scope/pkg"},
		{"npx", "-y pkg@~1.2.0", "npx", "pkg"},
		{"npx", "pkg@>=1.0.0", "npx", "pkg"},
		{"npx", "pkg@*", "npx", "pkg"},
		{"npx", "pkg@1.x", "npx", "pkg"},
		{"npx", "pkg@latest", "npx", "pkg"},
		{"npx", "pkg@next", "npx", "pkg"},
		{"npx", "pkg@1.2.3-rc.1", "", ""},
		{"npx", "--package pkg@2.0.0 run-it", "", ""},
		{"npx", "--package=pkg run-it", "npx", "pkg"},
		{"npx", "-p pkg@latest run-it", "npx", "pkg"},
		{"npx", "./local/server.js", "", ""},
		{"/usr/local/bin/npx", "-y mcp-remote", "npx", "mcp-remote"},
		{"npx.cmd", "-y mcp-remote@0.1.9", "", ""},
		{"cmd", "/c npx -y @upstash/context7-mcp", "npx", "@upstash/context7-mcp"},
		{"bunx", "pkg", "bunx", "pkg"},
		{"bunx", "pkg@3.1.4", "", ""},
		{"pnpm", "dlx pkg", "pnpm dlx", "pkg"},
		{"pnpm", "dlx pkg@1.0.0", "", ""},
		{"pnpm", "install pkg", "", ""},
		{"uvx", "mcp-server-fetch", "uvx", "mcp-server-fetch"},
		{"uvx", "mcp-server-fetch==2025.1.17", "", ""},
		{"uvx", "mcp-server-fetch@2025.1.17", "", ""},
		{"uvx", "mcp-server-fetch@latest", "uvx", "mcp-server-fetch"},
		{"uvx", "mcp-server-fetch>=1.0", "uvx", "mcp-server-fetch"},
		{"uvx", "--from mcp-server-git==0.6.2 mcp-server-git", "", ""},
		{"uvx", "--from mcp-server-git mcp-server-git", "uvx", "mcp-server-git"},
		{"uvx", "--python 3.12 tool[extra]", "uvx", "tool"},
		{"pipx", "run pkg", "pipx run", "pkg"},
		{"pipx", "run pkg==1.0.0", "", ""},
		{"pipx", "run --spec pkg==1.0.0 cmd", "", ""},
		{"pipx", "install pkg", "", ""},
		{"docker", "run -i --rm -e GITHUB_TOKEN ghcr.io/github/github-mcp-server", "docker run", "ghcr.io/github/github-mcp-server"},
		{"docker", "run -i --rm ghcr.io/github/github-mcp-server:v1.2.3", "docker run", "ghcr.io/github/github-mcp-server"},
		{"docker", "run -i --rm mcp/fetch:latest", "docker run", "mcp/fetch"},
		{"docker", "run -i --rm --name=x -v /a:/b localhost:5000/mcp/fetch:1", "docker run", "localhost:5000/mcp/fetch"},
		{"docker", "run -i --rm mcp/fetch@sha256:0123456789abcdef", "", ""},
		{"docker", "run mcp/fetch:1.0@sha256:0123456789abcdef", "", ""},
		{"docker", "container run -i mcp/time", "docker run", "mcp/time"},
		{"docker", "exec -i box mcp", "", ""},
		{"node", "server.js", "", ""},
		{"", "", "", ""},
	} {
		srv := platform.MCPServer{Command: c.cmd, Args: strings.Fields(c.args)}
		u, ok := UnpinnedLaunchOf(srv)
		if (c.launcher != "") != ok || u.Launcher != c.launcher || u.Package != c.pkg {
			t.Errorf("%s %s: %+v %v, want %q %q", c.cmd, c.args, u, ok, c.launcher, c.pkg)
		}
	}
}

func TestIsExactVersion(t *testing.T) {
	for v, want := range map[string]bool{
		"1.2.3": true, "v1.2.3": true, "1.2.3-rc.1": true, "2025.1.17": true, "1": true,
		"": false, "latest": false, "next": false, "^1.2.3": false, "~1.2": false, ">=1": false, "*": false,
		"1.x": false, "1.2.X": false, "1 - 2": false, "1.2.3 || 2": false,
	} {
		if got := isExactVersion(v); got != want {
			t.Errorf("isExactVersion(%q) = %v, want %v", v, got, want)
		}
	}
}

// MarkUnpinned marks the loaded server's node, in memory only.
func TestMarkUnpinned(t *testing.T) {
	on := platform.MCPServer{Client: "claude-code", Name: "gh", Command: "npx", Args: []string{"-y", "server-github"}, ConfigPath: "/h/.claude.json", ConfigDir: "/h/.claude", LoadsMCP: true}
	pinned := on
	pinned.Name, pinned.Args = "gh2", []string{"-y", "server-github@1.0.0"}
	g := Graph{Nodes: []Node{{ID: serverID(on), Kind: KindAgent, Type: AgentMCPServer}, {ID: serverID(pinned), Kind: KindAgent, Type: AgentMCPServer}}}
	MarkUnpinned(&g, []platform.MCPServer{on, pinned})
	if u := g.Nodes[0].Unpinned; u == nil || u.Launcher != "npx" || u.Package != "server-github" {
		t.Errorf("unpinned %+v", u)
	}
	if g.Nodes[1].Unpinned != nil {
		t.Errorf("a pinned server marked: %+v", g.Nodes[1].Unpinned)
	}
	raw, err := json.Marshal(g.Nodes[0])
	if err != nil || strings.Contains(string(raw), "server-github") || strings.Contains(string(raw), "npx") {
		t.Errorf("the mark reached the encoding: %s %v", raw, err)
	}
}
