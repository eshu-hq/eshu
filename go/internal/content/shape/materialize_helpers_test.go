// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shape

// itoa is a minimal int-to-string helper for test files that need to generate
// unique entity names without importing strconv or fmt.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

type EntityRecordExpectation struct {
	entityType  string
	entityName  string
	startLine   int
	endLine     int
	sourceCache string
	entityID    string
}

func boolPtr(value bool) *bool {
	return &value
}

func toStringSlice(value any) []string {
	items, ok := value.([]string)
	if ok {
		return items
	}
	rawItems, ok := value.([]any)
	if !ok {
		return nil
	}
	converted := make([]string, 0, len(rawItems))
	for _, item := range rawItems {
		text, ok := item.(string)
		if !ok {
			return nil
		}
		converted = append(converted, text)
	}
	return converted
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
