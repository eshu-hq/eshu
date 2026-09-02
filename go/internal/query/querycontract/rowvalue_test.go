// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "testing"

// TestFloatVal pins the numeric coercion. A graph driver is free to hand back
// a confidence as float32, int or int64 depending on how the value was stored,
// and every one of those has to survive as the same number. A branch that
// silently returned 0 would not fail a request: RelationshipConfidenceBasis
// reads confidence first and reports "" for anything at or below zero, so a
// broken coercion erases the basis from responses instead of erroring.
func TestFloatVal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		row  map[string]any
		key  string
		want float64
	}{
		{name: "float64", row: map[string]any{"c": float64(0.75)}, key: "c", want: 0.75},
		{name: "float32", row: map[string]any{"c": float32(0.5)}, key: "c", want: 0.5},
		{name: "int", row: map[string]any{"c": 3}, key: "c", want: 3},
		{name: "int64", row: map[string]any{"c": int64(7)}, key: "c", want: 7},
		{name: "negative", row: map[string]any{"c": float64(-1.5)}, key: "c", want: -1.5},
		{name: "missing key", row: map[string]any{"other": 1.0}, key: "c"},
		{name: "nil value", row: map[string]any{"c": nil}, key: "c"},
		{name: "string is not coerced", row: map[string]any{"c": "0.9"}, key: "c"},
		{name: "bool is not coerced", row: map[string]any{"c": true}, key: "c"},
		{name: "nil row", row: nil, key: "c"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := FloatVal(test.row, test.key); got != test.want {
				t.Fatalf("FloatVal(%q) = %v, want %v", test.key, got, test.want)
			}
		})
	}
}
