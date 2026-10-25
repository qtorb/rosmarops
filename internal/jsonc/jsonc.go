// Package jsonc reads JSON with comments (JSONC), as VS Code and Cursor
// write their settings.
package jsonc

import "bytes"

// Strip removes // and /* */ comments outside strings, and
// commas before a closing } or ] (JSONC).
func Strip(raw []byte) []byte {
	var out bytes.Buffer
	inString, escaped := false, false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case inString:
			out.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
		case c == '"':
			inString = true
			out.WriteByte(c)
		case c == '/' && i+1 < len(raw) && raw[i+1] == '/':
			for i < len(raw) && raw[i] != '\n' {
				i++
			}
			out.WriteByte('\n')
		case c == '/' && i+1 < len(raw) && raw[i+1] == '*':
			i += 2
			for i+1 < len(raw) && (raw[i] != '*' || raw[i+1] != '/') {
				i++
			}
			i++
		default:
			out.WriteByte(c)
		}
	}
	return trailingCommas(out.Bytes())
}

// trailingCommas removes a comma followed only by space before } or ],
// outside strings.
func trailingCommas(raw []byte) []byte {
	var out []byte
	inString, escaped := false, false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			out = append(out, c)
			continue
		}
		if c == '"' {
			inString = true
		}
		if c == ',' {
			j := i + 1
			for j < len(raw) && (raw[j] == ' ' || raw[j] == '\t' || raw[j] == '\n' || raw[j] == '\r') {
				j++
			}
			if j < len(raw) && (raw[j] == '}' || raw[j] == ']') {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}
