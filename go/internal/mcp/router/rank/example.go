// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import "github.com/eshu-hq/eshu/go/internal/mcp/router"

// example builds sample arguments from the tool's input schema alone, with no
// generated text: every required property, every property that has a schema
// default, and every anchor the query supplied that the tool accepts. It
// returns nil when the schema is not an object map, and an empty object for a
// tool that takes no arguments.
//
// An anchor property carries the value the caller supplied. Other required
// properties use the schema default, then the first enum value, then a
// placeholder by type: "<name>" for strings, 1 for numbers, false for booleans,
// and an empty array or object. Values from the schema are deep-cloned so a
// caller mutating the example cannot change the registry definition.
func example(schema map[string]any, anchors router.Anchors) map[string]any {
	if schema == nil {
		return nil
	}
	props := propertySchemas(schema)
	out := map[string]any{}
	for _, a := range anchorPairs(anchors) {
		if _, ok := props[a.prop]; ok {
			out[a.prop] = a.value
		}
	}
	for _, name := range requiredNames(schema) {
		if _, set := out[name]; set {
			continue
		}
		out[name] = placeholder(name, props[name])
	}
	for name, prop := range props {
		if _, set := out[name]; set {
			continue
		}
		if def, has := prop["default"]; has {
			out[name] = cloneValue(def)
		}
	}
	return out
}

// placeholder returns the sample value for one required property: its schema
// default, then the first enum value, then a placeholder by type.
func placeholder(name string, prop map[string]any) any {
	if def, ok := prop["default"]; ok {
		return cloneValue(def)
	}
	if first, ok := firstEnum(prop); ok {
		return cloneValue(first)
	}
	switch typeName(prop) {
	case "integer", "number":
		return 1
	case "boolean":
		return false
	case "array":
		return []any{}
	case "object":
		return map[string]any{}
	}
	return "<" + name + ">"
}

// cloneValue deep-copies the container types a schema default can hold, so
// mutating an example never reaches the registry definition. Scalars are
// immutable and returned as is.
func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = cloneValue(e)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = cloneValue(e)
		}
		return out
	case []string:
		return append([]string(nil), t...)
	case []int:
		return append([]int(nil), t...)
	case []float64:
		return append([]float64(nil), t...)
	case []bool:
		return append([]bool(nil), t...)
	case map[string]string:
		out := make(map[string]string, len(t))
		for k, e := range t {
			out[k] = e
		}
		return out
	}
	return v
}
