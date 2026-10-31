// Package schemaval validates JSON documents against RosmarOps's own JSON
// Schemas (decision 2026-10-25, sustituir gojsonschema, G2). It implements
// only the keywords those schemas use (§1.2) and the date-time format, and
// refuses any other keyword when a schema is compiled: a schema that starts
// using one fails in the tests, not in silence. References are local
// ("#/..."); nothing is ever loaded from outside the schema's bytes.
//
// Where the draft leaves room, it follows gojsonschema v1.2.0, the test
// oracle (G3): a number is an integer when its value is whole; enum, const
// and uniqueItems compare canonical JSON; minLength and maxLength count code
// points; a
// $ref replaces its siblings; date-time accepts the layouts gojsonschema
// accepts.
package schemaval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// annotations are keywords with no effect on validation (§1.2).
var annotations = map[string]bool{"$schema": true, "$id": true, "$comment": true, "title": true, "description": true, "default": true}

// node is one compiled (sub)schema.
type node struct {
	pass *bool // a boolean schema

	ref     string
	refNode *node

	types []string

	properties     map[string]*node
	required       []string
	additional     *node
	additionalBool *bool
	items          *node

	enum     []string // canonical JSON
	constVal *string

	pattern *regexp.Regexp
	format  string

	minLength, maxLength *int
	minimum, maximum     *big.Rat
	minItems, maxItems   *int
	uniqueItems          bool

	oneOf, allOf           []*node
	not, ifN, thenN, elseN *node
}

// Schema is a compiled schema.
type Schema struct {
	root *node
}

// compiler compiles one schema document.
type compiler struct {
	doc   any
	nodes map[string]*node // JSON pointer → node, for $ref
	refs  []*node
}

// Compile compiles a schema from its bytes. Any keyword outside §1.2, any
// format but date-time, or a reference that is not local is an error.
func Compile(raw []byte) (*Schema, error) {
	doc, err := decode(raw)
	if err != nil {
		return nil, fmt.Errorf("compile schema: %w", err)
	}
	c := &compiler{doc: doc, nodes: map[string]*node{}}
	root, err := c.compile(doc, "#")
	if err != nil {
		return nil, fmt.Errorf("compile schema: %w", err)
	}
	for _, n := range c.refs {
		target, ok := c.nodes[n.ref]
		if !ok {
			t, err := c.resolve(n.ref)
			if err != nil {
				return nil, fmt.Errorf("compile schema: %w", err)
			}
			target = t
		}
		n.refNode = target
	}
	return &Schema{root: root}, nil
}

// resolve compiles the subschema a local reference points to.
func (c *compiler) resolve(ref string) (*node, error) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, fmt.Errorf("reference %q is not local", ref)
	}
	var cur any = c.doc
	for _, tok := range strings.Split(ref[2:], "/") {
		tok = strings.ReplaceAll(strings.ReplaceAll(tok, "~1", "/"), "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("reference %q does not resolve", ref)
		}
		if cur, ok = m[tok]; !ok {
			return nil, fmt.Errorf("reference %q does not resolve", ref)
		}
	}
	n, err := c.compile(cur, ref)
	if err != nil {
		return nil, err
	}
	if n.ref != "" && n.refNode == nil {
		t, err := c.resolve(n.ref)
		if err != nil {
			return nil, err
		}
		n.refNode = t
	}
	return n, nil
}

