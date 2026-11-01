package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/state"
)

// connectedPaths lays out a temp state (SpacePermission true) and the kit/report
// paths for the P6 refusal tests.
func connectedPaths(t *testing.T) (kitPaths, *bytes.Buffer, *console) {
	t.Helper()
	dir := t.TempDir()
	paths := kitPaths{
		store:   filepath.Join(dir, "kit.key"),
		state:   filepath.Join(dir, "state.json"),
		reports: filepath.Join(dir, "reports"),
	}
	st, err := state.DefaultState("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	st.SpacePermission = true
	if err := state.SaveState(paths.state, st); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	c := &console{in: bufio.NewReader(strings.NewReader("")), out: out, cat: cat}
	return paths, out, c
}

// CTO-14 P6: kit create is refused while this computer is connected to a space,
// with the connected notice and no key written.
func TestKitCreateRefusedWhenConnected(t *testing.T) {
	paths, out, c := connectedPaths(t)
	err := kitCreate(c, paths)
	if !errors.Is(err, errSpaceConnected) {
		t.Fatalf("err = %v, want errSpaceConnected", err)
	}
	if !strings.Contains(out.String(), "connected to your space") {
		t.Errorf("notice not shown: %q", out.String())
	}
	if _, err := os.Stat(paths.store); err == nil {
		t.Error("a kit key was written despite the refusal")
	}
}

// CTO-14 P6: kit import is refused while connected, before the credential could
// be overwritten.
func TestKitImportRefusedWhenConnected(t *testing.T) {
	paths, out, c := connectedPaths(t)

	// A real kit file to import, so the refusal is reached after the file is
	// read and the state is loaded, not because the file is missing.
	salt := make([]byte, kit.SaltSize)
	rand.Read(salt)
	sec, err := kit.NewSecret(salt)
	if err != nil {
		t.Fatal(err)
	}
	kitID, err := kit.NewKitID()
	if err != nil {
		t.Fatal(err)
	}
	code, err := kit.NewRecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	f, err := kit.Seal(sec, kitID, "a-long-enough-phrase", code)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := kit.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "rosmarops-kit.json")
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	err = kitImport(c, paths, file)
	if !errors.Is(err, errSpaceConnected) {
		t.Fatalf("err = %v, want errSpaceConnected", err)
	}
	if !strings.Contains(out.String(), "connected to your space") {
		t.Errorf("notice not shown: %q", out.String())
	}
	if _, err := os.Stat(paths.store); err == nil {
		t.Error("a kit key was written despite the refusal")
	}
}
