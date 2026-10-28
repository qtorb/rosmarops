package windows_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/repos"
)

// Obviously fake values.
const (
	fakeHeaderToken  = "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	fakeUntypedToken = "ghp_ZYXWVUTSRQPONMLKJIHGFEDCBA9876543210"
	fakeEmail        = "owner.fake@example.com"
)

var (
	testSalt = []byte("0123456789abcdef0123456789abcdef")
	testTime = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
)

// claudeJSON is a ~/.claude.json with one user-scope server, two local-scope
// servers with the same name in different projects, an http server with a
// token in its headers, an entry with url and headers but no type (stdio for
// Claude Code), and keys that are not MCP configuration at all.
const claudeJSON = `{
  "numStartups": 42,
  "oauthAccount": {"emailAddress": "` + fakeEmail + `", "organizationName": "Fake Org"},
  "history": [{"display": "fix the login bug"}],
  "userID": "fake-user-id-123",
  "mcpServers": {
    "fs": {"command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem"]},
    "github-remote": {"type": "http", "url": "https://api.githubcopilot.com/mcp/",
                      "headers": {"Authorization": "Bearer ` + fakeHeaderToken + `"}},
    "untyped": {"url": "https://example.com/mcp", "headers": {"Authorization": "Bearer ` + fakeUntypedToken + `"}}
  },
  "projects": {
    "/home/me/repo-a": {"allowedTools": [], "history": [{"display": "secret prompt"}],
                        "mcpServers": {"db": {"command": "npx", "args": ["db-a"]}}},
    "/home/me/repo-b": {"mcpServers": {"db": {"command": "npx", "args": ["db-b"]}}}
  }
}`

func TestReadClaudeJSON_ScopesRemoteAndForeignKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	writeFile(t, path, claudeJSON)

	servers, src := windows.ReadClaudeJSON(path)
	if src.Status != platform.MCPSourceRead || src.Platform != "claude-code" {
		t.Fatalf("source: got %+v, want read claude-code", src)
	}
	if len(servers) != 5 {
		t.Fatalf("servers: got %d, want 5", len(servers))
	}

	byPointer := map[string]platform.MCPServer{}
	for _, s := range servers {
		byPointer[s.Pointer] = s
	}
	for _, p := range []string{
		"/mcpServers/fs", "/mcpServers/github-remote", "/mcpServers/untyped",
		"/projects/~1home~1me~1repo-a/mcpServers/db", "/projects/~1home~1me~1repo-b/mcpServers/db",
	} {
		if _, ok := byPointer[p]; !ok {
			t.Errorf("missing server at pointer %s", p)
		}
	}
	if h := byPointer["/mcpServers/github-remote"].Headers; len(h) != 1 || h[0].Key != "Authorization" {
		t.Errorf("http server headers: got %v, want Authorization", h)
	}
	if h := byPointer["/mcpServers/untyped"].Headers; h != nil {
		t.Errorf("entry without type is stdio: headers must not be read, got %v", h)
	}

	// Through the graph: 5 mcp_server, 1 ai_client, one credential (the http
	// header) received by the remote server, nothing from the untyped entry.
	f, err := graph.FromMCPScan(platform.MCPScan{Servers: servers, Sources: []platform.MCPSource{src}}, testSalt, testTime)
	if err != nil {
		t.Fatalf("FromMCPScan: %v", err)
	}
	counts := map[string]int{}
	for _, n := range f.Nodes {
		counts[n.Kind+"/"+n.Type]++
	}
	if counts["agent/mcp_server"] != 5 || counts["agent/ai_client"] != 1 || counts["credential/api_key"] != 1 {
		t.Errorf("node counts: got %v, want 5 mcp_server, 1 ai_client, 1 api_key credential", counts)
	}
	remote := ""
	for _, n := range f.Nodes {
		if n.Name == "github-remote" {
			remote = n.ID
		}
	}
	var receives []graph.Edge
	for _, e := range f.Edges {
		if e.Type == graph.EdgeReceives {
			receives = append(receives, e)
		}
	}
	if len(receives) != 1 || receives[0].To != remote ||
		!strings.HasSuffix(receives[0].Source, "#/mcpServers/github-remote/headers/Authorization") {
		t.Errorf("receives: got %+v, want one edge to the http server from its Authorization header", receives)
	}

	out, err := graph.Encode(graph.Build(f, graph.Provenance{EngineVersion: "0.1.0-dev", RulesVersion: "0", KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(testSalt)}, testTime))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, leaked := range []string{
		fakeEmail, "Fake Org", "fix the login bug", "secret prompt", "fake-user-id-123",
		"oauthAccount", "numStartups", "allowedTools", fakeHeaderToken, fakeUntypedToken,
		"/mcp/", // BV-91: the URL path never reaches the graph
	} {
		if strings.Contains(string(out), leaked) {
			t.Errorf("graph contains %q", leaked)
		}
	}
	// BV-91: the remote origin (scheme://host, no path) is an external_service
	// node, reached by a sends_to edge from the server.
	if !strings.Contains(string(out), `"https://api.githubcopilot.com"`) {
		t.Error("graph lacks the remote origin external_service")
	}
	var sendsTo int
	for _, e := range f.Edges {
		if e.Type == graph.EdgeSendsTo && e.From == remote {
			sendsTo++
		}
	}
	if sendsTo != 1 {
		t.Errorf("sends_to edges from the remote server: got %d, want 1", sendsTo)
	}
}

