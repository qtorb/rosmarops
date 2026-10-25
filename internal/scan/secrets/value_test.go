package secrets_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// leaks reports whether out contains the secret or its middle section.
func leaks(out, secret string) bool {
	return strings.Contains(out, secret) || strings.Contains(out, secret[4:len(secret)-4])
}

func TestSecretValue_NeverPrintsValue(t *testing.T) {
	secret := fakeGitHubClassic
	srv := platform.MCPServer{
		Client: "cursor", Name: "github", Command: "npx",
		EnvKeys: []string{"GITHUB_TOKEN"},
		Env:     []platform.EnvVar{{Key: "GITHUB_TOKEN", Value: secrets.NewSecretValue(secret)}},
	}

	outputs := map[string]string{
		"%v":  fmt.Sprintf("%v", srv),
		"%+v": fmt.Sprintf("%+v", srv),
		"%#v": fmt.Sprintf("%#v", srv),
		"%s":  fmt.Sprintf("%s", srv.Env[0].Value),
		"%d":  fmt.Sprintf("%d", srv.Env[0].Value),
		"%x":  fmt.Sprintf("%x", srv.Env[0].Value),
		"%q":  fmt.Sprintf("%q", srv.Env[0].Value),
	}
	js, err := json.Marshal(srv)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	outputs["json"] = string(js)
	txt, err := srv.Env[0].Value.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	outputs["text"] = string(txt)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info("server", "srv", srv, "value", srv.Env[0].Value, "env", srv.Env)
	outputs["slog json"] = buf.String()

	buf.Reset()
	slog.New(slog.NewTextHandler(&buf, nil)).Info("server", "srv", srv, "value", srv.Env[0].Value)
	outputs["slog text"] = buf.String()

	for name, out := range outputs {
		if leaks(out, secret) {
			t.Errorf("%s leaks the secret: %s", name, out)
		}
	}
	if !strings.Contains(outputs["json"], secrets.Redact(secret)) {
		t.Errorf("json should carry the redacted preview, got %s", outputs["json"])
	}
	if srv.Env[0].Value.Reveal() != secret {
		t.Error("Reveal does not return the value")
	}
}

func TestClassify_FirstMatchInBuiltinOrder(t *testing.T) {
	cases := []struct {
		value, wantID string
	}{
		{fakeGitHubClassic, "github_pat_classic"},
		{"Bearer-less prefix " + fakeAWSAccessKeyID, "aws_access_key_id"},
		{fakeStripeSecretKey, "stripe_secret_key"},
		{fakeAnthropicKey, "anthropic_api_key"},
	}
	for _, tc := range cases {
		id, matched, ok := secrets.Classify(tc.value, secrets.BuiltinPatterns())
		if !ok || id != tc.wantID {
			t.Errorf("Classify(%s…): got %q ok=%v, want %q", tc.value[:8], id, ok, tc.wantID)
		}
		if !strings.Contains(tc.value, matched) || matched == "" {
			t.Errorf("matched %q is not a substring of the value", secrets.Redact(matched))
		}
	}
	if _, _, ok := secrets.Classify("debug", secrets.BuiltinPatterns()); ok {
		t.Error("Classify(\"debug\") matched a pattern")
	}
}
