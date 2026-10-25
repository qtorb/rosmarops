package secrets_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// Obviously fake values.
var (
	fakeRailwayBareToken = "rw" + strings.Repeat("A1b2C3d4E5", 4)
	fakePEMBody          = "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQ"
	fakePEM              = "-----BEGIN PRIVATE KEY-----\n" + fakePEMBody + "\n-----END PRIVATE KEY-----"
	fakeSBSecret         = "sb_secret_" + strings.Repeat("aB3-", 5) + "Xy" + "_" + "Ck9_s-1Z"
	fakeSBPublic         = "sb_publishable_" + strings.Repeat("Qw8_", 5) + "pL" + "_" + "0aZ-9yB_"
)

func classifyEnv(key, value string) (string, string, bool) {
	return secrets.ClassifyEnv(key, secrets.NewSecretValue(value), secrets.BuiltinPatterns())
}

func TestClassifyEnv_KeywordPatternsMatchViaVariableName(t *testing.T) {
	cases := []struct {
		key, value, wantID, wantSecret string
	}{
		{"RAILWAY_TOKEN", fakeRailwayBareToken, "railway_api_token", fakeRailwayBareToken},
		{"DATABASE_URL", "postgres://admin:hunter2pass@db.example.com:5432/app", "database_url_with_credentials", "postgres://admin:hunter2pass@"},
		{"Authorization", "Bearer abcdefghijklmnopqrstuvwxyz012345", "authorization_bearer_hardcoded", "abcdefghijklmnopqrstuvwxyz012345"},
	}
	for _, tc := range cases {
		id, secret, ok := classifyEnv(tc.key, tc.value)
		if !ok || id != tc.wantID {
			t.Errorf("%s: got %q ok=%v, want %q", tc.key, id, ok, tc.wantID)
			continue
		}
		if secret != tc.wantSecret {
			t.Errorf("%s: secret span got %q, want %q", tc.key, secrets.Redact(secret), secrets.Redact(tc.wantSecret))
		}
		if strings.Contains(secret, tc.key) || strings.HasPrefix(secret, "=") || strings.HasPrefix(secret, ":") {
			t.Errorf("%s: secret includes the variable name or separator", tc.key)
		}
	}
}

func TestClassifyEnv_ValueAloneTriedFirst(t *testing.T) {
	// A value that matches by itself is classified without the synthetic line.
	id, secret, ok := classifyEnv("WHATEVER", fakeGitHubClassic)
	if !ok || id != "github_pat_classic" || secret != fakeGitHubClassic {
		t.Errorf("got %q ok=%v span-equal=%v", id, ok, secret == fakeGitHubClassic)
	}
}

func TestClassifyEnv_PEMSpanIsWholeBlock(t *testing.T) {
	id, secret, ok := classifyEnv("PRIVATE_KEY", "  "+fakePEM+"\n")
	if !ok || id != "private_key_pem" {
		t.Fatalf("got %q ok=%v", id, ok)
	}
	if secret != fakePEM {
		t.Errorf("PEM span should be header..footer, got %d bytes (want %d)", len(secret), len(fakePEM))
	}

	noFooter := "-----BEGIN PRIVATE KEY-----\n" + fakePEMBody
	if _, s, _ := classifyEnv("K", noFooter); s != noFooter {
		t.Errorf("PEM without footer should run to the end of the value")
	}

	other := strings.Replace(fakePEM, "MIIE", "ZZZZ", 1)
	_, a, _ := classifyEnv("K", fakePEM)
	_, b, _ := classifyEnv("K", other)
	if a == b {
		t.Error("two different PEM keys produced the same secret span")
	}
}

func TestClassifyEnv_BearerPrefixRemoved(t *testing.T) {
	token := "abcdefghijklmnopqrstuvwxyz012345"
	for _, value := range []string{"Bearer " + token, "bearer " + token, "Authorization: Bearer " + token} {
		if _, s, ok := classifyEnv("Authorization", value); !ok || s != token {
			t.Errorf("value %q: got span-equal=%v ok=%v, want the bare token", value[:12], s == token, ok)
		}
	}
}

func TestClassifyEnv_NoMatch(t *testing.T) {
	for key, value := range map[string]string{"LOG_LEVEL": "debug", "RAILWAY_TOKEN": "short", "DATABASE_URL": "postgres://db.example.com/app"} {
		if id, _, ok := classifyEnv(key, value); ok {
			t.Errorf("%s=%s matched %q", key, value, id)
		}
	}
}

func TestSupabasePatterns_MatchFormatNotLookalikes(t *testing.T) {
	for value, wantID := range map[string]string{fakeSBSecret: "supabase_secret_key", fakeSBPublic: "supabase_publishable_key"} {
		id, matched, ok := secrets.Classify(value, secrets.BuiltinPatterns())
		if !ok || id != wantID || matched != value {
			t.Errorf("%s…: got %q ok=%v whole=%v, want %q", value[:12], id, ok, matched == value, wantID)
		}
	}
	lookalikes := []string{
		"sb_secrets_" + strings.Repeat("a", 22) + "_" + strings.Repeat("b", 8),     // wrong prefix
		"sb_secret_" + strings.Repeat("a", 21) + "_" + strings.Repeat("b", 8),      // random part too short
		"sb_secret_" + strings.Repeat("a", 22) + "_" + strings.Repeat("b", 7),      // checksum too short
		"sb_public_" + strings.Repeat("a", 22) + "_" + strings.Repeat("b", 8),      // not a Supabase prefix
		"xsb_secret_" + strings.Repeat("a", 22) + "_" + strings.Repeat("b", 8),     // no word boundary
		"sb_publishable_" + strings.Repeat("a", 22) + strings.Repeat("b", 8) + "!", // missing separator
	}
	for _, v := range lookalikes {
		if id, _, ok := secrets.Classify(v, secrets.BuiltinPatterns()); ok && strings.HasPrefix(id, "supabase") {
			t.Errorf("lookalike %q matched %q", v, id)
		}
	}
}
