// Package tomlsubset reads the small part of TOML that Codex's config.toml and
// Grok Build's config.toml / trusted_folders.toml use (CTO-16, BV-140), with no
// dependency, in the line of internal/yamlsubset and internal/jsonc. It never
// guesses: any construct outside the subset, or a duplicate key, makes the whole
// document unread (an error), never a partial value.
//
// Read: comments (#); bare, quoted ("..."/'...') and dotted keys; standard
// tables [a.b.c] with bare, basic and literal-string segments; key = value with
// basic strings (with the standard escapes), literal strings, integers, floats,
// booleans; single-line arrays of those scalars ([a, "b", 'c']); single-line
// inline tables ({ a = 1, b = "x" }).
//
// Refused (the whole document errors, so the agent shows as detected, never
// half-read): arrays of tables ([[t]]); multi-line arrays, inline tables or
// strings; multi-line (""" / ”') strings; dates and times; a value that is not
// one of the kinds above; a duplicate key or a key written both as a leaf and a
// table; a table segment that is empty; more than MaxDepth nesting and more than
// MaxBytes.
package tomlsubset

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxBytes is the content-scan size limit (invariant 6).
const MaxBytes = 2 * 1024 * 1024

// MaxDepth is the deepest table nesting read.
const MaxDepth = 32

// Map is a TOML table. Arrays are Array. Scalars are string, int64, float64 or
// bool.
type (
	Map   = map[string]any
	Array = []any
)

