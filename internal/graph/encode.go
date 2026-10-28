package graph

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
)

// SchemaVersion is the graph schema version this package reads and writes.
const SchemaVersion = 2

// RulesVersion is the rules version recorded in provenance until a rules
// layer exists.
const RulesVersion = "0"

// KnowledgeVersion is the knowledge version recorded in provenance: the
// composite of every embedded base (decision 2026-10-25, cómo trabajan tus
// agentes, cálculo, T9).
const KnowledgeVersion = knowledge.Composite

// HostIDUnset is a placeholder provenance host_id for graphs built in tests.
// Real runs use HostID(salt).
const HostIDUnset = "unset"

// MarshalJSON emits only the fields defined for the node's kind, so the
// output satisfies the schema's additionalProperties: false.
func (n Node) MarshalJSON() ([]byte, error) {
	switch n.Kind {
	case KindAgent:
		return json.Marshal(struct {
			ID             string `json:"id"`
			Kind           string `json:"kind"`
			Type           string `json:"type"`
			Name           string `json:"name,omitempty"`
			Platform       string `json:"platform"`
			ConfigPath     string `json:"config_path"`
			DiscoveredAt   string `json:"discovered_at"`
			Transport      string `json:"transport,omitempty"`
			Command        string `json:"command,omitempty"`
			DestinationRef string `json:"destination_ref,omitempty"`
		}{n.ID, n.Kind, n.Type, n.Name, n.Platform, n.ConfigPath, n.DiscoveredAt, n.Transport, n.Command, n.DestinationRef})
	case KindCapability:
		return json.Marshal(struct {
			ID               string `json:"id"`
			Kind             string `json:"kind"`
			Type             string `json:"type"`
			Basis            string `json:"basis"`
			InForce          bool   `json:"in_force"`
			NotInForceReason string `json:"not_in_force_reason,omitempty"`
			Approval         string `json:"approval"`
			TargetScope      string `json:"target_scope"`
			AppliesIn        string `json:"applies_in"`
			Source           string `json:"source"`
			Rule             string `json:"rule,omitempty"`
		}{
			n.ID, n.Kind, n.Type, n.Basis, n.InForce, n.NotInForceReason,
			n.Approval, n.TargetScope, n.AppliesIn, n.Source, n.Rule,
		})
	case KindCredential:
		return json.Marshal(struct {
			ID           string   `json:"id"`
			Kind         string   `json:"kind"`
			Type         string   `json:"type"`
			Service      string   `json:"service"`
			Fingerprint  string   `json:"fingerprint"`
			Locations    []string `json:"locations"`
			DiscoveredAt string   `json:"discovered_at"`
		}{n.ID, n.Kind, n.Type, n.Service, n.Fingerprint, nonNil(n.Locations), n.DiscoveredAt})
	case KindResource:
		return json.Marshal(struct {
			ID           string `json:"id"`
			Kind         string `json:"kind"`
			Type         string `json:"type"`
			Path         string `json:"path"`
			DiscoveredAt string `json:"discovered_at"`
			Configures   string `json:"configures,omitempty"`
			Family       string `json:"family,omitempty"`
		}{n.ID, n.Kind, n.Type, n.Path, n.DiscoveredAt, n.Configures, n.Family})
	default:
		return nil, fmt.Errorf("node %q: cannot encode unknown kind %q", n.ID, n.Kind)
	}
}

