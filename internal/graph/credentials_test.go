package graph_test

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// Obviously fake values that match the built-in patterns.
const (
	fakeGitHubPAT = "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	fakeAWSKeyID  = "AKIAABCDEFGHIJ123456"
)

func env(kv ...string) []platform.EnvVar {
	var out []platform.EnvVar
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, platform.EnvVar{Key: kv[i], Value: secrets.NewSecretValue(kv[i+1])})
	}
	return out
}

func credServer(client, path, name string, e []platform.EnvVar) platform.MCPServer {
	keys := make([]string, 0, len(e))
	for _, v := range e {
		keys = append(keys, v.Key)
	}
	return platform.MCPServer{Client: client, Name: name, Command: "npx", Args: []string{}, EnvKeys: keys, Env: e, ConfigPath: path, LoadsMCP: true}
}

// credentials returns the credential nodes and receives edges built from
// servers (service resources and authenticates_to edges are checked apart).
func credentials(t *testing.T, servers []platform.MCPServer, salt []byte) ([]graph.Node, []graph.Edge) {
	t.Helper()
	nodes, edges := credentialGraph(t, servers, salt)
	return nodesOfKind(nodes, graph.KindCredential), edgesOfType(edges, graph.EdgeReceives)
}

// credentialGraph returns everything CredentialsFromMCP builds.
func credentialGraph(t *testing.T, servers []platform.MCPServer, salt []byte) ([]graph.Node, []graph.Edge) {
	t.Helper()
	nodes, edges, err := graph.CredentialsFromMCP(servers, salt, scanTime)
	if err != nil {
		t.Fatalf("CredentialsFromMCP: %v", err)
	}
	return nodes, edges
}

func nodesOfKind(nodes []graph.Node, kind string) []graph.Node {
	var out []graph.Node
	for _, n := range nodes {
		if n.Kind == kind {
			out = append(out, n)
		}
	}
	return out
}

