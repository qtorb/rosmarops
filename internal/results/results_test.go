package results_test

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/results"
	"github.com/qtorb/rosmarops/internal/schemas"
	"github.com/qtorb/rosmarops/internal/schemaval"
)

func testKey(b byte) []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = b
	}
	return k
}

func sampleHeader() results.Header {
	return results.Header{
		KitID:            "0123456789abcdef0123456789abcdef",
		KeyID:            "fedcba9876543210fedcba9876543210",
		HostID:           "host_0011223344556677",
		CreatedAt:        "2026-10-30T12:00:00Z",
		RosmaropsVersion: "0.1.0",
	}
}

// Round trip: a package sealed with the kit key opens with the same key and
// returns the content unchanged; the header is filled with format and version.
func TestSealOpen_RoundTrip(t *testing.T) {
	key := testKey(0x11)
	content := []byte(`{"hello":"space-view"}`)
	pkg, err := results.Seal(key, sampleHeader(), content)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Header.Format != results.Format || pkg.Header.Version != results.Version {
		t.Errorf("header format/version: %q %d", pkg.Header.Format, pkg.Header.Version)
	}
	got, err := results.Open(key, pkg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("round trip: got %q, want %q", got, content)
	}
	// Nothing in clear: the content string must not be in the file.
	raw, _ := results.Marshal(pkg)
	if strings.Contains(string(raw), "space-view") {
		t.Error("content appears in clear in the package")
	}
}

// A different nonce each time: two seals of the same content differ.
func TestSeal_FreshNonces(t *testing.T) {
	key := testKey(0x22)
	a, _ := results.Seal(key, sampleHeader(), []byte("x"))
	b, _ := results.Seal(key, sampleHeader(), []byte("x"))
	if a.Ciphertext == b.Ciphertext || a.WrappedKey == b.WrappedKey || a.Nonce == b.Nonce {
		t.Error("two seals of the same content are identical: nonce or data key reused")
	}
}

// A tampered header, wrapped key or ciphertext, or the wrong kit key, all fail
// to open.
func TestOpen_Tampered(t *testing.T) {
	key := testKey(0x33)
	base, err := results.Seal(key, sampleHeader(), []byte("secret-view"))
	if err != nil {
		t.Fatal(err)
	}
	flip := func(s string) string {
		b, _ := base64.StdEncoding.DecodeString(s)
		b[len(b)-1] ^= 0x01
		return base64.StdEncoding.EncodeToString(b)
	}
	cases := map[string]results.Package{
		"header":      func() results.Package { p := base; p.Header.HostID = "host_ffffffffffffffff"; return p }(),
		"wrapped_key": func() results.Package { p := base; p.WrappedKey = flip(p.WrappedKey); return p }(),
		"ciphertext":  func() results.Package { p := base; p.Ciphertext = flip(p.Ciphertext); return p }(),
		"nonce":       func() results.Package { p := base; p.Nonce = flip(p.Nonce); return p }(),
	}
	for name, p := range cases {
		if _, err := results.Open(key, p); err == nil {
			t.Errorf("%s tampered: Open succeeded, want failure", name)
		}
	}
	if _, err := results.Open(testKey(0x99), base); err == nil {
		t.Error("wrong kit key: Open succeeded, want failure")
	}
}

// The package validates against results.v1.schema.json.
func TestPackage_ValidatesSchema(t *testing.T) {
	schema, err := schemaval.Compile(schemas.ResultsSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := results.Seal(testKey(0x44), sampleHeader(), []byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := results.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	res, err := schema.Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid() {
		t.Errorf("package fails schema: %v", res.Errors())
	}
}
