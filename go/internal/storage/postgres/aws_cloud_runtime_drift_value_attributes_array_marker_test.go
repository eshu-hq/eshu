// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"testing"
)

// TestComparableScalarAttrTreatsArrayWrappedRedactionMarkerAsAbsent is the
// #5859 Finding 3 regression: terraformstate.applyLeafClassification
// recurses redaction into array elements for composite (repeated-block)
// attributes, so a redacted composite attribute decodes as
// []any{map[string]any{marker}}, not a bare marker map. redact.IsRedactedValue
// alone only recognizes the bare-map shape (checked directly below), so a
// naive comparableScalarAttr would fall through to coerceJSONString's
// default fmt.Sprint(value) branch and reproduce the exact garbage-string
// bug #5859 fixed for the bare-map case -- just one JSON array wrapper away.
//
// Not live today: none of the three allowlisted keys (ami, image_uri,
// version) are ever emitted as arrays. This test pins the boundary so
// adding a composite attribute to cloudruntime.ValueAttributeAllowlistFor
// cannot silently reintroduce the bug -- it fails if comparableScalarAttr
// (or the redactedAnywhere helper it calls) is reverted to a bare
// redact.IsRedactedValue(value) check.
func TestComparableScalarAttrTreatsArrayWrappedRedactionMarkerAsAbsent(t *testing.T) {
	t.Parallel()

	var attributes map[string]any
	payload := []byte(`{"ami": [` + redactionMarkerJSON("unknown_provider_schema", "resources.*.attributes.ami") + `]}`)
	if err := json.Unmarshal(payload, &attributes); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	got, degraded := comparableScalarAttr(attributes, "ami")
	if got != "" {
		t.Fatalf("comparableScalarAttr() = %q, want \"\" for an array-wrapped redaction marker", got)
	}
	if !degraded {
		t.Fatal("comparableScalarAttr() degraded = false; an array-wrapped marker is unreadable evidence, not absent evidence")
	}
}

// TestComparableScalarAttrPreservesGenuineArrayValue proves the array-wrapped
// guard is shape-based, not "any array is suspect": a genuinely array-typed
// attribute that is NOT redaction must still fall through to
// coerceJSONString unmodified, matching comparableScalarAttr's existing
// non-redacted behavior. Without it the guard could pass by swallowing every
// array, which would trade the false-positive drift for a false-negative one.
func TestComparableScalarAttrPreservesGenuineArrayValue(t *testing.T) {
	t.Parallel()

	attributes := map[string]any{"ami": []any{"ami-000000000000000a"}}
	got, degraded := comparableScalarAttr(attributes, "ami")
	want := "[ami-000000000000000a]"
	if degraded {
		t.Fatal("comparableScalarAttr() degraded = true; a genuine array value was read successfully")
	}
	if got != want {
		t.Fatalf("comparableScalarAttr() = %q, want %q (non-redacted array must pass through unmodified)", got, want)
	}
}

// TestRedactedAnywhere table-tests the shape recognition
// comparableScalarAttr relies on directly, independent of the JSON decode
// path, so each shape variant is pinned without needing a fresh payload
// fixture per case.
func TestRedactedAnywhere(t *testing.T) {
	t.Parallel()

	marker := map[string]any{
		"marker": "redacted:hmac-sha256:" + stringsRepeatA(64),
		"reason": "unknown_provider_schema",
		"source": "resources.*.attributes.ami",
	}

	tests := []struct {
		name  string
		value any
		want  bool
	}{
		{name: "bare marker map", value: marker, want: true},
		{name: "single-element array wrapping marker", value: []any{marker}, want: true},
		{name: "marker as second element of multi-element array", value: []any{"ami-000000000000000a", marker}, want: true},
		{name: "plain string", value: "ami-000000000000000a", want: false},
		{name: "array of plain strings", value: []any{"a", "b"}, want: false},
		{name: "nil", value: nil, want: false},
		{name: "empty array", value: []any{}, want: false},
		{name: "map without marker key", value: map[string]any{"reason": "x"}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := redactedAnywhere(tc.value); got != tc.want {
				t.Fatalf("redactedAnywhere(%#v) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func stringsRepeatA(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = 'a'
	}
	return string(out)
}
