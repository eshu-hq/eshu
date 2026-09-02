// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadcore

import (
	"fmt"
	"strconv"
	"strings"
)

// PayloadStr renders payload[key] as a trimmed string, returning "" for an
// absent key. A value that renders as the literal "<nil>" is also reported as
// "", which is what distinguishes it from PayloadString.
func PayloadStr(payload map[string]any, key string) string {
	val, ok := payload[key]
	if !ok {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(val))
	if s == "<nil>" {
		return ""
	}
	return s
}

// PayloadInt parses payload[key] (via PayloadStr) as a base-10 integer,
// returning 0 for an absent key, a blank value, or a value that fails to
// parse.
func PayloadInt(payload map[string]any, key string) int {
	value := PayloadStr(payload, key)
	if value == "" {
		return 0
	}
	parsed, _ := strconv.Atoi(value)
	return parsed
}

// PayloadString renders payload[key] as a trimmed string, returning "" for an
// absent key or a nil value. It renders non-string values via fmt.Sprint.
func PayloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

// SemanticPayloadString returns payload[key] trimmed when it holds a real
// string. Unlike PayloadStr it does not render non-string values, so a numeric
// or boolean value yields "".
func SemanticPayloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	str, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(str)
}

// PayloadMap returns payload[key] when it holds a nested map, and nil for an
// absent key, a nil value, or a value of any other type.
func PayloadMap(payload map[string]any, key string) map[string]any {
	if payload == nil {
		return nil
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return nil
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

// PayloadOrderedStrings returns the trimmed, non-empty strings at payload[key]
// in their original order, accepting a []string, a []any of renderable items,
// or a single string.
func PayloadOrderedStrings(payload map[string]any, key string) []string {
	raw, ok := payload[key]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			if value = strings.TrimSpace(value); value != "" {
				out = append(out, value)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			text := strings.TrimSpace(PayloadString(map[string]any{"value": value}, "value"))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			return []string{trimmed}
		}
	}
	return nil
}

// SemanticPayloadStringSlice returns the trimmed, non-empty strings at
// payload[key], accepting a []string or a []any of strings. It returns nil
// rather than an empty slice when nothing survives trimming.
func SemanticPayloadStringSlice(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				continue
			}
			if trimmed := strings.TrimSpace(text); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

// PayloadBool reports whether payload[key] is true, accepting either a real
// bool or a case-insensitive "true"/"false" string. Use BoolPayload where only
// a real bool should count.
func PayloadBool(payload map[string]any, key string) bool {
	value, ok := PayloadBoolPointerValue(payload, key)
	return ok && value
}

// PayloadBoolPointerValue reads payload[key] as a boolean and reports whether a
// usable value was present, distinguishing an explicit false from an absent key,
// a blank string, or a non-boolean type. Any other non-blank string counts as
// present and compares equal-fold against "true", so "banana" reads as
// (false, true).
func PayloadBoolPointerValue(payload map[string]any, key string) (bool, bool) {
	switch value := payload[key].(type) {
	case bool:
		return value, true
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return false, false
		}
		return strings.EqualFold(trimmed, "true"), true
	default:
		return false, false
	}
}

// BoolPayload reports whether payload[key] holds the boolean true. Unlike
// PayloadBool it accepts only a real bool, never a "true" string.
func BoolPayload(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}

// CopyPayload returns a shallow copy of m so a caller can mutate the copy
// without disturbing the payload an intent still references.
func CopyPayload(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// MapSlice coerces a payload value into a slice of maps, accepting a
// []map[string]any or a []any whose items are maps. Non-map items are skipped
// and any other value yields nil.
func MapSlice(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			asMap, ok := item.(map[string]any)
			if ok {
				result = append(result, asMap)
			}
		}
		return result
	default:
		return nil
	}
}

// ToStringSlice coerces a payload value into a string slice, accepting a
// []string (returned as-is), a []any of renderable items, or a single scalar.
// Empty and "<nil>" renderings are dropped from the []any and scalar forms
// only; a []string is never filtered.
func ToStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			text := fmt.Sprint(item)
			if text == "" || text == "<nil>" {
				continue
			}
			result = append(result, text)
		}
		return result
	default:
		text := fmt.Sprint(value)
		if text == "" || text == "<nil>" {
			return nil
		}
		return []string{text}
	}
}

// AnyToString renders any payload value as a string, returning it unchanged
// when it already is one and formatting it with %v otherwise. A nil value
// becomes the empty string. It does not trim.
func AnyToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
