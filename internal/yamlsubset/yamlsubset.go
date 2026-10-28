// Package yamlsubset reads the small part of YAML that Hermes Agent's
// config.yaml uses (decision 2026-10-27, agentes nuevos, N2; LECTURA-100 §2),
// with no dependency, in the line of internal/jsonc. It never guesses: any
// construct outside the subset, or a duplicate key, makes the whole document
// unread (an error), never a partial value.
//
// Read: comments (#); block mappings, nested by indentation (spaces only);
// plain, double-quoted and single-quoted scalars on one line; flow sequences
// of scalars on one line ([a, "b", 'c']); block sequences of scalars
// ("- item"). YAML 1.1 booleans (yes/no/on/off/true/false, any case) are
// booleans: Hermes reads its config as YAML 1.1, so «mode: off» is false.
//
// Refused: anchors (&), aliases (*), merge keys (<<), tags (!), directives
// (%), several documents (---, ...), block scalars (| and >), flow mappings
// ({}), complex keys (?), multi-line scalars, tabs in indentation,
// inconsistent indentation, a duplicate key, more than MaxDepth levels and
// more than MaxBytes.
package yamlsubset

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxBytes is the content-scan size limit (invariant 6).
const MaxBytes = 2 * 1024 * 1024

// MaxDepth is the deepest nesting read.
const MaxDepth = 32

// Map is a block mapping; Seq a sequence. Scalars are Scalar. A key with no
// value holds a null Scalar.
type (
	Map = map[string]any
	Seq = []any
)

// Scalar is a scalar as written: its text (quotes removed, escapes
// resolved) and whether it was quoted. A quoted scalar is always a string.
type Scalar struct {
	Text   string
	Quoted bool
}

// Bool is the scalar as a YAML 1.1 boolean: a plain yes/no/on/off/true/false
// in any case.
func (s Scalar) Bool() (value, ok bool) {
	if s.Quoted {
		return false, false
	}
	switch strings.ToLower(s.Text) {
	case "yes", "on", "true":
		return true, true
	case "no", "off", "false":
		return false, true
	}
	return false, false
}

// IsNull reports a plain empty, ~ or null scalar.
func (s Scalar) IsNull() bool {
	if s.Quoted {
		return false
	}
	switch s.Text {
	case "", "~", "null", "Null", "NULL":
		return true
	}
	return false
}

// line is one meaningful line: its number, indentation and content without
// the comment.
type line struct {
	n      int
	indent int
	text   string
}

