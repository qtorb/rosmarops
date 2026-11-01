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

// connectedPaths lays out a temp state (SpacePermission true) and a kit holding
// a send credential — i.e. a genuinely connected computer (CTO-15 PR2) — plus
// the console for the P6 refusal tests. It returns the stored credential so a
// test can check the refusal left it untouched.
func connectedPaths(t *testing.T) (kitPaths, *bytes.Buffer, *console, []byte) {
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
	cred := make([]byte, 32)
	rand.Read(cred)
	if err := kit.Save(paths.store, kit.Stored{KitID: kitID, Secret: sec, SpaceCredential: cred}); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	c := &console{in: bufio.NewReader(strings.NewReader("")), out: out, cat: cat}
	return paths, out, c, cred
}

// kitCredentialUnchanged fails if the connected kit's credential changed.
func kitCredentialUnchanged(t *testing.T, storePath string, want []byte) {
	t.Helper()
	stored, err := kit.Load(storePath)
	if err != nil || !bytes.Equal(stored.SpaceCredential, want) {
		t.Errorf("the connected kit's credential was changed despite the refusal (err=%v)", err)
	}
}

// CTO-14 P6: kit create is refused while this computer is connected to a space,
// with the connected notice and no key written.
func TestKitCreateRefusedWhenConnected(t *testing.T) {
	paths, out, c, cred := connectedPaths(t)
	err := kitCreate(c, paths)
	if !errors.Is(err, errSpaceConnected) {
		t.Fatalf("err = %v, want errSpaceConnected", err)
	}
	if !strings.Contains(out.String(), "connected to your space") {
		t.Errorf("notice not shown: %q", out.String())
	}
	kitCredentialUnchanged(t, paths.store, cred)
}

// CTO-14 P6: kit import is refused while connected, before the credential could
// be overwritten.
func TestKitImportRefusedWhenConnected(t *testing.T) {
	paths, out, c, cred := connectedPaths(t)

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
	kitCredentialUnchanged(t, paths.store, cred)
}
