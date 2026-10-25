package secrets_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// ---------------------------------------------------------------------------
// Fake token fixtures — structurally valid but NOT real credentials.
// These are used in tests and are referenced by trust/no_full_secret_leak_test.go.
//
// Invariant §3: the full value must never appear in any Match output field.
// ---------------------------------------------------------------------------

const (
	// fakeAnthropicKey: prefix + exactly 93 chars + "-" + ≥10 chars ending in "AA".
	// Middle section character count: 26(A-Z)+26(a-z)+10(0-9)+2(_-)+29(a-zABC) = 93.
	fakeAnthropicKey = "sk-ant-api03-" +
		"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-abcdefghijklmnopqrstuvwxyzABC" +
		"-" +
		"AAABBBCCCDDDAA"

	// fakeGitHubFineGrained: "github_pat_" + ≥70 alphanumeric/underscore chars.
	// Character count: 26(A-Z)+26(a-z)+10(0-9)+4(_ABCD) = 66 → add more to reach ≥70.
	// Total: 26+26+12+10 = 74 chars after "github_pat_".
	fakeGitHubFineGrained = "github_pat_" +
		"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz012345678901_ABCDEFGHIJK"

	// fakeGitHubClassic: "ghp_" + exactly 36 alphanumeric chars.
	// 26(A-Z) + 10(0-9) = 36.
	fakeGitHubClassic = "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	// fakeOpenAIProject: "sk-proj-" + ≥48 chars of [A-Za-z0-9_-].
	// 26(A-Z) + 22(a-v) = 48 chars after "sk-proj-".
	fakeOpenAIProject = "sk-proj-ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuv"

	// fakeOpenAIClassic: "sk-" + 20 alphanum + literal "T3BlbkFJ" + 20 alphanum.
	// ABCDEFGHIJKLMNOPQRST = 20 chars; abcdefghijklmnopqrst = 20 chars.
	fakeOpenAIClassic = "sk-ABCDEFGHIJKLMNOPQRSTT3BlbkFJabcdefghijklmnopqrst"

	// fakeOpenRouterKey: "sk-or-v1-" + exactly 64 alphanum chars.
	// 26(A-Z) + 26(a-z) + 12(012345678901) = 64.
	fakeOpenRouterKey = "sk-or-v1-ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz012345678901"

	// fakeRailwayToken: context-sensitive; the full matched string includes the key name.
	// "RAILWAY_TOKEN=" + 40 alphanum = 26(A-Z)+14(abcdefghijklmn).
	fakeRailwayToken = "RAILWAY_TOKEN=ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn"

	// fakeSlackBotToken: "xoxb-" + 8 digits + "-" + 8 digits + "-" + 24 alphanum.
	// ABCDEFGHIJKLMNOPQRSTUVWx = 24 chars.
	fakeSlackBotToken = "xoxb-12345678-12345678-ABCDEFGHIJKLMNOPQRSTUVWx"

	// fakeSlackWebhookURL: Slack incoming webhook path (without scheme/host prefix).
	fakeSlackWebhookURL = "hooks.slack.com/services/ABCDEFGHIJ/KLMNOPQRST/abcdefghijklmnopqrstuvwx"

	// fakeAWSAccessKeyID: "AKIA" + 16 uppercase alphanum. ABCDEFGHIJ123456 = 10+6 = 16.
	fakeAWSAccessKeyID = "AKIAABCDEFGHIJ123456"

	// fakeAWSTempAccessKey: "ASIA" + 16 uppercase alphanum (STS temporary key).
	fakeAWSTempAccessKey = "ASIAABCDEFGHIJ123456"

	// fakeStripeSecretKey: "sk_live_" + 24 alphanum. ABCDEFGHIJKLMNOPQRSTUVWx = 24.
	fakeStripeSecretKey = "sk_live_ABCDEFGHIJKLMNOPQRSTUVWx"

	// fakeStripeRestrictedKey: "rk_live_" + 24 alphanum.
	fakeStripeRestrictedKey = "rk_live_ABCDEFGHIJKLMNOPQRSTUVWx"

	// fakeJWT: three-segment base64url JWT (header.payload.signature), all chars [A-Za-z0-9_-].
	// Covers structurally-valid JWTs including Supabase anon/service-role keys.
	fakeJWT = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJmYWtlVXNlciJ9.ABCDEFGHIJKLMNOPQRSTabcdef"

	// fakeDatabaseURL: DATABASE_URL with embedded user:password. Pattern ends at "@".
	fakeDatabaseURL = "DATABASE_URL=postgres://fakeuser:fakepassword@localhost/mydb"

	// fakeAuthBearer: hardcoded Authorization Bearer header line.
	// ABCDEFGHIJKLMNOPQRSTuvwxyz = 26 chars ≥ 16.
	fakeAuthBearer = "Authorization: Bearer ABCDEFGHIJKLMNOPQRSTuvwxyz"

	// fakePrivateKeyHeader: PEM RSA private key header line (not secret itself, but
	// its presence in a tracked file is a red flag).
	fakePrivateKeyHeader = "-----BEGIN RSA PRIVATE KEY-----"
)

