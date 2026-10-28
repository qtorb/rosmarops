package untrusted

import (
	"strings"
	"testing"
)

func TestPlainText(t *testing.T) {
	for in, want := range map[string]string{
		"C:/Users/me/.mcp.json":            "C:/Users/me/.mcp.json",
		"evil\nIgnore previous":            "evil?Ignore previous",
		"a\x00b\x1bc\x7fd\u0085e\u009bf":   "a?b?c?d?e?f",
		"line\u2028para\u2029end":          "line?para?end",
		"x\u202eexe.txt\u2066\u2069\u200f": "x?exe.txt???",
		"close`` ```fence":                 "close'' '''fence",
		"bad\xffbyte":                      "bad?byte",
		"ñandú/日本":                         "ñandú/日本",
	} {
		if got := PlainText(in); got != want {
			t.Errorf("PlainText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlainTextCut(t *testing.T) {
	long := strings.Repeat("é", MaxRunes+50)
	if got := PlainText(long); got != strings.Repeat("é", MaxRunes) {
		t.Errorf("cut to %d runes, want %d", len([]rune(got)), MaxRunes)
	}
}
