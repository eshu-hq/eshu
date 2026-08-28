// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package routecontract

// Arguments holds one MCP tool call's decoded arguments.
type Arguments map[string]any

// String returns the named string argument or an empty string when it is
// absent or has another type.
func (a Arguments) String(key string) string {
	value, _ := a[key].(string)
	return value
}

// IntOr returns the named integer-compatible argument or fallback.
func (a Arguments) IntOr(key string, fallback int) int {
	switch value := a[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return fallback
	}
}

// BoolOr returns the named boolean argument or fallback.
func (a Arguments) BoolOr(key string, fallback bool) bool {
	value, ok := a[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

// OptionalFloat returns the named numeric argument and whether its type is
// supported by MCP dispatch.
func (a Arguments) OptionalFloat(key string) (float64, bool) {
	switch value := a[key].(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	default:
		return 0, false
	}
}

// StringSlice returns the named string-array argument in the []any shape used
// by MCP request bodies.
func (a Arguments) StringSlice(key string) []any {
	raw, ok := a[key]
	if !ok {
		return nil
	}
	values, ok := raw.([]any)
	if ok {
		return values
	}
	stringValues, ok := raw.([]string)
	if !ok {
		return nil
	}
	result := make([]any, 0, len(stringValues))
	for _, value := range stringValues {
		result = append(result, value)
	}
	return result
}

// Request describes an internal HTTP request selected for an MCP tool call.
type Request struct {
	Method string
	Path   string
	Body   any
	Query  map[string]string
}