// Parse reads raw (a leading UTF-8 BOM and CRLF line ends are accepted) and
// returns its top-level value: a Map, or nil for an empty document.
func Parse(raw []byte) (any, error) {
	if len(raw) > MaxBytes {
		return nil, fmt.Errorf("%d bytes > %d", len(raw), MaxBytes)
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(raw) {
		return nil, errors.New("not UTF-8")
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if strings.ContainsRune(text, '\r') {
		return nil, errors.New("a carriage return outside a line end")
	}
	var lines []line
	for i, l := range strings.Split(text, "\n") {
		n := i + 1
		indent := 0
		for indent < len(l) && (l[indent] == ' ' || l[indent] == '\t') {
			if l[indent] == '\t' {
				return nil, fmt.Errorf("line %d: a tab in the indentation", n)
			}
			indent++
		}
		content, err := stripComment(l[indent:])
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		content = strings.TrimRight(content, " \t")
		if content == "" {
			continue
		}
		if indent == 0 && (content == "---" || content == "..." || strings.HasPrefix(content, "--- ") || strings.HasPrefix(content, "%")) {
			return nil, fmt.Errorf("line %d: document markers and directives are not read", n)
		}
		lines = append(lines, line{n, indent, content})
	}
	if len(lines) == 0 {
		return nil, nil
	}
	if lines[0].indent != 0 {
		return nil, fmt.Errorf("line %d: the document does not start at column 0", lines[0].n)
	}
	p := &parser{lines: lines}
	v, err := p.block(0, 1)
	if err != nil {
		return nil, err
	}
	if p.i < len(p.lines) {
		return nil, fmt.Errorf("line %d: inconsistent indentation", p.lines[p.i].n)
	}
	if _, ok := v.(Map); !ok {
		return nil, errors.New("the document is not a mapping")
	}
	return v, nil
}

type parser struct {
	lines []line
	i     int
}

// block reads the mapping or sequence whose lines start at indent.
func (p *parser) block(indent, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, fmt.Errorf("line %d: more than %d levels", p.lines[p.i].n, MaxDepth)
	}
	if isSeqItem(p.lines[p.i].text) {
		return p.sequence(indent)
	}
	return p.mapping(indent, depth)
}

func isSeqItem(s string) bool { return s == "-" || strings.HasPrefix(s, "- ") }

func (p *parser) mapping(indent, depth int) (Map, error) {
	m := Map{}
	for p.i < len(p.lines) {
		l := p.lines[p.i]
		if l.indent < indent {
			break
		}
		if l.indent > indent || isSeqItem(l.text) {
			return nil, fmt.Errorf("line %d: inconsistent indentation", l.n)
		}
		key, rest, err := splitKey(l.text)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", l.n, err)
		}
		if _, dup := m[key]; dup {
			return nil, fmt.Errorf("line %d: a duplicate key", l.n)
		}
		p.i++
		if rest != "" {
			v, err := inlineValue(rest)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", l.n, err)
			}
			if p.i < len(p.lines) && p.lines[p.i].indent > indent {
				return nil, fmt.Errorf("line %d: a value on several lines", p.lines[p.i].n)
			}
			m[key] = v
			continue
		}
		switch {
		case p.i < len(p.lines) && p.lines[p.i].indent > indent:
			v, err := p.block(p.lines[p.i].indent, depth+1)
			if err != nil {
				return nil, err
			}
			m[key] = v
		case p.i < len(p.lines) && p.lines[p.i].indent == indent && isSeqItem(p.lines[p.i].text):
			// key:\n- item, the sequence at the key's own indentation.
			v, err := p.sequence(indent)
			if err != nil {
				return nil, err
			}
			m[key] = v
		default:
			m[key] = Scalar{}
		}
	}
	return m, nil
}

// sequence reads "- item" lines of scalars or flow sequences.
func (p *parser) sequence(indent int) (Seq, error) {
	s := Seq{}
	for p.i < len(p.lines) {
		l := p.lines[p.i]
		if l.indent < indent || l.indent == indent && !isSeqItem(l.text) {
			break
		}
		if l.indent > indent {
			return nil, fmt.Errorf("line %d: inconsistent indentation", l.n)
		}
		item := strings.TrimSpace(strings.TrimPrefix(l.text, "-"))
		if item == "" || isSeqItem(item) {
			return nil, fmt.Errorf("line %d: only scalars are read in a block sequence", l.n)
		}
		if _, _, err := splitKey(item); err == nil {
			return nil, fmt.Errorf("line %d: only scalars are read in a block sequence", l.n)
		}
		v, err := inlineValue(item)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", l.n, err)
		}
		p.i++
		if p.i < len(p.lines) && p.lines[p.i].indent > indent {
			return nil, fmt.Errorf("line %d: a value on several lines", p.lines[p.i].n)
		}
		s = append(s, v)
	}
	return s, nil
}

// refusedStart are the first characters of what the subset never reads.
const refusedStart = "&*!|>{}%@`?"

// splitKey splits "key: rest" (or "key:") into the key and the rest.
func splitKey(s string) (key, rest string, err error) {
	var k string
	switch s[0] {
	case '"', '\'':
		v, n, err := quoted(s)
		if err != nil {
			return "", "", err
		}
		k, s = v, s[n:]
		if !strings.HasPrefix(s, ":") {
			return "", "", errors.New("no ': ' after a quoted key")
		}
		s = s[1:]
	default:
		i := colon(s)
		if i < 0 {
			return "", "", errors.New("not a 'key: value' line")
		}
		k, s = s[:i], s[i+1:]
		if strings.ContainsAny(k[:1], refusedStart+"[],#") {
			return "", "", errors.New("a key that is not a plain scalar")
		}
	}
	if s != "" && s[0] != ' ' {
		return "", "", errors.New("no space after ':'")
	}
	if k == "<<" {
		return "", "", errors.New("merge keys are not read")
	}
	return k, strings.TrimSpace(s), nil
}

// colon is the index of the first ':' followed by a space or the end.
func colon(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' && (i == len(s)-1 || s[i+1] == ' ') {
			return i
		}
	}
	return -1
}

