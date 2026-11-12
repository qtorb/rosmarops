package winpkg

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"unicode/utf16"
)

// publisherID is Windows's package publisher id: the first 8 bytes of the
// SHA-256 of the Publisher string in UTF-16LE, as 13 characters of the
// package base-32 alphabet.
func publisherID(publisher string) string {
	var b []byte
	for _, u := range utf16.Encode([]rune(publisher)) {
		b = append(b, byte(u), byte(u>>8))
	}
	sum := sha256.Sum256(b)
	const alpha = "0123456789abcdefghjkmnpqrstvwxyz"
	var bits uint64
	for _, c := range sum[:8] {
		bits = bits<<8 | uint64(c)
	}
	out := make([]byte, 13)
	for i := 0; i < 13; i++ {
		shift := 64 - 5*(i+1)
		var v uint64
		if shift >= 0 {
			v = bits >> uint(shift)
		} else {
			v = bits << uint(-shift)
		}
		out[i] = alpha[v&31]
	}
	return string(out)
}

// TestFamilyMatchesManifest (CTO-41 §1.3): the fixed family name is the one
// Windows derives from the manifest msix.yml packages (Name and Publisher).
func TestFamilyMatchesManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "packaging", "msix", "AppxManifest.xml"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`<Identity\s+Name="([^"]+)"\s+Publisher="([^"]+)"`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("no Identity in the manifest")
	}
	if got := string(m[1]) + "_" + publisherID(string(m[2])); got != Family {
		t.Errorf("family from the manifest %q, fixed %q", got, Family)
	}
}

func TestVersion(t *testing.T) {
	for in, want := range map[string]string{
		"AlbertGarciaPujadas.RosmarOps_0.11.3.0_x64__2a9trf99pxwv0":   "0.11.3.0",
		"AlbertGarciaPujadas.RosmarOps_0.11.4.0_arm64__2a9trf99pxwv0": "0.11.4.0",
		"": "", "garbage": "",
	} {
		if got := Version(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
