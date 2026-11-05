package repos

import (
	"context"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-186: the tracked-secret scan does not open images, documents or other
// binary files. A deterministic sentinel pattern makes "was it scanned?"
// observable: a file that is opened and scanned yields a secret_tracked
// finding, a skipped one does not.

const bv186Secret = "SEKRET-deadbeef"

func bv186Pattern() secrets.Pattern {
	return secrets.NewPattern("sentinel", "sentinel", regexp.MustCompile("SEKRET-[0-9a-f]{8}"))
}

// bv186Scan commits every file in a new repo and scans it, returning the set of
// files a secret_tracked finding named and the count of binary files skipped.
func bv186Scan(t *testing.T, files map[string]string) (matched map[string]bool, binarySkipped int) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	r := t.TempDir()
	gitT(t, r, "init", "-q")
	for name, content := range files {
		writeT(t, filepath.Join(r, filepath.FromSlash(name)), content)
	}
	gitT(t, r, "add", "-A")
	gitT(t, r, "commit", "-qm", "init")

	cfg := ScanConfig{
		Patterns:        []secrets.Pattern{bv186Pattern()},
		OnBinarySkipped: func(n int) { binarySkipped = n },
	}
	fs, _, _ := ScanAllRepos(context.Background(), []string{r}, nil, cfg)
	matched = map[string]bool{}
	for _, f := range fs {
		if f.Category == "secret_tracked" {
			matched[filepath.ToSlash(f.Evidence.File)] = true
		}
	}
	return matched, binarySkipped
}

// TestBinaryExtensionsNotOpened: .png, .pdf and .docx with the sentinel inside
// are not opened (no finding), counted as binary (3); a .env, a .svg and a
// .json with the same sentinel are still read and detected.
func TestBinaryExtensionsNotOpened(t *testing.T) {
	matched, skipped := bv186Scan(t, map[string]string{
		"img.png":    bv186Secret, // image
		"doc.pdf":    bv186Secret, // document
		"sheet.docx": bv186Secret, // document
		"app/.env":   bv186Secret, // read
		"logo.svg":   bv186Secret, // read (not a binary extension)
		"data.json":  bv186Secret, // read
	})
	if skipped != 3 {
		t.Errorf("binary skipped = %d, want 3", skipped)
	}
	for _, bin := range []string{"img.png", "doc.pdf", "sheet.docx"} {
		if matched[bin] {
			t.Errorf("%s was opened and scanned; it should be skipped", bin)
		}
	}
	for _, text := range []string{"app/.env", "logo.svg", "data.json"} {
		if !matched[text] {
			t.Errorf("%s was not scanned; it is not a binary extension", text)
		}
	}
}

// TestBinaryByNulByte: a file with no listed extension but a NUL byte at the
// start is treated as binary — not scanned, counted — while a text file with
// the same secret is detected.
func TestBinaryByNulByte(t *testing.T) {
	matched, skipped := bv186Scan(t, map[string]string{
		"blob":     "\x00" + bv186Secret, // NUL at the start: binary
		"keys.txt": bv186Secret,          // text: read
	})
	if skipped != 1 {
		t.Errorf("binary skipped = %d, want 1", skipped)
	}
	if matched["blob"] {
		t.Error("the NUL-byte file was scanned; it should be skipped as binary")
	}
	if !matched["keys.txt"] {
		t.Error("keys.txt was not scanned")
	}
}
