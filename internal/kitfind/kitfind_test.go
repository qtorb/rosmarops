package kitfind

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/kit"
)

// writeKit seals a real kit under kitID and writes it to dir as name, with
// modification time mod.
func writeKit(t *testing.T, dir, name, kitID string, mod time.Time) {
	t.Helper()
	salt := []byte("0123456789abcdef0123456789abcdef")
	sec, err := kit.NewSecret(salt)
	if err != nil {
		t.Fatal(err)
	}
	f, err := kit.Seal(sec, kitID, "a phrase for the test", "CODECODECODECODECODECODE")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := kit.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
}

const (
	kitA = "aaaaaaaa11112222333344445555aaaa"
	kitB = "bbbbbbbb11112222333344445555bbbb"
)

func TestScan_FindsValidKit(t *testing.T) {
	dir := t.TempDir()
	writeKit(t, dir, "rosmarops-kit-"+kitA[:8]+".json", kitA, time.Now())
	got := Scan(dir)
	if got == nil {
		t.Fatal("no kit found")
	}
	if got.KitID != kitA || got.ShowDate {
		t.Errorf("got %+v", got)
	}
}

func TestScan_Rejects(t *testing.T) {
	dir := t.TempDir()
	// Name prefix does not match the kit_id inside.
	writeKit(t, dir, "rosmarops-kit-99999999.json", kitA, time.Now())
	// Not a kit file name.
	if err := os.WriteFile(filepath.Join(dir, "notes.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Right name, but not a kit (garbage).
	if err := os.WriteFile(filepath.Join(dir, "rosmarops-kit-cccccccc.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Over the size cap.
	big := filepath.Join(dir, "rosmarops-kit-dddddddd.json")
	if err := os.WriteFile(big, make([]byte, MaxSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Scan(dir); got != nil {
		t.Errorf("expected no valid kit, got %+v", got)
	}
}

func TestScan_NewestAmongSeveralKits(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-time.Hour)
	writeKit(t, dir, "rosmarops-kit-"+kitA[:8]+".json", kitA, old)
	writeKit(t, dir, "rosmarops-kit-"+kitB[:8]+" (1).json", kitB, time.Now()) // browser duplicate suffix, newer
	got := Scan(dir)
	if got == nil || got.KitID != kitB {
		t.Fatalf("want newest kitB, got %+v", got)
	}
	if !got.ShowDate {
		t.Error("two distinct kit_ids: the date should be shown")
	}
}

func TestScan_SameKitTwiceNoDate(t *testing.T) {
	dir := t.TempDir()
	writeKit(t, dir, "rosmarops-kit-"+kitA[:8]+".json", kitA, time.Now().Add(-time.Hour))
	writeKit(t, dir, "rosmarops-kit-"+kitA[:8]+" (1).json", kitA, time.Now())
	got := Scan(dir)
	if got == nil || got.KitID != kitA || got.ShowDate {
		t.Fatalf("one kit, duplicated: no date; got %+v", got)
	}
}

func TestScan_NoRecursionNoSymlink(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	writeKit(t, sub, "rosmarops-kit-"+kitA[:8]+".json", kitA, time.Now()) // in a subfolder: not scanned
	// A symlink with a valid name pointing at a real kit elsewhere: skipped.
	writeKit(t, dir, "real-kit.json", kitB, time.Now())
	link := filepath.Join(dir, "rosmarops-kit-"+kitB[:8]+".json")
	if err := os.Symlink(filepath.Join(dir, "real-kit.json"), link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if got := Scan(dir); got != nil {
		t.Errorf("subfolder and symlink must be ignored, got %+v", got)
	}
}

func TestScan_MissingDir(t *testing.T) {
	if got := Scan(filepath.Join(t.TempDir(), "does-not-exist")); got != nil {
		t.Errorf("missing dir: want nil, got %+v", got)
	}
}

// guard the name pattern is anchored (no path separators, no extra text).
func TestScan_NameAnchored(t *testing.T) {
	dir := t.TempDir()
	writeKit(t, dir, "rosmarops-kit-"+kitA[:8]+".json.bak", kitA, time.Now())
	writeKit(t, dir, "x-rosmarops-kit-"+kitA[:8]+".json", kitA, time.Now())
	if got := Scan(dir); got != nil {
		t.Errorf("want nil for unanchored names, got %+v", got)
	}
}
