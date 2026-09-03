// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplanesatisfiedby

import (
	"fmt"
	"strings"
)

// payloadString and asString are a byte-for-byte copy of root's
// go/internal/projector/payload.go helpers of the same name, trimmed to the
// one function this family's trigger predicate calls. This package cannot
// import root — root imports this package to dispatch, so the reverse
// direction cycles — so the shared logic is duplicated here rather than
// referenced. Keep both in sync with root's copy if root's semantics change;
// nothing enforces that automatically.
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
