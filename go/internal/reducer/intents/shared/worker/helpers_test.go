// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// --- Test stubs ---
//
// These mirror the reducer root's stubSharedIntentReader/
// partitionKeyForTestPartition (still defined there for the root's own
// remaining tests) so worker's own tests do not need to import the reducer
// root, which this package must never do.

type stubSharedIntentReader struct {
	pending        []sharedintent.Row
	completedIDs   []string
	limitRequests  []int
	limitResponder func(limit int) []sharedintent.Row
}

func (s *stubSharedIntentReader) ListPendingDomainIntents(_ context.Context, _ string, limit int) ([]sharedintent.Row, error) {
	s.limitRequests = append(s.limitRequests, limit)
	if s.limitResponder != nil {
		return s.limitResponder(limit), nil
	}
	if limit > 0 && len(s.pending) > limit {
		return append([]sharedintent.Row(nil), s.pending[:limit]...), nil
	}
	return append([]sharedintent.Row(nil), s.pending...), nil
}

func (s *stubSharedIntentReader) MarkIntentsCompleted(_ context.Context, intentIDs []string, _ time.Time) error {
	s.completedIDs = append(s.completedIDs, intentIDs...)
	return nil
}

func partitionKeyForTestPartition(t *testing.T, wantPartition, partitionCount int, prefix string) string {
	t.Helper()

	for i := 0; i < 10_000; i++ {
		key := prefix + "-" + time.Date(2026, time.April, 17, 0, 0, i%60, 0, time.UTC).Format("150405") + "-" + string(rune('a'+(i%26)))
		got, err := sharedintent.PartitionForKey(key, partitionCount)
		if err != nil {
			t.Fatalf("PartitionForKey(%q) error = %v", key, err)
		}
		if got == wantPartition {
			return key
		}
	}
	t.Fatalf("could not find partition key for partition %d of %d", wantPartition, partitionCount)
	return ""
}
