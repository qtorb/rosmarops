// Package secrets implements the built-in secret pattern registry.
//
// Design: Match is a pure function that takes the pattern slice as a parameter.
// Built-in patterns are returned by BuiltinPatterns(); user-defined patterns are
// compiled by the caller (see NewPattern) and appended before calling Match.
// This keeps the package free of any config/state imports and makes unit testing
// trivial — no filesystem access ever occurs here.
package secrets

import (
	"bytes"
	"regexp"
	"time"
)

// Pattern is a compiled secret-detection pattern.
type Pattern struct {
	ID   string // short identifier, e.g. "anthropic_api_key"
	Type string // human-readable type surfaced in findings, e.g. "Anthropic API key"
	re   *regexp.Regexp
	pre  prefilter // necessary literal; empty for user patterns
}

// prefilter is a literal every match of a pattern contains (fold: in any
// ASCII case; a fold literal holds only letters whose case folding is ASCII
// only, so no match escapes it). Content without it cannot match, so the regex is skipped.
// TestBuiltinPrefiltersAreNecessary proves, from each regex's syntax tree,
// that the literal is necessary.
type prefilter struct {
	lit  string
	fold bool
}

// admits reports whether content may match: it contains the literal.
func (p prefilter) admits(content []byte) bool {
	switch {
	case p.lit == "":
		return true
	case !p.fold:
		return bytes.Contains(content, []byte(p.lit))
	default:
		return containsFoldASCII(content, p.lit)
	}
}

// containsFoldASCII reports whether content contains lit, ASCII letters in
// either case. lit is ASCII. Linear in len(content): the next position of
// each case of lit's first byte is searched again only once passed.
func containsFoldASCII(content []byte, lit string) bool {
	if lit == "" {
		return true
	}
	n := len(content)
	lo, up := lower(lit[0]), upper(lit[0])
	nextLo, nextUp := -1, -1
	next := func(c byte, at, from int) int {
		if at >= from {
			return at
		}
		if k := bytes.IndexByte(content[from:], c); k >= 0 {
			return from + k
		}
		return n
	}
	for i := 0; i+len(lit) <= n; {
		nextLo = next(lo, nextLo, i)
		start := nextLo
		if lo != up {
			nextUp = next(up, nextUp, i)
			start = min(start, nextUp)
		}
		if start+len(lit) > n {
			return false
		}
		if bytes.EqualFold(content[start:start+len(lit)], []byte(lit)) {
			return true
		}
		i = start + 1
	}
	return false
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

func upper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - ('a' - 'A')
	}
	return c
}

// WithoutPrefilters returns copies of patterns with no prefilter: every
// regex is run on every content, as before BV-33. For equivalence tests.
func WithoutPrefilters(patterns []Pattern) []Pattern {
	out := make([]Pattern, len(patterns))
	for i, p := range patterns {
		p.pre = prefilter{}
		out[i] = p
	}
	return out
}

// builtinPrefilters holds, per built-in pattern, a literal every match
// contains (BV-33). A pattern without one is always run.
var builtinPrefilters = map[string]prefilter{
	"anthropic_api_key":              {"sk-ant-api03-", false},
	"openai_project_key":             {"sk-proj-", false},
	"openai_api_key":                 {"T3BlbkFJ", false},
	"openrouter_api_key":             {"sk-or-v1-", false},
	"github_pat_fine_grained":        {"github_pat_", false},
	"github_pat_classic":             {"ghp_", false},
	"github_oauth_token":             {"gho_", false},
	"github_user_to_server_token":    {"ghu_", false},
	"npm_access_token":               {"npm_", false},
	"railway_api_token":              {"RAILWAY_TO", true}, // not "TOKEN": (?i)K also matches U+212A
	"aws_access_key_id":              {"AKIA", false},
	"aws_temp_access_key":            {"ASIA", false},
	"stripe_secret_key":              {"sk_live_", false},
	"stripe_restricted_key":          {"rk_live_", false},
	"slack_bot_token":                {"xoxb-", false},
	"slack_webhook_url":              {"hooks.slack.com/services/", false},
	"jwt_token":                      {"eyJ", false},
	"supabase_secret_key":            {"sb_secret_", false},
	"supabase_publishable_key":       {"sb_publishable_", false},
	"database_url_with_credentials":  {"databa", true}, // not "database": (?i)s also matches U+017F
	"authorization_bearer_hardcoded": {"Authorization", true},
	"private_key_pem":                {"-----BEGIN", false},
}