// Parse reads the TOML subset of b into a nested Map. A construct outside the
// subset, or any ambiguity, returns an error and no partial map.
func Parse(b []byte) (Map, error) {
	if len(b) > MaxBytes {
		return nil, fmt.Errorf("tomlsubset: over %d bytes", MaxBytes)
	}
	if !utf8.Valid(b) {
		return nil, errors.New("tomlsubset: invalid UTF-8")
	}
	root := Map{}
	// defined tracks every table path created by a header, to reject a
	// duplicate [table]. leaves tracks dotted/leaf keys set, to reject a
	// duplicate key. Both keyed by the joined, normalized path.
	defined := map[string]bool{}
	cur := root // the table new keys go into
	lines := splitLines(string(b))
	for i, raw := range lines {
		line := stripComment(strings.TrimSpace(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[[") {
			return nil, lineErr(i, "arrays of tables are not supported")
		}
		if strings.HasPrefix(line, "[") {
			path, err := tableHeader(line)
			if err != nil {
				return nil, lineErr(i, err.Error())
			}
			if len(path) > MaxDepth {
				return nil, lineErr(i, "table nested too deep")
			}
			t, err := descend(root, path)
			if err != nil {
				return nil, lineErr(i, err.Error())
			}
			key := strings.Join(path, "\x00")
			if defined[key] {
				return nil, lineErr(i, "duplicate table")
			}
			defined[key] = true
			cur = t
			continue
		}
		key, val, err := keyValue(line)
		if err != nil {
			return nil, lineErr(i, err.Error())
		}
		if err := setDotted(cur, key, val); err != nil {
			return nil, lineErr(i, err.Error())
		}
	}
	return root, nil
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}

func lineErr(i int, msg string) error { return fmt.Errorf("tomlsubset: line %d: %s", i+1, msg) }

// stripComment removes a trailing # comment that is not inside a string. It
// scans the line honouring basic and literal strings so a # inside a value is
// kept.
func stripComment(s string) string {
	inBasic, inLit := false, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case inBasic:
			if c == '\\' {
				i++ // skip the escaped char
			} else if c == '"' {
				inBasic = false
			}
		case inLit:
			if c == '\'' {
				inLit = false
			}
		case c == '"':
			inBasic = true
		case c == '\'':
			inLit = true
		case c == '#':
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

// tableHeader parses "[a.b.'c']" into its path segments.
func tableHeader(line string) ([]string, error) {
	if !strings.HasSuffix(line, "]") {
		return nil, errors.New("table header not closed")
	}
	inner := strings.TrimSpace(line[1 : len(line)-1])
	if inner == "" {
		return nil, errors.New("empty table header")
	}
	return splitDottedKey(inner)
}

// descend walks/creates the tables along path from root, failing if a segment
// is already a non-table value.
func descend(root Map, path []string) (Map, error) {
	cur := root
	for _, seg := range path {
		switch v := cur[seg].(type) {
		case nil:
			t := Map{}
			cur[seg] = t
			cur = t
		case Map:
			cur = v
		default:
			return nil, fmt.Errorf("key %q is not a table", seg)
		}
	}
	return cur, nil
}

// keyValue splits "key = value" at the first top-level '='.
func keyValue(line string) (string, any, error) {
	eq := topLevelEquals(line)
	if eq < 0 {
		return "", nil, errors.New("not a key/value pair")
	}
	keyPart := strings.TrimSpace(line[:eq])
	valPart := strings.TrimSpace(line[eq+1:])
	if keyPart == "" || valPart == "" {
		return "", nil, errors.New("empty key or value")
	}
	v, err := parseValue(valPart)
	if err != nil {
		return "", nil, err
	}
	return keyPart, v, nil
}

// topLevelEquals finds the first '=' not inside a quoted key segment.
func topLevelEquals(s string) int {
	inBasic, inLit := false, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case inBasic:
			if c == '\\' {
				i++
			} else if c == '"' {
				inBasic = false
			}
		case inLit:
			if c == '\'' {
				inLit = false
			}
		case c == '"':
			inBasic = true
		case c == '\'':
			inLit = true
		case c == '=':
			return i
		}
	}
	return -1
}

// setDotted sets key (which may be dotted) to val inside table cur, creating
// intermediate tables and rejecting duplicates.
func setDotted(cur Map, key string, val any) error {
	segs, err := splitDottedKey(key)
	if err != nil {
		return err
	}
	for i, seg := range segs {
		if i == len(segs)-1 {
			if _, dup := cur[seg]; dup {
				return fmt.Errorf("duplicate key %q", seg)
			}
			cur[seg] = val
			return nil
		}
		switch v := cur[seg].(type) {
		case nil:
			t := Map{}
			cur[seg] = t
			cur = t
		case Map:
			cur = v
		default:
			return fmt.Errorf("key %q is not a table", seg)
		}
	}
	return nil
}

// splitDottedKey splits "a.b.'c'" into segments, honouring quoted segments.
func splitDottedKey(s string) ([]string, error) {
	var segs []string
	i := 0
	for {
		for i < len(s) && s[i] == ' ' {
			i++
		}
		if i >= len(s) {
			return nil, errors.New("empty key segment")
		}
		var seg string
		switch s[i] {
		case '"':
			str, n, err := scanBasicString(s[i:])
			if err != nil {
				return nil, err
			}
			seg, i = str, i+n
		case '\'':
			str, n, err := scanLiteralString(s[i:])
			if err != nil {
				return nil, err
			}
			seg, i = str, i+n
		default:
			j := i
			for j < len(s) && s[j] != '.' && s[j] != ' ' {
				j++
			}
			seg, i = s[i:j], j
			if !bareKeyOK(seg) {
				return nil, fmt.Errorf("invalid bare key %q", seg)
			}
		}
		segs = append(segs, seg)
		for i < len(s) && s[i] == ' ' {
			i++
		}
		if i >= len(s) {
			return segs, nil
		}
		if s[i] != '.' {
			return nil, errors.New("expected '.' between key segments")
		}
		i++
	}
}

func bareKeyOK(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// parseValue parses a scalar, a single-line array or a single-line inline table.
func parseValue(s string) (any, error) {
	v, n, err := scanValue(s)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(s[n:]) != "" {
		return nil, errors.New("trailing characters after value")
	}
	return v, nil
}

// scanValue reads one value from the start of s and returns it and how many
// bytes it consumed.
func scanValue(s string) (any, int, error) {
	t := strings.TrimLeft(s, " \t")
	lead := len(s) - len(t)
	if t == "" {
		return nil, 0, errors.New("missing value")
	}
	switch t[0] {
	case '"':
		if strings.HasPrefix(t, `"""`) {
			return nil, 0, errors.New("multi-line strings are not supported")
		}
		str, n, err := scanBasicString(t)
		return str, lead + n, err
	case '\'':
		if strings.HasPrefix(t, `'''`) {
			return nil, 0, errors.New("multi-line strings are not supported")
		}
		str, n, err := scanLiteralString(t)
		return str, lead + n, err
	case '[':
		arr, n, err := scanArray(t)
		return arr, lead + n, err
	case '{':
		m, n, err := scanInlineTable(t)
		return m, lead + n, err
	default:
		// a bare token: bool, int, float. Ends at , ] } or end.
		j := 0
		for j < len(t) && !strings.ContainsRune(",]}", rune(t[j])) {
			j++
		}
		tok := strings.TrimSpace(t[:j])
		v, err := scalarToken(tok)
		return v, lead + j, err
	}
}

func scalarToken(tok string) (any, error) {
	switch tok {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "":
		return nil, errors.New("empty value")
	}
	clean := strings.ReplaceAll(tok, "_", "")
	if n, err := strconv.ParseInt(clean, 10, 64); err == nil {
		return n, nil
	}
	if f, err := strconv.ParseFloat(clean, 64); err == nil {
		return f, nil
	}
	return nil, fmt.Errorf("unsupported value %q", tok)
}

// scanBasicString reads a "..." string with standard escapes.
func scanBasicString(s string) (string, int, error) {
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			if i+1 >= len(s) {
				return "", 0, errors.New("bad escape")
			}
			i++
			switch e := s[i]; e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'u':
				if i+4 >= len(s) {
					return "", 0, errors.New("bad \\u escape")
				}
				r, err := strconv.ParseUint(s[i+1:i+5], 16, 32)
				if err != nil {
					return "", 0, errors.New("bad \\u escape")
				}
				b.WriteRune(rune(r))
				i += 4
			case 'U':
				if i+8 >= len(s) {
					return "", 0, errors.New("bad \\U escape")
				}
				r, err := strconv.ParseUint(s[i+1:i+9], 16, 32)
				if err != nil {
					return "", 0, errors.New("bad \\U escape")
				}
				b.WriteRune(rune(r))
				i += 8
			default:
				return "", 0, fmt.Errorf("unsupported escape \\%c", e)
			}
		case '"':
			return b.String(), i + 1, nil
		case '\n':
			return "", 0, errors.New("newline in string")
		default:
			b.WriteByte(c)
		}
	}
	return "", 0, errors.New("unterminated string")
}