// inlineValue reads a scalar or a flow sequence of scalars.
func inlineValue(s string) (any, error) {
	if s[0] == '[' {
		return flowSeq(s)
	}
	return scalar(s, false)
}

// scalar reads one scalar that must take all of s. In a flow sequence, the
// flow indicators are refused in plain scalars.
func scalar(s string, inFlow bool) (Scalar, error) {
	if s == "" {
		return Scalar{}, nil
	}
	if s[0] == '"' || s[0] == '\'' {
		v, n, err := quoted(s)
		if err != nil {
			return Scalar{}, err
		}
		if strings.TrimSpace(s[n:]) != "" {
			return Scalar{}, errors.New("text after a quoted scalar")
		}
		return Scalar{Text: v, Quoted: true}, nil
	}
	if strings.ContainsAny(s[:1], refusedStart+"[]#") || s[0] == '-' && len(s) > 1 && s[1] == ' ' {
		return Scalar{}, fmt.Errorf("%q starts what the subset does not read", s[:1])
	}
	if colon(s) >= 0 {
		return Scalar{}, errors.New("a mapping inside a value")
	}
	if inFlow && strings.ContainsAny(s, "[]{},") {
		return Scalar{}, errors.New("flow indicators in a plain scalar")
	}
	return Scalar{Text: s}, nil
}

// flowSeq reads [a, "b", 'c'] on one line.
func flowSeq(s string) (Seq, error) {
	if !strings.HasSuffix(s, "]") {
		return nil, errors.New("a flow sequence that does not end on its line")
	}
	body := strings.TrimSpace(s[1 : len(s)-1])
	out := Seq{}
	if body == "" {
		return out, nil
	}
	for body != "" {
		var item string
		if body[0] == '"' || body[0] == '\'' {
			_, n, err := quoted(body)
			if err != nil {
				return nil, err
			}
			item, body = body[:n], strings.TrimSpace(body[n:])
		} else {
			i := strings.IndexByte(body, ',')
			if i < 0 {
				i = len(body)
			}
			item, body = strings.TrimSpace(body[:i]), body[i:]
		}
		v, err := scalar(item, true)
		if err != nil {
			return nil, err
		}
		if item == "" {
			return nil, errors.New("an empty item in a flow sequence")
		}
		out = append(out, v)
		switch {
		case body == "":
		case body[0] == ',':
			body = strings.TrimSpace(body[1:])
			if body == "" {
				return nil, errors.New("a trailing comma in a flow sequence")
			}
		default:
			return nil, errors.New("text after an item of a flow sequence")
		}
	}
	return out, nil
}

// quoted reads the quoted scalar at the start of s and returns its value and
// the bytes it took. Double quotes resolve \\ \" \/ \n \t \r \0 and \uXXXX;
// single quotes resolve ”.
func quoted(s string) (string, int, error) {
	q := s[0]
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case c == q && q == '\'' && i+1 < len(s) && s[i+1] == '\'':
			b.WriteByte('\'')
			i++
		case c == q:
			return b.String(), i + 1, nil
		case c == '\\' && q == '"':
			if i+1 >= len(s) {
				return "", 0, errors.New("an escape at the end of the line")
			}
			i++
			switch s[i] {
			case '\\', '"', '/':
				b.WriteByte(s[i])
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '0':
				b.WriteByte(0)
			case 'u':
				if i+4 >= len(s) {
					return "", 0, errors.New("a short \\u escape")
				}
				r, err := strconv.ParseUint(s[i+1:i+5], 16, 32)
				if err != nil {
					return "", 0, errors.New("a bad \\u escape")
				}
				b.WriteRune(rune(r))
				i += 4
			default:
				return "", 0, fmt.Errorf("the escape \\%c is not read", s[i])
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", 0, errors.New("a quoted scalar that does not end on its line")
}

// stripComment removes a comment: a # at the start or after a space or a
// tab, outside quotes.
func stripComment(s string) (string, error) {
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q == '"' && c == '\\':
			i++
		case q != 0 && c == q:
			if q == '\'' && i+1 < len(s) && s[i+1] == '\'' {
				i++
				continue
			}
			q = 0
		case q == 0 && (c == '"' || c == '\'') && (i == 0 || strings.ContainsRune(" [,:-", rune(s[i-1]))):
			q = c
		case q == 0 && c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return s[:i], nil
		}
	}
	return s, nil
}