// NewPattern constructs a Pattern from a pre-compiled regexp.
// Use this when compiling user-defined extra_secret_patterns from config.
func NewPattern(id, typ string, re *regexp.Regexp) Pattern {
	return Pattern{ID: id, Type: typ, re: re}
}

// SecretMatch records a single secret detected in scanned content.
// The full secret value is NEVER stored — only the redacted preview (invariant §3).
type SecretMatch struct {
	PatternID       string // Pattern.ID of the matching pattern
	Type            string // Pattern.Type
	RedactedPreview string // "XXXX ... XXXX" format (invariant §3)

	// Offset and Ref are set only by MatchRefs: the byte offset of the
	// match in the content, and what the identify function made of the
	// secret (its salted fingerprint), never the secret itself.
	Offset int
	Line   int
	Ref    *Ref

	// File is set by a caller that matched a commit's added lines: the file
	// the match is in.
	File string
}

// builtinSpecs is the authoritative list of built-in secret patterns.
// All regexes use RE2 syntax (Go regexp package). \b word boundaries are supported.
//
// Adding a new built-in pattern requires explicit owner approval per CLAUDE.md.
// Patterns are keyword-anchored where possible to reduce false-positive rate;
// high-entropy standalone tokens use structural anchors (prefix + length + suffix).
//
// Owner-approved 2026-05-22 (initial 4) + 2026-05-23 (expanded to full catalog).
// Patterns sourced from public token-format documentation.
var builtinSpecs = []struct {
	id, typ, expr string
}{
	// ---- AI providers -------------------------------------------------------
	{
		"anthropic_api_key",
		"Anthropic API key",
		`sk-ant-api03-[A-Za-z0-9_-]{93}-[A-Za-z0-9_-]{10,}AA`,
	},
	{
		"openai_project_key",
		"OpenAI project API key",
		`\bsk-proj-[A-Za-z0-9_-]{48,}\b`,
	},
	{
		// Classic OpenAI key: sk- + 20 alphanum + literal "T3BlbkFJ" (base64 of "OpenAI") + 20 alphanum.
		"openai_api_key",
		"OpenAI API key (classic)",
		`\bsk-[A-Za-z0-9]{20}T3BlbkFJ[A-Za-z0-9]{20}\b`,
	},
	{
		"openrouter_api_key",
		"OpenRouter API key",
		`\bsk-or-v1-[A-Za-z0-9]{64}\b`,
	},

	// ---- Source-control / VCS tokens ----------------------------------------
	{
		"github_pat_fine_grained",
		"GitHub fine-grained personal access token",
		`github_pat_[A-Za-z0-9_]{70,}`,
	},
	{
		"github_pat_classic",
		"GitHub classic personal access token",
		`\bghp_[A-Za-z0-9]{36}\b`,
	},
	// gho_ and ghu_ (BV-34): same body as ghp_, prefix changed. Source:
	// https://github.blog/security/behind-githubs-new-authentication-token-formats/
	// "gho for OAuth access tokens", "ghu for GitHub user-to-server tokens",
	// "all without changing the token length", "A 32 bit checksum in the
	// last 6 digits of each token".
	{
		"github_oauth_token",
		"GitHub OAuth access token",
		`\bgho_[A-Za-z0-9]{36}\b`,
	},
	{
		"github_user_to_server_token",
		"GitHub user-to-server token",
		`\bghu_[A-Za-z0-9]{36}\b`,
	},

	// ---- Package registries ------------------------------------------------
	// npm_ (BV-34). Source:
	// https://github.blog/security/announcing-npms-new-access-token-format
	// "access tokens now start with an identifiable prefix: npm", "the
	// delimiter following after is no longer a - but an underscore _",
	// "raising the entropy from 128 to 178", "The last six characters of the
	// tokens consist of CRC32 checksum, which is encoded in our Base62
	// implementation". Body: 178 bits / log2(62) ≈ 29.9 → 30 Base62
	// characters, plus 6 of checksum = 36. Consistent with the npm_ prefix of
	// the truncated example in npm 10.9.4's docs (npm-token.md).
	{
		"npm_access_token",
		"npm access token",
		`\bnpm_[A-Za-z0-9]{36}\b`,
	},

	// ---- Cloud / infrastructure tokens --------------------------------------
	{
		"railway_api_token",
		"Railway API token",
		`(?i)RAILWAY_TOKEN\s*[=:]\s*["']?[A-Za-z0-9_-]{40,}`,
	},
	{
		// AKIA = long-term IAM access key; ASIA = STS temporary key.
		"aws_access_key_id",
		"AWS access key ID",
		`\bAKIA[A-Z0-9]{16}\b`,
	},
	{
		"aws_temp_access_key",
		"AWS temporary access key ID (STS)",
		`\bASIA[A-Z0-9]{16}\b`,
	},

	// ---- Payment providers --------------------------------------------------
	{
		"stripe_secret_key",
		"Stripe secret key",
		`\bsk_live_[A-Za-z0-9]{24,}\b`,
	},
	{
		"stripe_restricted_key",
		"Stripe restricted key",
		`\brk_live_[A-Za-z0-9]{24,}\b`,
	},

	// ---- Messaging / webhooks -----------------------------------------------
	{
		"slack_bot_token",
		"Slack bot token",
		`\bxoxb-[0-9]{8,12}-[0-9]{8,12}-[A-Za-z0-9]{24}\b`,
	},
	{
		"slack_webhook_url",
		"Slack incoming webhook URL",
		`hooks\.slack\.com/services/[A-Za-z0-9]+/[A-Za-z0-9]+/[A-Za-z0-9]+`,
	},

	// ---- JWT / Supabase ------------------------------------------------------
	{
		// JWT three-part base64url structure. Covers Supabase anon/service-role keys,
		// which are JWTs with the "eyJ" header prefix on every segment boundary.
		"jwt_token",
		"JWT token (includes Supabase anon/service-role keys)",
		`eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`,
	},
	{
		// Supabase opaque API keys (2025 format): prefix + 22 base64url chars +
		// "_" + 8 base64url checksum chars. Format per the official docs
		// (https://supabase.com/docs/guides/self-hosting/self-hosted-auth-keys,
		// "sb_secret_<22-char-random>_<8-char-checksum>", same format as the
		// platform) and Supabase's generator generateOpaqueKey in
		// https://github.com/supabase/supabase/blob/master/docker/utils/add-new-auth-keys.sh
		// (randomBytes(17).toString("base64url").slice(0, 22); checksum is
		// sha256(...).digest("base64url").slice(0, 8)).
		// Owner-approved 2026-10-22 (BV-14).
		"supabase_secret_key",
		"Supabase secret API key",
		`\bsb_secret_[A-Za-z0-9_-]{22}_[A-Za-z0-9_-]{8}`,
	},
	{
		// Supabase publishable API key: same format and sources as above.
		"supabase_publishable_key",
		"Supabase publishable API key",
		`\bsb_publishable_[A-Za-z0-9_-]{22}_[A-Za-z0-9_-]{8}`,
	},

	// ---- Database connection strings -----------------------------------------
	{
		// Matches DATABASE_URL= (or DATABASE_URL:) followed by a scheme://user:pass@...
		// Pattern ends at "@" — captures the credential segment without the host/db.
		"database_url_with_credentials",
		"Database URL with embedded credentials",
		`(?i)\bdatabase_url\s*[=:]\s*["']?(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis|mariadb|cockroachdb)://[^:@\s]+:[^@\s]+@`,
	},

	// ---- HTTP authorization --------------------------------------------------
	{
		// Catches hardcoded Bearer tokens in source files, config, or shell scripts.
		"authorization_bearer_hardcoded",
		"Hardcoded Authorization Bearer token",
		`(?i)Authorization[\s:=]+["']?Bearer\s+[A-Za-z0-9_.-]{16,}`,
	},

	// ---- Private key material -----------------------------------------------
	{
		// Matches the PEM header line for any private key type.
		// The pattern intentionally stops at the closing dashes — it does not read
		// the base64 body — which is sufficient to flag the file for review.
		"private_key_pem",
		"PEM-encoded private key",
		`-----BEGIN\s+(?:RSA\s+|EC\s+|DSA\s+|OPENSSH\s+|ENCRYPTED\s+)?PRIVATE\s+KEY-----`,
	},
}