func edgesOfType(edges []graph.Edge, typ string) []graph.Edge {
	var out []graph.Edge
	for _, e := range edges {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func TestCredentialKind_EveryBuiltinPatternMapped(t *testing.T) {
	for _, p := range secrets.BuiltinPatterns() {
		typ, service, ok := graph.CredentialKind(p.ID)
		if !ok || typ == "" || service == "" {
			t.Errorf("pattern %q has no credential type/service mapping", p.ID)
		}
	}
}

func TestCredentialsFromMCP_SameKeyInTwoServers(t *testing.T) {
	servers := []platform.MCPServer{
		credServer("cursor", cursorConfig, "github", env("GITHUB_TOKEN", fakeGitHubPAT)),
		credServer("claude-code", globalPath, "gh", env("GH_TOKEN", fakeGitHubPAT)),
	}
	nodes, edges := credentials(t, servers, testSalt)

	if len(nodes) != 1 {
		t.Fatalf("credential nodes: got %d, want 1", len(nodes))
	}
	n := nodes[0]
	if n.Kind != graph.KindCredential || n.Type != graph.CredentialAPIKey || n.Service != "github" {
		t.Errorf("credential: got kind %q type %q service %q", n.Kind, n.Type, n.Service)
	}
	wantLocs := []string{
		cursorConfig + "#/mcpServers/github/env/GITHUB_TOKEN",
		globalPath + "#/mcpServers/gh/env/GH_TOKEN",
	}
	if strings.Join(n.Locations, "|") != strings.Join(sortedCopy(wantLocs), "|") {
		t.Errorf("locations: got %v, want %v", n.Locations, sortedCopy(wantLocs))
	}
	if len(edges) != 2 {
		t.Fatalf("receives edges: got %d, want 2", len(edges))
	}
	for _, e := range edges {
		if e.Type != graph.EdgeReceives || e.From != n.ID || e.Certainty != graph.CertaintyObserved {
			t.Errorf("edge %+v: want receives from %s, observed", e, n.ID)
		}
		if !strings.HasPrefix(e.ID, "edge_receives_") {
			t.Errorf("edge id %q: want edge_receives_ prefix", e.ID)
		}
	}
}

func TestCredentialsFromMCP_FingerprintDependsOnSalt(t *testing.T) {
	servers := []platform.MCPServer{credServer("cursor", cursorConfig, "aws", env("AWS_ACCESS_KEY_ID", fakeAWSKeyID))}

	a, _ := credentials(t, servers, testSalt)
	b, _ := credentials(t, servers, testSalt)
	if a[0].Fingerprint != b[0].Fingerprint || a[0].ID != b[0].ID {
		t.Errorf("same salt, same input: fingerprints differ (%s vs %s)", a[0].Fingerprint, b[0].Fingerprint)
	}

	other, _ := credentials(t, servers, []byte("ffffffffffffffffffffffffffffffff"))
	if other[0].Fingerprint == a[0].Fingerprint {
		t.Error("different salt produced the same fingerprint")
	}
	if !regexp.MustCompile(`^hmac-sha256:[0-9a-f]{64}$`).MatchString(a[0].Fingerprint) {
		t.Errorf("fingerprint format: got %q", a[0].Fingerprint)
	}
	if strings.Contains(a[0].Fingerprint, fakeAWSKeyID) || strings.Contains(a[0].ID, fakeAWSKeyID) {
		t.Error("fingerprint or id contains the secret")
	}
}

func TestCredentialsFromMCP_FingerprintIsOfMatchedText(t *testing.T) {
	// The same key wrapped differently in two values is one credential.
	servers := []platform.MCPServer{
		credServer("cursor", cursorConfig, "a", env("TOKEN", fakeGitHubPAT)),
		credServer("cursor", cursorConfig, "b", env("AUTH", "token "+fakeGitHubPAT)),
	}
	nodes, _ := credentials(t, servers, testSalt)
	if len(nodes) != 1 {
		t.Errorf("same key in different contexts: got %d credential nodes, want 1", len(nodes))
	}
}

func TestCredentialsFromMCP_NoMatchNoNode(t *testing.T) {
	servers := []platform.MCPServer{credServer("cursor", cursorConfig, "x", env("LOG_LEVEL", "debug", "REGION", "eu-west-1"))}
	nodes, edges := credentials(t, servers, testSalt)
	if len(nodes) != 0 || len(edges) != 0 {
		t.Errorf("non-secret env: got %d nodes %d edges, want none", len(nodes), len(edges))
	}
}

func TestCredentialsFromMCP_RequiresSalt(t *testing.T) {
	servers := []platform.MCPServer{credServer("cursor", cursorConfig, "aws", env("K", fakeAWSKeyID))}
	if _, _, err := graph.CredentialsFromMCP(servers, nil, scanTime); err == nil {
		t.Error("no salt: got nil error")
	}
}

func TestFromMCPScan_CredentialsReceiveIntoServerAndValidate(t *testing.T) {
	scan := sampleScan()
	scan.Servers[0].Env = env("GITHUB_TOKEN", fakeGitHubPAT)
	scan.Servers[0].EnvKeys = []string{"GITHUB_TOKEN"}

	f := fromScan(t, scan)
	byID := map[string]graph.Node{}
	for _, n := range f.Nodes {
		byID[n.ID] = n
	}
	var receives int
	for _, e := range f.Edges {
		if e.Type != graph.EdgeReceives {
			continue
		}
		receives++
		if byID[e.From].Kind != graph.KindCredential || byID[e.To].Type != graph.AgentMCPServer || byID[e.To].Name != "github" {
			t.Errorf("receives edge %s: got %s -> %s", e.ID, byID[e.From].Kind, byID[e.To].Type)
		}
	}
	if receives != 1 {
		t.Errorf("receives edges: got %d, want 1", receives)
	}

	out, err := graph.Encode(graph.Build(f, testProvenance, scanTime))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.Contains(string(out), fakeGitHubPAT) {
		t.Error("encoded graph contains the secret value")
	}
}

func TestHostID_DeterministicFormat(t *testing.T) {
	a, b := graph.HostID(testSalt), graph.HostID(testSalt)
	if a != b {
		t.Errorf("HostID not deterministic: %q vs %q", a, b)
	}
	if !regexp.MustCompile(`^host_[0-9a-f]{16}$`).MatchString(a) {
		t.Errorf("HostID format: got %q, want host_ + 16 hex", a)
	}
	if graph.HostID([]byte("ffffffffffffffffffffffffffffffff")) == a {
		t.Error("different salts give the same host_id")
	}
}

func sortedCopy(ss []string) []string {
	out := append([]string(nil), ss...)
	sort.Strings(out)
	return out
}