func (c *compiler) compile(v any, at string) (*node, error) {
	if n, ok := c.nodes[at]; ok {
		return n, nil
	}
	n := &node{}
	c.nodes[at] = n
	if b, ok := v.(bool); ok {
		n.pass = &b
		return n, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: a schema must be an object or a boolean", at)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sub := func(k string, v any) (*node, error) { return c.compile(v, at+"/"+escape(k)) }
	for _, k := range keys {
		val := m[k]
		var err error
		switch k {
		case "$ref":
			s, ok := val.(string)
			if !ok || !strings.HasPrefix(s, "#") {
				return nil, fmt.Errorf("%s: $ref %v is not a local reference", at, val)
			}
			n.ref = s
			c.refs = append(c.refs, n)
		case "definitions", "$defs":
			defs, ok := val.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s: %s is not an object", at, k)
			}
			for name, d := range defs {
				if _, err := c.compile(d, at+"/"+k+"/"+escape(name)); err != nil {
					return nil, err
				}
			}
		case "type":
			switch t := val.(type) {
			case string:
				n.types = []string{t}
			case []any:
				for _, x := range t {
					s, ok := x.(string)
					if !ok {
						return nil, fmt.Errorf("%s: type %v", at, val)
					}
					n.types = append(n.types, s)
				}
			default:
				return nil, fmt.Errorf("%s: type %v", at, val)
			}
			for _, t := range n.types {
				switch t {
				case "string", "object", "integer", "number", "array", "boolean", "null":
				default:
					return nil, fmt.Errorf("%s: unknown type %q", at, t)
				}
			}
		case "properties":
			props, ok := val.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s: properties is not an object", at)
			}
			n.properties = map[string]*node{}
			for name, p := range props {
				if n.properties[name], err = c.compile(p, at+"/properties/"+escape(name)); err != nil {
					return nil, err
				}
			}
		case "required":
			list, ok := val.([]any)
			if !ok {
				return nil, fmt.Errorf("%s: required is not an array", at)
			}
			for _, x := range list {
				s, ok := x.(string)
				if !ok {
					return nil, fmt.Errorf("%s: required %v", at, val)
				}
				n.required = append(n.required, s)
			}
		case "additionalProperties":
			if b, ok := val.(bool); ok {
				n.additionalBool = &b
			} else if n.additional, err = sub(k, val); err != nil {
				return nil, err
			}
		case "items":
			if _, ok := val.([]any); ok {
				return nil, fmt.Errorf("%s: items as an array is not supported", at)
			}
			if n.items, err = sub(k, val); err != nil {
				return nil, err
			}
		case "enum":
			list, ok := val.([]any)
			if !ok {
				return nil, fmt.Errorf("%s: enum is not an array", at)
			}
			for _, x := range list {
				n.enum = append(n.enum, canonical(x))
			}
		case "const":
			s := canonical(val)
			n.constVal = &s
		case "pattern":
			s, ok := val.(string)
			if !ok {
				return nil, fmt.Errorf("%s: pattern is not a string", at)
			}
			if n.pattern, err = regexp.Compile(s); err != nil {
				return nil, fmt.Errorf("%s: pattern: %w", at, err)
			}
		case "format":
			if val != "date-time" {
				return nil, fmt.Errorf("%s: format %v is not supported", at, val)
			}
			n.format = "date-time"
		case "minLength", "maxLength", "minItems", "maxItems":
			i, err := nonNegative(val)
			if err != nil {
				return nil, fmt.Errorf("%s: %s: %w", at, k, err)
			}
			switch k {
			case "minLength":
				n.minLength = &i
			case "maxLength":
				n.maxLength = &i
			case "minItems":
				n.minItems = &i
			default:
				n.maxItems = &i
			}
		case "minimum", "maximum":
			num, ok := val.(json.Number)
			if !ok {
				return nil, fmt.Errorf("%s: %s is not a number", at, k)
			}
			r, ok := new(big.Rat).SetString(string(num))
			if !ok {
				return nil, fmt.Errorf("%s: %s is not a number", at, k)
			}
			if k == "minimum" {
				n.minimum = r
			} else {
				n.maximum = r
			}
		case "uniqueItems":
			b, ok := val.(bool)
			if !ok {
				return nil, fmt.Errorf("%s: uniqueItems is not a boolean", at)
			}
			n.uniqueItems = b
		case "oneOf", "allOf":
			list, ok := val.([]any)
			if !ok || len(list) == 0 {
				return nil, fmt.Errorf("%s: %s is not a non-empty array", at, k)
			}
			for i, x := range list {
				s, err := c.compile(x, at+"/"+k+"/"+strconv.Itoa(i))
				if err != nil {
					return nil, err
				}
				if k == "oneOf" {
					n.oneOf = append(n.oneOf, s)
				} else {
					n.allOf = append(n.allOf, s)
				}
			}
		case "not":
			if n.not, err = sub(k, val); err != nil {
				return nil, err
			}
		case "if":
			if n.ifN, err = sub(k, val); err != nil {
				return nil, err
			}
		case "then":
			if n.thenN, err = sub(k, val); err != nil {
				return nil, err
			}
		case "else":
			if n.elseN, err = sub(k, val); err != nil {
				return nil, err
			}
		default:
			if !annotations[k] {
				return nil, fmt.Errorf("%s: keyword %q is not supported", at, k)
			}
		}
	}
	return n, nil
}

