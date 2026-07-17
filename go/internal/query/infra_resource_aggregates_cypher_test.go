// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

// TestMergeInfraResourceAggregateBucketsSkipsZeroCountRows guards the exact
// equivalence with the old whole-graph aggregate: an empty per-label branch
// (empty label, or filtered to nothing) emits one grouped row with a null
// bucket and count 0, which must not create a spurious "" bucket the old
// `MATCH (n) WHERE (n:A OR ...)` read never produced. Real buckets (count >= 1)
// from different label branches sharing a value must still be summed.
func TestMergeInfraResourceAggregateBucketsSkipsZeroCountRows(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"bucket": "aws", "bucket_count": int64(40)},
		{"bucket": nil, "bucket_count": int64(0)},    // empty CloudResource branch
		{"bucket": "", "bucket_count": int64(0)},     // empty branch, blank bucket
		{"bucket": "aws", "bucket_count": int64(2)},  // another label, same provider
		{"bucket": "gcp", "bucket_count": int64(11)}, // distinct provider
	}
	merged := mergeInfraResourceAggregateBuckets(rows)

	if _, ok := merged[""]; ok {
		t.Fatalf("merged buckets contain a spurious empty bucket from zero-count rows: %#v", merged)
	}
	if merged["aws"] != 42 {
		t.Fatalf("merged[aws] = %d, want 42 (summed across contributing label branches)", merged["aws"])
	}
	if merged["gcp"] != 11 {
		t.Fatalf("merged[gcp] = %d, want 11", merged["gcp"])
	}
	if len(merged) != 2 {
		t.Fatalf("merged bucket count = %d, want 2 (aws, gcp); got %#v", len(merged), merged)
	}
}

// TestSortedInfraResourceAggregateBucketsOrder proves the Go-side ordering
// matches the ORDER BY bucket_count DESC, bucket the graph query used before
// aggregation moved application-side.
func TestSortedInfraResourceAggregateBucketsOrder(t *testing.T) {
	t.Parallel()

	sorted := sortedInfraResourceAggregateBuckets(map[string]int{
		"beta": 5, "alpha": 5, "gamma": 9, "delta": 1,
	})
	got := make([]string, 0, len(sorted))
	for _, row := range sorted {
		got = append(got, row.Bucket)
	}
	// gamma(9), then the 5-count tie broken alphabetically (alpha, beta), then delta(1).
	if want := "gamma,alpha,beta,delta"; strings.Join(got, ",") != want {
		t.Fatalf("bucket order = %v, want %s (count desc, then bucket asc tie-break)", got, want)
	}
}
