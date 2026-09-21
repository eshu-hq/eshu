// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// reducerAckSplitBenchBatch builds the shape the batch acker actually flushes:
// one ack per distinct work item, mixed across the three ack statements, with
// no duplicate. That is the hot path -- a lease reclaim duplicate (#6162) is
// rare, so the question this benchmark answers is whether resolving duplicates
// cost the common case anything.
func reducerAckSplitBenchBatch(size int) []reducer.Intent {
	claimedAt := time.Date(2026, time.September, 21, 15, 11, 48, 0, time.UTC)
	domains := []reducer.Domain{
		reducer.DomainGCPResourceMaterialization,
		reducer.DomainContainerImageIdentity,
		reducer.DomainCICDRunCorrelation,
		reducer.DomainOwnership,
	}
	intents := make([]reducer.Intent, 0, size)
	for index := 0; index < size; index++ {
		at := claimedAt.Add(time.Duration(index) * time.Millisecond)
		intents = append(intents, reducer.Intent{
			IntentID:     fmt.Sprintf("reducer_bench_6162_work_item_%04d", index),
			Domain:       domains[index%len(domains)],
			AttemptCount: 1,
			ClaimEpoch:   int64(index % 3),
			ClaimedAt:    &at,
		})
	}
	return intents
}

func BenchmarkSplitReducerAckBatchIntents(b *testing.B) {
	for _, size := range []int{16, 64, 256} {
		b.Run(fmt.Sprintf("batch_%d", size), func(b *testing.B) {
			intents := reducerAckSplitBenchBatch(size)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				split, err := splitReducerAckBatchIntents(intents)
				if err != nil {
					b.Fatalf("split: %v", err)
				}
				if len(split.target)+len(split.cicd)+len(split.unrelated) != size {
					b.Fatalf("split dropped intents: got %d, want %d",
						len(split.target)+len(split.cicd)+len(split.unrelated), size)
				}
			}
		})
	}
}
