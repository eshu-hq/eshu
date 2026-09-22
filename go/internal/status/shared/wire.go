// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import "time"

// NamedCountJSON is the wire shape of one named status bucket. Its field tags
// are part of the operator-facing status contract and must not drift from
// NamedCount, which it is converted from directly.
type NamedCountJSON struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// NamedCountsJSON projects named buckets into their wire shape. It always
// returns a non-nil slice so an empty section renders as [] rather than null.
func NamedCountsJSON(rows []NamedCount) []NamedCountJSON {
	projected := make([]NamedCountJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, NamedCountJSON(row))
	}
	return projected
}

// NullableRFC3339Value renders a timestamp as UTC RFC3339, or the empty string
// when it is unset. Status sections that distinguish "absent" from "zero time"
// use the pointer-returning form in the root package instead.
func NullableRFC3339Value(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
