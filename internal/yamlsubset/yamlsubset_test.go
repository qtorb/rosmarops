package yamlsubset

import (
	"reflect"
	"strings"
	"testing"
)

// The subset of LECTURA-100 §2, as Hermes' config.yaml writes it.
func TestParseSubset(t *testing.T) {
	doc := "\ufeff# Hermes config\r\n" +
		"model:\r\n" +
		"  default: \"anthropic/claude\"  # comment\r\n" +
		"approvals:\n" +
		"  mode: off\n" +
		"  timeout: 300\n" +
		"security:\n" +
		"  protected_instruction_files: Yes\n" +
		"mcp_servers:\n" +
		"  github:\n" +
		"    command: npx\n" +
		"    args: [\"-y\", '@modelcontextprotocol/server-github', plain]\n" +
		"    env:\n" +
		"      GITHUB_TOKEN: \"ghp_x#not-a-comment\"\n" +
		"      QUOTE: 'it''s'\n" +
		"  remote:\n" +
		"    url: https://mcp.example.com/sse?x=1\n" +
		"    headers: {}\n"
	if _, err := Parse([]byte(doc)); err == nil {
		t.Fatal("a flow mapping must make the document unread")
	}
	doc = strings.Replace(doc, "    headers: {}\n", "    headers:\n      Authorization: Bearer abc\n    tags:\n    - one\n    - \"two\"\n    empty: []\n    nothing:\n", 1)
	v, err := Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	m := v.(Map)
	if b, ok := m["approvals"].(Map)["mode"].(Scalar).Bool(); !ok || b {
		t.Errorf("mode: off is false: %v %v", b, ok)
	}
	if b, ok := m["security"].(Map)["protected_instruction_files"].(Scalar).Bool(); !ok || !b {
		t.Error("Yes is true")
	}
	gh := m["mcp_servers"].(Map)["github"].(Map)
	if !reflect.DeepEqual(gh["args"], Seq{Scalar{"-y", true}, Scalar{"@modelcontextprotocol/server-github", true}, Scalar{"plain", false}}) {
		t.Errorf("args %v", gh["args"])
	}
	env := gh["env"].(Map)
	if env["GITHUB_TOKEN"] != (Scalar{"ghp_x#not-a-comment", true}) || env["QUOTE"] != (Scalar{"it's", true}) {
		t.Errorf("env %v", env)
	}
	if m["model"].(Map)["default"] != (Scalar{"anthropic/claude", true}) {
		t.Errorf("model %v", m["model"])
	}
	r := m["mcp_servers"].(Map)["remote"].(Map)
	if r["url"] != (Scalar{Text: "https://mcp.example.com/sse?x=1"}) || r["headers"].(Map)["Authorization"] != (Scalar{Text: "Bearer abc"}) {
		t.Errorf("remote %v", r)
	}
	if !reflect.DeepEqual(r["tags"], Seq{Scalar{Text: "one"}, Scalar{"two", true}}) || !reflect.DeepEqual(r["empty"], Seq{}) || !r["nothing"].(Scalar).IsNull() {
		t.Errorf("remote %v", r)
	}
	if _, ok := (Scalar{"off", true}).Bool(); ok {
		t.Error("a quoted 'off' is a string")
	}
}

// Anything outside the subset makes the whole document unread: never a
// guess, never a partial value.
func TestParseRefuses(t *testing.T) {
	deep := ""
	for i := 0; i <= MaxDepth+1; i++ {
		deep += strings.Repeat("  ", i) + "k:\n"
	}
	deep += strings.Repeat("  ", MaxDepth+2) + "v: 1\n"
	for name, doc := range map[string]string{
		"anchor and nested aliases": "a: &x [1, 2]\nb: [*x, *x]\nc: *x\n",
		"alias as a value":          "a: b\nc: *a\n",
		"anchor on a key":           "&k a: b\n",
		"merge key":                 "base:\n  x: 1\nother:\n  <<: 1\n  y: 2\n",
		"tag":                       "a: !!python/object:os.system x\n",
		"str tag":                   "a: !!str 1\n",
		"two documents":             "a: 1\n---\nb: 2\n",
		"document start":            "---\na: 1\n",
		"document end":              "a: 1\n...\n",
		"directive":                 "%YAML 1.1\na: 1\n",
		"literal block":             "a: |\n  text\n",
		"folded block":              "a: >-\n  text\n",
		"flow mapping":              "a: {b: 1}\n",
		"duplicate key":             "mcp_servers:\n  x:\n    command: a\n  x:\n    command: b\n",
		"duplicate top key":         "a: 1\na: 2\n",
		"tab indentation":           "a:\n\tb: 1\n",
		"inconsistent indentation":  "a:\n    b: 1\n  c: 2\n",
		"over-indented sibling":     "a: 1\n  b: 2\n",
		"multi-line plain":          "a: one\n  two\n",
		"multi-line quoted":         "a: \"one\n  two\"\n",
		"complex key":               "? a\n: b\n",
		"map in a sequence":         "a:\n- b: 1\n",
		"nested sequence":           "a:\n- - b\n",
		"nested flow":               "a: [b, [c]]\n",
		"unclosed flow":             "a: [b, c\n",
		"trailing comma":            "a: [b, ]\n",
		"lone carriage return":      "a: 1\rb: 2\n",
		"bad escape":                "a: \"\\x41\"\n",
		"top-level sequence":        "- a\n- b\n",
		"top-level scalar":          "just text\n",
		"indented start":            "  a: 1\n",
		"too deep":                  deep,
		"not UTF-8":                 "a: \xff\n",
	} {
		if v, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: read as %v", name, v)
		}
	}
	if _, err := Parse(make([]byte, MaxBytes+1)); err == nil {
		t.Error("over 2 MB read")
	}
}

// An empty or comment-only document is empty, not an error.
func TestParseEmpty(t *testing.T) {
	for _, doc := range []string{"", "\n\n", "# only a comment\n   # another\n", "\ufeff"} {
		if v, err := Parse([]byte(doc)); err != nil || v != nil {
			t.Errorf("%q: %v %v", doc, v, err)
		}
	}
}
