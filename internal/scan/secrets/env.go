package secrets

import (
	"regexp"
	"strings"
)

// pemFooter matches the closing line of a PEM private key block.
var pemFooter = regexp.MustCompile(`-----END\s+(?:RSA\s+|EC\s+|DSA\s+|OPENSSH\s+|ENCRYPTED\s+)?PRIVATE\s+KEY-----`)

// bearerPrefix matches a leading "Bearer " scheme, case-insensitively, and an
// "Authorization:" header name before it when the value carries the whole
// header, so the secret is always the bare token.
var bearerPrefix = regexp.MustCompile(`(?i)^(?:authorization[\s:=]+["']?)?bearer\s+`)

// ClassifyEnv classifies an environment variable and returns the pattern that
// matched and the secret: the span of the match that falls inside the value.
//
// Attempts, in order, each using the patterns in the given order:
//  1. the value alone;
//  2. the synthetic line key + "=" + value;
//  3. the synthetic line key + ": " + value.
//
// Attempts 2 and 3 let keyword-anchored patterns (RAILWAY_TOKEN=,
// DATABASE_URL=, Authorization: Bearer) match when the keyword is the variable
// name. The secret never includes the key or the synthetic separator: only
// the part of the match inside the value, with surrounding spaces trimmed. If
// that part is empty there is no credential. Pattern-specific span rules:
//   - private_key_pem: from the BEGIN header to the matching END footer, both
//     included (to the end of the value if there is no footer), so two
//     different keys never share a span;
//   - authorization_bearer_hardcoded: a leading "Bearer " is removed (and an
//     "Authorization:" header name before it, if the value has one).
//
// This span rule (the value, never the name) is the one every future reader
// (e.g. .env files) must use, so that the same secret yields the same
// fingerprint in any context.
//
// Readers of key=value formats (.env, settings) MUST split key and value first
// and call ClassifyEnv; never classify the raw line.
//
// The returned secret is in clear: never log or persist it. Use it only to
// compute a fingerprint.
func ClassifyEnv(key string, value SecretValue, patterns []Pattern) (patternID, secret string, ok bool) {
	v := value.Reveal()
	for _, prefix := range []string{"", key + "=", key + ": "} {
		line := prefix + v
		off := len(prefix)
		for _, p := range patterns {
			loc := p.re.FindStringIndex(line)
			if loc == nil {
				continue
			}
			start, end := loc[0]-off, loc[1]-off
			if start < 0 {
				start = 0
			}
			if end <= start {
				continue // the match lies entirely in the key or separator
			}
			if s := secretSpan(p.ID, v, start, end); s != "" {
				return p.ID, s, true
			}
		}
	}
	return "", "", false
}

// secretSpan applies the pattern-specific span rule to v[start:end].
func secretSpan(patternID, v string, start, end int) string {
	switch patternID {
	case "private_key_pem":
		if f := pemFooter.FindStringIndex(v[end:]); f != nil {
			end += f[1]
		} else {
			end = len(v)
		}
		return strings.TrimSpace(v[start:end])
	case "authorization_bearer_hardcoded":
		s := strings.TrimSpace(v[start:end])
		return strings.TrimSpace(bearerPrefix.ReplaceAllString(s, ""))
	default:
		return strings.TrimSpace(v[start:end])
	}
}
