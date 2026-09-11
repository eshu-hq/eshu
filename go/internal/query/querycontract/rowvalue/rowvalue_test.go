// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rowvalue

import (
	"reflect"
	"testing"
)

// TestStringVal pins the asymmetry that separates StringVal from the other
// four: a present value of the wrong type is rendered with %v rather than
// discarded. A driver that hands back a repository name as an int64 still
// carries the name the caller asked for, and discarding it would blank a field
// in the response with no error anywhere to explain it.
func TestStringVal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		row  map[string]any
		key  string
		want string
	}{
		{name: "string", row: map[string]any{"n": "eshu"}, key: "n", want: "eshu"},
		{name: "empty string stays empty", row: map[string]any{"n": ""}, key: "n"},
		{name: "int is rendered", row: map[string]any{"n": 42}, key: "n", want: "42"},
		{name: "int64 is rendered", row: map[string]any{"n": int64(42)}, key: "n", want: "42"},
		{name: "bool is rendered", row: map[string]any{"n": true}, key: "n", want: "true"},
		{name: "missing key", row: map[string]any{"other": "x"}, key: "n"},
		{name: "nil value", row: map[string]any{"n": nil}, key: "n"},
		{name: "nil row", row: nil, key: "n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := StringVal(test.row, test.key); got != test.want {
				t.Fatalf("StringVal(%q) = %q, want %q", test.key, got, test.want)
			}
		})
	}
}

// TestBoolVal pins the discarding half of that asymmetry. A non-bool is not
// rendered or coerced; it yields false, which is what a caller gating an
// optional feature on a missing column needs.
func TestBoolVal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		row  map[string]any
		key  string
		want bool
	}{
		{name: "true", row: map[string]any{"b": true}, key: "b", want: true},
		{name: "false", row: map[string]any{"b": false}, key: "b"},
		{name: "string true is not coerced", row: map[string]any{"b": "true"}, key: "b"},
		{name: "nonzero int is not coerced", row: map[string]any{"b": 1}, key: "b"},
		{name: "missing key", row: map[string]any{"other": true}, key: "b"},
		{name: "nil value", row: map[string]any{"b": nil}, key: "b"},
		{name: "nil row", row: nil, key: "b"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := BoolVal(test.row, test.key); got != test.want {
				t.Fatalf("BoolVal(%q) = %v, want %v", test.key, got, test.want)
			}
		})
	}
}

// TestIntVal pins the three numeric shapes a driver actually produces for the
// same column: int64 over Bolt, int from an in-process fake, float64 after a
// JSON round trip. A branch that dropped one would turn a real count into 0,
// which reads as an empty result rather than a decode failure.
func TestIntVal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		row  map[string]any
		key  string
		want int
	}{
		{name: "int64 over bolt", row: map[string]any{"c": int64(7)}, key: "c", want: 7},
		{name: "int from an in-process fake", row: map[string]any{"c": 7}, key: "c", want: 7},
		{name: "float64 after a json round trip", row: map[string]any{"c": float64(7)}, key: "c", want: 7},
		{name: "float64 truncates toward zero", row: map[string]any{"c": float64(7.9)}, key: "c", want: 7},
		{name: "negative", row: map[string]any{"c": int64(-3)}, key: "c", want: -3},
		{name: "string is not coerced", row: map[string]any{"c": "7"}, key: "c"},
		{name: "float32 is not accepted", row: map[string]any{"c": float32(7)}, key: "c"},
		{name: "missing key", row: map[string]any{"other": 1}, key: "c"},
		{name: "nil value", row: map[string]any{"c": nil}, key: "c"},
		{name: "nil row", row: nil, key: "c"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := IntVal(test.row, test.key); got != test.want {
				t.Fatalf("IntVal(%q) = %d, want %d", test.key, got, test.want)
			}
		})
	}
}

// TestStringSliceVal pins the list decode, including the element-level skip: a
// driver list column arrives as []any, and one non-string element must drop
// rather than abort the whole column.
func TestStringSliceVal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		row  map[string]any
		key  string
		want []string
	}{
		{name: "already typed", row: map[string]any{"l": []string{"a", "b"}}, key: "l", want: []string{"a", "b"}},
		{name: "driver any list", row: map[string]any{"l": []any{"a", "b"}}, key: "l", want: []string{"a", "b"}},
		{name: "non-string elements are skipped", row: map[string]any{"l": []any{"a", 1, nil, "b"}}, key: "l", want: []string{"a", "b"}},
		{name: "empty any list yields empty not nil", row: map[string]any{"l": []any{}}, key: "l", want: []string{}},
		{name: "scalar is not wrapped", row: map[string]any{"l": "a"}, key: "l"},
		{name: "missing key", row: map[string]any{"other": []any{"a"}}, key: "l"},
		{name: "nil value", row: map[string]any{"l": nil}, key: "l"},
		{name: "nil row", row: nil, key: "l"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := StringSliceVal(test.row, test.key)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("StringSliceVal(%q) = %#v, want %#v", test.key, got, test.want)
			}
		})
	}
}

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
