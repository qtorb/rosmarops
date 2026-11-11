// Package graph loads and validates the v2 capability graph.
// A graph file is plain JSON validated against the embedded graph.v2 schema,
// then checked for referential integrity (what JSON Schema cannot express).
// A graph that fails either check is rejected: no graph is produced.
package graph

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/qtorb/rosmarops/internal/intel"
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
	EdgeReadsConfigOf   = "reads_config_of" // BV-208 C2: a client reads another product's config at rest
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

	// Malicious, when set, is the threat-data match for this node (BV-100,
	// F0a). In memory only: it is never serialized to graph.v2.json, so the
	// graph schema does not change. The rules layer emits
	// package_known_malicious from it.
	Malicious *intel.Match `json:"-"`

	// ScriptPath and ScriptSize, when set on an agent_executable resource
	// built from a project settings file, say its command runs a script that
	// lives inside the project (BV-107): the script's project-relative path
	// and its size in bytes. In memory only, like Malicious — never
	// serialized to graph.v2.json, so the graph schema does not change; the
	// rules layer emits project_hook_runs_repo_script from them.
	ScriptPath string `json:"-"`
	ScriptSize int64  `json:"-"`

	// ExternalAction, when set on an mcp_server node, is the catalog match for
	// a known MCP server that acts outside the machine (BV-117/BV-121). In
	// memory only, like Malicious — never serialized to graph.v2.json, so the
	// graph schema does not change; the rules layer emits mcp_external_action
	// and the services list from it.
	ExternalAction *ExternalActionMatch `json:"-"`

	// N8N, when set on an mcp_server node, marks the server as an n8n instance
	// used over MCP (BV-175): the signal that recognised it. In memory only,
	// like Malicious — never serialized to graph.v2.json, so the graph schema
	// does not change; the rules layer emits n8n_mcp_bridge from it.
	N8N *N8NMatch `json:"-"`

	// CodexUntrustedRoots, when set on a Codex ai_client node, lists the repo
	// roots the user config marks trust_level = "untrusted" (BV-140, CTO-16
	// Codex §4–§5): Codex loads nothing from them and asks before commands
	// there, so its inference writes nothing inside them. In memory only, set
	// after Build (MarkCodexUntrusted), like Malicious — never serialized.
	CodexUntrustedRoots []string `json:"-"`

	// GeminiUntrustedRoots, when set on a Gemini CLI ai_client node, lists the
	// repo roots that are not trusted folders (BV-193, CTO-22 Gemini §2): Gemini
	// loads nothing from them, so its inference reaches no credential and writes
	// no config inside them. In memory only, set after Build
	// (MarkGeminiUntrusted), like Malicious — never serialized.
	GeminiUntrustedRoots []string `json:"-"`

	// N8NKey, when true on a file resource node for an n8n user folder, marks
	// that the folder holds n8n's database alongside the config with its
	// credential encryption key (BV-175). In memory only, like Malicious —
	// never serialized to graph.v2.json; the rules layer emits
	// n8n_key_with_data from it. The key's value is never read, only its
	// presence (invariant 3).
	N8NKey bool `json:"-"`
}

// N8NMatch marks an mcp_server node recognised as an n8n instance (BV-175).
// Signal is how it was recognised ("endpoint", "cloud", "port" or "package");
// Host is the host of the matching URL, empty for a package/stdio match. The
// path, query and any header or token are never carried — only the host, which
// the finding shows alongside the server name.
type N8NMatch struct {
	Signal string
	Host   string
}

