package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/firstrun"
	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/kitfind"
)

// setKitSeams saves and restores the BV-131 onboarding seams.
func setKitSeams(t *testing.T) {
	t.Helper()
	ps, dd, sc, ak, rv := kitStorePath, downloadsDir, scanDownloads, askKitPhrase, revealInExplorer
	t.Cleanup(func() {
		kitStorePath, downloadsDir, scanDownloads, askKitPhrase, revealInExplorer = ps, dd, sc, ak, rv
	})
}

// writeKit seals a real kit for phrase and leaves its file in dir, returning
// the file path and the kit_id.
func writeKit(t *testing.T, dir, phrase string) (string, string) {
	t.Helper()
	salt := make([]byte, 32)
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
	f, err := kit.Seal(sec, kitID, phrase, code)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := kit.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, kit.ExportName(f.KitID))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, f.KitID
}

func kitHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	for _, n := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		d := filepath.Join(root, strings.ToLower(n))
		if n == "HOME" || n == "USERPROFILE" {
			d = home
		}
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(n, d)
	}
	return home
}

// A kit file in Downloads and the right phrase: the key is imported (the store
// appears) and the window saw the catalog texts.
func TestKitOnboardingImports(t *testing.T) {
	setKitSeams(t)
	home := kitHome(t)
	dl := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(dl, 0o700); err != nil {
		t.Fatal(err)
	}
	const phrase = "correct horse battery staple"
	kfile, kitID := writeKit(t, dl, phrase)
	store := filepath.Join(home, ".rosmarops", "kit.store")

	var gotCfg firstrun.KitConfig
	kitStorePath = func() (string, error) { return store, nil }
	downloadsDir = func() (string, error) { return dl, nil }
	scanDownloads = func(string) *kitfind.Found { return &kitfind.Found{Path: kfile, KitID: kitID} }
	askKitPhrase = func(cfg firstrun.KitConfig, check func(string) bool) bool {
		gotCfg = cfg
		return check(phrase)
	}

	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	maybeImportKitFromDownloads(cat, "en")

	if !fileExists(store) {
		t.Fatal("the kit was not imported")
	}
	if _, err := kit.Load(store); err != nil {
		t.Fatalf("stored kit does not open: %v", err)
	}
	if gotCfg.Found != cat.Text("firstrun.kit_found", nil) || gotCfg.Use != cat.Text("firstrun.kit_use", nil) {
		t.Errorf("the window did not get the catalog texts: %+v", gotCfg)
	}
	// One kit found → no date line.
	if gotCfg.Date != "" {
		t.Errorf("date shown for a single kit: %q", gotCfg.Date)
	}
}

// The date line appears only when the scan reports several distinct kits.
func TestKitOnboardingShowsDateForSeveralKits(t *testing.T) {
	setKitSeams(t)
	home := kitHome(t)
	dl := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(dl, 0o700); err != nil {
		t.Fatal(err)
	}
	kfile, kitID := writeKit(t, dl, "correct horse battery staple")
	store := filepath.Join(home, ".rosmarops", "kit.store")

	var gotCfg firstrun.KitConfig
	kitStorePath = func() (string, error) { return store, nil }
	downloadsDir = func() (string, error) { return dl, nil }
	scanDownloads = func(string) *kitfind.Found {
		return &kitfind.Found{Path: kfile, KitID: kitID, ShowDate: true}
	}
	askKitPhrase = func(cfg firstrun.KitConfig, _ func(string) bool) bool { gotCfg = cfg; return false }

	cat, _ := catalog.Load("es")
	maybeImportKitFromDownloads(cat, "es")
	if gotCfg.Date == "" || !strings.Contains(gotCfg.Date, "Fichero de la clave") {
		t.Errorf("date line not shown for several kits: %q", gotCfg.Date)
	}
}

// «Not now» (the window returns false) leaves the computer without a key.
func TestKitOnboardingNotNow(t *testing.T) {
	setKitSeams(t)
	home := kitHome(t)
	dl := filepath.Join(home, "Downloads")
	_ = os.MkdirAll(dl, 0o700)
	kfile, kitID := writeKit(t, dl, "correct horse battery staple")
	store := filepath.Join(home, ".rosmarops", "kit.store")

	called := false
	kitStorePath = func() (string, error) { return store, nil }
	downloadsDir = func() (string, error) { return dl, nil }
	scanDownloads = func(string) *kitfind.Found { return &kitfind.Found{Path: kfile, KitID: kitID} }
	askKitPhrase = func(firstrun.KitConfig, func(string) bool) bool { called = true; return false }

	cat, _ := catalog.Load("en")
	maybeImportKitFromDownloads(cat, "en")
	if !called {
		t.Fatal("the phrase window was not offered")
	}
	if fileExists(store) {
		t.Error("a key was saved though the window was dismissed")
	}
}

// A computer that already has a key never looks in Downloads.
func TestKitOnboardingSkippedWithExistingKey(t *testing.T) {
	setKitSeams(t)
	home := kitHome(t)
	store := filepath.Join(home, ".rosmarops", "kit.store")
	if err := os.MkdirAll(filepath.Dir(store), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	looked := false
	kitStorePath = func() (string, error) { return store, nil }
	downloadsDir = func() (string, error) { looked = true; return "", nil }
	scanDownloads = func(string) *kitfind.Found { looked = true; return nil }
	askKitPhrase = func(firstrun.KitConfig, func(string) bool) bool { looked = true; return false }

	cat, _ := catalog.Load("en")
	maybeImportKitFromDownloads(cat, "en")
	if looked {
		t.Error("Downloads was searched although the computer has a key")
	}
}

// The wrong phrase does not open the key: the check returns false, so the
// window keeps asking and nothing is saved.
func TestImportKitWithPhraseWrong(t *testing.T) {
	setKitSeams(t)
	home := kitHome(t)
	dl := filepath.Join(home, "Downloads")
	_ = os.MkdirAll(dl, 0o700)
	kfile, _ := writeKit(t, dl, "correct horse battery staple")
	store := filepath.Join(home, ".rosmarops", "kit.store")

	if importKitWithPhrase(store, kfile, "the wrong phrase entirely") {
		t.Error("the wrong phrase opened the key")
	}
	if fileExists(store) {
		t.Error("a key was saved for a wrong phrase")
	}
	if !importKitWithPhrase(store, kfile, "correct horse battery staple") {
		t.Error("the right phrase did not open the key")
	}
	if !fileExists(store) {
		t.Error("the right phrase did not save the key")
	}
}

// revealPackage calls Explorer only when a package was written.
func TestRevealPackage(t *testing.T) {
	setKitSeams(t)
	var seen []string
	revealInExplorer = func(p string) error { seen = append(seen, p); return nil }
	revealPackage("")
	if len(seen) != 0 {
		t.Errorf("Explorer opened with no package: %v", seen)
	}
	revealPackage("/some/reports/rosmarops-results-2026-10-31-abcd1234.locked.json")
	if len(seen) != 1 {
		t.Fatalf("Explorer not opened for a written package: %v", seen)
	}
}
