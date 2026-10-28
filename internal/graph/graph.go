// Package graph loads and validates the v2 capability graph.
// A graph file is plain JSON validated against the embedded graph.v2 schema,
// then checked for referential integrity (what JSON Schema cannot express).
// A graph that fails either check is rejected: no graph is produced.
package graph

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/qtorb/rosmarops/internal/schemaval"

	"github.com/qtorb/rosmarops/internal/schemas"
)

// Node kinds.
const (
	KindAgent      = "agent"
	KindCapability = "capability"
	KindCredential = "credential"
	KindResource   = "resource"
)

// Edge types.
const (
	EdgeHas             = "has"
	EdgeReaches         = "reaches"
	EdgeLoads           = "loads"
	EdgeReceives        = "receives"
	EdgeAuthenticatesTo = "authenticates_to"
	EdgeReadsEnv        = "reads_env"
	EdgeWritesConfigOf  = "writes_config_of"
	EdgeCommittedIn     = "committed_in"
	EdgeSendsTo         = "sends_to" // mcp_server → external_service (BV-91, E1)
)

// Resource types referenced by the integrity rules.
const (
	ResourceFile              = "file"
	ResourceExternalService   = "external_service"
	ResourceAgentConfig       = "agent_config"
	ResourceAgentInstructions = "agent_instructions"
	ResourceAgentExecutable   = "agent_executable"
	ResourceGitRepository     = "git_repository"
)

// Graph mirrors schemas/v2/graph.v2.schema.json.
// Node and Edge are flat: they carry every field any variant can have, with
// Kind (nodes) and Type (edges) as the discriminator. Fields that do not apply
// to a variant are left empty. Node and Edge implement json.Marshaler (see
// encode.go) so that encoding emits only the fields of their variant.
type Graph struct {
	SchemaVersion int        `json:"schema_version"`
	GeneratedAt   string     `json:"generated_at"`
	Coverage      Coverage   `json:"coverage"`
	Provenance    Provenance `json:"provenance"`
	Nodes         []Node     `json:"nodes"`
	Edges         []Edge     `json:"edges"`
}

// Coverage records what was read, what failed and which agents were seen but
// cannot be read yet.
type Coverage struct {
	SourcesRead       []string           `json:"sources_read"`
	SourcesFailed     []SourceFailed     `json:"sources_failed"`
	AgentsUnsupported []AgentUnsupported `json:"agents_unsupported"`
}

// SourceFailed is a source that could not be read.
type SourceFailed struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// AgentUnsupported is an agent that was detected but has no reader.
type AgentUnsupported struct {
	Platform   string `json:"platform"`
	ConfigPath string `json:"config_path"`
}

// Provenance identifies what produced the graph.
type Provenance struct {
	EngineVersion    string `json:"engine_version"`
	RulesVersion     string `json:"rules_version"`
	KnowledgeVersion string `json:"knowledge_version"`
	HostID           string `json:"host_id"`
}

// Node is any graph node (agent, capability, credential or resource).
type Node struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Type string `json:"type"`

	// agent. Name is for display only (required for mcp_server); ID is the identifier.
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	ConfigPath string `json:"config_path"`

	// agent, mcp_server only (BV-91, D1/E1): transport, the local command
	// (arguments excluded), and the salted HMAC of the full canonical
	// destination. All optional.
	Transport      string `json:"transport"`
	Command        string `json:"command"`
	DestinationRef string `json:"destination_ref"`

	// capability
	Basis            string `json:"basis"`
	InForce          bool   `json:"in_force"`
	NotInForceReason string `json:"not_in_force_reason"`
	Approval         string `json:"approval"`
	TargetScope      string `json:"target_scope"`
	AppliesIn        string `json:"applies_in"`
	Source           string `json:"source"`
	Rule             string `json:"rule"`

	// credential (never the secret value)
	Service     string   `json:"service"`
	Fingerprint string   `json:"fingerprint"`
	Locations   []string `json:"locations"`

	// resource
	Path       string `json:"path"`
	Configures string `json:"configures"`
	Family     string `json:"family"` // git_repository

	// agent, credential, resource
	DiscoveredAt string `json:"discovered_at"`
}

// Edge is any graph edge.
type Edge struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	From   string `json:"from"`
	To     string `json:"to"`
	Source string `json:"source"`

	// all edges except has
	Certainty string `json:"certainty"`

	// reaches
	Via string `json:"via"`

	// reaches, writes_config_of
	DerivedFrom []string `json:"derived_from"`
	Assumptions []string `json:"assumptions"`

	// authenticates_to
	ObservedScope string `json:"observed_scope"`

	// loads
	AppliesIn     string `json:"applies_in"`
	ApprovalState string `json:"approval_state"`

	// committed_in
	Presence  string `json:"presence"`
	HasRemote bool   `json:"has_remote"`
}

