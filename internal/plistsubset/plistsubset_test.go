package plistsubset_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/internal/plistsubset"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The same dictionary in the XML and binary forms (written by Python's
// plistlib): booleans, an integer and a UTF-16 string are read; the nested
// dictionary, the array and the data are skipped.
func TestParse_XMLAndBinary(t *testing.T) {
	for _, name := range []string{"managed.xml.plist", "managed.binary.plist"} {
		got, err := plistsubset.Parse(load(t, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := map[string]plistsubset.Value{
			"isLocalDevMcpEnabled":            {Kind: plistsubset.KindBool},
			"isDesktopExtensionEnabled":       {Kind: plistsubset.KindBool, Bool: true},
			"mcpPersistentAlwaysAllowEnabled": {Kind: plistsubset.KindBool},
			"count":                           {Kind: plistsubset.KindInt, Int: 300},
			"name":                            {Kind: plistsubset.KindString, String: "Über corp"},
		}
		if len(got) != len(want) {
			t.Errorf("%s: %d values, want %d: %+v", name, len(got), len(want), got)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: %s = %+v, want %+v", name, k, got[k], v)
			}
		}
	}
}

// A dictionary of more than 14 entries uses the long length form.
func TestParse_BinaryLongDictionary(t *testing.T) {
	got, err := plistsubset.Parse(load(t, "many.binary.plist"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 20 || !got["k00"].Bool || got["k01"].Bool {
		t.Fatalf("got %+v", got)
	}
}

// What is not a top-level dictionary, or is malformed, is an error.
func TestParse_Refused(t *testing.T) {
	bin := load(t, "managed.binary.plist")
	cases := map[string][]byte{
		"top-level array":   load(t, "array.binary.plist"),
		"truncated binary":  bin[:len(bin)-5],
		"short binary":      []byte("bplist00\x00"),
		"not a plist":       []byte(`{"isLocalDevMcpEnabled": false}`),
		"xml array":         []byte(`<?xml version="1.0"?><plist><array><true/></array></plist>`),
		"xml key no value":  []byte(`<plist><dict><key>a</key></dict></plist>`),
		"xml duplicate key": []byte(`<plist><dict><key>a</key><true/><key>a</key><false/></dict></plist>`),
		"xml bad integer":   []byte(`<plist><dict><key>a</key><integer>x</integer></dict></plist>`),
	}
	for name, raw := range cases {
		if _, err := plistsubset.Parse(raw); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// Every byte of a binary plist cut short or corrupted is an error, never
// a panic.
func TestParse_BinaryCorruptionNeverPanics(t *testing.T) {
	bin := load(t, "managed.binary.plist")
	for i := range bin {
		for _, b := range []byte{0x00, 0xFF, 0x0F, 0xDF} {
			c := append([]byte{}, bin...)
			c[i] = b
			_, _ = plistsubset.Parse(c)
		}
		_, _ = plistsubset.Parse(bin[:i])
	}
}
