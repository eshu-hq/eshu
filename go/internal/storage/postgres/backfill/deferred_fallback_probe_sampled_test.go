// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// TestDeferredRelationshipKeyFallbackProbeIsSampled is the #10971 P1
// regression: the per-partition fallback probe (a partition-wide COUNT
// measured at ~10% of a partition load) must not run on every partition
// load. Exactly the sampled loads issue the probe query; every load still
// runs the main query.
func TestDeferredRelationshipKeyFallbackProbeIsSampled(t *testing.T) {
	fake := &fakeExecQueryer{
		deferredFactsByScope: map[string][][]any{"s": {}},
	}
	params := DeferredScopedFactQueryParams{
		NonRepoIDLike:      array.StringArray{"%svc%"},
		RepoIDValues:       array.StringArray{"github.com/org/svc"},
		RepoIDReferenceKey: array.StringArray{"github.com/org/svc"},
		NonRepoIDRawAnchors: array.StringArray{
			"svc",
		},
	}
	const loads = 65
	for i := 0; i < loads; i++ {
		if _, err := loadDeferredScopedRelationshipFactsForPartition(context.Background(), fake, params, "s", "g"); err != nil {
			t.Fatalf("load %d: %v", i, err)
		}
	}
	var probes, mains int
	for _, q := range fake.queries {
		switch q.query {
		case DeferredRelationshipKeyFallbackProbeSQL:
			probes++
		case ListDeferredScopedRelationshipFactRecordsQuery:
			mains++
		}
	}
	if mains != loads {
		t.Fatalf("main queries = %d, want %d (every load runs the load)", mains, loads)
	}
	if probes != 2 {
		t.Fatalf("probe queries = %d, want 2 (first load plus every 64th)", probes)
	}
}
