package n8n

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// theKey is an obviously-fake n8n encryption key. The whole point of the
// scanner is that this value is never read into any output (invariant 3): the
// tests assert it never leaves the file.
const theKey = "FAKE-n8n-encryption-key-0123456789abcdefNEVER-LEAKED"

func writeN8N(t *testing.T, withKey, withDB bool, pkgs map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".n8n")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if withKey {
		body := fmt.Sprintf(`{"encryptionKey": %q, "database": {"type": "sqlite"}}`, theKey)
		if err := os.WriteFile(filepath.Join(dir, "config"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if withDB {
		if err := os.WriteFile(filepath.Join(dir, "database.sqlite"), []byte("SQLite format 3\x00"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, ver := range pkgs {
		pd := filepath.Join(dir, "nodes", "node_modules", name)
		if err := os.MkdirAll(pd, 0o700); err != nil {
			t.Fatal(err)
		}
		man := fmt.Sprintf(`{"name": %q, "version": %q}`, name, ver)
		if err := os.WriteFile(filepath.Join(pd, "package.json"), []byte(man), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestScan_KeyWithData(t *testing.T) {
	dir := writeN8N(t, true, true, map[string]string{"n8n-nodes-evil": "1.0.0"})
	res, errs := Scan(dir)
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if !res.Present || !res.KeyPresent || !res.DBPresent || !res.KeyAndData() {
		t.Fatalf("want present+key+db+keyAndData, got %+v", res)
	}
	if len(res.Packages) != 1 || res.Packages[0].Name != "n8n-nodes-evil" || res.Packages[0].Version != "1.0.0" {
		t.Fatalf("packages: %+v", res.Packages)
	}
}

// TestScan_KeyWithoutDB and the no-DB folder give no n8n_key_with_data
// condition: the key alone, or the database alone, is not reported.
func TestScan_KeyAlone(t *testing.T) {
	dir := writeN8N(t, true, false, nil)
	res, _ := Scan(dir)
	if res.KeyAndData() {
		t.Error("key without database should not be KeyAndData")
	}
	dir2 := writeN8N(t, false, true, nil)
	res2, _ := Scan(dir2)
	if res2.KeyAndData() {
		t.Error("database without key should not be KeyAndData")
	}
	if res2.KeyPresent {
		t.Error("no config: KeyPresent should be false")
	}
}

func TestScan_Missing(t *testing.T) {
	res, errs := Scan(filepath.Join(t.TempDir(), "nope", ".n8n"))
	if len(errs) != 0 || res.Present {
		t.Fatalf("missing folder: want absent, no error; got %+v errs=%v", res, errs)
	}
}

// TestScan_KeyValueNeverLeaks is the BV-175 trust check: the encryptionKey
// value never appears in the scan Result (the only thing that leaves the
// scanner) nor in anything the scanner logs.
func TestScan_KeyValueNeverLeaks(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)

	dir := writeN8N(t, true, true, map[string]string{"n8n-nodes-evil": "1.0.0"})
	res, errs := Scan(dir)
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	// The value is in the file on disk...
	raw, _ := os.ReadFile(filepath.Join(dir, "config"))
	if !bytes.Contains(raw, []byte(theKey)) {
		t.Fatal("test setup: key not in config file")
	}
	// ...but never in the Result the scanner returns, nor in the logs.
	if dumped := fmt.Sprintf("%+v", res); bytes.Contains([]byte(dumped), []byte(theKey)) {
		t.Error("encryptionKey value leaked into the scan Result")
	}
	if bytes.Contains(logs.Bytes(), []byte(theKey)) {
		t.Error("encryptionKey value leaked into the logs")
	}
}
