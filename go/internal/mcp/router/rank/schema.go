// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

// This file reads a tool input schema without assuming one Go spelling of the
// same JSON. The registry builds schemas by hand, and a survey of it found
// []string enums (88) beside []any enums (2), []string and []any "required"
// lists, and []any, bool, int, and string defaults. Every accessor here accepts
// each spelling it has seen, plus the obvious typed variants, and treats
// anything else as absent.

// propertyNames returns the names in a schema's "properties", whether the value
// is map[string]any or map[string]map[string]any.
func propertyNames(schema map[string]any) []string {
	switch p := schema["properties"].(type) {
	case map[string]any:
		out := make([]string, 0, len(p))
		for name := range p {
			out = append(out, name)
		}
		return out
	case map[string]map[string]any:
		out := make([]string, 0, len(p))
		for name := range p {
			out = append(out, name)
		}
		return out
	}
	return nil
}

// propertySchemas returns each property's schema keyed by name. A property whose
// schema is not an object map is present with a nil schema.
func propertySchemas(schema map[string]any) map[string]map[string]any {
	switch p := schema["properties"].(type) {
	case map[string]any:
		out := make(map[string]map[string]any, len(p))
		for name, raw := range p {
			out[name], _ = raw.(map[string]any)
		}
		return out
	case map[string]map[string]any:
		return p
	}
	return nil
}

// requiredNames reads a schema's "required" list, written as []string or []any.
func requiredNames(schema map[string]any) []string {
	switch r := schema["required"].(type) {
	case []string:
		return r
	case []any:
		out := make([]string, 0, len(r))
		for _, v := range r {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// firstEnum returns the first value of a property's "enum", written as []string
// or []any. It reports false for a missing, mistyped, or empty enum.
func firstEnum(prop map[string]any) (any, bool) {
	switch e := prop["enum"].(type) {
	case []string:
		if len(e) > 0 {
			return e[0], true
		}
	case []any:
		if len(e) > 0 {
			return e[0], true
		}
	}
	return nil, false
}

// typeName returns a property's JSON type. A type list such as
// ["string", "null"] yields its first non-null entry.
func typeName(prop map[string]any) string {
	switch t := prop["type"].(type) {
	case string:
		return t
	case []string:
		for _, s := range t {
			if s != "null" {
				return s
			}
		}
	case []any:
		for _, v := range t {
			if s, ok := v.(string); ok && s != "null" {
				return s
			}
		}
	}
	return ""
}
