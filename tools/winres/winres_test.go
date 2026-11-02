package main

import (
	"crypto/sha256"
	"encoding/hex"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// icoPath / gitignorePath are relative to this package (tools/winres).
const (
	repoICO       = "../../assets/windows/rosmarops.ico"
	repoGitignore = "../../.gitignore"
	sysoRelPath   = "cmd/rosmarops/rsrc_windows_amd64.syso"
	// The official rosemary icon (BV-149, signed).
	icoSHA256 = "8675c06fc09caac9bbb1848dabd4619ef1434828a4215dcf6301451c417c7227"
)

func readICO(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(repoICO)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// BV-149 point 1: the committed icon is exactly the signed one.
func TestICOHash(t *testing.T) {
	sum := sha256.Sum256(readICO(t))
	if got := hex.EncodeToString(sum[:]); got != icoSHA256 {
		t.Errorf("assets/windows/rosmarops.ico SHA-256 = %s, want %s", got, icoSHA256)
	}
}

// BV-149 point 2: the .syso is byte-for-byte deterministic — the same inputs
// always produce the same object, with nothing time- or machine-dependent.
func TestDeterministic(t *testing.T) {
	ico := readICO(t)
	a, err := buildSyso(ico, "0.7.2")
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildSyso(ico, "0.7.2")
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("two runs produced different bytes")
	}
	if len(a) == 0 {
		t.Fatal("empty object")
	}
	// A different version must change the object (the version is really in it).
	c, err := buildSyso(ico, "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if string(a) == string(c) {
		t.Error("version is not reflected in the object")
	}
}

// BV-149 point 2: the generator uses only the standard library (no third-party
// dependency, and nothing that reaches the network).
func TestNoExternalImports(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			// Standard-library import paths have no dot in their first segment;
			// third-party ones look like host.tld/owner/repo.
			first := path
			if i := strings.IndexByte(path, '/'); i >= 0 {
				first = path[:i]
			}
			if strings.Contains(first, ".") {
				t.Errorf("%s imports non-standard package %q", e.Name(), path)
			}
			// Explicitly: no networking, even from the standard library.
			if path == "net" || strings.HasPrefix(path, "net/") || path == "crypto/tls" {
				t.Errorf("%s imports networking package %q", e.Name(), path)
			}
		}
	}
}

// BV-149 point 2: the generated .syso is git-ignored (it is a build input, not
// committed).
func TestSysoIgnored(t *testing.T) {
	b, err := os.ReadFile(repoGitignore)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == sysoRelPath {
			found = true
		}
	}
	if !found {
		t.Errorf(".gitignore does not ignore %q", sysoRelPath)
	}
}
