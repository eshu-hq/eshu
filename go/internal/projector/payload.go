package projector

import (
	"fmt"
	"strconv"
	"strings"
)

func payloadAttributes(payload map[string]any, excluded ...string) map[string]string {
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

func payloadHasKey(payload map[string]any, key string) bool {
	if len(payload) == 0 {
		return false
	}

	_, ok := payload[key]
	return ok
}

func payloadInt(payload map[string]any, key string) (int, bool) {
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
		return int(typed), true
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

func payloadIntPtr(payload map[string]any, key string) *int {
	value, ok := payloadInt(payload, key)
	if !ok {
		return nil
	}

	cloned := value
	return &cloned
}

func payloadBoolPtr(payload map[string]any, key string) *bool {
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
