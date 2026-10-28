package graph

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-91 (cierre): the userinfo (user:password@) of a remote server's URL is not
// part of destination_ref, so rotating a credential embedded in the URL is not
// a change of destination (as E2 for env and headers); the host, port, path and
// query are, so changing where the server points is a change.
func TestDestinationRef_URLUserinfoIgnored(t *testing.T) {
	salt := []byte("0123456789abcdef0123456789abcdef")
	patterns := secrets.BuiltinPatterns()
	ref := func(url string) string {
		return destinationRef(salt, platform.MCPServer{URL: url}, patterns)
	}

	base := ref("https://user:pass@api.example.com/mcp?x=1")
	if base == "" {
		t.Fatal("no destination_ref for a remote server")
	}

	// Same destination, different embedded credential → same ref.
	for _, same := range []string{
		"https://user:pass@api.example.com/mcp?x=1",    // identical
		"https://user:ROTATED@api.example.com/mcp?x=1", // password rotated
		"https://other:creds@api.example.com/mcp?x=1",  // user and password changed
		"https://api.example.com/mcp?x=1",              // no userinfo at all
	} {
		if got := ref(same); got != base {
			t.Errorf("rotating/removing the URL credential changed destination_ref:\n %q\n vs base\n %q", same, got)
		}
	}

	// A real change of destination → different ref.
	for name, other := range map[string]string{
		"host":   "https://user:pass@evil.example.com/mcp?x=1",
		"port":   "https://user:pass@api.example.com:8443/mcp?x=1",
		"path":   "https://user:pass@api.example.com/other?x=1",
		"query":  "https://user:pass@api.example.com/mcp?x=2",
		"scheme": "http://user:pass@api.example.com/mcp?x=1",
	} {
		if ref(other) == base {
			t.Errorf("%s change did not change destination_ref", name)
		}
	}
}
