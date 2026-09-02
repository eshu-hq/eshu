// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parsertest

import "testing"

// TestFunctionByNameAndClassRejectsMalformedRow pins the predicate behind
// AssertFunctionByNameAndClass as fail-closed. A functions row whose name or
// class_context is present but not a string must be rejected, not skipped.
//
// Skipping is what a discarded type assertion does: the malformed value reads
// as the empty string, the row silently does not match, and a later valid row
// satisfies the lookup. The assertion then passes while the parser emitted a
// malformed row, which is the failure this helper exists to catch. The
// equivalent root-package helper has rejected malformed rows since it was
// hardened; this predicate had not, so tests relocated from root onto
// parsertest were quietly weaker than the ones they replaced.
//
// Both malformed rows are placed BEFORE the valid row on purpose: a predicate
// that returns on first match would otherwise never reach them.
func TestFunctionByNameAndClassRejectsMalformedRow(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		row  map[string]any
	}{
		{name: "non-string name", row: map[string]any{"name": 42, "class_context": "C"}},
		{name: "non-string class_context", row: map[string]any{"name": "other", "class_context": []string{"C"}}},
	} {
		payload := map[string]any{"functions": []map[string]any{
			tt.row,
			{"name": "wanted", "class_context": "C"},
		}}
		if _, err := functionByNameAndClass(payload, "wanted", "C"); err == nil {
			t.Errorf("%s: malformed row was skipped and the lookup succeeded; the predicate must fail closed", tt.name)
		}
	}
}

// TestFunctionByNameAndClassStillMatchesWellFormedRows guards the fix from
// over-correcting: a well-formed payload must still resolve.
func TestFunctionByNameAndClassStillMatchesWellFormedRows(t *testing.T) {
	t.Parallel()

	payload := map[string]any{"functions": []map[string]any{
		{"name": "other", "class_context": "C"},
		{"name": "wanted", "class_context": "C"},
	}}
	function, err := functionByNameAndClass(payload, "wanted", "C")
	if err != nil {
		t.Fatalf("well-formed payload rejected: %v", err)
	}
	if function["name"] != "wanted" {
		t.Fatalf("resolved the wrong row: %#v", function)
	}
}
