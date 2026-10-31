// Package untrusted makes text read from disk safe to show (decision
// 2026-10-27, blindaje, B7; CTO-11 C3): a path, a server name or a variable
// name comes from files anyone with the user's rights can write, and it ends
// up in the report, in the closed package the customer area paints, and in the
// text the user pastes into an AI or a terminal. A deceptive character there
// can make what is shown differ from what is pasted.
package untrusted

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxRunes is where PlainText cuts a value.
const MaxRunes = 200

// deceptive reports a character neutralised in shown text (CTO-11 C3): the
// C0 and C1 control ranges (DEL included), the zero-width and bidirectional
// format characters U+200B–U+200F, the embeddings and overrides U+202A–U+202E,
// the isolates U+2066–U+2069, the Arabic letter mark U+061C, the line and
// paragraph separators U+2028/U+2029, and the byte-order mark U+FEFF.
func deceptive(r rune) bool {
	switch {
	case r <= 0x1F, r == 0x7F, r >= 0x80 && r <= 0x9F: // C0, DEL, C1
		return true
	case r >= 0x200B && r <= 0x200F: // zero-width, LRM/RLM/ALM
		return true
	case r >= 0x202A && r <= 0x202E: // bidi embeddings and overrides
		return true
	case r >= 0x2066 && r <= 0x2069: // bidi isolates
		return true
	case r == 0x061C: // Arabic letter mark
		return true
	case r == 0x2028, r == 0x2029: // line and paragraph separators
		return true
	case r == 0xFEFF: // byte-order mark / zero-width no-break space
		return true
	}
	return false
}

// visibleRune is a deceptive character shown as the literal ⟨U+XXXX⟩, in
// uppercase hexadecimal of four digits or more (CTO-11 C3).
func visibleRune(r rune) string { return fmt.Sprintf("⟨U+%04X⟩", r) }

// PlainText is s as one plain line, safe to show: each deceptive character
// (see deceptive) and each invalid UTF-8 byte become the visible literal
// ⟨U+XXXX⟩; the backtick becomes "'", so a value never closes a Markdown code
// span or fence; the result is cut to MaxRunes characters.
//
// It is applied where each value read from disk enters a text, not to the
// whole text.
func PlainText(s string) string {
	var b strings.Builder
	n := 0
	for i, w := 0, 0; i < len(s) && n < MaxRunes; i += w {
		var r rune
		r, w = utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && w <= 1:
			b.WriteString(visibleRune(0xFFFD))
		case deceptive(r):
			b.WriteString(visibleRune(r))
		case r == '`':
			b.WriteByte('\'')
		default:
			b.WriteRune(r)
		}
		n++
	}
	return b.String()
}

// VisiblePaste is the paste-block form of PlainText (CTO-11 C3): a newline and
// a tab are kept as they are, because a paste block is literal text the user
// copies into a terminal; every other deceptive character and every invalid
// UTF-8 byte become ⟨U+XXXX⟩. The backtick is left alone and nothing is cut:
// the schema bounds the block, and a paste block is not Markdown.
func VisiblePaste(s string) string {
	var b strings.Builder
	for i, w := 0, 0; i < len(s); i += w {
		var r rune
		r, w = utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r == utf8.RuneError && w <= 1:
			b.WriteString(visibleRune(0xFFFD))
		case deceptive(r):
			b.WriteString(visibleRune(r))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
