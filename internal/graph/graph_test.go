package graph_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
)

// fixturePath is the canonical v2 example, relative to this package directory.
var fixturePath = filepath.Join("..", "..", "schemas", "v2", "graph.v2.example.json")

// loadFixture loads the valid v2 example graph.
func loadFixture(t *testing.T) graph.Graph {
	t.Helper()
	g, err := graph.LoadGraph(fixturePath)
	if err != nil {
		t.Fatalf("LoadGraph(fixture): %v", err)
	}
	return g
}

func nodeByID(t *testing.T, g *graph.Graph, id string) *graph.Node {
	t.Helper()
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i]
		}
	}
	t.Fatalf("fixture has no node %q", id)
	return nil
}

func edgeByID(t *testing.T, g *graph.Graph, id string) *graph.Edge {
	t.Helper()
	for i := range g.Edges {
		if g.Edges[i].ID == id {
			return &g.Edges[i]
		}
	}
	t.Fatalf("fixture has no edge %q", id)
	return nil
}

// wantIntegrityError asserts that ValidateIntegrity fails and that the error
// mentions every given fragment.
func wantIntegrityError(t *testing.T, g graph.Graph, fragments ...string) {
	t.Helper()
	err := graph.ValidateIntegrity(g)
	if err == nil {
		t.Fatal("ValidateIntegrity: got nil, want error")
	}
	for _, f := range fragments {
		if !strings.Contains(err.Error(), f) {
			t.Errorf("error does not mention %q:\n%v", f, err)
		}
	}
}

func TestLoadGraph_ValidFixture(t *testing.T) {
	g := loadFixture(t)
	if g.SchemaVersion != 2 {
		t.Errorf("schema_version: got %d, want 2", g.SchemaVersion)
	}
	if len(g.Nodes) == 0 || len(g.Edges) == 0 {
		t.Errorf("fixture decoded empty: %d nodes, %d edges", len(g.Nodes), len(g.Edges))
	}
}

func TestValidateIntegrity_ValidFixture(t *testing.T) {
	g := loadFixture(t)
	if err := graph.ValidateIntegrity(g); err != nil {
		t.Errorf("valid fixture: got %v, want nil", err)
	}
}

func TestLoadGraph_IntegrityFailureIsFatal(t *testing.T) {
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	// Schema-valid but integrity-broken: a second node reusing an existing id.
	nodes := doc["nodes"].([]any)
	dup := map[string]any{}
	for k, v := range nodes[0].(map[string]any) {
		dup[k] = v
	}
	doc["nodes"] = append(nodes, dup)

	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "graph.json")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	g, err := graph.LoadGraph(path)
	if err == nil {
		t.Fatal("LoadGraph with duplicate node id: got nil error, want error")
	}
	if !strings.Contains(err.Error(), "integrity") {
		t.Errorf("error should be an integrity failure, got: %v", err)
	}
	if len(g.Nodes) != 0 || len(g.Edges) != 0 {
		t.Errorf("LoadGraph returned a graph alongside the error: %+v", g)
	}
}

func TestValidateIntegrity_DuplicateNodeID(t *testing.T) {
	g := loadFixture(t)
	g.Nodes = append(g.Nodes, g.Nodes[0])
	wantIntegrityError(t, g, `duplicate node id "`+g.Nodes[0].ID+`"`)
}

func TestValidateIntegrity_EdgeFromMissing(t *testing.T) {
	g := loadFixture(t)
	edgeByID(t, &g, "edge_cursor_has_shell").From = "agent_ghost"
	wantIntegrityError(t, g, `edge "edge_cursor_has_shell"`, `from "agent_ghost" does not exist`)
}

func TestValidateIntegrity_EdgeToMissing(t *testing.T) {
	g := loadFixture(t)
	edgeByID(t, &g, "edge_cursor_has_shell").To = "cap_ghost"
	wantIntegrityError(t, g, `edge "edge_cursor_has_shell"`, `to "cap_ghost" does not exist`)
}

func TestValidateIntegrity_HasFromNotAgent(t *testing.T) {
	g := loadFixture(t)
	edgeByID(t, &g, "edge_cursor_has_shell").From = "cred_db_servicerole"
	wantIntegrityError(t, g, `edge "edge_cursor_has_shell" (has)`, `from "cred_db_servicerole" is kind "credential", want agent`)
}

