package yamlsubset

import (
	"strings"
	"testing"
)

// BV-101, decision 1 (LECTURA-103 §5): ParseKeys reads only the given top-level
// keys strictly and skips every other section, so an unsupported construct
// outside the read keys is not an error, while the same construct inside a read
// key's section stays a parse error (fail-secure).

// A representative config in the shape Hermes writes at install: the generator is
// DEFAULT_CONFIG plus the commented example blocks, written by ruamel
// (hermes_cli/config.py _commented_sections_for_save; config_defaults.py).
// ruamel emits empty mappings as the flow form `{}`, and Hermes documents the
// custom-provider form `model: {name:…, provider:…}` — both are outside the
// subset. The dogfood failure was exactly such a `{` (LOCALAPPDATA\hermes\
// config.yaml line 1366). None of it is under a key RosmarOps reads.
const hermesDefaultShape = `# Hermes configuration
model: {name: my-model, provider: custom, base_url: https://api.example/v1}
compression:
  enabled: true
  ratios: {}
fallback_model: []
database:
  dsn: &dsn "sqlite:///x"
  replica: *dsn
notes: |
  a multi-line
  block scalar
approvals:
  mode: smart
  timeout: 300
security:
  protected_instruction_files: true
mcp_servers:
  files:
    command: npx
    args: ["-y", "server"]
`

func TestParseKeys_SkipsUnreadConstructsInRealShape(t *testing.T) {
	v, err := ParseKeys([]byte(hermesDefaultShape), "approvals", "security", "mcp_servers")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := v.(Map)
	if !ok {
		t.Fatalf("not a map: %T", v)
	}
	// The read keys are present and correct.
	if s, ok := m["approvals"].(Map)["mode"].(Scalar); !ok || s.Text != "smart" {
		t.Errorf("approvals.mode = %#v", m["approvals"])
	}
	if _, ok := m["mcp_servers"].(Map)["files"]; !ok {
		t.Errorf("mcp_servers.files missing: %#v", m["mcp_servers"])
	}
	if _, ok := m["security"].(Map)["protected_instruction_files"].(Scalar); !ok {
		t.Errorf("security.protected_instruction_files missing: %#v", m["security"])
	}
	// The skipped sections (inline map, flow seq, anchors, alias, block scalar)
	// are not in the result and did NOT raise an error.
	for _, k := range []string{"model", "compression", "fallback_model", "database", "notes"} {
		if _, present := m[k]; present {
			t.Errorf("unread key %q should be skipped, got %#v", k, m[k])
		}
	}
}

func TestParseKeys_FailsInsideApprovals(t *testing.T) {
	doc := "model: {x: 1}\napprovals:\n  mode: {a: 1}\n" // inline map INSIDE a read key
	if _, err := ParseKeys([]byte(doc), "approvals", "security", "mcp_servers"); err == nil {
		t.Fatal("an unsupported construct inside approvals must be a parse error")
	} else if !strings.Contains(err.Error(), "{") {
		t.Errorf("error should point at the construct: %v", err)
	}
}

func TestParseKeys_FailsInsideMcpServers(t *testing.T) {
	doc := "model: {x: 1}\nmcp_servers: {}\n" // {} INSIDE a read key
	if _, err := ParseKeys([]byte(doc), "approvals", "security", "mcp_servers"); err == nil {
		t.Fatal("an unsupported construct inside mcp_servers must be a parse error")
	}
	// A multi-line flow mapping inside mcp_servers also fails.
	doc2 := "mcp_servers:\n  files: {command: npx}\n"
	if _, err := ParseKeys([]byte(doc2), "approvals", "security", "mcp_servers"); err == nil {
		t.Fatal("an inline mapping inside mcp_servers must be a parse error")
	}
}

func TestParseKeys_UnsupportedUnderUnreadKeyIsOK(t *testing.T) {
	// Anchor/alias and inline constructs under non-read keys only.
	doc := "base: &b\n  x: 1\nother: *b\nextra: {a: [1, 2], b: {c: 3}}\n"
	v, err := ParseKeys([]byte(doc), "approvals", "mcp_servers")
	if err != nil {
		t.Fatalf("unread unsupported constructs must be skipped: %v", err)
	}
	if m, _ := v.(Map); len(m) != 0 {
		t.Errorf("no read keys present, want empty map, got %#v", m)
	}
}

func TestParseKeys_DuplicateReadKey(t *testing.T) {
	doc := "approvals:\n  mode: smart\napprovals:\n  mode: off\n"
	if _, err := ParseKeys([]byte(doc), "approvals"); err == nil {
		t.Fatal("a duplicate read key must be a parse error")
	}
}

func TestParseKeys_EmptyAndComments(t *testing.T) {
	v, err := ParseKeys([]byte("# only comments\n\n"), "approvals")
	if err != nil {
		t.Fatalf("comments-only: %v", err)
	}
	if v != nil {
		if m, ok := v.(Map); !ok || len(m) != 0 {
			t.Errorf("comments-only should yield nil or empty map, got %#v", v)
		}
	}
}