// ExternalActionMatch is an mcp_server node's catalog match (BV-117/BV-121):
// the service and its category, whether it writes, and whether the local
// configuration makes it read-only (a read-only server is still listed but
// yields no finding).
type ExternalActionMatch struct {
	ID       string // catalog entry id
	Service  string // display name from the catalog
	Category string // money, infrastructure, …
	Writes   bool   // the catalog says the server writes
	ReadOnly bool   // --read-only, read_only=true, /readonly or DISALLOW_WRITES
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
	EdgeReadsConfigOf: {
		from: []endpoint{{kind: KindAgent}},
		to:   []endpoint{{kind: KindAgent}},
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
// one message per violation (none when valid). A node that fails the top-level
// `node` oneOf is re-checked against its own kind so the message names the kind,
// type, basis and the property that fails — never a value, a path or an id
// (BV-202 point 3).
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
		if d := explainNodeOneOf(raw, e); d != "" {
			msgs[i] = d
			continue
		}
		msgs[i] = e.String()
	}
	return msgs, nil
}

// kindDefinition maps a node kind to its schema definition name.
var kindDefinition = map[string]string{
	KindAgent: "agent", KindCapability: "capability",
	KindCredential: "credential", KindResource: "resource",
}

// nodeKindSchemas compiles, once, a validator per node kind from the graph
// schema's own definitions, so a node that fails the `node` oneOf can be
// re-validated against the single schema its kind should satisfy.
var nodeKindSchemas = map[string]*schemaval.Schema{}

