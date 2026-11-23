// Package plistsubset reads the small part of a property list that a
// managed-preferences file uses (BV-277): a top-level dictionary whose
// values RosmarOps reads are booleans, integers and strings, in either the
// XML or the binary (bplist00) form, with no dependency, in the line of
// internal/yamlsubset. Any other value (an array, a nested dictionary, data,
// a date, a real) is skipped, not guessed: its key is absent from the
// result. A document that is not a property list, is malformed, or whose
// top level is not a dictionary is an error, never a partial value.
package plistsubset

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"
)

// MaxBytes is the content-scan size limit (invariant 6).
const MaxBytes = 2 * 1024 * 1024

// Value is one top-level value read: exactly one of Bool, Int or String is
// meaningful, as Kind says.
type Value struct {
	Kind   Kind
	Bool   bool
	Int    int64
	String string
}

// Kind is the type of a Value.
type Kind int

const (
	KindBool Kind = iota + 1
	KindInt
	KindString
)

// Parse reads raw and returns the top-level dictionary's boolean, integer
// and string values by key.
func Parse(raw []byte) (map[string]Value, error) {
	if len(raw) > MaxBytes {
		return nil, fmt.Errorf("more than %d bytes", MaxBytes)
	}
	if bytes.HasPrefix(raw, []byte("bplist00")) {
		return parseBinary(raw)
	}
	return parseXML(raw)
}

// --- XML ---

func parseXML(raw []byte) (map[string]Value, error) {
	d := xml.NewDecoder(bytes.NewReader(raw))
	d.Strict = true
	// The DOCTYPE names Apple's DTD by URL; nothing is ever fetched.
	seenPlist := false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil, errors.New("no dictionary")
		}
		if err != nil {
			return nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch {
		case !seenPlist && se.Name.Local == "plist":
			seenPlist = true
		case seenPlist && se.Name.Local == "dict":
			return xmlDict(d)
		default:
			return nil, fmt.Errorf("unexpected element <%s>", se.Name.Local)
		}
	}
}

// xmlDict reads the top-level dictionary's entries up to its end.
func xmlDict(d *xml.Decoder) (map[string]Value, error) {
	out := map[string]Value{}
	key, haveKey := "", false
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local != "dict" || haveKey {
				return nil, errors.New("a key with no value")
			}
			return out, nil
		case xml.StartElement:
			if t.Name.Local == "key" {
				if haveKey {
					return nil, errors.New("two keys in a row")
				}
				k, err := xmlText(d)
				if err != nil {
					return nil, err
				}
				if _, dup := out[k]; dup {
					return nil, fmt.Errorf("a duplicate key %q", k)
				}
				key, haveKey = k, true
				continue
			}
			if !haveKey {
				return nil, fmt.Errorf("a value <%s> with no key", t.Name.Local)
			}
			v, ok, err := xmlValue(d, t)
			if err != nil {
				return nil, err
			}
			if ok {
				out[key] = v
			}
			haveKey = false
		}
	}
}

// xmlValue reads one value. ok is false for a type the subset skips.
func xmlValue(d *xml.Decoder, se xml.StartElement) (Value, bool, error) {
	switch se.Name.Local {
	case "true", "false":
		if err := d.Skip(); err != nil {
			return Value{}, false, err
		}
		return Value{Kind: KindBool, Bool: se.Name.Local == "true"}, true, nil
	case "integer":
		s, err := xmlText(d)
		if err != nil {
			return Value{}, false, err
		}
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return Value{}, false, fmt.Errorf("integer: %w", err)
		}
		return Value{Kind: KindInt, Int: n}, true, nil
	case "string":
		s, err := xmlText(d)
		if err != nil {
			return Value{}, false, err
		}
		return Value{Kind: KindString, String: s}, true, nil
	case "array", "dict", "data", "date", "real":
		return Value{}, false, d.Skip()
	}
	return Value{}, false, fmt.Errorf("unknown element <%s>", se.Name.Local)
}

// xmlText reads an element's character data up to its end; a child
// element is an error.
func xmlText(d *xml.Decoder) (string, error) {
	var b strings.Builder
	for {
		tok, err := d.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.EndElement:
			return b.String(), nil
		case xml.StartElement:
			return "", fmt.Errorf("unexpected element <%s>", t.Name.Local)
		}
	}
}

// --- binary (bplist00) ---

type bplist struct {
	raw                []byte
	offsets            []uint64
	refSize, offsetEnd int
}

