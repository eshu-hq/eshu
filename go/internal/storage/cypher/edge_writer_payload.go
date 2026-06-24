// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"strings"
)

// payloadString reads a string-typed payload field, returning "" when the field
// is absent or not a string.
func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	v, ok := payload[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// payloadBool reads a bool-typed payload field, returning false when the field
// is absent or not a bool.
func payloadBool(payload map[string]any, key string) bool {
	if payload == nil {
		return false
	}
	value, ok := payload[key]
	if !ok {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}

// payloadInt accepts numeric shapes produced by Go maps, JSON decoding, and
// database drivers.
func payloadInt(payload map[string]any, key string) int {
	if payload == nil {
		return 0
	}
	switch value := payload[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

// payloadFloat accepts numeric shapes produced by Go maps, JSON decoding, and
// database drivers.
func payloadFloat(payload map[string]any, key string) float64 {
	if payload == nil {
		return 0
	}
	switch value := payload[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

// payloadMapSlice normalizes graph-story evidence summaries after JSON
// decoding or direct Go construction in reducer tests.
func payloadMapSlice(payload map[string]any, key string) []map[string]any {
	if payload == nil {
		return nil
	}
	switch value := payload[key].(type) {
	case []map[string]any:
		return value
	case []any:
		out := make([]map[string]any, 0, len(value))
		for _, item := range value {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}

// payloadStringSlice normalizes evidence-kind arrays before passing them to
// graph drivers.
func payloadStringSlice(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	switch value := payload[key].(type) {
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text == "" || text == "<nil>" {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}
