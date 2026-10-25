package graph_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

// fakeJWT builds an unsigned-looking JWT with the given JSON payload. The
// signature is filler: RosmarOps never verifies it.
func fakeJWT(payload string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + enc([]byte(payload)) + ".c2lnbmF0dXJlLWZpbGxlcg"
}

const supabaseRef = "abcdefghijklmnopqrst"

var (
	fakeSupabaseJWT = fakeJWT(`{"iss":"supabase","ref":"` + supabaseRef + `","role":"service_role","iat":1700000000,"email":"owner@example.com"}`)
	fakeOtherJWT    = fakeJWT(`{"iss":"https://auth.example.com","sub":"user-123","role":"admin"}`)
	fakeRailway     = "rw" + strings.Repeat("Z9y8X7w6V5", 4)
	fakeSBSecretKey = "sb_secret_" + strings.Repeat("aB3-", 5) + "Xy_Ck9_s-1Z"
	pemA            = "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQAA\n-----END PRIVATE KEY-----"
	pemB            = "-----BEGIN PRIVATE KEY-----\nMIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQBB\n-----END PRIVATE KEY-----"
)

func edgeTo(edges []graph.Edge, typ, from string) (graph.Edge, bool) {
	for _, e := range edges {
		if e.Type == typ && e.From == from {
			return e, true
		}
	}
	return graph.Edge{}, false
}

func nodeWithID(nodes []graph.Node, id string) (graph.Node, bool) {
	for _, n := range nodes {
		if n.ID == id {
			return n, true
		}
	}
	return graph.Node{}, false
}

func TestCredentialsFromMCP_DistinctPEMsAreDistinctNodes(t *testing.T) {
	creds, _ := credentials(t, []platform.MCPServer{
		credServer("cursor", cursorConfig, "a", env("KEY_A", pemA)),
		credServer("cursor", cursorConfig, "b", env("KEY_B", pemB)),
	}, testSalt)
	if len(creds) != 2 {
		t.Errorf("two different PEM keys: got %d credential nodes, want 2", len(creds))
	}
}

func TestCredentialsFromMCP_SamePEMInTwoServersIsOneNode(t *testing.T) {
	creds, receives := credentials(t, []platform.MCPServer{
		credServer("cursor", cursorConfig, "a", env("PRIVATE_KEY", pemA)),
		credServer("claude-code", globalPath, "b", env("SSH_KEY", pemA)),
	}, testSalt)
	if len(creds) != 1 || len(creds[0].Locations) != 2 || len(receives) != 2 {
		t.Errorf("same PEM in two servers: got %d nodes, %v locations, %d receives; want 1, 2, 2",
			len(creds), creds, len(receives))
	}
}

func TestCredentialsFromMCP_KeywordSecretsInEnvDetected(t *testing.T) {
	creds, _ := credentials(t, []platform.MCPServer{credServer("cursor", cursorConfig, "x", env(
		"RAILWAY_TOKEN", fakeRailway,
		"DATABASE_URL", "postgres://admin:hunter2pass@db.example.com:5432/app",
		"Authorization", "Bearer abcdefghijklmnopqrstuvwxyz012345",
	))}, testSalt)
	types := map[string]bool{}
	for _, c := range creds {
		types[c.Type+"/"+c.Service] = true
	}
	for _, want := range []string{"api_key/railway", "db_credential/unknown", "generic_secret/unknown"} {
		if !types[want] {
			t.Errorf("missing credential %s; got %v", want, types)
		}
	}
}

func TestCredentialsFromMCP_RailwayFingerprintIsOfBareToken(t *testing.T) {
	creds, _ := credentials(t, []platform.MCPServer{credServer("cursor", cursorConfig, "x", env("RAILWAY_TOKEN", fakeRailway))}, testSalt)
	if len(creds) != 1 {
		t.Fatalf("got %d credentials, want 1", len(creds))
	}
	if want := graph.Fingerprint(testSalt, fakeRailway); creds[0].Fingerprint != want {
		t.Error("Railway fingerprint is not Fingerprint(salt, token): the name or separator leaked into the span")
	}
}

func TestCredentialsFromMCP_SupabaseJWTIdentified(t *testing.T) {
	nodes, edges := credentialGraph(t, []platform.MCPServer{credServer("cursor", cursorConfig, "db", env("SUPABASE_SERVICE_KEY", fakeSupabaseJWT))}, testSalt)
	creds := nodesOfKind(nodes, graph.KindCredential)
	if len(creds) != 1 || creds[0].Service != "supabase" || creds[0].Type != graph.CredentialAPIKey {
		t.Fatalf("credential: got %+v, want one api_key with service supabase", creds)
	}
	auth, ok := edgeTo(edges, graph.EdgeAuthenticatesTo, creds[0].ID)
	if !ok {
		t.Fatal("no authenticates_to edge")
	}
	if auth.Certainty != graph.CertaintyObserved || auth.ObservedScope != "service_role" {
		t.Errorf("authenticates_to: got certainty %q scope %q, want observed / service_role", auth.Certainty, auth.ObservedScope)
	}
	res, ok := nodeWithID(nodes, auth.To)
	if !ok || res.Type != graph.ResourceExternalService || res.Path != "https://"+supabaseRef+".supabase.co" {
		t.Errorf("resource: got %+v, want external_service https://%s.supabase.co", res, supabaseRef)
	}
}

