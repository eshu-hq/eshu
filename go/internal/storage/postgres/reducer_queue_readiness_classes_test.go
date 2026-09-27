// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"slices"
	"testing"
)

// TestNonCountingReducerRetryFailureClassesReturnsACopy pins the accessor's
// copy contract: both claim paths derive their retry-budget exemption from
// nonCountingReducerRetryFailureClasses, so a caller that writes into the
// returned slice must not be able to change the set, what a later call
// returns, or what IsNonCountingReducerRetryFailureClass answers.
func TestNonCountingReducerRetryFailureClassesReturnsACopy(t *testing.T) {
	t.Parallel()

	original := NonCountingReducerRetryFailureClasses()
	if len(original) == 0 {
		t.Fatal("NonCountingReducerRetryFailureClasses() returned no classes")
	}
	want := slices.Clone(original)
	firstClass := want[0]

	const overwritten = "caller_overwrote_this_class"
	original[0] = overwritten

	if got := NonCountingReducerRetryFailureClasses(); !slices.Equal(got, want) {
		t.Fatalf("second call after mutating the first result = %v, want %v", got, want)
	}
	if !IsNonCountingReducerRetryFailureClass(firstClass) {
		t.Fatalf("IsNonCountingReducerRetryFailureClass(%q) = false after a caller overwrote its copy", firstClass)
	}
	if IsNonCountingReducerRetryFailureClass(overwritten) {
		t.Fatalf("IsNonCountingReducerRetryFailureClass(%q) = true; the caller's write reached the shared set", overwritten)
	}
}
