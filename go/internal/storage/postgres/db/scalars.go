// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

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