// cachedBuiltin is lazily compiled once on first call to BuiltinPatterns.
var cachedBuiltin []Pattern

// BuiltinPatterns returns a copy of the compiled built-in pattern slice.
// Safe to call from multiple goroutines; the underlying slice is read-only after init.
func BuiltinPatterns() []Pattern {
	if cachedBuiltin == nil {
		cachedBuiltin = compileSpecs(builtinSpecs)
	}
	out := make([]Pattern, len(cachedBuiltin))
	copy(out, cachedBuiltin)
	return out
}

func compileSpecs(specs []struct{ id, typ, expr string }) []Pattern {
	out := make([]Pattern, 0, len(specs))
	for _, s := range specs {
		out = append(out, Pattern{
			ID:   s.id,
			Type: s.typ,
			re:   regexp.MustCompile(s.expr),
			pre:  builtinPrefilters[s.id],
		})
	}
	return out
}

// Match scans content for every pattern in patterns and returns all matches.
// Call with append(BuiltinPatterns(), extraPatterns...) to include user patterns.
//
// Each matched token is immediately redacted — the raw value is never stored.
// One SecretMatch is returned per occurrence; the same token can match multiple patterns.
func Match(content []byte, patterns []Pattern) []SecretMatch {
	return MatchTimed(content, patterns, nil)
}