// endpoint is the set of nodes allowed at one end of an edge: a node kind and,
// when non-empty, the node types allowed within that kind.
type endpoint struct {
	kind  string
	types []string
}

// edgeEndpoints is the single table of which nodes each end of each edge type
// accepts. An end with several entries accepts any of them.
var edgeEndpoints = map[string]struct{ from, to []endpoint }{
	EdgeHas: {
		from: []endpoint{{kind: KindAgent}},
		to:   []endpoint{{kind: KindCapability}},
	},
	EdgeReaches: {
		from: []endpoint{{kind: KindAgent}},
		to:   []endpoint{{kind: KindCredential}, {kind: KindResource}},
	},
	EdgeLoads: {
		from: []endpoint{{kind: KindAgent}},
		to:   []endpoint{{kind: KindAgent}},
	},
	EdgeReceives: {
		from: []endpoint{{kind: KindCredential}},
		to:   []endpoint{{kind: KindAgent}},
	},
	EdgeAuthenticatesTo: {
		from: []endpoint{{kind: KindCredential}},
		to:   []endpoint{{kind: KindResource, types: []string{ResourceExternalService}}},
	},
	EdgeReadsEnv: {
		from: []endpoint{{kind: KindResource, types: []string{ResourceFile}}},
		to:   []endpoint{{kind: KindCredential}},
	},
	EdgeCommittedIn: {
		from: []endpoint{{kind: KindCredential}},
		to:   []endpoint{{kind: KindResource, types: []string{ResourceGitRepository}}},
	},
	EdgeWritesConfigOf: {
		from: []endpoint{{kind: KindAgent}},
		to: []endpoint{{kind: KindResource, types: []string{
			ResourceAgentConfig, ResourceAgentInstructions, ResourceAgentExecutable,
		}}},
	},
	EdgeSendsTo: {
		from: []endpoint{{kind: KindAgent, types: []string{AgentMCPServer}}},
		to:   []endpoint{{kind: KindResource, types: []string{ResourceExternalService}}},
	},
}

// compiledSchema is the parsed graph schema, initialized once on first use.
var compiledSchema *schemaval.Schema

func getSchema() (*schemaval.Schema, error) {
	if compiledSchema != nil {
		return compiledSchema, nil
	}
	s, err := schemaval.Compile(schemas.GraphV2Schema)
	if err != nil {
		return nil, fmt.Errorf("compile graph schema: %w", err)
	}
	compiledSchema = s
	return compiledSchema, nil
}

// schemaErrors validates raw JSON against the embedded graph schema and returns
// one message per violation (none when valid).
func schemaErrors(raw []byte) ([]string, error) {
	schema, err := getSchema()
	if err != nil {
		return nil, err
	}
	result, err := schema.Validate(raw)
	if err != nil {
		return nil, err
	}
	errs := result.Errors()
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.String()
	}
	return msgs, nil
}

// LoadGraph reads the graph file at path, validates it against the embedded
// schema and for referential integrity, and returns a typed Graph.
// Any failure is a hard error: an invalid graph is never returned.
func LoadGraph(path string) (Graph, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Graph{}, fmt.Errorf("read graph %s: %w", path, err)
	}

	// Validate JSON syntax first so we get a clean parse error, not a schema error.
	var probe any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return Graph{}, fmt.Errorf("parse graph %s: %w", path, err)
	}

	msgs, err := schemaErrors(raw)
	if err != nil {
		return Graph{}, fmt.Errorf("validate graph %s: %w", path, err)
	}
	if len(msgs) > 0 {
		return Graph{}, fmt.Errorf("graph %s fails schema validation:\n  %s",
			path, joinLines(msgs))
	}

	var g Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		return Graph{}, fmt.Errorf("decode graph %s: %w", path, err)
	}

	if g.SchemaVersion != 2 {
		return Graph{}, fmt.Errorf("graph %s: unsupported schema_version %d (expected 2)", path, g.SchemaVersion)
	}

	if err := ValidateIntegrity(g); err != nil {
		return Graph{}, fmt.Errorf("graph %s: %w", path, err)
	}

	return g, nil
}