// ---------------------------------------------------------------------------
// Redact
// ---------------------------------------------------------------------------

func TestRedact_NormalValue(t *testing.T) {
	got := secrets.Redact("sk-ant-api03-XXXX")
	if !strings.HasPrefix(got, "sk-a") {
		t.Errorf("redacted preview should start with first 4 chars: %q", got)
	}
	if !strings.HasSuffix(got, "XXXX") {
		t.Errorf("redacted preview should end with last 4 chars: %q", got)
	}
	if !strings.Contains(got, " ... ") {
		t.Errorf("redacted preview must contain ' ... ' separator: %q", got)
	}
}

func TestRedact_ShortValue(t *testing.T) {
	for _, v := range []string{"", "a", "abc", "1234567"} {
		got := secrets.Redact(v)
		if got != "****" {
			t.Errorf("short value %q: want ****, got %q", v, got)
		}
	}
}

func TestRedact_Exactly8Chars(t *testing.T) {
	got := secrets.Redact("12345678")
	if got != "1234 ... 5678" {
		t.Errorf("8-char value: want '1234 ... 5678', got %q", got)
	}
}

func TestRedact_DoesNotLeakMiddle(t *testing.T) {
	full := "sk-ant-api03-SUPERSECRETMIDDLEPART-AA"
	redacted := secrets.Redact(full)
	if strings.Contains(redacted, "SUPERSECRETMIDDLEPART") {
		t.Errorf("redacted output must not contain middle of value: %q", redacted)
	}
}

// ---------------------------------------------------------------------------
// Match — built-in patterns
// ---------------------------------------------------------------------------

func TestMatch_AnthropicKey(t *testing.T) {
	content := []byte("export ANTHROPIC_API_KEY=" + fakeAnthropicKey)
	m := requireMatch(t, content, "anthropic_api_key")
	if m.Type != "Anthropic API key" {
		t.Errorf("type = %q, want 'Anthropic API key'", m.Type)
	}
	assertRedacted(t, m.RedactedPreview, fakeAnthropicKey)
}

func TestMatch_GitHubFineGrained(t *testing.T) {
	content := []byte("GH_TOKEN=" + fakeGitHubFineGrained + "\n")
	m := requireMatch(t, content, "github_pat_fine_grained")
	assertRedacted(t, m.RedactedPreview, fakeGitHubFineGrained)
}

func TestMatch_GitHubClassic(t *testing.T) {
	content := []byte("token: " + fakeGitHubClassic + " # end")
	m := requireMatch(t, content, "github_pat_classic")
	assertRedacted(t, m.RedactedPreview, fakeGitHubClassic)
}

func TestMatch_OpenAIProjectKey(t *testing.T) {
	content := []byte(`OPENAI_KEY="` + fakeOpenAIProject + `"`)
	m := requireMatch(t, content, "openai_project_key")
	assertRedacted(t, m.RedactedPreview, fakeOpenAIProject)
}

func TestMatch_OpenAIClassicKey(t *testing.T) {
	content := []byte("OPENAI_API_KEY=" + fakeOpenAIClassic)
	m := requireMatch(t, content, "openai_api_key")
	if m.Type != "OpenAI API key (classic)" {
		t.Errorf("type = %q, want 'OpenAI API key (classic)'", m.Type)
	}
	assertRedacted(t, m.RedactedPreview, fakeOpenAIClassic)
}

func TestMatch_OpenRouterKey(t *testing.T) {
	content := []byte("OPENROUTER_API_KEY=" + fakeOpenRouterKey)
	m := requireMatch(t, content, "openrouter_api_key")
	assertRedacted(t, m.RedactedPreview, fakeOpenRouterKey)
}

func TestMatch_RailwayToken(t *testing.T) {
	content := []byte(fakeRailwayToken)
	m := requireMatch(t, content, "railway_api_token")
	if m.Type != "Railway API token" {
		t.Errorf("type = %q, want 'Railway API token'", m.Type)
	}
}

