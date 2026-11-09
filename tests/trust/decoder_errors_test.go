package trust_test

// BV-202 S2 (CTO-24 §0): the hand-written config decoders must never put the
// bytes they read into their error text. A malformed agent config — a secret
// in a place the format does not allow — is recorded in the report's coverage
// as a parse_error reason, so a decoder error that echoed the offending token
// would leak the secret into graph.v2.json. These errors carry a position and
// a stable code only.

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/jsonc"
	"github.com/qtorb/rosmarops/internal/jsonkeys"
	"github.com/qtorb/rosmarops/internal/tomlsubset"
	"github.com/qtorb/rosmarops/internal/yamlsubset"
)

// decoderSecret is an obvious fake with the OpenAI key shape, placed below in
// positions each format rejects.
const decoderSecret = "sk-proj-FAKEsecretFAKEsecretFAKEsecret0000"

func containsSecret(s string) bool {
	if strings.Contains(s, decoderSecret) {
		return true
	}
	// Also reject the middle, in case of partial echoing.
	mid := decoderSecret[4 : len(decoderSecret)-4]
	return strings.Contains(s, mid)
}

func TestDecoderErrorsCarryNoInput(t *testing.T) {
	// TOML (Codex / Grok config.toml): the secret as an unsupported value, as a
	// value inside an inline table, and as an invalid bare key.
	tomlCases := []string{
		"k = " + decoderSecret + "\n",         // bare, unsupported value — tomlsubset.go:396
		"k = { a = " + decoderSecret + " }\n", // unsupported value in an inline table
		decoderSecret + "$ = 1\n",             // '$' makes an invalid bare key
		"[" + decoderSecret + "$]\nk = 1\n",   // invalid key in a table header
		"a = 1\na = " + decoderSecret + "\n",  // the duplicate-key path sees the value line
	}
	for _, in := range tomlCases {
		if _, err := tomlsubset.Parse([]byte(in)); err == nil {
			t.Errorf("tomlsubset.Parse(%q) did not error", in)
		} else if containsSecret(err.Error()) {
			t.Errorf("tomlsubset error leaked the secret: %q", err.Error())
		}
	}

	// YAML (Hermes config.yaml): the secret as a value starting with a refused
	// character, inside a refused escape, and after an empty key.
	yamlCases := []string{
		"k: @" + decoderSecret + "\n",       // a value starting with a refused char — yamlsubset.go:377
		"k: \"\\q" + decoderSecret + "\"\n", // a refused escape in a quoted scalar — yamlsubset.go:476
		": " + decoderSecret + "\n",         // an empty key, the secret is the value
	}
	for _, in := range yamlCases {
		if _, err := yamlsubset.Parse([]byte(in)); err == nil {
			t.Errorf("yamlsubset.Parse(%q) did not error", in)
		} else if containsSecret(err.Error()) {
			t.Errorf("yamlsubset error leaked the secret: %q", err.Error())
		}
	}

	// JSON (settings.json etc.) through jsonkeys: the secret as an unquoted
	// token (a syntax error), and as the wrong type for a struct field.
	var target struct {
		N int `json:"n"`
	}
	jsonCases := []string{
		`{"a": ` + decoderSecret + `}`,   // unquoted value → syntax error
		`{"n": "` + decoderSecret + `"}`, // a string where an int is expected → type error
		`[` + decoderSecret + `]`,        // unquoted array element → syntax error
	}
	for _, in := range jsonCases {
		if _, err := jsonkeys.Decode([]byte(in), &target); err == nil {
			t.Errorf("jsonkeys.Decode(%q) did not error", in)
		} else if containsSecret(err.Error()) {
			t.Errorf("jsonkeys error leaked the secret: %q", err.Error())
		}
	}

	// jsonc.Strip has no error channel (it returns bytes); its only duty here is
	// not to expand or echo anything into an error. It is covered by jsonkeys
	// above, which strips then decodes. Sanity: Strip of a malformed document
	// with the secret must not panic and returns bytes.
	if out := jsonc.Strip([]byte(`{"a": /* ` + decoderSecret + ` */ 1,}`)); len(out) == 0 {
		t.Error("jsonc.Strip returned nothing")
	}
}
