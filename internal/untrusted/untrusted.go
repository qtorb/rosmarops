// Package untrusted makes text read from disk safe to show (decision
// 2026-10-27, blindaje, B7): a path, a server name or a variable name comes
// from files anyone with the user's rights can write, and it ends up in the
// report and in the text the user pastes into an AI.
package untrusted

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxRunes is where PlainText cuts a value.
const MaxRunes = 200

// PlainText is s as one plain line: control characters (C0, DEL and C1),
// the line and paragraph separators U+2028/U+2029, the bidirectional
// controls and invalid UTF-8 become '?'; the backtick becomes "'", so a
// value never closes a Markdown code span or fence; the result is cut to
// MaxRunes characters.
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
		case r == utf8.RuneError && w <= 1, unicode.IsControl(r), r == '\u2028', r == '\u2029', isBidi(r):
			b.WriteByte('?')
		case r == '`':
			b.WriteByte('\'')
		default:
			b.WriteRune(r)
		}
		n++
	}
	return b.String()
}

// isBidi reports the bidirectional formatting characters: ALM, LRM and RLM,
// the embeddings and overrides U+202A–U+202E and the isolates
// U+2066–U+2069.
func isBidi(r rune) bool {
	return r == '\u061c' || r == '\u200e' || r == '\u200f' ||
		r >= '\u202a' && r <= '\u202e' || r >= '\u2066' && r <= '\u2069'
}