// MarshalJSON emits only the fields defined for the edge's type.
func (e Edge) MarshalJSON() ([]byte, error) {
	switch e.Type {
	case EdgeHas:
		return json.Marshal(struct {
			ID     string `json:"id"`
			Type   string `json:"type"`
			From   string `json:"from"`
			To     string `json:"to"`
			Source string `json:"source"`
		}{e.ID, e.Type, e.From, e.To, e.Source})
	case EdgeReaches:
		return json.Marshal(struct {
			ID          string   `json:"id"`
			Type        string   `json:"type"`
			From        string   `json:"from"`
			To          string   `json:"to"`
			Source      string   `json:"source"`
			Certainty   string   `json:"certainty"`
			Via         string   `json:"via"`
			DerivedFrom []string `json:"derived_from"`
			Assumptions []string `json:"assumptions"`
		}{e.ID, e.Type, e.From, e.To, e.Source, e.Certainty, e.Via, nonNil(e.DerivedFrom), nonNil(e.Assumptions)})
	case EdgeWritesConfigOf:
		return json.Marshal(struct {
			ID          string   `json:"id"`
			Type        string   `json:"type"`
			From        string   `json:"from"`
			To          string   `json:"to"`
			Source      string   `json:"source"`
			Certainty   string   `json:"certainty"`
			DerivedFrom []string `json:"derived_from"`
			Assumptions []string `json:"assumptions"`
		}{e.ID, e.Type, e.From, e.To, e.Source, e.Certainty, nonNil(e.DerivedFrom), nonNil(e.Assumptions)})
	case EdgeLoads:
		return json.Marshal(struct {
			ID            string `json:"id"`
			Type          string `json:"type"`
			From          string `json:"from"`
			To            string `json:"to"`
			Source        string `json:"source"`
			Certainty     string `json:"certainty"`
			AppliesIn     string `json:"applies_in,omitempty"`
			ApprovalState string `json:"approval_state,omitempty"`
		}{e.ID, e.Type, e.From, e.To, e.Source, e.Certainty, e.AppliesIn, e.ApprovalState})
	case EdgeReceives, EdgeReadsEnv, EdgeSendsTo:
		return json.Marshal(struct {
			ID        string `json:"id"`
			Type      string `json:"type"`
			From      string `json:"from"`
			To        string `json:"to"`
			Source    string `json:"source"`
			Certainty string `json:"certainty"`
		}{e.ID, e.Type, e.From, e.To, e.Source, e.Certainty})
	case EdgeAuthenticatesTo:
		return json.Marshal(struct {
			ID            string `json:"id"`
			Type          string `json:"type"`
			From          string `json:"from"`
			To            string `json:"to"`
			Source        string `json:"source"`
			Certainty     string `json:"certainty"`
			ObservedScope string `json:"observed_scope,omitempty"`
		}{e.ID, e.Type, e.From, e.To, e.Source, e.Certainty, e.ObservedScope})
	case EdgeCommittedIn:
		return json.Marshal(struct {
			ID        string `json:"id"`
			Type      string `json:"type"`
			From      string `json:"from"`
			To        string `json:"to"`
			Source    string `json:"source"`
			Certainty string `json:"certainty"`
			Presence  string `json:"presence"`
			HasRemote bool   `json:"has_remote"`
		}{e.ID, e.Type, e.From, e.To, e.Source, e.Certainty, e.Presence, e.HasRemote})
	default:
		return nil, fmt.Errorf("edge %q: cannot encode unknown type %q", e.ID, e.Type)
	}
}

// Build assembles a complete graph from a fragment. Nil slices become empty
// ones because the schema requires arrays, not nulls.
func Build(f Fragment, prov Provenance, generatedAt time.Time) Graph {
	return Graph{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   generatedAt.UTC().Format(time.RFC3339),
		Coverage: Coverage{
			SourcesRead:       nonNil(f.Coverage.SourcesRead),
			SourcesFailed:     nonNil(f.Coverage.SourcesFailed),
			AgentsUnsupported: nonNil(f.Coverage.AgentsUnsupported),
		},
		Provenance: prov,
		Nodes:      nonNil(f.Nodes),
		Edges:      nonNil(f.Edges),
	}
}

// Encode serializes g as indented JSON and validates the result against the
// embedded schema and for referential integrity. An invalid graph is never
// returned: any violation is an error.
func Encode(g Graph) ([]byte, error) {
	out, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal graph: %w", err)
	}
	msgs, err := schemaErrors(out)
	if err != nil {
		return nil, fmt.Errorf("validate encoded graph: %w", err)
	}
	if len(msgs) > 0 {
		return nil, fmt.Errorf("encoded graph fails schema validation:\n  %s", joinLines(msgs))
	}
	if err := ValidateIntegrity(g); err != nil {
		return nil, fmt.Errorf("encoded graph: %w", err)
	}
	return append(out, '\n'), nil
}

// nonNil returns s, or an empty slice if s is nil, so it encodes as [].
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
