package hash_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/hash"
)

func TestBytes_KnownVector(t *testing.T) {
	// SHA-256("") = e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
	got := hash.Bytes([]byte{})
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Errorf("Bytes(empty): got %q, want %q", got, want)
	}
}

func TestBytes_Lowercase(t *testing.T) {
	result := hash.Bytes([]byte("test"))
	if result != strings.ToLower(result) {
		t.Errorf("Bytes: result is not lowercase: %q", result)
	}
	if len(result) != 64 {
		t.Errorf("Bytes: expected 64 hex chars, got %d", len(result))
	}
}

func TestFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile.txt")
	content := []byte("hello vigil")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := hash.File(path)
	if err != nil {
		t.Fatalf("File: unexpected error: %v", err)
	}

	// Must match Bytes() on the same content.
	want := hash.Bytes(content)
	if got != want {
		t.Errorf("File: got %q, want %q", got, want)
	}
}

func TestFile_MissingFile(t *testing.T) {
	_, err := hash.File("/nonexistent/path/file.txt")
	if err == nil {
		t.Fatal("File: expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "open") {
		t.Errorf("File: error should mention 'open', got: %v", err)
	}
}

func TestFile_DeterministicSameContent(t *testing.T) {
	dir := t.TempDir()
	path1 := filepath.Join(dir, "a.txt")
	path2 := filepath.Join(dir, "b.txt")
	content := []byte("deterministic content")
	if err := os.WriteFile(path1, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path2, content, 0o600); err != nil {
		t.Fatal(err)
	}

	h1, err := hash.File(path1)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := hash.File(path2)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("same content: got different hashes %q vs %q", h1, h2)
	}
}

func TestFile_DifferentContent(t *testing.T) {
	dir := t.TempDir()
	path1 := filepath.Join(dir, "a.txt")
	path2 := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(path1, []byte("content A"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path2, []byte("content B"), 0o600); err != nil {
		t.Fatal(err)
	}

	h1, _ := hash.File(path1)
	h2, _ := hash.File(path2)
	if h1 == h2 {
		t.Error("different content: got same hash (collision)")
	}
}

func TestKeyed_HMACSHA256(t *testing.T) {
	// RFC 4231 test case 2.
	got := hash.Keyed([]byte("Jefe"), []byte("what do ya want for nothing?"))
	want := "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"
	if got != want {
		t.Errorf("Keyed: got %s, want %s", got, want)
	}
	if hash.Keyed([]byte("a"), []byte("x")) == hash.Keyed([]byte("b"), []byte("x")) {
		t.Error("different keys give the same HMAC")
	}
}