// ValidateIntegrity checks what the JSON Schema cannot: unique node and edge
// ids, edge and attribute references that resolve to existing nodes,
// derived_from ids that resolve to a node or an edge, and the node kind/type
// allowed at each end of each edge type. It reports every problem found, not
// just the first.
func ValidateIntegrity(g Graph) error {
	var msgs []string

	nodes := make(map[string]Node, len(g.Nodes))
	for _, n := range g.Nodes {
		if _, dup := nodes[n.ID]; dup {
			msgs = append(msgs, fmt.Sprintf("duplicate node id %q", n.ID))
			continue
		}
		nodes[n.ID] = n
	}

	for _, n := range g.Nodes {
		if n.Configures == "" {
			continue
		}
		target, ok := nodes[n.Configures]
		switch {
		case !ok:
			msgs = append(msgs, fmt.Sprintf("node %q: configures %q does not exist", n.ID, n.Configures))
		case target.Kind != KindAgent:
			msgs = append(msgs, fmt.Sprintf("node %q: configures %q is kind %q, want %s",
				n.ID, n.Configures, target.Kind, KindAgent))
		}
	}

	edges := make(map[string]bool, len(g.Edges))
	for _, e := range g.Edges {
		if edges[e.ID] {
			msgs = append(msgs, fmt.Sprintf("duplicate edge id %q", e.ID))
		}
		edges[e.ID] = true
	}

	for _, e := range g.Edges {
		for _, id := range e.DerivedFrom {
			if _, isNode := nodes[id]; !isNode && !edges[id] {
				msgs = append(msgs, fmt.Sprintf("edge %q (%s): derived_from %q is neither a node nor an edge", e.ID, e.Type, id))
			}
		}
		rule, known := edgeEndpoints[e.Type]
		if !known {
			msgs = append(msgs, fmt.Sprintf("edge %q: unknown type %q", e.ID, e.Type))
			continue
		}
		msgs = append(msgs, checkEnd(e, "from", e.From, rule.from, nodes)...)
		msgs = append(msgs, checkEnd(e, "to", e.To, rule.to, nodes)...)

		if e.Type == EdgeReaches {
			via, ok := nodes[e.Via]
			switch {
			case !ok:
				msgs = append(msgs, fmt.Sprintf("edge %q (%s): via %q does not exist", e.ID, e.Type, e.Via))
			case via.Kind != KindCapability:
				msgs = append(msgs, fmt.Sprintf("edge %q (%s): via %q is kind %q, want %s",
					e.ID, e.Type, e.Via, via.Kind, KindCapability))
			}
		}
	}

	// A credential with no location on disk is one found only in git
	// history: it has a committed_in edge (CTO-7 change 3). family is for
	// git_repository resources only.
	committed := map[string]bool{}
	for _, e := range g.Edges {
		if e.Type == EdgeCommittedIn {
			committed[e.From] = true
		}
	}
	for _, n := range g.Nodes {
		switch {
		case n.Kind == KindCredential && len(n.Locations) == 0 && !committed[n.ID]:
			msgs = append(msgs, fmt.Sprintf("credential %q: no location and no committed_in edge", n.ID))
		case n.Family != "" && (n.Kind != KindResource || n.Type != ResourceGitRepository):
			msgs = append(msgs, fmt.Sprintf("node %q: family is for git_repository resources only", n.ID))
		}
	}

	if len(msgs) > 0 {
		return fmt.Errorf("graph fails integrity validation:\n  %s", joinLines(msgs))
	}
	return nil
}

// checkEnd validates one end of an edge: the node must exist and match one of
// the allowed endpoints.
func checkEnd(e Edge, side, id string, allowed []endpoint, nodes map[string]Node) []string {
	n, ok := nodes[id]
	if !ok {
		return []string{fmt.Sprintf("edge %q (%s): %s %q does not exist", e.ID, e.Type, side, id)}
	}
	for _, a := range allowed {
		if n.Kind == a.kind && (len(a.types) == 0 || contains(a.types, n.Type)) {
			return nil
		}
	}
	return []string{fmt.Sprintf("edge %q (%s): %s %q is %s, want %s",
		e.ID, e.Type, side, id, describeNode(n), describeEndpoints(allowed))}
}

func describeNode(n Node) string {
	if n.Kind == KindResource {
		return fmt.Sprintf("%s of type %q", n.Kind, n.Type)
	}
	return fmt.Sprintf("kind %q", n.Kind)
}

func describeEndpoints(eps []endpoint) string {
	out := ""
	for i, ep := range eps {
		if i > 0 {
			out += " or "
		}
		out += ep.kind
		if len(ep.types) > 0 {
			out += " of type "
			for j, t := range ep.types {
				if j > 0 {
					out += "/"
				}
				out += t
			}
		}
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func joinLines(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += "\n  "
		}
		out += s
	}
	return out
}
