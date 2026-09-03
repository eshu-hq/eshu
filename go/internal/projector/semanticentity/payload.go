// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticentity

import (
	"fmt"
	"strings"
)

// payloadString and asString are a byte-for-byte copy of root's
// go/internal/projector/payload.go helpers of the same name, trimmed to the
// two functions this family calls. This package cannot import root -- root
// imports this package to dispatch, so the reverse direction cycles -- so the
// shared logic is duplicated here rather than referenced. Keep both in sync
// with root's copy if root's semantics change; nothing enforces that
// automatically.
func payloadString(payload map[string]any, key string) (string, bool) {
	if len(payload) == 0 {
		return "", false
	}

	value, ok := payload[key]
	if !ok {
		return "", false
	}

	text, ok := asString(value)
	if !ok {
		return "", false
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}

	return text, true
}

func asString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case fmt.Stringer:
		return typed.String(), true
	default:
		return "", false
	}
}

// The three payloadMetadata* readers and payloadStringSlice below MOVED with
// this family rather than being copied: root had no other caller for any of
// them, so no root twin remains to drift against. Each reads the flat payload
// key first and falls back to the nested entity_metadata map, because the
// collector emits some parser metadata flat and some nested depending on the
// language adapter.

// payloadMetadataString reads a string field from the payload, falling back
// to the nested entity_metadata map.
func payloadMetadataString(payload map[string]any, key string) string {
	if value, ok := payloadString(payload, key); ok {
		return value
	}
	raw, ok := payload["entity_metadata"]
	if !ok || raw == nil {
		return ""
	}
	metadata, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	value, ok := payloadString(metadata, key)
	if !ok {
		return ""
	}
	return value
}

// payloadMetadataStringSlice reads a string-slice field from the payload,
// falling back to the nested entity_metadata map. An empty flat slice falls
// through to the nested read.
func payloadMetadataStringSlice(payload map[string]any, key string) []string {
	if values := payloadStringSlice(payload, key); len(values) > 0 {
		return values
	}
	raw, ok := payload["entity_metadata"]
	if !ok || raw == nil {
		return nil
	}
	metadata, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return payloadStringSlice(metadata, key)
}

// payloadMetadataBool reads a bool field from the payload, falling back to
// the nested entity_metadata map. Unlike payloadMetadataString it accepts
// only a real bool; a "true" string is not coerced.
func payloadMetadataBool(payload map[string]any, key string) bool {
	if value, ok := payload[key]; ok {
		if typed, ok := value.(bool); ok {
			return typed
		}
	}
	raw, ok := payload["entity_metadata"]
	if !ok || raw == nil {
		return false
	}
	metadata, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	value, ok := metadata[key]
	if !ok {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}

// payloadStringSlice normalizes a string-slice payload field, tolerating both
// []string and the []any shape a JSON round-trip produces. Blank entries are
// dropped and an all-blank slice reads as absent.
func payloadStringSlice(payload map[string]any, key string) []string {
	if len(payload) == 0 {
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
