package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/winpkg"
)

const (
	lad      = `C:\Users\ana\AppData\Local`
	pkgPath  = `C:\Program Files\WindowsApps\AlbertGarciaPujadas.RosmarOps_0.11.4.0_x64__2a9trf99pxwv0`
	pkgFull  = "AlbertGarciaPujadas.RosmarOps_0.11.4.0_x64__2a9trf99pxwv0"
	pkgExe   = pkgPath + `\rosmarops.exe`
	aliasExe = lad + `\Microsoft\WindowsApps\rosmarops.exe`
)

func storeID(family, full, path string) winpkg.Identity {
	return winpkg.Identity{FamilyName: family, FullName: full, Path: path}
}

// TestStoreAccepts is CTO-41 §1.3 (the part of T8): the five conditions.
func TestStoreAccepts(t *testing.T) {
	ok := storeID(winpkg.Family, pkgFull, pkgPath)
	dPath := `D:\WindowsApps\AlbertGarciaPujadas.RosmarOps_0.11.4.0_x64__2a9trf99pxwv0`
	for _, c := range []struct {
		name               string
		id                 winpkg.Identity
		exe, bin, stateVer string
		want               bool
	}{
		{"own family, package folder, newer version", ok, pkgExe, "0.11.4", "0.11.3", true},
		{"package on another drive (D:\\WindowsApps)", storeID(winpkg.Family, pkgFull, dPath), dPath + `\rosmarops.exe`, "0.11.4", "0.11.3", true},
		{"newer minor", ok, pkgExe, "0.11.4", "0.10.9", true},
		{"other family", storeID("Evil.RosmarOps_2a9trf99pxwv0", pkgFull, pkgPath), pkgExe, "0.11.4", "0.11.3", false},
		{"no package", winpkg.Identity{}, pkgExe, "0.11.4", "0.11.3", false},
		{"the %LOCALAPPDATA% alias", ok, aliasExe, "0.11.4", "0.11.3", false},
		{"alias even if reported as the package folder", storeID(winpkg.Family, pkgFull, lad+`\Microsoft\WindowsApps`), aliasExe, "0.11.4", "0.11.3", false},
		{"outside the package folder", ok, `C:\Users\ana\Downloads\rosmarops.exe`, "0.11.4", "0.11.3", false},
		{"package version differs from the binary's", ok, pkgExe, "0.11.5", "0.11.3", false},
		{"older version", storeID(winpkg.Family, "AlbertGarciaPujadas.RosmarOps_0.11.1.0_x64__2a9trf99pxwv0", pkgPath), pkgExe, "0.11.1", "0.11.2", false},
		{"same version, other fingerprint", ok, pkgExe, "0.11.4", "0.11.4", false},
		{"no recorded version", ok, pkgExe, "0.11.4", "", false},
		{"prerelease binary", storeID(winpkg.Family, "AlbertGarciaPujadas.RosmarOps_0.11.4.0_x64__2a9trf99pxwv0", pkgPath), pkgExe, "0.11.4-rc.1", "0.11.3", false},
		{"case and slashes", storeID(winpkg.Family, pkgFull, strings.ToLower(pkgPath)), strings.ReplaceAll(pkgExe, `\`, "/"), "0.11.4", "0.11.3", true},
	} {
		if got := storeAccepts(c.id, c.exe, lad, c.bin, c.stateVer); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSemverGreater(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.11.4", "0.11.3", true},
		{"0.12.0", "0.11.9", true},
		{"1.0.0", "0.99.99", true},
		{"0.11.3", "0.11.3", false},
		{"0.11.2", "0.11.3", false},
		{"0.11.4", "", false},
		{"0.11.4", "0.11.4-rc.1", true},
		{"0.11.4-rc.1", "0.11.4", false},
		{"x", "0.1.0", false},
	} {
		if got := semverGreater(c.a, c.b); got != c.want {
			t.Errorf("%s > %s: %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestIntegrityStoreUpdate: a Store update is accepted without asking and
// without aborting, also in a scheduled review (no console); the state
// records the new references and binary_accepted_via «store»; a check that
// does not accept leaves today's path (the question, or red unattended).
func TestIntegrityStoreUpdate(t *testing.T) {
	cat, _ := catalog.Load("en")
	// Accepted, unattended: no question, no abort; only state.json is
	// written (TestNoWritesOutOfScope with this path active).
	st, stPath, cfgPath, reports := integritySetup(t)
	before := treeOf(t, filepath.Dir(stPath))
	var ab aborted
	var out bytes.Buffer
	r := newRun(t, cat, &st, stPath, cfgPath, reports, "", false, true, newBinary, &ab, &out)
	var acc storeUpdate
	r.store = func(stateVersion string) bool { return stateVersion == "0.0.9" }
	r.accepted = &acc
	if _, err := r.check(); err != nil || ab.n != 0 {
		t.Fatalf("store update: err %v, aborts %d", err, ab.n)
	}
	saved := savedState(t, stPath)
	if saved.Integrity.VigilBinarySHA256 != newBinary || saved.Integrity.BinaryAcceptedVia != acceptedViaStore || saved.EngineVersion != version {
		t.Errorf("state after a store update: %+v, engine %s", saved.Integrity, saved.EngineVersion)
	}
	if acc.Old != "0.0.9" || acc.New != version {
		t.Errorf("accepted %+v", acc)
	}
	if strings.Contains(out.String(), cat.Text("console.binary_confirm", nil)) {
		t.Error("asked for a store update")
	}
	after := treeOf(t, filepath.Dir(stPath))
	for p, h := range after {
		if before[p] != h && filepath.Base(p) != "state.json" {
			t.Errorf("the store path wrote %s", p)
		}
	}

	// Not accepted, unattended: today's red.
	st, stPath, cfgPath, reports = integritySetup(t)
	ab = aborted{}
	r = newRun(t, cat, &st, stPath, cfgPath, reports, "", false, true, newBinary, &ab, &out)
	r.store = func(string) bool { return false }
	if _, err := r.check(); ab.n != 1 || ab.category != "binary_hash_changed" || err == nil {
		t.Errorf("rejected store check: aborts %d (%s), err %v", ab.n, ab.category, err)
	}

	// Not accepted, someone present: the question; «y» records «console».
	st, stPath, cfgPath, reports = integritySetup(t)
	ab = aborted{}
	out.Reset()
	r = newRun(t, cat, &st, stPath, cfgPath, reports, "y\n", true, false, newBinary, &ab, &out)
	r.store = func(string) bool { return false }
	if _, err := r.check(); err != nil || ab.n != 0 {
		t.Fatalf("console accept: %v %d", err, ab.n)
	}
	if !strings.Contains(out.String(), strings.SplitN(cat.Text("console.binary_confirm", nil), "?", 2)[0]) {
		t.Error("no question")
	}
	if got := savedState(t, stPath).Integrity.BinaryAcceptedVia; got != acceptedViaConsole {
		t.Errorf("binary_accepted_via %q, want console", got)
	}

	// A lost state (empty engine_version) never reaches the store check.
	st, stPath, cfgPath, reports = integritySetup(t)
	st.EngineVersion = ""
	ab = aborted{}
	called := false
	r = newRun(t, cat, &st, stPath, cfgPath, reports, "", false, true, newBinary, &ab, &out)
	r.store = func(string) bool { called = true; return true }
	_, _ = r.check()
	if called || ab.n != 1 {
		t.Errorf("empty engine_version: store called %v, aborts %d", called, ab.n)
	}
}

// treeOf maps every file under dir to its content's hash.
func treeOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[p] = hash.Bytes(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