func kindSchema(kind string) (*schemaval.Schema, bool) {
	if s, ok := nodeKindSchemas[kind]; ok {
		return s, s != nil
	}
	def, ok := kindDefinition[kind]
	if !ok {
		nodeKindSchemas[kind] = nil
		return nil, false
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(schemas.GraphV2Schema, &root); err != nil {
		nodeKindSchemas[kind] = nil
		return nil, false
	}
	wrapper, err := json.Marshal(map[string]any{
		"$ref":        "#/definitions/" + def,
		"definitions": json.RawMessage(root["definitions"]),
	})
	if err != nil {
		nodeKindSchemas[kind] = nil
		return nil, false
	}
	s, err := schemaval.Compile(wrapper)
	if err != nil {
		nodeKindSchemas[kind] = nil
		return nil, false
	}
	nodeKindSchemas[kind] = s
	return s, true
}

// explainNodeOneOf turns a bare "nodes.N: Must validate one and only one schema
// (oneOf)" into a message naming the node's kind, type, basis and the failing
// property, with no value, path or id. It returns "" for any other error.
func explainNodeOneOf(raw []byte, e schemaval.Error) string {
	if !strings.HasPrefix(e.Field, "nodes.") || !strings.Contains(e.Description, "oneOf") {
		return ""
	}
	idxStr := strings.TrimPrefix(e.Field, "nodes.")
	if i := strings.IndexByte(idxStr, '.'); i >= 0 {
		idxStr = idxStr[:i]
	}
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		return ""
	}
	var doc struct {
		Nodes []map[string]json.RawMessage `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || idx < 0 || idx >= len(doc.Nodes) {
		return ""
	}
	n := doc.Nodes[idx]
	kind := jsonString(n["kind"])
	// type and basis are echoed only when they are real schema enum values;
	// anything else becomes <not-in-enum>, so the diagnostic never repeats a
	// value that is not an id, enum or stable code (BV-202 C3, CTO-28).
	desc := "node kind=" + orDash(kind) +
		" type=" + enumOrMarker(kind, "type", jsonString(n["type"])) +
		" basis=" + enumOrMarker(kind, "basis", jsonString(n["basis"]))
	// The node oneOf has one branch per kind (a const). The nearest branch is
	// the one for this node's kind; name it so the reader knows which subschema
	// the properties below were checked against. Unknown kind → no branch.
	if def, ok := kindDefinition[kind]; ok {
		desc += " oneof-branch=" + def
	} else {
		desc += " oneof-branch=none"
	}
	props := failingProperties(n, kind)
	if len(props) == 0 {
		return e.Field + ": " + desc + " does not match its kind's schema"
	}
	return e.Field + ": " + desc + " invalid property: " + strings.Join(props, ", ")
}

// failingProperties re-validates one node against its kind's schema and returns
// the names of the properties that fail — only names, never values. Returns nil
// when the kind is unknown or the node validates (an impossible oneOf match).
func failingProperties(node map[string]json.RawMessage, kind string) []string {
	s, ok := kindSchema(kind)
	if !ok {
		return nil
	}
	raw, err := json.Marshal(node)
	if err != nil {
		return nil
	}
	res, err := s.Validate(raw)
	if err != nil || res.Valid() {
		return nil
	}
	seen := map[string]bool{}
	var props []string
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			props = append(props, p)
		}
	}
	for _, se := range res.Errors() {
		switch {
		case strings.HasSuffix(se.Description, " is required"):
			add(strings.TrimSuffix(se.Description, " is required") + " (missing)")
		case strings.HasPrefix(se.Description, "Additional property "):
			name := strings.TrimPrefix(se.Description, "Additional property ")
			name = strings.TrimSuffix(name, " is not allowed")
			add(name + " (not allowed)")
		case strings.Contains(se.Description, "Must not validate the schema (not)"):
			// A forbidden-when-present property: name it from the kind's
			// conditional rules by looking only at which key is present.
			for _, p := range forbiddenPresent(node, kind) {
				add(p + " (not allowed here)")
			}
		case se.Field != "(root)":
			add(lastField(se.Field) + " (invalid)")
		default:
			add("(schema)")
		}
	}
	return props
}

// forbiddenPresent names the properties a node carries that its kind's
// conditional rules forbid in that node's shape (value-free).
func forbiddenPresent(node map[string]json.RawMessage, kind string) []string {
	var out []string
	_, hasConfigures := node["configures"]
	_, hasRule := node["rule"]
	_, hasReason := node["not_in_force_reason"]
	switch kind {
	case KindResource:
		if hasConfigures {
			out = append(out, "configures")
		}
	case KindCapability:
		if hasRule {
			out = append(out, "rule")
		}
		if hasReason {
			out = append(out, "not_in_force_reason")
		}
	}
	return out
}

func jsonString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

var (
	schemaEnumsOnce sync.Once
	typeEnumByKind  map[string]map[string]bool // node kind -> valid type values
	basisEnumSet    map[string]bool            // valid basis values (capability)
)

// loadSchemaEnums reads, once, the valid type values per node kind and the valid
// basis values from the embedded graph schema's own definitions, so the
// diagnostic's enum check stays in step with the schema.
func loadSchemaEnums() {
	typeEnumByKind = map[string]map[string]bool{}
	basisEnumSet = map[string]bool{}
	var root struct {
		Definitions map[string]struct {
			Properties struct {
				Type struct {
					Enum []string `json:"enum"`
				} `json:"type"`
				Basis struct {
					Enum []string `json:"enum"`
				} `json:"basis"`
			} `json:"properties"`
		} `json:"definitions"`
	}
	if json.Unmarshal(schemas.GraphV2Schema, &root) != nil {
		return
	}
	for kind, def := range kindDefinition {
		d, ok := root.Definitions[def]
		if !ok {
			continue
		}
		set := map[string]bool{}
		for _, v := range d.Properties.Type.Enum {
			set[v] = true
		}
		typeEnumByKind[kind] = set
		for _, v := range d.Properties.Basis.Enum {
			basisEnumSet[v] = true
		}
	}
}

// enumOrMarker returns value when it is a real schema enum value for the field
// under kind, "-" when empty, and "<not-in-enum>" otherwise — so a type or
// basis that is not a known enum (a value from elsewhere) is never echoed
// (BV-202 C3).
func enumOrMarker(kind, field, value string) string {
	if value == "" {
		return "-"
	}
	schemaEnumsOnce.Do(loadSchemaEnums)
	ok := false
	switch field {
	case "type":
		ok = typeEnumByKind[kind][value]
	case "basis":
		ok = basisEnumSet[value]
	}
	if ok {
		return value
	}
	return "<not-in-enum>"
}

func lastField(field string) string {
	if i := strings.LastIndexByte(field, '.'); i >= 0 {
		return field[i+1:]
	}
	return field
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
