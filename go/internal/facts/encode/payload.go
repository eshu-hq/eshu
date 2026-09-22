// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package encode

import "reflect"

// IntPtr returns a pointer to value, or nil when value is zero. A zero
// optional integer is an unset line, page, or offset, not a real position.
func IntPtr(value int) *int {
	if value == 0 {
		return nil
	}
	return &value
}

// StringPtr returns a pointer to value, or nil when value is empty. An
// omitted optional string field and an explicitly empty one are the same
// absence in a fact payload, so the encoders emit neither.
func StringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// BoolPtr returns a pointer to value, or nil when value is false. A false
// optional boolean is an absent claim in a fact payload, not a negative one.
func BoolPtr(value bool) *bool {
	if !value {
		return nil
	}
	return &value
}

// StringValue returns payload[key] when it holds a string, and the empty
// string for a missing key or any other type. Decoding a payload map never
// fails on a wrong type here; the field reads as absent instead.
func StringValue(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

// StringPtrFromMap returns a pointer to payload[key]'s string value, or nil
// when the key is missing, holds a non-string, or holds the empty string.
func StringPtrFromMap(payload map[string]any, key string) *string {
	value := StringValue(payload, key)
	if value == "" {
		return nil
	}
	return &value
}

// IntPtrFromMap returns a pointer to payload[key]'s integer value, accepting
// the int, int64, and float64 forms a JSON round-trip can produce. A float64
// that is not integral, a missing key, and any other type all return nil, so a
// fractional value is never silently truncated into a line or page number.
func IntPtrFromMap(payload map[string]any, key string) *int {
	switch value := payload[key].(type) {
	case int:
		return &value
	case int64:
		converted := int(value)
		return &converted
	case float64:
		converted := int(value)
		if float64(converted) != value {
			return nil
		}
		return &converted
	default:
		return nil
	}
}

// JSONShapeMap normalizes one payload map into the shape a JSON round-trip
// produces, so an in-process payload and a payload read back from storage
// compare equal: integers widen to float64, map[string]string widens to
// map[string]any, and typed slices widen element by element.
//
// It deliberately takes and returns only the map. An owning package pairs it
// with its own unexported two-value adapter so its encoder's error is
// returned by same-package code; routing that error back out through this
// package would make every encoder return an error from an external package
// with no context to add, which wrapcheck correctly rejects.
func JSONShapeMap(payload map[string]any) map[string]any {
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		out[key] = normalizeJSONShapeValue(value)
	}
	return out
}

func normalizeJSONShapeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return JSONShapeMap(typed)
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = value
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, value := range typed {
			out = append(out, normalizeJSONShapeValue(value))
		}
		return out
	case []string:
		out := make([]any, 0, len(typed))
		for _, value := range typed {
			out = append(out, value)
		}
		return out
	case []int:
		out := make([]any, 0, len(typed))
		for _, value := range typed {
			out = append(out, float64(value))
		}
		return out
	case int:
		return float64(typed)
	case int8:
		return float64(typed)
	case int16:
		return float64(typed)
	case int32:
		return float64(typed)
	case int64:
		return float64(typed)
	case uint:
		return float64(typed)
	case uint8:
		return float64(typed)
	case uint16:
		return float64(typed)
	case uint32:
		return float64(typed)
	case uint64:
		return float64(typed)
	}
	if value == nil {
		return nil
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Slice {
		return value
	}
	out := make([]any, 0, reflected.Len())
	for i := 0; i < reflected.Len(); i++ {
		out = append(out, normalizeJSONShapeValue(reflected.Index(i).Interface()))
	}
	return out
}
