package secrets

import (
	"strings"
	"testing"
)

// The span MatchRefs fingerprints in a file is the one ClassifyEnv picks
// for the same value in a .env line (CTO-7 §2.2): the same key, the same
// fingerprint, the same graph node.
func TestContentSpanMatchesClassifyEnv(t *testing.T) {
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIabc\n-----END RSA PRIVATE KEY-----"
	for _, c := range []struct{ key, value, line string }{
		{"GITHUB_TOKEN", "ghp_" + strings.Repeat("a", 36), ""},
		{"RAILWAY_TOKEN", strings.Repeat("r", 40), ""},
		{"DATABASE_URL", "postgres://app:pw@db.example.com/app", ""},
		{"Authorization", "Bearer " + strings.Repeat("b", 20), "Authorization: Bearer " + strings.Repeat("b", 20)},
		{"KEY", pem, "KEY=" + pem},
		{"SUPABASE_SECRET_KEY", "sb_secret_" + strings.Repeat("S", 22) + "_" + strings.Repeat("T", 8), ""},
	} {
		line := c.line
		if line == "" {
			line = c.key + "=" + c.value
		}
		_, want, ok := ClassifyEnv(c.key, NewSecretValue(c.value), BuiltinPatterns())
		if !ok {
			t.Fatalf("%s: not classified", c.key)
		}
		var got []string
		MatchRefs([]byte("# config\n"+line+"\n"), BuiltinPatterns(), nil, func(_, secret string) Ref {
			got = append(got, secret)
			return Ref{Fingerprint: "x"}
		})
		if len(got) == 0 || got[0] != want {
			t.Errorf("%s: span %q, want %q", c.key, got, want)
		}
	}
}

// MatchRefs sets the line and the Ref; Match never does.
func TestMatchRefsLineAndRef(t *testing.T) {
	content := []byte("a\nb\ntoken = ghp_" + strings.Repeat("z", 36) + "\n")
	m := MatchRefs(content, BuiltinPatterns(), nil, func(_, _ string) Ref { return Ref{Fingerprint: "fp"} })
	if len(m) != 1 || m[0].Line != 3 || m[0].Ref == nil || m[0].Ref.Fingerprint != "fp" {
		t.Fatalf("%+v", m)
	}
	if p := Match(content, BuiltinPatterns()); p[0].Ref != nil || p[0].Line != 0 {
		t.Errorf("Match sets refs: %+v", p)
	}
}
