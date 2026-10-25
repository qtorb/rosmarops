package secrets

import (
	"bytes"
	"regexp"
	"strings"
	"time"
)

// Ref is what a scanner keeps of a secret it found in a file or a commit
// (decision 2026-10-24, memoria, §2.2): its salted fingerprint and the
// service it opens, computed while the value is in memory. The value
// itself is discarded at once.
type Ref struct {
	Fingerprint   string
	CredType      string
	Service       string
	ServicePath   string
	ObservedScope string
	Observed      bool // the token itself names its issuer and project
}

// Identify turns a pattern and the secret it matched into a Ref. The secret
// is in clear: an Identify function must never log or keep it.
type Identify func(patternID, secret string) Ref

// MatchRefs is MatchTimed that also sets, on every match, its offset in
// content and the Ref identify makes of its secret. The secret is the same
// span ClassifyEnv picks for a value (the value, never a key name), so a key
// committed in a repository and the same key in a .env file share a
// fingerprint.
func MatchRefs(content []byte, patterns []Pattern, perPattern map[string]time.Duration, identify Identify) []SecretMatch {
	return matchAll(content, patterns, perPattern, identify)
}

// keywordValue is the separator and optional quote after the keyword of a
// keyword-anchored pattern (RAILWAY_TOKEN=, DATABASE_URL:).
var keywordValue = regexp.MustCompile(`^[^=:]*[=:]\s*["']?`)

// contentSpan is the secret of a match in a file or a diff: the match
// without the keyword of keyword-anchored patterns, a bearer token without
// its scheme, and a PEM key from its header to its footer (to the end of
// the line when there is none), trimmed, as secretSpan does for a value.
func contentSpan(patternID string, content []byte, start, end int) string {
	raw := string(content[start:end])
	switch patternID {
	case "railway_api_token", "database_url_with_credentials":
		raw = keywordValue.ReplaceAllString(raw, "")
	case "authorization_bearer_hardcoded":
		raw = strings.TrimSpace(raw)
		if i := strings.Index(strings.ToLower(raw), "bearer"); i >= 0 {
			raw = raw[i:]
		}
		raw = bearerPrefix.ReplaceAllString(raw, "")
	case "private_key_pem":
		rest := content[end:]
		if f := pemFooter.FindIndex(rest); f != nil {
			end += f[1]
		} else if nl := bytes.IndexByte(rest, '\n'); nl >= 0 {
			end += nl
		} else {
			end = len(content)
		}
		raw = string(content[start:end])
	}
	return strings.TrimSpace(raw)
}
