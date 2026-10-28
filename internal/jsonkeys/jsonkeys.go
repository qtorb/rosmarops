// Package jsonkeys decodes a configuration file's JSON with exact keys
// (decision 2026-10-27, blindaje, B5). encoding/json matches object keys to
// struct fields without regard to case, and the last one wins; the agents
// (Node's JSON.parse) take "command" and "Command" as two keys. A file could
// then show RosmarOps one command while the agent runs another. Decode reads
// each level as a map, keeps only the keys whose name is exactly a field's,
// and reports the keys that match a field only without regard to case.
package jsonkeys

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Decode decodes raw into v (a pointer) like json.Unmarshal, except that an
// object key fills a struct field only if it is the field's JSON name
// exactly. Conflicts are the JSON Pointers (RFC 6901) of the keys that
// differ from a field's name only in case: they are dropped, as the agent
// drops them, and returned sorted. An error is json.Unmarshal's.
func Decode(raw []byte, v any) ([]string, error) {
	if !json.Valid(raw) {
		return nil, json.Unmarshal(raw, v) // the same syntax error as before
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, err
	}
	var conflicts []string
	tree = clean(tree, reflect.TypeOf(v), "", &conflicts)
	exact, err := json.Marshal(tree)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(exact, v); err != nil {
		return nil, err
	}
	sort.Strings(conflicts)
	return conflicts, nil
}

var unmarshaler = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()

// clean drops, from node, the keys that would fill a field of t only by a
// case-insensitive match, recording their pointers.
func clean(node any, t reflect.Type, pointer string, conflicts *[]string) any {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || reflect.PointerTo(t).Implements(unmarshaler) {
		return node
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := node.(map[string]any)
		if !ok {
			return node
		}
		fields := fieldsOf(t)
		for k, child := range obj {
			if ft, ok := fields[k]; ok {
				obj[k] = clean(child, ft, pointer+"/"+escape(k), conflicts)
				continue
			}
			for name := range fields {
				if strings.EqualFold(name, k) {
					*conflicts = append(*conflicts, pointer+"/"+escape(k))
					delete(obj, k)
					break
				}
			}
		}
	case reflect.Map:
		if obj, ok := node.(map[string]any); ok {
			for k, child := range obj {
				obj[k] = clean(child, t.Elem(), pointer+"/"+escape(k), conflicts)
			}
		}
	case reflect.Slice, reflect.Array:
		if arr, ok := node.([]any); ok {
			for i, child := range arr {
				arr[i] = clean(child, t.Elem(), pointer+"/"+strconv.Itoa(i), conflicts)
			}
		}
	}
	return node
}

// fieldsOf maps the JSON names of t's fields (embedded structs promoted) to
// their types, as encoding/json names them.
func fieldsOf(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				for k, v := range fieldsOf(ft) {
					if _, ok := out[k]; !ok {
						out[k] = v
					}
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = f.Type
	}
	return out
}

var pointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

func escape(s string) string { return pointerEscaper.Replace(s) }