// scanLiteralString reads a '...' string (no escapes).
func scanLiteralString(s string) (string, int, error) {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\'':
			return s[1:i], i + 1, nil
		case '\n':
			return "", 0, errors.New("newline in string")
		}
	}
	return "", 0, errors.New("unterminated literal string")
}

// scanArray reads a single-line [ ... ] array of scalars/arrays.
func scanArray(s string) (Array, int, error) {
	arr := Array{}
	i := 1
	for {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			return nil, 0, errors.New("unterminated array")
		}
		if s[i] == ']' {
			return arr, i + 1, nil
		}
		v, n, err := scanValue(s[i:])
		if err != nil {
			return nil, 0, err
		}
		arr = append(arr, v)
		i += n
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			return nil, 0, errors.New("unterminated array")
		}
		switch s[i] {
		case ',':
			i++
		case ']':
			return arr, i + 1, nil
		default:
			return nil, 0, errors.New("expected ',' or ']' in array")
		}
	}
}

// scanInlineTable reads a single-line { k = v, ... } inline table.
func scanInlineTable(s string) (Map, int, error) {
	m := Map{}
	i := 1
	for {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			return nil, 0, errors.New("unterminated inline table")
		}
		if s[i] == '}' {
			return m, i + 1, nil
		}
		// key
		var key string
		switch s[i] {
		case '"':
			str, n, err := scanBasicString(s[i:])
			if err != nil {
				return nil, 0, err
			}
			key, i = str, i+n
		case '\'':
			str, n, err := scanLiteralString(s[i:])
			if err != nil {
				return nil, 0, err
			}
			key, i = str, i+n
		default:
			j := i
			for j < len(s) && s[j] != '=' && s[j] != ' ' && s[j] != '\t' {
				j++
			}
			key, i = s[i:j], j
			if !bareKeyOK(key) {
				return nil, 0, fmt.Errorf("invalid key %q in inline table", key)
			}
		}
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) || s[i] != '=' {
			return nil, 0, errors.New("expected '=' in inline table")
		}
		i++
		v, n, err := scanValue(s[i:])
		if err != nil {
			return nil, 0, err
		}
		if _, dup := m[key]; dup {
			return nil, 0, fmt.Errorf("duplicate key %q in inline table", key)
		}
		m[key] = v
		i += n
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			return nil, 0, errors.New("unterminated inline table")
		}
		switch s[i] {
		case ',':
			i++
		case '}':
			return m, i + 1, nil
		default:
			return nil, 0, errors.New("expected ',' or '}' in inline table")
		}
	}
}
