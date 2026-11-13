package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/qtorb/rosmarops/internal/hash"
)

// TestOwnBinaryCheck (BV-226 §4, condition 3): our binary by its path or by
// the PATH, never another program named rosmarops or an older copy.
func TestOwnBinaryCheck(t *testing.T) {
	dir := t.TempDir()
	ours := filepath.Join(dir, "ours", "rosmarops")
	other := filepath.Join(dir, "other", "rosmarops")
	for p, body := range map[string]string{ours: "#!/bin/sh\n# ours\n", other: "#!/bin/sh\n# another program\n"} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ref, err := hash.File(ours)
	if err != nil {
		t.Fatal(err)
	}
	check := ownBinaryCheck(ref, "")
	if !check(ours) {
		t.Error("our binary is not ours")
	}
	if check(other) {
		t.Error("another program named rosmarops is ours")
	}
	if check("relative/rosmarops") {
		t.Error("a relative path is ours")
	}
	if ownBinaryCheck("", "")(ours) {
		t.Error("ours without a reference")
	}
	if runtime.GOOS == "windows" {
		return // the PATH lookup below uses a Unix executable
	}
	t.Setenv("PATH", filepath.Dir(other))
	if check("rosmarops") {
		t.Error("the rosmarops in PATH is another program, yet ours")
	}
	t.Setenv("PATH", filepath.Dir(ours))
	if !check("rosmarops") {
		t.Error("the rosmarops in PATH is ours, yet not recognised")
	}
}
