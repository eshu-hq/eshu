// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scalars

import (
	"database/sql"
	"strings"
	"time"
)

// Blank reports whether a string carries no non-space content. Stores use it
// to reject empty identifiers and scopes before touching the database.
func Blank(value string) bool {
	return strings.TrimSpace(value) == ""
}

// NullTime maps a possibly-zero time to a nullable database value.
func NullTime(value time.Time) sql.NullTime {
	return sql.NullTime{Time: value, Valid: !value.IsZero()}
}

// NullTimePtr maps a possibly-nil timestamp pointer to a nullable database
// value, normalizing present values to UTC.
func NullTimePtr(value *time.Time) sql.NullTime {
	if value == nil || value.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: value.UTC(), Valid: true}
}

// TimePtrFromNull maps a nullable database value back to a timestamp
// pointer, normalizing present values to UTC.
func TimePtrFromNull(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	timestamp := value.Time.UTC()
	return &timestamp
}

// DurationFromSeconds converts a non-negative seconds value read from a
// Postgres numeric column into a time.Duration, treating a non-positive
// value as zero.
func DurationFromSeconds(value float64) time.Duration {
	if value <= 0 {
		return 0
	}
	return time.Duration(value * float64(time.Second))
}

// NullableTimeUTC maps a nullable database value to a zero time.Time when
// absent, and to its UTC value when present.
func NullableTimeUTC(value sql.NullTime) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

// NullableTime binds a zero time as SQL NULL and normalizes any other value
// to UTC before it is bound as a statement argument.
func NullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// StringMapToAny widens a string-valued map to an any-valued map for JSON
// payload marshaling, or nil when the input is empty.
func StringMapToAny(input map[string]string) map[string]any {
	if len(input) == 0 {
		return nil
	}

	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}

	return output
}

// CleanStringSet trims, drops empties, and deduplicates a string slice while
// preserving first-seen order.
func CleanStringSet(values []string) []string {
	seen := map[string]struct{}{}
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}