func TestValidateIntegrity_ReachesViaNotCapability(t *testing.T) {
	g := loadFixture(t)
	edgeByID(t, &g, "edge_cursor_reaches_db_servicerole").Via = "agent_claude_code"
	wantIntegrityError(t, g, `edge "edge_cursor_reaches_db_servicerole" (reaches)`, `via "agent_claude_code" is kind "agent", want capability`)
}

func TestValidateIntegrity_ConfiguresNotAgent(t *testing.T) {
	g := loadFixture(t)
	nodeByID(t, &g, "res_claude_config").Configures = "cap_cursor_write"
	wantIntegrityError(t, g, `node "res_claude_config"`, `configures "cap_cursor_write" is kind "capability", want agent`)
}

func TestValidateIntegrity_AuthenticatesToNotExternalService(t *testing.T) {
	g := loadFixture(t)
	g.Nodes = append(g.Nodes, graph.Node{
		ID:   "res_local_db",
		Kind: graph.KindResource,
		Type: "database",
		Path: "localhost:5432",
	})
	g.Edges = append(g.Edges, graph.Edge{
		ID:        "edge_db_servicerole_authenticates",
		Type:      graph.EdgeAuthenticatesTo,
		From:      "cred_db_servicerole",
		To:        "res_local_db",
		Source:    ".env:12",
		Certainty: "observed",
	})
	wantIntegrityError(t, g, `edge "edge_db_servicerole_authenticates" (authenticates_to)`,
		`to "res_local_db" is resource of type "database", want resource of type external_service`)
}

func TestValidateIntegrity_MultipleErrorsAllReported(t *testing.T) {
	g := loadFixture(t)
	g.Nodes = append(g.Nodes, g.Nodes[0])
	edgeByID(t, &g, "edge_cursor_has_shell").From = "agent_ghost"
	edgeByID(t, &g, "edge_cursor_reaches_db_servicerole").Via = "agent_claude_code"
	nodeByID(t, &g, "res_claude_config").Configures = "cap_cursor_write"
	wantIntegrityError(t, g,
		`duplicate node id "agent_cursor"`,
		`from "agent_ghost" does not exist`,
		`via "agent_claude_code" is kind "agent", want capability`,
		`configures "cap_cursor_write" is kind "capability", want agent`,
	)
}

func TestValidateIntegrity_DuplicateEdgeID(t *testing.T) {
	g := loadFixture(t)
	g.Edges = append(g.Edges, g.Edges[0])
	wantIntegrityError(t, g, `duplicate edge id "`+g.Edges[0].ID+`"`)
}

func TestValidateIntegrity_DerivedFromMissing(t *testing.T) {
	g := loadFixture(t)
	e := edgeByID(t, &g, "edge_cursor_reaches_db_servicerole")
	e.DerivedFrom = append(e.DerivedFrom, "cap_ghost")
	wantIntegrityError(t, g, `edge "edge_cursor_reaches_db_servicerole" (reaches)`,
		`derived_from "cap_ghost" is neither a node nor an edge`)
}

// derived_from may name a node or another edge.
func TestValidateIntegrity_DerivedFromNodeAndEdge(t *testing.T) {
	g := loadFixture(t)
	e := edgeByID(t, &g, "edge_cursor_reaches_db_servicerole")
	e.DerivedFrom = append(e.DerivedFrom, "edge_cursor_has_shell")
	if err := graph.ValidateIntegrity(g); err != nil {
		t.Errorf("derived_from of existing node and edge ids: got %v, want nil", err)
	}
}

func TestValidateIntegrity_EdgeIDAndDerivedFromErrorsAllReported(t *testing.T) {
	g := loadFixture(t)
	g.Edges = append(g.Edges, g.Edges[0])
	e := edgeByID(t, &g, "edge_cursor_reaches_db_servicerole")
	e.DerivedFrom = append(e.DerivedFrom, "cap_ghost", "edge_ghost")
	g.Nodes = append(g.Nodes, g.Nodes[0])
	wantIntegrityError(t, g,
		`duplicate edge id "`+g.Edges[0].ID+`"`,
		`derived_from "cap_ghost" is neither a node nor an edge`,
		`derived_from "edge_ghost" is neither a node nor an edge`,
		`duplicate node id "agent_cursor"`,
	)
}
