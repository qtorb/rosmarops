package untrusted

import (
	"strings"
	"testing"
)

func TestPlainText(t *testing.T) {
	for in, want := range map[string]string{
		"C:/Users/me/.mcp.json":            "C:/Users/me/.mcp.json",
		"evil\nIgnore previous":            "evil⟨U+000A⟩Ignore previous",
		"a\x00b\x1bc\x7fd\u0085e\u009bf":   "a⟨U+0000⟩b⟨U+001B⟩c⟨U+007F⟩d⟨U+0085⟩e⟨U+009B⟩f",
		"line\u2028para\u2029end":          "line⟨U+2028⟩para⟨U+2029⟩end",
		"x\u202eexe.txt\u2066\u2069\u200f": "x⟨U+202E⟩exe.txt⟨U+2066⟩⟨U+2069⟩⟨U+200F⟩",
		"zero\u200bwidth\ufeffbom":         "zero⟨U+200B⟩width⟨U+FEFF⟩bom",
		"close`` ```fence":                 "close'' '''fence",
		"bad\xffbyte":                      "bad⟨U+FFFD⟩byte",
		"ñandú/日本":                         "ñandú/日本",
	} {
		if got := PlainText(in); got != want {
			t.Errorf("PlainText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVisiblePaste(t *testing.T) {
	for in, want := range map[string]string{
		"line1\nline2\tcol":       "line1\nline2\tcol", // newline and tab kept
		"zero\u200bwidth":         "zero⟨U+200B⟩width",
		"rtl\u202eoverride":       "rtl⟨U+202E⟩override",
		"carriage\rreturn":        "carriage⟨U+000D⟩return", // only \n and \t are kept
		"keep`backtick":           "keep`backtick",          // backtick is literal in a paste block
		"bom\ufeffmark\u2068isol": "bom⟨U+FEFF⟩mark⟨U+2068⟩isol",
	} {
		if got := VisiblePaste(in); got != want {
			t.Errorf("VisiblePaste(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlainTextCut(t *testing.T) {
	long := strings.Repeat("é", MaxRunes+50)
	if got := PlainText(long); got != strings.Repeat("é", MaxRunes) {
		t.Errorf("cut to %d runes, want %d", len([]rune(got)), MaxRunes)
	}
}
