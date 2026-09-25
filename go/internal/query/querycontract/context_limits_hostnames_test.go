// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"fmt"
	"slices"
	"testing"
)

func hostnameEntrypointRows(kind string, n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{"target": fmt.Sprintf("%s-%03d.example.test", kind, i)})
	}
	return rows
}

// TestWorkloadContextResultLimitsCapsHostnamesAndEntrypoints proves the
// hostname and entrypoint lists are cut to ContextStoryItemLimit in place, the
// pre-cut totals are reported, and the cut is disclosed on result_limits and
// on partial_reasons (#7169).
func TestWorkloadContextResultLimitsCapsHostnamesAndEntrypoints(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"hostnames":   hostnameEntrypointRows("host", 671),
		"entrypoints": hostnameEntrypointRows("entry", 671),
	}

	limits := WorkloadContextResultLimits(ctx, "workload:svc", "context")

	if got, want := len(MapSliceValue(ctx, "hostnames")), ContextStoryItemLimit; got != want {
		t.Fatalf("hostnames len = %d, want %d", got, want)
	}
	if got, want := len(MapSliceValue(ctx, "entrypoints")), ContextStoryItemLimit; got != want {
		t.Fatalf("entrypoints len = %d, want %d", got, want)
	}
	if got, want := IntVal(limits, "hostname_count"), 671; got != want {
		t.Fatalf("result_limits.hostname_count = %d, want %d (total before the cut)", got, want)
	}
	if got, want := IntVal(limits, "entrypoint_count"), 671; got != want {
		t.Fatalf("result_limits.entrypoint_count = %d, want %d (total before the cut)", got, want)
	}
	if !BoolVal(limits, "truncated") {
		t.Fatal("result_limits.truncated = false next to a cut list, want true")
	}
	reasons := ContextPartialReasons(ctx)
	for _, want := range []string{"hostnames_truncated", "entrypoints_truncated"} {
		if !slices.Contains(reasons, want) {
			t.Fatalf("partial_reasons = %#v, want %q", reasons, want)
		}
	}
}

// TestWorkloadContextResultLimitsLeavesWithinCapListsAlone proves a list at or
// under the cap is returned whole, with counts and no truncation marker.
func TestWorkloadContextResultLimitsLeavesWithinCapListsAlone(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"hostnames":   hostnameEntrypointRows("host", ContextStoryItemLimit),
		"entrypoints": hostnameEntrypointRows("entry", 3),
	}

	limits := WorkloadContextResultLimits(ctx, "workload:svc", "context")

	if got, want := len(MapSliceValue(ctx, "hostnames")), ContextStoryItemLimit; got != want {
		t.Fatalf("hostnames len = %d, want %d (at cap is not cut)", got, want)
	}
	if got, want := IntVal(limits, "hostname_count"), ContextStoryItemLimit; got != want {
		t.Fatalf("result_limits.hostname_count = %d, want %d", got, want)
	}
	if got, want := IntVal(limits, "entrypoint_count"), 3; got != want {
		t.Fatalf("result_limits.entrypoint_count = %d, want %d", got, want)
	}
	if BoolVal(limits, "truncated") {
		t.Fatal("result_limits.truncated = true with nothing cut, want false")
	}
	if reasons := ContextPartialReasons(ctx); len(reasons) != 0 {
		t.Fatalf("partial_reasons = %#v, want none", reasons)
	}
}