// No claim other than iss/ref/role reaches the encoded graph.
func TestFromMCPScan_SupabaseGraphHasNoOtherClaims(t *testing.T) {
	scan := sampleScan()
	scan.Servers[0].Env = env("SUPABASE_SERVICE_KEY", fakeSupabaseJWT)
	out, err := graph.Encode(graph.Build(fromScan(t, scan), testProvenance, scanTime))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, leaked := range []string{"owner@example.com", "1700000000", strings.Split(fakeSupabaseJWT, ".")[1], fakeSupabaseJWT} {
		if strings.Contains(string(out), leaked) {
			t.Errorf("encoded graph contains %q", leaked[:min(len(leaked), 20)])
		}
	}
	if !strings.Contains(string(out), `"observed_scope": "service_role"`) {
		t.Error("encoded graph lacks the observed scope")
	}
}

func TestCredentialsFromMCP_NonSupabaseJWTIsUnknownWithoutEdge(t *testing.T) {
	nodes, edges := credentialGraph(t, []platform.MCPServer{credServer("cursor", cursorConfig, "x", env("TOKEN", fakeOtherJWT))}, testSalt)
	creds := nodesOfKind(nodes, graph.KindCredential)
	if len(creds) != 1 || creds[0].Type != graph.CredentialGenericSecret || creds[0].Service != graph.ServiceUnknown {
		t.Fatalf("non-Supabase JWT: got %+v, want generic_secret/unknown", creds)
	}
	if len(nodesOfKind(nodes, graph.KindResource)) != 0 || len(edgesOfType(edges, graph.EdgeAuthenticatesTo)) != 0 {
		t.Error("unknown service produced a resource or authenticates_to edge")
	}
}

func TestCredentialsFromMCP_GitHubServiceInferred(t *testing.T) {
	nodes, edges := credentialGraph(t, []platform.MCPServer{credServer("cursor", cursorConfig, "gh", env("GITHUB_TOKEN", fakeGitHubPAT))}, testSalt)
	cred := nodesOfKind(nodes, graph.KindCredential)[0]
	auth, ok := edgeTo(edges, graph.EdgeAuthenticatesTo, cred.ID)
	if !ok || auth.Certainty != graph.CertaintyInferred || auth.ObservedScope != "" {
		t.Fatalf("github authenticates_to: got %+v ok=%v, want inferred without scope", auth, ok)
	}
	if res, _ := nodeWithID(nodes, auth.To); res.Path != "service://github" {
		t.Errorf("resource path: got %q, want service://github", res.Path)
	}
}

func TestCredentialsFromMCP_SupabaseOpaqueKeyScopeFromPrefix(t *testing.T) {
	nodes, edges := credentialGraph(t, []platform.MCPServer{credServer("cursor", cursorConfig, "db", env("SUPABASE_KEY", fakeSBSecretKey))}, testSalt)
	cred := nodesOfKind(nodes, graph.KindCredential)[0]
	auth, _ := edgeTo(edges, graph.EdgeAuthenticatesTo, cred.ID)
	res, _ := nodeWithID(nodes, auth.To)
	if cred.Service != "supabase" || auth.ObservedScope != "secret" || auth.Certainty != graph.CertaintyInferred || res.Path != "service://supabase" {
		t.Errorf("sb_secret_ key: got service %q scope %q certainty %q path %q", cred.Service, auth.ObservedScope, auth.Certainty, res.Path)
	}
}

func TestCredentialsFromMCP_SharedServiceResourceDeduplicated(t *testing.T) {
	other := "ghp_" + strings.Repeat("Q", 36)
	nodes, edges := credentialGraph(t, []platform.MCPServer{credServer("cursor", cursorConfig, "gh", env("A", fakeGitHubPAT, "B", other))}, testSalt)
	if n := len(nodesOfKind(nodes, graph.KindResource)); n != 1 {
		t.Errorf("two GitHub tokens: got %d service resources, want 1", n)
	}
	if n := len(edgesOfType(edges, graph.EdgeAuthenticatesTo)); n != 2 {
		t.Errorf("two GitHub tokens: got %d authenticates_to edges, want 2", n)
	}
}
