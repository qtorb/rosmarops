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