func parseBinary(raw []byte) (map[string]Value, error) {
	if len(raw) < 8+32 {
		return nil, errors.New("binary plist too short")
	}
	tr := raw[len(raw)-32:]
	offSize, refSize := int(tr[6]), int(tr[7])
	numObjects := binary.BigEndian.Uint64(tr[8:16])
	top := binary.BigEndian.Uint64(tr[16:24])
	tableAt := binary.BigEndian.Uint64(tr[24:32])
	end := uint64(len(raw) - 32)
	if !validSize(offSize) || !validSize(refSize) || numObjects == 0 || top >= numObjects ||
		tableAt < 8 || tableAt > end || numObjects > (end-tableAt)/uint64(offSize) {
		return nil, errors.New("binary plist: bad trailer")
	}
	p := bplist{raw: raw, refSize: refSize, offsetEnd: int(tableAt)}
	p.offsets = make([]uint64, numObjects)
	for i := range p.offsets {
		at := int(tableAt) + i*offSize
		o := readUint(raw[at : at+offSize])
		if o < 8 || o >= tableAt {
			return nil, errors.New("binary plist: bad offset")
		}
		p.offsets[i] = o
	}
	at := int(p.offsets[top])
	if raw[at]>>4 != 0xD {
		return nil, errors.New("the top level is not a dictionary")
	}
	n, body, err := p.count(at)
	if err != nil {
		return nil, err
	}
	if uint64(body)+2*n*uint64(refSize) > uint64(p.offsetEnd) {
		return nil, errors.New("binary plist: dictionary out of range")
	}
	out := map[string]Value{}
	for i := uint64(0); i < n; i++ {
		kref := p.ref(body + int(i)*refSize)
		vref := p.ref(body + int(n+i)*refSize)
		if kref >= uint64(len(p.offsets)) || vref >= uint64(len(p.offsets)) {
			return nil, errors.New("binary plist: bad reference")
		}
		k, ok, err := p.scalar(int(p.offsets[kref]))
		if err != nil {
			return nil, err
		}
		if !ok || k.Kind != KindString {
			return nil, errors.New("binary plist: a key that is not a string")
		}
		if _, dup := out[k.String]; dup {
			return nil, fmt.Errorf("a duplicate key %q", k.String)
		}
		v, ok, err := p.scalar(int(p.offsets[vref]))
		if err != nil {
			return nil, err
		}
		if ok {
			out[k.String] = v
		}
	}
	return out, nil
}

func validSize(n int) bool { return n == 1 || n == 2 || n == 4 || n == 8 }

func readUint(b []byte) uint64 {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

func (p bplist) ref(at int) uint64 { return readUint(p.raw[at : at+p.refSize]) }

// count is an object's length (the low nibble, or the integer that follows
// when it is 0xF) and where its body starts.
func (p bplist) count(at int) (uint64, int, error) {
	n := uint64(p.raw[at] & 0x0F)
	at++
	if n != 0x0F {
		return n, at, nil
	}
	if at >= p.offsetEnd || p.raw[at]>>4 != 0x1 {
		return 0, 0, errors.New("binary plist: bad length")
	}
	size := 1 << (p.raw[at] & 0x0F)
	if size > 8 || at+1+size > p.offsetEnd {
		return 0, 0, errors.New("binary plist: bad length")
	}
	return readUint(p.raw[at+1 : at+1+size]), at + 1 + size, nil
}

// scalar reads a boolean, integer or string object; ok is false for any
// other type, which the subset skips.
func (p bplist) scalar(at int) (Value, bool, error) {
	marker := p.raw[at]
	switch marker >> 4 {
	case 0x0:
		switch marker {
		case 0x08:
			return Value{Kind: KindBool}, true, nil
		case 0x09:
			return Value{Kind: KindBool, Bool: true}, true, nil
		}
		return Value{}, false, nil
	case 0x1:
		size := 1 << (marker & 0x0F)
		if size > 8 || at+1+size > p.offsetEnd {
			return Value{}, false, errors.New("binary plist: bad integer")
		}
		v := readUint(p.raw[at+1 : at+1+size])
		return Value{Kind: KindInt, Int: int64(v)}, true, nil
	case 0x5, 0x6:
		n, body, err := p.count(at)
		if err != nil {
			return Value{}, false, err
		}
		width := uint64(1)
		if marker>>4 == 0x6 {
			width = 2
		}
		if n > uint64(p.offsetEnd) || uint64(body)+n*width > uint64(p.offsetEnd) {
			return Value{}, false, errors.New("binary plist: string out of range")
		}
		b := p.raw[body : body+int(n*width)]
		if width == 1 {
			for _, c := range b {
				if c > 0x7F {
					return Value{}, false, errors.New("binary plist: an ASCII string that is not ASCII")
				}
			}
			return Value{Kind: KindString, String: string(b)}, true, nil
		}
		u := make([]uint16, n)
		for i := range u {
			u[i] = binary.BigEndian.Uint16(b[2*i:])
		}
		return Value{Kind: KindString, String: string(utf16.Decode(u))}, true, nil
	}
	return Value{}, false, nil
}