// MatchTimed is Match that also adds to perPattern the time each pattern
// took (prefilter and regex), keyed by pattern ID, when perPattern is not
// nil. For the run log (BV-35).
func MatchTimed(content []byte, patterns []Pattern, perPattern map[string]time.Duration) []SecretMatch {
	return matchAll(content, patterns, perPattern, nil)
}

func matchAll(content []byte, patterns []Pattern, perPattern map[string]time.Duration, identify Identify) []SecretMatch {
	var results []SecretMatch
	for _, p := range patterns {
		var start time.Time
		if perPattern != nil {
			start = time.Now()
		}
		if !p.pre.admits(content) {
			if perPattern != nil {
				perPattern[p.ID] += time.Since(start)
			}
			continue // no match is possible without the literal
		}
		found := p.re.FindAllIndex(content, -1)
		if perPattern != nil {
			perPattern[p.ID] += time.Since(start)
		}
		for _, loc := range found {
			m := SecretMatch{
				PatternID:       p.ID,
				Type:            p.Type,
				RedactedPreview: Redact(string(content[loc[0]:loc[1]])),
			}
			if identify != nil {
				m.Offset = loc[0]
				m.Line = 1 + bytes.Count(content[:loc[0]], []byte("\n"))
				if secret := contentSpan(p.ID, content, loc[0], loc[1]); secret != "" {
					r := identify(p.ID, secret)
					m.Ref = &r
				}
			}
			results = append(results, m)
		}
	}
	return results
}

// Classify returns the first pattern, in the order of patterns, that matches
// anywhere in value, together with the matched text. Call it with
// BuiltinPatterns() to use the built-in order.
//
// The returned matched string is the secret in clear: it must never be
// logged or persisted. Use it only to compute a fingerprint.
func Classify(value string, patterns []Pattern) (patternID, matched string, ok bool) {
	for _, p := range patterns {
		if m := p.re.FindString(value); m != "" {
			return p.ID, m, true
		}
	}
	return "", "", false
}

// RedactMatches returns s with every match of patterns replaced by its
// Redact preview, applied pattern by pattern in order. Use it to keep text
// that may embed a secret, such as a permission rule; the secret itself never
// survives.
func RedactMatches(s string, patterns []Pattern) string {
	for _, p := range patterns {
		s = p.re.ReplaceAllStringFunc(s, Redact)
	}
	return s
}

// Redact returns a privacy-safe preview: first 4 chars + " ... " + last 4 chars.
// If the value is shorter than 8 characters it is fully masked as "****".
//
// This implements invariant §3: the full secret value must never appear in
// any report, log, or state file. Tested in patterns_test.go.
func Redact(value string) string {
	if len(value) < 8 {
		return "****"
	}
	return value[:4] + " ... " + value[len(value)-4:]
}