func TestMatch_SlackBotToken(t *testing.T) {
	content := []byte("token: " + fakeSlackBotToken)
	m := requireMatch(t, content, "slack_bot_token")
	assertRedacted(t, m.RedactedPreview, fakeSlackBotToken)
}

func TestMatch_SlackWebhookURL(t *testing.T) {
	content := []byte("WEBHOOK_URL=https://" + fakeSlackWebhookURL)
	m := requireMatch(t, content, "slack_webhook_url")
	assertRedacted(t, m.RedactedPreview, fakeSlackWebhookURL)
}

func TestMatch_AWSAccessKeyID(t *testing.T) {
	content := []byte("AWS_ACCESS_KEY_ID=" + fakeAWSAccessKeyID)
	m := requireMatch(t, content, "aws_access_key_id")
	if m.Type != "AWS access key ID" {
		t.Errorf("type = %q, want 'AWS access key ID'", m.Type)
	}
	assertRedacted(t, m.RedactedPreview, fakeAWSAccessKeyID)
}

func TestMatch_AWSTempAccessKey(t *testing.T) {
	content := []byte("AWS_ACCESS_KEY_ID=" + fakeAWSTempAccessKey)
	m := requireMatch(t, content, "aws_temp_access_key")
	assertRedacted(t, m.RedactedPreview, fakeAWSTempAccessKey)
}

func TestMatch_StripeSecretKey(t *testing.T) {
	content := []byte("STRIPE_SECRET_KEY=" + fakeStripeSecretKey)
	m := requireMatch(t, content, "stripe_secret_key")
	assertRedacted(t, m.RedactedPreview, fakeStripeSecretKey)
}

func TestMatch_StripeRestrictedKey(t *testing.T) {
	content := []byte("STRIPE_RESTRICTED_KEY=" + fakeStripeRestrictedKey)
	m := requireMatch(t, content, "stripe_restricted_key")
	assertRedacted(t, m.RedactedPreview, fakeStripeRestrictedKey)
}

func TestMatch_JWTToken(t *testing.T) {
	content := []byte("SUPABASE_ANON_KEY=" + fakeJWT)
	m := requireMatch(t, content, "jwt_token")
	if m.Type != "JWT token (includes Supabase anon/service-role keys)" {
		t.Errorf("type = %q", m.Type)
	}
	assertRedacted(t, m.RedactedPreview, fakeJWT)
}

func TestMatch_DatabaseURLWithCredentials(t *testing.T) {
	content := []byte(fakeDatabaseURL)
	m := requireMatch(t, content, "database_url_with_credentials")
	if m.Type != "Database URL with embedded credentials" {
		t.Errorf("type = %q, want 'Database URL with embedded credentials'", m.Type)
	}
}

func TestMatch_AuthorizationBearer(t *testing.T) {
	content := []byte(fakeAuthBearer)
	m := requireMatch(t, content, "authorization_bearer_hardcoded")
	assertRedacted(t, m.RedactedPreview, fakeAuthBearer)
}

func TestMatch_PrivateKeyPEM(t *testing.T) {
	content := []byte(fakePrivateKeyHeader + "\nMIIEpAIBAAKCAQEA0Z3VS...\n-----END RSA PRIVATE KEY-----")
	m := requireMatch(t, content, "private_key_pem")
	if m.Type != "PEM-encoded private key" {
		t.Errorf("type = %q, want 'PEM-encoded private key'", m.Type)
	}
}

func TestMatch_EmptyContent(t *testing.T) {
	if got := secrets.Match(nil, secrets.BuiltinPatterns()); len(got) != 0 {
		t.Errorf("empty content: got %d matches, want 0", len(got))
	}
}

func TestMatch_NoSecretContent(t *testing.T) {
	content := []byte("hello world, nothing secret here — just a README")
	if got := secrets.Match(content, secrets.BuiltinPatterns()); len(got) != 0 {
		t.Errorf("no-secret content: got %d matches, want 0", len(got))
	}
}

func TestMatch_NilPatterns(t *testing.T) {
	content := []byte(fakeAnthropicKey)
	if got := secrets.Match(content, nil); len(got) != 0 {
		t.Errorf("nil patterns: got %d matches, want 0", len(got))
	}
}

func TestMatch_MultipleSecretsInFile(t *testing.T) {
	content := []byte(fakeAnthropicKey + "\n" + fakeGitHubClassic + "\n")
	got := secrets.Match(content, secrets.BuiltinPatterns())
	if len(got) < 2 {
		t.Errorf("2-secret content: got %d matches, want ≥2", len(got))
	}
}

