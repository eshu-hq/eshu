// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mediadoc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func addWarnings(metadata map[string]string, warnings ...string) {
	if len(warnings) == 0 {
		return
	}
	seen := map[string]bool{}
	for _, warning := range strings.Split(metadata["warning"], ",") {
		warning = strings.TrimSpace(warning)
		if warning != "" {
			seen[warning] = true
		}
	}
	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)
		if warning != "" {
			seen[warning] = true
		}
	}
	ordered := make([]string, 0, len(seen))
	for warning := range seen {
		ordered = append(ordered, warning)
	}
	sort.Strings(ordered)
	metadata["warning"] = strings.Join(ordered, ",")
}

func addPositiveInt(metadata map[string]string, key string, value int) {
	if value > 0 {
		metadata[key] = strconv.Itoa(value)
	}
}

func addPositiveInt64(metadata map[string]string, key string, value int64) {
	if value > 0 {
		metadata[key] = strconv.FormatInt(value, 10)
	}
}

func confidenceBucket(confidence float64) string {
	switch {
	case confidence >= 0.85:
		return "high"
	case confidence >= 0.60:
		return "medium"
	case confidence > 0:
		return "low"
	default:
		return "unknown"
	}
}

func cleanID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func hashBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func hashText(text string) string {
	return hashBytes([]byte(text))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func formatMillis(value int64) string {
	hours := value / 3_600_000
	value %= 3_600_000
	minutes := value / 60_000
	value %= 60_000
	seconds := value / 1000
	millis := value % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, seconds, millis)
}
