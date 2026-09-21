// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backup

import (
	"strings"
	"time"
)

func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

func isKMSKeyARN(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "arn:") && strings.Contains(value, ":kms:")
}

func uniqueARNs(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if !isARN(trimmed) {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	out := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func int64OrNil(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func boolOrNil(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}
