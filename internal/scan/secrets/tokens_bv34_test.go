package secrets

import (
	"strings"
	"testing"
)

// BV-34 token formats.
//   - gho_, ghu_: https://github.blog/security/behind-githubs-new-authentication-token-formats/
//     ("gho for OAuth access tokens", "ghu for GitHub user-to-server tokens",
//     "all without changing the token length": the ghp_ body, 36 characters).
//   - npm_: https://github.blog/security/announcing-npms-new-access-token-format
//     ("access tokens now start with an identifiable prefix: npm", "the
//     delimiter following after is no longer a - but an underscore _";
//     178 bits / log2(62) ≈ 29.9 → 30 Base62 characters + 6 of CRC32
//     checksum = 36).
func TestBV34TokenPatterns(t *testing.T) {
	body := strings.Repeat("A1b2C3", 6) // 36 alphanumerics
	cases := []struct {
		id, text string
		want     bool
	}{
		{"github_oauth_token", "oauth_token: gho_" + body, true},
		{"github_oauth_token", "gho_" + body[:35], false}, // length 35
		{"github_oauth_token", "gho" + body, false},       // no underscore
		{"github_oauth_token", "gho-" + body, false},      // wrong delimiter
		{"github_user_to_server_token", "x=ghu_" + body + "\n", true},
		{"github_user_to_server_token", "ghu_" + body[:35], false}, // length 35
		{"github_user_to_server_token", "ghu" + body, false},       // no underscore
		{"npm_access_token", "//registry.npmjs.org/:_authToken=npm_" + body, true},
		{"npm_access_token", "npm_" + body[:35], false}, // length 35
		{"npm_access_token", "npm" + body, false},       // no underscore
		{"npm_access_token", "npm-" + body, false},      // the old "-" delimiter
	}
	for _, c := range cases {
		var got bool
		for _, m := range Match([]byte(c.text), BuiltinPatterns()) {
			got = got || m.PatternID == c.id
		}
		if got != c.want {
			t.Errorf("%s on %q: matched %v, want %v", c.id, c.text, got, c.want)
		}
	}
}