func nonNegative(v any) (int, error) {
	num, ok := v.(json.Number)
	if !ok {
		return 0, fmt.Errorf("not a number")
	}
	i, err := strconv.Atoi(string(num))
	if err != nil || i < 0 {
		return 0, fmt.Errorf("not a non-negative integer")
	}
	return i, nil
}

func escape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// decode decodes JSON keeping numbers as json.Number; trailing data is an
// error.
func decode(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err == nil {
		return nil, fmt.Errorf("trailing data after the JSON value")
	}
	return v, nil
}

// canonical is a value's JSON with numbers normalised (decoded as float64)
// and object keys sorted, as gojsonschema compares enum, const and
// uniqueItems. A value it cannot normalise (a number beyond float64) is
// "", which matches no enum or const and makes uniqueItems fail: fail-secure
// where gojsonschema has no defined outcome.
func canonical(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	var plain any
	if json.Unmarshal(b, &plain) != nil {
		return ""
	}
	out, err := json.Marshal(plain)
	if err != nil {
		return ""
	}
	return string(out)
}

// Error is one validation error: the field (dotted path, "(root)" for the
// document) and what is wrong.
type Error struct {
	Field       string
	Description string
}

func (e Error) String() string { return e.Field + ": " + e.Description }

// Result is the outcome of a validation.
type Result struct {
	errs []Error
}

// Valid reports a document with no error.
func (r Result) Valid() bool { return len(r.errs) == 0 }

// Errors are the validation errors, in document order.
func (r Result) Errors() []Error { return r.errs }

// Validate validates a JSON document. A document that is not JSON is an
// error; a JSON document that breaks the schema is a Result with errors.
func (s *Schema) Validate(raw []byte) (Result, error) {
	doc, err := decode(raw)
	if err != nil {
		return Result{}, fmt.Errorf("decode document: %w", err)
	}
	var r Result
	s.root.validate(doc, "(root)", &r)
	return r, nil
}

func (r *Result) add(field, format string, a ...any) {
	r.errs = append(r.errs, Error{Field: field, Description: fmt.Sprintf(format, a...)})
}

func child(field, name string) string {
	if field == "(root)" {
		return name
	}
	return field + "." + name
}

// typeOf is the JSON type of a decoded value; a whole number is "integer".
func typeOf(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case json.Number:
		if r, ok := new(big.Rat).SetString(string(x)); ok && r.IsInt() {
			return "integer"
		}
		return "number"
	}
	return "unknown"
}

func (n *node) typeOK(v any) bool {
	if len(n.types) == 0 {
		return true
	}
	t := typeOf(v)
	for _, want := range n.types {
		if want == t || want == "number" && t == "integer" {
			return true
		}
	}
	return false
}

// valid reports whether v validates against n, without collecting errors.
func (n *node) valid(v any) bool {
	var r Result
	n.validate(v, "(root)", &r)
	return r.Valid()
}

