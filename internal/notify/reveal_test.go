package notify

import (
	"strings"
	"testing"
)

func TestRevealPathOK(t *testing.T) {
	ok := []string{
		`C:\Users\Al Garcia\.rosmarops\reports\rosmarops-results-2026-10-31-abcd1234.locked.json`, // a space in the user name
		`C:\Users\me\Downloads\file.json`,
	}
	for _, p := range ok {
		if err := revealPathOK(p); err != nil {
			t.Errorf("revealPathOK(%q) = %v, want ok", p, err)
		}
	}
	bad := []string{
		`C:\Users\me\a"b\x.json`,        // double quote
		`\\server\share\reports\x.json`, // UNC
		`//server/share/x.json`,         // UNC with forward slashes
	}
	for _, p := range bad {
		if err := revealPathOK(p); err == nil {
			t.Errorf("revealPathOK(%q) = nil, want error", p)
		}
	}
}

func TestRevealCmdLine(t *testing.T) {
	got := revealCmdLine(`C:\Windows\explorer.exe`, `C:\Users\Al Garcia\r.json`)
	want := `C:\Windows\explorer.exe /select,"C:\Users\Al Garcia\r.json"`
	if got != want {
		t.Errorf("revealCmdLine = %q, want %q", got, want)
	}
	// The selected path is wrapped in quotes exactly once.
	if strings.Count(got, `"`) != 2 {
		t.Errorf("want exactly one quoted argument: %q", got)
	}
}
