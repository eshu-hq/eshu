package php

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func normalizePHPTypeName(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "?")
	if index := strings.Index(trimmed, "|"); index >= 0 {
		parts := strings.Split(trimmed, "|")
		normalized := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			normalized = append(normalized, normalizePHPTypeName(part))
		}
		return strings.Join(normalized, "|")
	}
	return shared.LastPathSegment(trimmed, `\`)
}