func (n *node) validate(v any, field string, r *Result) {
	if n.pass != nil {
		if !*n.pass {
			r.add(field, "False always fails validation")
		}
		return
	}
	if n.refNode != nil {
		n.refNode.validate(v, field, r)
		return
	}
	if !n.typeOK(v) {
		r.add(field, "Invalid type. Expected: %s, given: %s", strings.Join(n.types, "/"), typeOf(v))
		return
	}

	// Combinators.
	if len(n.oneOf) > 0 {
		count := 0
		for _, s := range n.oneOf {
			if s.valid(v) {
				count++
			}
		}
		if count != 1 {
			r.add(field, "Must validate one and only one schema (oneOf)")
		}
	}
	for _, s := range n.allOf {
		s.validate(v, field, r)
	}
	if n.not != nil && n.not.valid(v) {
		r.add(field, "Must not validate the schema (not)")
	}
	if n.ifN != nil {
		if n.ifN.valid(v) {
			if n.thenN != nil {
				n.thenN.validate(v, field, r)
			}
		} else if n.elseN != nil {
			n.elseN.validate(v, field, r)
		}
	}

	// Common.
	if n.constVal != nil && canonical(v) != *n.constVal {
		r.add(field, "%s does not match: %s", field, *n.constVal)
	}
	if len(n.enum) > 0 {
		c, found := canonical(v), false
		for _, e := range n.enum {
			found = found || e == c
		}
		if !found {
			r.add(field, "%s must be one of the following: %s", field, strings.Join(n.enum, ", "))
		}
	}

	switch x := v.(type) {
	case string:
		if n.maxLength != nil && utf8.RuneCountInString(x) > *n.maxLength {
			r.add(field, "String length must be less than or equal to %d", *n.maxLength)
		}
		if n.minLength != nil && utf8.RuneCountInString(x) < *n.minLength {
			r.add(field, "String length must be greater than or equal to %d", *n.minLength)
		}
		if n.pattern != nil && !n.pattern.MatchString(x) {
			r.add(field, "Does not match pattern '%s'", n.pattern.String())
		}
		if n.format != "" && !isDateTime(x) {
			r.add(field, "Does not match format 'date-time'")
		}
	case json.Number:
		num, _ := new(big.Rat).SetString(string(x))
		if n.minimum != nil && num != nil && num.Cmp(n.minimum) < 0 {
			r.add(field, "Must be greater than or equal to %s", n.minimum.RatString())
		}
		if n.maximum != nil && num != nil && num.Cmp(n.maximum) > 0 {
			r.add(field, "Must be less than or equal to %s", n.maximum.RatString())
		}
		if n.format != "" {
			r.add(field, "Does not match format 'date-time'") // as gojsonschema: a number is no date-time
		}
	case []any:
		if n.minItems != nil && len(x) < *n.minItems {
			r.add(field, "Array must have at least %d items", *n.minItems)
		}
		if n.maxItems != nil && len(x) > *n.maxItems {
			r.add(field, "Array must have at most %d items", *n.maxItems)
		}
		if n.uniqueItems {
			seen := map[string]bool{}
			for _, e := range x {
				c := canonical(e)
				if seen[c] || c == "" {
					r.add(field, "Array items must be unique")
					break
				}
				seen[c] = true
			}
		}
		if n.items != nil {
			for i, e := range x {
				n.items.validate(e, child(field, strconv.Itoa(i)), r)
			}
		}
	case map[string]any:
		for _, k := range n.required {
			if _, ok := x[k]; !ok {
				r.add(field, "%s is required", k)
			}
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if p, ok := n.properties[k]; ok {
				p.validate(x[k], child(field, k), r)
				continue
			}
			switch {
			case n.additionalBool != nil && !*n.additionalBool:
				r.add(field, "Additional property %s is not allowed", k)
			case n.additional != nil:
				n.additional.validate(x[k], child(field, k), r)
			}
		}
	}
}

// dateTimeLayouts are the layouts gojsonschema's date-time checker accepts
// (gojsonschema v1.2.0, format_checkers.go).
var dateTimeLayouts = []string{"15:04:05", "15:04:05Z07:00", "2006-01-02", time.RFC3339, time.RFC3339Nano}

func isDateTime(s string) bool {
	for _, l := range dateTimeLayouts {
		if _, err := time.Parse(l, s); err == nil {
			return true
		}
	}
	return false
}