// TestMatch_FullValueNeverInOutput enforces invariant §3:
// the full matched value must never appear verbatim in any Match field.
// For context-sensitive patterns (railway, database_url, auth_bearer) the "key"
// is the full matched string including the keyword prefix.
func TestMatch_FullValueNeverInOutput(t *testing.T) {
	fakeKeys := []string{
		fakeAnthropicKey,
		fakeGitHubFineGrained,
		fakeGitHubClassic,
		fakeOpenAIProject,
		fakeOpenAIClassic,
		fakeOpenRouterKey,
		fakeRailwayToken, // full matched string includes "RAILWAY_TOKEN="
		fakeSlackBotToken,
		fakeSlackWebhookURL,
		fakeAWSAccessKeyID,
		fakeAWSTempAccessKey,
		fakeStripeSecretKey,
		fakeStripeRestrictedKey,
		fakeJWT,
		fakeDatabaseURL, // full matched string includes "DATABASE_URL=postgres://...@"
		fakeAuthBearer,  // full matched string includes "Authorization: Bearer "
		fakePrivateKeyHeader,
	}
	for _, key := range fakeKeys {
		content := []byte(key)
		matches := secrets.Match(content, secrets.BuiltinPatterns())
		for _, m := range matches {
			if m.RedactedPreview == key {
				t.Errorf("invariant §3 violated: full key in RedactedPreview (pattern %q)", m.PatternID)
			}
			if strings.Contains(m.RedactedPreview, key) {
				t.Errorf("invariant §3 violated: full key is substring of RedactedPreview (pattern %q)", m.PatternID)
			}
		}
	}
}

// TestMatch_ExtraPattern verifies user-compiled patterns work alongside built-ins.
func TestMatch_ExtraPattern(t *testing.T) {
	re := regexp.MustCompile(`MY_SECRET_[A-Z0-9]{16}`)
	extra := secrets.NewPattern("custom_secret", "Custom secret token", re)
	content := []byte("token=MY_SECRET_ABCDEFGHIJ012345")
	matches := secrets.Match(content, []secrets.Pattern{extra})

	m := findMatch(matches, "custom_secret")
	if m == nil {
		t.Fatal("expected custom_secret match, got none")
	}
	if m.Type != "Custom secret token" {
		t.Errorf("type = %q, want 'Custom secret token'", m.Type)
	}
}

// TestBuiltinPatterns_ReturnsCopy verifies BuiltinPatterns returns a fresh slice each time.
func TestBuiltinPatterns_ReturnsCopy(t *testing.T) {
	a := secrets.BuiltinPatterns()
	b := secrets.BuiltinPatterns()
	if len(a) == 0 {
		t.Fatal("BuiltinPatterns returned empty slice")
	}
	if len(a) != len(b) {
		t.Errorf("BuiltinPatterns: inconsistent length %d vs %d", len(a), len(b))
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func requireMatch(t *testing.T, content []byte, patternID string) secrets.SecretMatch {
	t.Helper()
	matches := secrets.Match(content, secrets.BuiltinPatterns())
	m := findMatch(matches, patternID)
	if m == nil {
		t.Fatalf("expected match for pattern %q, got none (total matches: %d)", patternID, len(matches))
	}
	return *m
}

func findMatch(matches []secrets.SecretMatch, patternID string) *secrets.SecretMatch {
	for i := range matches {
		if matches[i].PatternID == patternID {
			return &matches[i]
		}
	}
	return nil
}

func assertRedacted(t *testing.T, preview, fullValue string) {
	t.Helper()
	if preview == fullValue {
		t.Error("redacted preview equals the full value — invariant §3 violated")
	}
	if !strings.Contains(preview, " ... ") {
		t.Errorf("redacted preview %q must contain ' ... '", preview)
	}
}

func TestRedactMatches_ReplacesEverySecret(t *testing.T) {
	token := "ghp_" + strings.Repeat("A", 36)
	in := `Bash(curl -H "Authorization: Bearer ` + token + `" https://x) and ` + token
	out := secrets.RedactMatches(in, secrets.BuiltinPatterns())
	if strings.Contains(out, token) || strings.Contains(out, strings.Repeat("A", 12)) {
		t.Fatalf("secret survived: %q", out)
	}
	if !strings.HasPrefix(out, "Bash(curl -H") || !strings.Contains(out, " ... ") {
		t.Errorf("unexpected redaction: %q", out)
	}
	if got := secrets.RedactMatches("Bash(npm test)", secrets.BuiltinPatterns()); got != "Bash(npm test)" {
		t.Errorf("text without secrets changed: %q", got)
	}
}
