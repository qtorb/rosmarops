package graph

import (
	"strings"
	"testing"
	"time"
)

// TestEncodeNamesFailingNode (BV-202 point 3): when a node fails the schema's
// node oneOf, the Encode error names the node's kind, type, basis and the
// offending property — and never a value, a path or an id.
func TestEncodeNamesFailingNode(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	seen := now.UTC().Format(time.RFC3339)
	const fakeSecret = "sk-proj-FAKE-secret-never-in-error-0000"
	const fakeID = "cap_deadbeefdeadbeef"
	const fakePath = "/home/albert/.codex/config.toml"

	g := Build(Fragment{Nodes: []Node{
		// A product_default capability must not carry a rule: it does here.
		{
			ID: fakeID, Kind: KindCapability, Type: CapNetwork, Basis: BasisProductDefault,
			InForce: true, Approval: ApprovalAllow, TargetScope: "any_destination",
			Source: "kb:gemini-cli/web_search_default@x", Rule: fakeSecret,
		},
		// A git_repository resource must not carry configures: it does here.
		{
			ID: "res_git_abc", Kind: KindResource, Type: ResourceGitRepository,
			Path: fakePath, DiscoveredAt: seen, Configures: "agent_x",
		},
	}}, Provenance{EngineVersion: "t", RulesVersion: RulesVersion, KnowledgeVersion: "k", HostID: "host_0"}, now)

	_, err := Encode(g)
	if err == nil {
		t.Fatal("expected schema failure")
	}
	msg := err.Error()

	// Names kind/type/basis, the nearest oneOf branch and the property.
	for _, want := range []string{
		"kind=capability", "type=network", "basis=product_default",
		"oneof-branch=capability", "rule (not allowed here)",
		"kind=resource", "type=git_repository",
		"oneof-branch=resource", "configures (not allowed here)",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q:\n%s", want, msg)
		}
	}
	// Never a value, a path or an id.
	for _, leak := range []string{fakeSecret, fakeID, fakePath, "res_git_abc", "agent_x"} {
		if strings.Contains(msg, leak) {
			t.Errorf("error leaked %q:\n%s", leak, msg)
		}
	}
}

// TestDiagnosticTypeBasisNotInEnum (BV-202 C3, CTO-28): when a node's type or
// basis is not a schema enum value, the diagnostic writes <not-in-enum>, never
// the value itself — the brief's "nunca un valor" covers these fields too.
func TestDiagnosticTypeBasisNotInEnum(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	const secret = "sk-proj-FAKE-cto28-secret-00000000"
	prov := Provenance{EngineVersion: "t", RulesVersion: RulesVersion, KnowledgeVersion: "k", HostID: "host_0"}

	t.Run("type", func(t *testing.T) {
		g := Build(Fragment{Nodes: []Node{{
			ID: "cap_a", Kind: KindCapability, Type: secret, Basis: BasisProductDefault,
			InForce: true, Approval: ApprovalAllow, TargetScope: "any_destination", Source: "kb:x@1",
		}}}, prov, now)
		_, err := Encode(g)
		if err == nil {
			t.Fatal("expected schema failure")
		}
		msg := err.Error()
		if strings.Contains(msg, secret) {
			t.Errorf("diagnostic leaked the non-enum type value:\n%s", msg)
		}
		if !strings.Contains(msg, "type=<not-in-enum>") {
			t.Errorf("diagnostic did not mark the non-enum type:\n%s", msg)
		}
	})

	t.Run("basis", func(t *testing.T) {
		g := Build(Fragment{Nodes: []Node{{
			ID: "cap_b", Kind: KindCapability, Type: CapNetwork, Basis: secret,
			InForce: true, Approval: ApprovalAllow, TargetScope: "any_destination", Source: "kb:x@1",
		}}}, prov, now)
		_, err := Encode(g)
		if err == nil {
			t.Fatal("expected schema failure")
		}
		msg := err.Error()
		if strings.Contains(msg, secret) {
			t.Errorf("diagnostic leaked the non-enum basis value:\n%s", msg)
		}
		if !strings.Contains(msg, "basis=<not-in-enum>") {
			t.Errorf("diagnostic did not mark the non-enum basis:\n%s", msg)
		}
	})
}
