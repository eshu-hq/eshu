// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recovery

import (
	"strings"
	"testing"
)

// TestSkippedScopesSampleIsBounded pins the response-size bound: counts are
// exact, the sample is capped, so a deployment with thousands of skipped scopes
// cannot blow up the recover-generations response body.
func TestSkippedScopesSampleIsBounded(t *testing.T) {
	t.Parallel()

	var skipped SkippedScopes
	for i := 0; i < SkippedScopeSampleLimit+5; i++ {
		skipped.Add(SkipReasonNoRecoverableGeneration, "scope")
	}
	if got, want := skipped.ByReason[SkipReasonNoRecoverableGeneration], SkippedScopeSampleLimit+5; got != want {
		t.Fatalf("count = %d, want the exact %d", got, want)
	}
	if got, want := len(skipped.Samples[SkipReasonNoRecoverableGeneration]), SkippedScopeSampleLimit; got != want {
		t.Fatalf("sample length = %d, want it capped at %d", got, want)
	}
}

// TestSkippedScopesTotalAndReasons covers the derived views the handler logs
// and emits per reason.
func TestSkippedScopesTotalAndReasons(t *testing.T) {
	t.Parallel()

	var skipped SkippedScopes
	if skipped.Total() != 0 || len(skipped.Reasons()) != 0 {
		t.Fatalf("zero value Total/Reasons = %d/%v, want 0/empty", skipped.Total(), skipped.Reasons())
	}
	skipped.Add(SkipReasonUnknownScope, "b")
	skipped.Add(SkipReasonNoActiveGeneration, "a")
	skipped.Add(SkipReasonNoActiveGeneration, "c")

	if got, want := skipped.Total(), 3; got != want {
		t.Fatalf("Total() = %d, want %d", got, want)
	}
	if got, want := strings.Join(skipped.Reasons(), ","), SkipReasonNoActiveGeneration+","+SkipReasonUnknownScope; got != want {
		t.Fatalf("Reasons() = %q, want sorted %q", got, want)
	}
}
