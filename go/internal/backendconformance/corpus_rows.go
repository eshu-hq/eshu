// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// compareReadRows reports whether got equals want as a multiset of rows.
//
// Each row is normalized to its JSON encoding, which sorts map keys and erases
// the difference between driver value types that mean the same thing (int64 and
// int, []any and []string). Row order is ignored because not every production
// statement carries an ORDER BY, and a backend is free to return an unordered
// result in any order.
func compareReadRows(got, want []map[string]any) error {
	gotKeys, err := normalizedRows(got)
	if err != nil {
		return fmt.Errorf("normalize returned rows: %w", err)
	}
	wantKeys, err := normalizedRows(want)
	if err != nil {
		return fmt.Errorf("normalize expected rows: %w", err)
	}
	if slices.Equal(gotKeys, wantKeys) {
		return nil
	}
	return fmt.Errorf("returned rows differ from the exact expected rows\n got (%d): %s\nwant (%d): %s",
		len(gotKeys), strings.Join(gotKeys, " "), len(wantKeys), strings.Join(wantKeys, " "))
}

func normalizedRows(rows []map[string]any) ([]string, error) {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		encoded, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("encode row %v: %w", row, err)
		}
		out = append(out, string(encoded))
	}
	slices.Sort(out)
	return out, nil
}
