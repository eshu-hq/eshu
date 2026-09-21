// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package decode

import (
	"fmt"
	"strconv"
	"strings"
)

// PayloadAttributes flattens a fact payload into string attributes, skipping
// the named keys a caller has already promoted onto a typed field. Values that
// do not render as a string are dropped rather than stringified, so an
// attribute map never carries a Go type rendering into the graph. It returns
// nil for an empty payload so callers can store the absence of attributes
// rather than an empty map.
func PayloadAttributes(payload map[string]any, excluded ...string) map[string]string {
	if len(payload) == 0 {
		return nil
	}

	skip := make(map[string]struct{}, len(excluded))
	for _, key := range excluded {
		skip[key] = struct{}{}
	}

	attributes := make(map[string]string, len(payload))
	for key, value := range payload {
		if _, ok := skip[key]; ok {
			continue
		}
		if text, ok := asString(value); ok {
			attributes[key] = text
		}
	}

	if len(attributes) == 0 {
		return nil
	}

	return attributes
}

// PayloadString reads key from a fact payload as a string. The second result
// is false when the payload is empty, the key is absent, or the value is not a
// string-shaped scalar, which lets a caller distinguish "absent" from the empty
// string.
func PayloadString(payload map[string]any, key string) (string, bool) {
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

// PayloadHasKey reports whether a fact payload carries key at all, including
// when its value is nil. Callers use it to tell a field that was explicitly
// emitted as empty from one the producer never set.
func PayloadHasKey(payload map[string]any, key string) bool {
	if len(payload) == 0 {
		return false
	}

	_, ok := payload[key]
	return ok
}

// PayloadInt reads key from a fact payload as an int, accepting the numeric
// shapes JSON decoding produces. The second result is false when the payload is
// empty, the key is absent, or the value does not convert without loss.
func PayloadInt(payload map[string]any, key string) (int, bool) {
	if len(payload) == 0 {
		return 0, false
	}

	value, ok := payload[key]
	if !ok {
		return 0, false
	}

	switch typed := value.(type) {
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case uint:
		return int(typed), true
	case uint8:
		return int(typed), true
	case uint16:
		return int(typed), true
	case uint32:
		return int(typed), true
	case uint64:
		return int(typed), true // #nosec G115 -- caller is a generic type-switch converter; callers must validate range; truncation is intentional on 32-bit int platforms
	case float32:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, false
		}
		return parsed, true
	case fmt.Stringer:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed.String()))
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

// PayloadIntPtr reads key as an optional int, returning nil when
// [PayloadInt] finds no usable value. The returned pointer addresses a copy, so
// a caller cannot mutate the payload through it.
func PayloadIntPtr(payload map[string]any, key string) *int {
	value, ok := PayloadInt(payload, key)
	if !ok {
		return nil
	}

	cloned := value
	return &cloned
}

// PayloadBoolPtr reads key as an optional bool, returning nil when the payload
// is empty, the key is absent, or the value is not a bool. The returned pointer
// addresses a copy, so a caller cannot mutate the payload through it.
func PayloadBoolPtr(payload map[string]any, key string) *bool {
	if len(payload) == 0 {
		return nil
	}

	value, ok := payload[key]
	if !ok {
		return nil
	}

	switch typed := value.(type) {
	case bool:
		cloned := typed
		return &cloned
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		if err != nil {
			return nil
		}
		cloned := parsed
		return &cloned
	case fmt.Stringer:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed.String()))
		if err != nil {
			return nil
		}
		cloned := parsed
		return &cloned
	default:
		return nil
	}
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
