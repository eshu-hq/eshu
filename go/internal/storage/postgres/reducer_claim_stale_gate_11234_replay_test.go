// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"
)

// TestStaleGate11234ClaimReplayLive is the #11234 built-binary replay: on
// the QA shape (12,236 scopes, 10,000 pending/retrying reducer rows) it
// EXPLAINs the whole batch claim five times (rolled back) and then drives
// ReducerQueue.ClaimBatch through the production code path, logging
// buffers and time per claim. Run the test binary built at the base commit
// and at the change on the same seed for the before/after pair; the test
// itself pins nothing. Operator-gated on ESHU_STALE_GATE_11234_REPLAY=1.
func TestStaleGate11234ClaimReplayLive(t *testing.T) {
	if os.Getenv("ESHU_STALE_GATE_11234_REPLAY") != "1" {
		t.Skip("set ESHU_STALE_GATE_11234_REPLAY=1 to run the #11234 built-binary claim replay")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	for _, staleRows := range []int{0, 200} {
		t.Run(fmt.Sprintf("stale=%d", staleRows), func(t *testing.T) {
			sqlConn, conn := openStaleGate11234Live(ctx, t, fmt.Sprintf("replay%d", staleRows), true)
			shape := staleGate11234QAShape()
			shape.staleRows = staleRows
			seedStaleGate11234(ctx, t, conn, shape)
			now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
			for _, domains := range []any{nil, staleGate11234Domains[:2]} {
				args := []any{now, domains, "replay-11234", now.Add(time.Minute), false, 0, 0, 4}
				var ms []float64
				var buffers []int64
				for range 5 {
					plan := explainStaleGate11234(ctx, t, sqlConn, nil, claimReducerWorkBatchQuery, args...)
					ms = append(ms, plan.execMS)
					buffers = append(buffers, plan.buffers)
				}
				sort.Float64s(ms)
				sort.Slice(buffers, func(i, j int) bool { return buffers[i] < buffers[j] })
				t.Logf("stale=%d domains=%v batch claim EXPLAIN: median %.1f ms, median %d buffers (min %.1f ms, %d buffers)",
					staleRows, domains != nil, ms[2], buffers[2], ms[0], buffers[0])
			}
			queue := ReducerQueue{
				database:      conn,
				LeaseOwner:    "replay-11234",
				LeaseDuration: time.Minute,
				Now:           func() time.Time { return now },
			}
			var latencies []float64
			claimed := 0
			for range 40 {
				start := time.Now()
				intents, err := queue.ClaimBatch(ctx, 4)
				latencies = append(latencies, time.Since(start).Seconds()*1000)
				if err != nil {
					t.Fatalf("ClaimBatch: %v", err)
				}
				claimed += len(intents)
			}
			if claimed == 0 {
				t.Fatal("replay claimed nothing; the seed is not claimable")
			}
			t.Logf("stale=%d ClaimBatch(4) x40: claimed=%d p50=%.1f ms p95=%.1f ms",
				staleRows, claimed, percentileSorted(latencies, 0.50), percentileSorted(latencies, 0.95))
		})
	}
}
