package knowledge_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/schemas"
)

// capabilityEnums reads the capability type and approval enums from the
// embedded graph schema, so the base cannot drift from the contract.
func capabilityEnums(t *testing.T) (types, approvals map[string]bool) {
	t.Helper()
	var s struct {
		Definitions struct {
			Capability struct {
				Properties struct {
					Type     struct{ Enum []string } `json:"type"`
					Approval struct{ Enum []string } `json:"approval"`
				} `json:"properties"`
			} `json:"capability"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal(schemas.GraphV2Schema, &s); err != nil {
		t.Fatal(err)
	}
	types, approvals = map[string]bool{}, map[string]bool{}
	for _, v := range s.Definitions.Capability.Properties.Type.Enum {
		types[v] = true
	}
	for _, v := range s.Definitions.Capability.Properties.Approval.Enum {
		approvals[v] = true
	}
	if len(types) == 0 || len(approvals) == 0 {
		t.Fatal("capability enums not found in the graph schema")
	}
	return types, approvals
}

func TestClaudeCode_LoadsStrictly(t *testing.T) {
	b, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatalf("ClaudeCode: %v", err)
	}
	if b.Product != knowledge.ClaudeCodeProduct || b.Version != knowledge.Version {
		t.Errorf("product/version: got %q/%q, want %q/%q", b.Product, b.Version, knowledge.ClaudeCodeProduct, knowledge.Version)
	}
	if len(b.Entries) != 9 {
		t.Errorf("entries: got %d, want 9", len(b.Entries))
	}
}

func TestClaudeCode_EntriesUseSchemaEnums(t *testing.T) {
	types, approvals := capabilityEnums(t)
	b, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range b.Entries {
		if !types[e.Type] {
			t.Errorf("entry %s: type %q is not a capability type of the graph schema", e.ID, e.Type)
		}
		if !approvals[e.Approval] {
			t.Errorf("entry %s: approval %q is not an approval of the graph schema", e.ID, e.Approval)
		}
	}
}

// The protected paths entry is the documented list
// (https://code.claude.com/docs/en/permission-modes#protected-paths), and the
// only entry with a path list.
func TestClaudeCode_ProtectedPaths(t *testing.T) {
	b, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	e, ok := b.Entry("protected_paths")
	if !ok {
		t.Fatal("no protected_paths entry")
	}
	if e.IsCapability() || e.SourceURL != "https://code.claude.com/docs/en/permission-modes#protected-paths" ||
		!strings.HasPrefix(e.Verified, "doc 2026-10-24") {
		t.Errorf("entry: %+v", e)
	}
	if len(e.Paths) != 48 || len(e.Exceptions) != 5 {
		t.Errorf("paths %d, exceptions %d: want 48 and 5", len(e.Paths), len(e.Exceptions))
	}
	for _, want := range []string{".git/", ".claude/", ".mcp.json", ".claude.json", ".npmrc", ".bashrc", "~/.claude/dev-mods/"} {
		found := false
		for _, p := range e.Paths {
			found = found || p == want
		}
		if !found {
			t.Errorf("paths lack %q", want)
		}
	}
	for _, other := range b.Entries {
		if other.ID != e.ID && !other.IsCapability() {
			t.Errorf("entry %s has a path list", other.ID)
		}
	}
}

// E5: the five actions of the rules layer, with the magnitudes of §2 (ask
// 1, a one-line user setting 2, a mode change 3, removing from disk 3), each
// with its source.
func TestClaudeCode_Mitigations(t *testing.T) {
	b, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"ask_edit_rule": 1, "ask_read_rule": 1, "block_reads_outside": 2, "manual_default_mode": 3, "remove_from_disk": 3}
	if len(b.Mitigations) != len(want) {
		t.Errorf("mitigations: %d, want %d", len(b.Mitigations), len(want))
	}
	for id, mag := range want {
		m, ok := b.Mitigation(id)
		if !ok || m.Magnitude != mag || !strings.HasPrefix(m.SourceURL, "https://") || m.Verified == "" {
			t.Errorf("%s: %+v", id, m)
		}
	}
}

// T9 (decision 2026-10-25, cómo trabajan tus agentes, cálculo): the
// composite knowledge_version names every embedded base, sorted, and
// changes with any of them.
func TestComposite(t *testing.T) {
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	if got := knowledge.CompositeOf(bases); got != knowledge.Composite || !strings.HasPrefix(got, "claude-code@") || !strings.Contains(got, ";cursor@") {
		t.Fatalf("composite %q, const %q", got, knowledge.Composite)
	}
	c := bases[knowledge.CursorProduct]
	c.Version = "2026-11-01"
	bases[knowledge.CursorProduct] = c
	if knowledge.CompositeOf(bases) == knowledge.Composite {
		t.Error("a change in cursor.json does not change the composite")
	}
}
