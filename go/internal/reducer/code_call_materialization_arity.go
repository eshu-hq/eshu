package reducer

import (
	"strconv"
	"strings"
)

// codeCallAppendArityNames adds overload-sensitive candidate names after the
// unqualified candidates so older facts still resolve when no arity is known.
func codeCallAppendArityNames(names []string, arity int) []string {
	if arity < 0 || len(names) == 0 {
		return names
	}
	originalLen := len(names)
	for i := 0; i < originalLen; i++ {
		arityName := names[i] + "#" + strconv.Itoa(arity)
		exists := false
		for _, existing := range names {
			if existing == arityName {
				exists = true
				break
			}
		}
		if !exists {
			names = append(names, arityName)
		}
	}
	return names
}

// codeCallMetadataInt reads parser metadata that may arrive from in-memory
// tests, JSON-decoded facts, or older string-shaped fixture values.
func codeCallMetadataInt(item map[string]any, key string) (int, bool) {
	value, ok := item[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return typed, typed >= 0
	case int64:
		if typed < 0 || typed > int64(^uint(0)>>1) {
			return 0, false
		}
		return int(typed), true
	case float64:
		if typed < 0 || typed != float64(int(typed)) {
			return 0, false
		}
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil || parsed < 0 {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
