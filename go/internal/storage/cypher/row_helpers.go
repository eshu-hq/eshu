// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"strings"
)

// ValidateStaticGraphToken runs the character-class screen first (to keep the
// error for unsafe input precise) then the closed-vocabulary allowlist check,
// returning the token for interpolation. This is the single enforcement point for
// the schema-surface contract on the direction relationship type and the
// heterogeneous endpoint label.
func ValidateStaticGraphToken(row map[string]any, field string, vocabulary map[string]struct{}, label string) (string, error) {
	raw, ok := row[field].(string)
	if !ok || raw == "" || raw != strings.TrimSpace(raw) {
		return "", fmt.Errorf("%s must be a non-empty string", label)
	}
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return "", fmt.Errorf("%s %q contains unsupported character %q", label, raw, ch)
	}
	if _, ok := vocabulary[raw]; !ok {
		return "", fmt.Errorf("%s %q is outside the closed vocabulary", label, raw)
	}
	return raw, nil
}

// CloneRowWith returns a shallow copy of row with the extra annotations applied,
// so the writer never mutates the caller's resolved row maps.
func CloneRowWith(row map[string]any, annotations map[string]any) map[string]any {
	cloned := make(map[string]any, len(row)+len(annotations))
	for key, value := range row {
		cloned[key] = value
	}
	for key, value := range annotations {
		cloned[key] = value
	}
	return cloned
}
