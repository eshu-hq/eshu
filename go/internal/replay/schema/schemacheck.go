// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schema

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// checkAgainstSchema validates a decoded JSON document against the generated
// cassette-format schema and returns field-level error messages (empty when the
// document conforms). It is a focused interpreter over exactly the JSON Schema
// vocabulary CassetteFormatV1 emits — type, required, properties,
// additionalProperties (bool or sub-schema), const, enum, minLength, minimum,
// minItems, items, and $ref into $defs. It is deliberately not a general schema
// engine: validating against the schema we actually publish is what keeps the
// author-time gate and the published contract from drifting, and a bounded
// interpreter over our own vocabulary cannot silently ignore a keyword we use.
//
// doc must be decoded with json.Number (use decodeWithNumbers) so integer
// bounds are checked without a float round-trip.
func checkAgainstSchema(doc any) []string {
	root := cassetteFormatSchema()
	defs, _ := root["$defs"].(map[string]any)
	v := &schemaChecker{defs: defs}
	v.check("", root, doc)
	sort.Strings(v.errs)
	return v.errs
}

// decodeWithNumbers decodes JSON preserving integer literals as json.Number so
// minimum bounds and integer-type checks are exact.
func decodeWithNumbers(data []byte) (any, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}
	return out, nil
}

type schemaChecker struct {
	defs map[string]any
	errs []string
}

func (v *schemaChecker) fail(path, format string, args ...any) {
	at := path
	if at == "" {
		at = "(root)"
	}
	v.errs = append(v.errs, at+": "+fmt.Sprintf(format, args...))
}

// check validates value against node, resolving a $ref first.
func (v *schemaChecker) check(path string, node map[string]any, value any) {
	if ref, ok := node["$ref"].(string); ok {
		node = v.resolve(ref)
		if node == nil {
			v.fail(path, "internal: unresolved $ref %q", ref)
			return
		}
	}

	if c, ok := node["const"]; ok && !jsonEqual(value, c) {
		v.fail(path, "must equal %v", c)
	}
	if enum, ok := node["enum"].([]string); ok && !enumContains(enum, value) {
		v.fail(path, "must be one of %v", enum)
	}

	switch node["type"] {
	case "object":
		v.checkObject(path, node, value)
	case "array":
		v.checkArray(path, node, value)
	case "string":
		v.checkString(path, node, value)
	case "integer":
		v.checkInteger(path, node, value)
	case "boolean":
		if _, ok := value.(bool); !ok && value != nil {
			v.fail(path, "must be a boolean")
		}
	}
}

func (v *schemaChecker) checkObject(path string, node map[string]any, value any) {
	obj, ok := value.(map[string]any)
	if !ok {
		v.fail(path, "must be an object")
		return
	}
	props, _ := node["properties"].(map[string]any)
	for _, req := range stringSlice(node["required"]) {
		if _, present := obj[req]; !present {
			v.fail(join(path, req), "is required")
		}
	}
	addl, hasAddl := node["additionalProperties"]
	for key, child := range obj {
		propSchema, declared := props[key]
		switch {
		case declared:
			if cs, ok := propSchema.(map[string]any); ok {
				v.check(join(path, key), cs, child)
			}
		case hasAddl:
			switch a := addl.(type) {
			case bool:
				if !a {
					// Report at the parent object's path naming the offending
					// key, so a typo reads "scopes[0]: unknown field \"scope_knd\"".
					v.fail(path, "unknown field %q", key)
				}
			case map[string]any:
				v.check(join(path, key), a, child)
			}
		}
	}
}

func (v *schemaChecker) checkArray(path string, node map[string]any, value any) {
	arr, ok := value.([]any)
	if !ok {
		v.fail(path, "must be an array")
		return
	}
	if min, ok := intField(node, "minItems"); ok && int64(len(arr)) < min {
		v.fail(path, "must have at least %d item(s)", min)
	}
	if items, ok := node["items"].(map[string]any); ok {
		for i, elem := range arr {
			v.check(fmt.Sprintf("%s[%d]", path, i), items, elem)
		}
	}
}

func (v *schemaChecker) checkString(path string, node map[string]any, value any) {
	s, ok := value.(string)
	if !ok {
		v.fail(path, "must be a string")
		return
	}
	if min, ok := intField(node, "minLength"); ok && int64(len(s)) < min {
		v.fail(path, "must be at least %d character(s)", min)
	}
}

func (v *schemaChecker) checkInteger(path string, node map[string]any, value any) {
	num, ok := value.(json.Number)
	if !ok {
		v.fail(path, "must be an integer")
		return
	}
	n, err := num.Int64()
	if err != nil {
		v.fail(path, "must be an integer")
		return
	}
	if min, ok := intField(node, "minimum"); ok && n < min {
		v.fail(path, "must be >= %d", min)
	}
}

// resolve dereferences a local "#/$defs/<name>" pointer.
func (v *schemaChecker) resolve(ref string) map[string]any {
	const prefix = "#/$defs/"
	if !strings.HasPrefix(ref, prefix) {
		return nil
	}
	def, _ := v.defs[strings.TrimPrefix(ref, prefix)].(map[string]any)
	return def
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func stringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, e := range s {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

// intField reads an integer schema keyword that the builder stores as a Go int.
func intField(node map[string]any, key string) (int64, bool) {
	switch n := node[key].(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	default:
		return 0, false
	}
}

func enumContains(enum []string, value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	for _, e := range enum {
		if e == s {
			return true
		}
	}
	return false
}

// jsonEqual compares a decoded JSON scalar against a Go schema literal (const).
func jsonEqual(value, want any) bool {
	switch w := want.(type) {
	case string:
		s, ok := value.(string)
		return ok && s == w
	default:
		return fmt.Sprintf("%v", value) == fmt.Sprintf("%v", want)
	}
}