func TestReadClaudeJSON_LocalServersSameNameDistinctIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	writeFile(t, path, claudeJSON)
	servers, src := windows.ReadClaudeJSON(path)
	f, err := graph.FromMCPScan(platform.MCPScan{Servers: servers, Sources: []platform.MCPSource{src}}, testSalt, testTime)
	if err != nil {
		t.Fatalf("FromMCPScan: %v", err)
	}
	ids := map[string]bool{}
	for _, n := range f.Nodes {
		if n.Name == "db" {
			ids[n.ID] = true
		}
	}
	if len(ids) != 2 {
		t.Errorf("two local 'db' servers in different projects: got %d distinct ids, want 2", len(ids))
	}
}

func TestReadMCPFile_RemoteTypes(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]bool{
		`"type": "http"`:            true,
		`"type": "streamable-http"`: true,
		`"type": "sse"`:             true,
		`"type": "ws"`:              true,
		`"type": "stdio"`:           false,
		`"command": "npx"`:          false, // no type: stdio
	}
	i := 0
	for field, remote := range cases {
		i++
		path := filepath.Join(dir, string(rune('a'+i))+".json")
		writeFile(t, path, `{"mcpServers": {"s": {`+field+`, "url": "https://x", "headers": {"X-Api-Key": "k"}}}}`)
		servers, _ := windows.ReadMCPFile(path, "claude-code-project", true)
		if got := servers[0].Headers != nil; got != remote {
			t.Errorf("%s: headers read = %v, want %v", field, got, remote)
		}
	}

	// BV-91 (D2): Cursor and Cline headers are now read as credentials, like
	// env, whatever the server's type.
	for _, client := range []string{"cursor", "cline"} {
		path := filepath.Join(dir, client+".json")
		writeFile(t, path, `{"mcpServers": {"s": {"type": "http", "url": "https://x", "headers": {"X-Api-Key": "k"}}}}`)
		if servers, _ := windows.ReadMCPFile(path, client, true); servers[0].Headers == nil {
			t.Errorf("%s headers not read; BV-91 reads them as credentials", client)
		}
	}
}

func TestListMCPServers_ReadsProjectMCPJSON(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	writeFile(t, filepath.Join(repo, ".mcp.json"), `{"mcpServers": {"shared": {"type": "http", "url": "https://example.com/mcp"}}}`)

	scan, err := windows.New().ListMCPServers(context.Background(), []string{repo})
	if err != nil {
		t.Fatalf("ListMCPServers: %v", err)
	}
	f, err := graph.FromMCPScan(scan, testSalt, testTime)
	if err != nil {
		t.Fatalf("FromMCPScan: %v", err)
	}
	var client, server bool
	for _, n := range f.Nodes {
		if n.Type == graph.AgentAIClient && n.Platform == "claude-code" && n.ConfigPath == filepath.Join(home, ".claude") {
			client = true
		}
		if n.Type == graph.AgentMCPServer && n.Name == "shared" {
			server = true
		}
	}
	if !client || !server {
		t.Errorf(".mcp.json: ai_client found=%v, server found=%v; want both", client, server)
	}
	for _, sf := range f.Coverage.SourcesFailed {
		if strings.HasPrefix(sf.Reason, graph.ReasonSourceUnsupported) {
			t.Errorf("source still declared unsupported: %+v", sf)
		}
	}
}

func TestReadMCPFile_OverSizeLimitNotOpened(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	big := `{"mcpServers": {"s": {"command": "npx"}}}` + strings.Repeat(" ", windows.MaxConfigBytes)
	writeFile(t, path, big)

	servers, src := windows.ReadClaudeJSON(path)
	if src.Status != platform.MCPSourceFailed || !strings.HasPrefix(src.Reason, graph.ReasonSizeLimitExceeded+": ") {
		t.Errorf("over 2 MB: got %+v, want failed with size_limit_exceeded", src)
	}
	if len(servers) != 0 {
		t.Errorf("over 2 MB: got %d servers, want none", len(servers))
	}
	f, err := graph.FromMCPScan(platform.MCPScan{Servers: servers, Sources: []platform.MCPSource{src}}, testSalt, testTime)
	if err != nil {
		t.Fatalf("FromMCPScan: %v", err)
	}
	if len(f.Nodes) != 0 {
		t.Errorf("over-size source produced %d nodes", len(f.Nodes))
	}
}

func TestMaxConfigBytes_MatchesInvariant(t *testing.T) {
	got, want := int64(windows.MaxConfigBytes), int64(repos.MaxFileBytes)
	if got != want {
		t.Errorf("MaxConfigBytes = %d, want %d (invariant 6, same as repos.MaxFileBytes)", got, want)
	}
}

func TestReadMCPFile_EveryFailureReasonHasCode(t *testing.T) {
	dir := t.TempDir()
	malformed := filepath.Join(dir, "bad.json")
	writeFile(t, malformed, `{"mcpServers": `)
	big := filepath.Join(dir, "big.json")
	writeFile(t, big, strings.Repeat(" ", windows.MaxConfigBytes+1))
	unreadable := filepath.Join(dir, "a-directory.json") // a directory cannot be read as a file
	if err := os.Mkdir(unreadable, 0o700); err != nil {
		t.Fatal(err)
	}

	for path, wantCode := range map[string]string{
		malformed:  graph.ReasonParseError,
		big:        graph.ReasonSizeLimitExceeded,
		unreadable: graph.ReasonReadError,
	} {
		_, src := windows.ReadMCPFile(path, "cursor", true)
		if src.Status != platform.MCPSourceFailed || !strings.HasPrefix(src.Reason, wantCode+": ") {
			t.Errorf("%s: got %+v, want failed with %s", filepath.Base(path), src, wantCode)
		}
		hasCode := false
		for _, c := range graph.ReasonCodes {
			if strings.HasPrefix(src.Reason, c+": ") {
				hasCode = true
			}
		}
		if !hasCode {
			t.Errorf("%s: reason %q does not start with a known code", filepath.Base(path), src.Reason)
		}
	}
}
