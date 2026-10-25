package secrets

import (
	"fmt"
	"log/slog"
)

// SecretValue carries a secret read from a config file (e.g. an MCP server's
// env value) from the adapter to the credential fingerprint, and nowhere else.
//
// Every way of printing or encoding it — fmt verbs, json, text, slog — yields
// only the redacted preview (invariant §3). The value in clear is available
// solely through Reveal, which exists for computing fingerprints.
type SecretValue struct {
	v string
}

// NewSecretValue wraps a secret value.
func NewSecretValue(v string) SecretValue {
	return SecretValue{v: v}
}

// Reveal returns the secret in clear. Use it only to compute a fingerprint;
// never log, persist or return the result.
func (s SecretValue) Reveal() string {
	return s.v
}

// String returns the redacted preview.
func (s SecretValue) String() string {
	return Redact(s.v)
}

// GoString returns the redacted preview, so %#v never shows the value.
func (s SecretValue) GoString() string {
	return Redact(s.v)
}

// Format writes the redacted preview for every fmt verb (including %d and
// %x, which would otherwise print the wrapped string through reflection).
func (s SecretValue) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(Redact(s.v)))
}

// MarshalJSON encodes the redacted preview as a JSON string.
func (s SecretValue) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", Redact(s.v))), nil
}

// MarshalText returns the redacted preview.
func (s SecretValue) MarshalText() ([]byte, error) {
	return []byte(Redact(s.v)), nil
}

// LogValue makes slog record the redacted preview.
func (s SecretValue) LogValue() slog.Value {
	return slog.StringValue(Redact(s.v))
}
