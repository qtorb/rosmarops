package schemaval_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/schemas"
)

// BV-203 point 4 (CTO-24 E6).

// repoRoot is the module root, two levels up from this test's directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(self), "..", ".."))
}

// schemaKey turns a schema file's base name into its differential-table key:
// "report.v2.schema.json" -> "report.v2", "config.v1.json" -> "config.v1". An
// example file (graph.v2.example.json) is not a schema and returns "".
func schemaKey(base string) string {
	if strings.HasSuffix(base, ".example.json") {
		return ""
	}
	switch {
	case strings.HasSuffix(base, ".schema.json"):
		return strings.TrimSuffix(base, ".schema.json")
	case strings.HasSuffix(base, ".json"):
		return strings.TrimSuffix(base, ".json")
	}
	return ""
}

// TestOracleCoversEverySchema fails if any schema file under schemas/ or
// internal/schemas/ is not represented in the differential table (schemaSet),
// so a new schema cannot be added without entering the oracle.
func TestOracleCoversEverySchema(t *testing.T) {
	root := repoRoot(t)
	missing := map[string]string{}
	for _, base := range []string{"schemas", filepath.Join("internal", "schemas")} {
		dir := filepath.Join(root, base)
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
				return nil
			}
			key := schemaKey(d.Name())
			if key == "" {
				return nil
			}
			if _, ok := schemaSet[key]; !ok {
				rel, _ := filepath.Rel(root, path)
				missing[key] = rel
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	if len(missing) > 0 {
		keys := make([]string, 0, len(missing))
		for k := range missing {
			keys = append(keys, k+" ("+missing[k]+")")
		}
		sort.Strings(keys)
		t.Fatalf("schemas not in the differential table (schemaSet): %s", strings.Join(keys, ", "))
	}
}

// nodePair is one kind/type pair of a graph.v2 node oneOf branch.
type nodePair struct{ kind, typ string }

// graphNodePairs reads the node oneOf of the graph.v2 schema and returns every
// kind/type pair it pins, so a new branch or type is picked up automatically.
func graphNodePairs(t *testing.T) []nodePair {
	t.Helper()
	var s struct {
		Definitions map[string]json.RawMessage `json:"definitions"`
	}
	if err := json.Unmarshal(schemas.GraphV2Schema, &s); err != nil {
		t.Fatalf("parse graph schema: %v", err)
	}
	var node struct {
		OneOf []struct {
			Ref string `json:"$ref"`
		} `json:"oneOf"`
	}
	if err := json.Unmarshal(s.Definitions["node"], &node); err != nil {
		t.Fatalf("parse node def: %v", err)
	}
	enumOf := func(raw json.RawMessage) []string {
		var p struct {
			Const string   `json:"const"`
			Enum  []string `json:"enum"`
		}
		_ = json.Unmarshal(raw, &p)
		if p.Const != "" {
			return []string{p.Const}
		}
		return p.Enum
	}
	var pairs []nodePair
	for _, branch := range node.OneOf {
		name := strings.TrimPrefix(branch.Ref, "#/definitions/")
		var def struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(s.Definitions[name], &def); err != nil {
			t.Fatalf("parse def %s: %v", name, err)
		}
		kinds := enumOf(def.Properties["kind"])
		types := enumOf(def.Properties["type"])
		if len(kinds) != 1 {
			t.Fatalf("branch %s: expected one kind, got %v", name, kinds)
		}
		for _, ty := range types {
			pairs = append(pairs, nodePair{kind: kinds[0], typ: ty})
		}
	}
	return pairs
}

// TestGraphFixtureCoversEveryNodeType reads the node oneOf of the graph schema
// and fails if the all-node-types fixture lacks a node for any kind/type pair.
// A new branch or node type thus requires extending the fixture.
func TestGraphFixtureCoversEveryNodeType(t *testing.T) {
	pairs := graphNodePairs(t)
	if len(pairs) < 20 {
		t.Fatalf("only %d kind/type pairs read from the schema — parsing is wrong", len(pairs))
	}

	raw := everyNodeTypeGraphJSON(t)

	// The fixture is a valid graph.v2 (the committed copy under testdata/valid
	// is the same bytes, exercised by the differential oracle).
	have := map[nodePair]bool{}
	var g struct {
		Nodes []struct {
			Kind string `json:"kind"`
			Type string `json:"type"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	for _, n := range g.Nodes {
		have[nodePair{kind: n.Kind, typ: n.Type}] = true
	}
	var missing []string
	for _, p := range pairs {
		if !have[p] {
			missing = append(missing, p.kind+"/"+p.typ)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("all-node-types graph fixture is missing %d kind/type pairs: %s", len(missing), strings.Join(missing, ", "))
	}

	// And the committed fixture file stays in sync with the builder (regenerate
	// with ROSMAROPS_REGEN_FIXTURES=1 if this fails).
	committed, err := os.ReadFile(filepath.Join("testdata", "valid", "graph.v2.all-node-types.json"))
	if err != nil {
		t.Fatalf("read committed fixture: %v (regenerate with ROSMAROPS_REGEN_FIXTURES=1)", err)
	}
	// Compare line-ending-insensitively: git may check the committed JSON out
	// with CRLF on Windows, which is not staleness.
	norm := func(b []byte) string { return strings.TrimSpace(strings.ReplaceAll(string(b), "\r\n", "\n")) }
	if norm(committed) != norm(raw) {
		t.Error("testdata/valid/graph.v2.all-node-types.json is stale; regenerate with ROSMAROPS_REGEN_FIXTURES=1")
	}
}

// everyNodeTypeGraphJSON builds a graph with at least one node of every
// kind/type pair the graph.v2 schema allows and returns its validated encoding
// (graph.Encode checks schema and integrity).
func everyNodeTypeGraphJSON(t *testing.T) []byte {
	t.Helper()
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	g := graph.Build(graph.Fragment{}, graph.Provenance{
		EngineVersion: "0.10.3-fixture", RulesVersion: graph.RulesVersion,
		KnowledgeVersion: "k", HostID: "host_0123456789abcdef",
	}, now)
	g.Nodes = everyNodeTypeNodes(now.UTC().Format(time.RFC3339))
	out, err := graph.Encode(g)
	if err != nil {
		t.Fatalf("encode all-node-types graph: %v", err)
	}
	return out
}

// everyNodeTypeNodes is one node per kind/type pair, with the minimal required
// fields of each node variant (a credential carries a location so it passes
// integrity).
func everyNodeTypeNodes(seen string) []graph.Node {
	var out []graph.Node
	id := 0
	nid := func(prefix string) string {
		id++
		return prefix + "_" + string(rune('a'+id%26)) + itoaPad(id)
	}
	var aiClientID string
	agentTypes := []string{"ai_client", "mcp_server", "extension", "bot"}
	for _, ty := range agentTypes {
		aid := nid("agent")
		if ty == "ai_client" {
			aiClientID = aid
		}
		out = append(out, graph.Node{
			ID: aid, Kind: graph.KindAgent, Type: ty,
			Name: "Fixture " + ty, Platform: "claude-code", ConfigPath: "/cfg", DiscoveredAt: seen,
		})
	}
	capTypes := []string{"execute_shell", "read_files", "write_files", "network", "invoke_mcp_tool", "permission_mode", "operate_app"}
	for _, ty := range capTypes {
		out = append(out, graph.Node{
			ID: nid("cap"), Kind: graph.KindCapability, Type: ty,
			Basis: "product_default", InForce: true, Approval: "ask",
			TargetScope: "/scope", AppliesIn: "", Source: "kb:claude-code/x@1",
		})
	}
	credTypes := []string{"api_key", "db_credential", "oauth_token", "webhook_secret", "ssh_key", "private_key", "generic_secret"}
	for _, ty := range credTypes {
		out = append(out, graph.Node{
			ID: nid("cred"), Kind: graph.KindCredential, Type: ty,
			Service: "fixture", Fingerprint: "fp" + itoaPad(id), Locations: []string{"/secrets:1"}, DiscoveredAt: seen,
		})
	}
	resTypes := []string{"file", "database", "external_service", "agent_config", "agent_instructions", "agent_executable", "git_repository"}
	for _, ty := range resTypes {
		n := graph.Node{ID: nid("res"), Kind: graph.KindResource, Type: ty, Path: "/res", DiscoveredAt: seen}
		switch ty {
		case "agent_config", "agent_instructions", "agent_executable":
			n.Configures = aiClientID // these resources configure an agent
		}
		out = append(out, n)
	}
	return out
}

// itoaPad is a tiny zero-free integer-to-string for unique ids.
func itoaPad(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
