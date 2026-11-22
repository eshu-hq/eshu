// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestAckPostCommitWakeTimesOutAndMeters pins the #11228 bound: a post-commit
// re-wake stuck behind locks ends at its timeout, returns nothing to the
// committed Ack, runs under a deadline detached from a canceled caller, and
// counts outcome=error on the ack_post_commit wake site.
func TestAckPostCommitWakeTimesOutAndMeters(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := &postCommitWakeFakeDB{block: true}
	instruments, reader := newEnqueueInstruments(t)
	queue := ProjectorQueue{database: fake, Instruments: instruments}
	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-apc", CollectorKind: scope.CollectorKind(ackParkRaceCollectorKind)},
		Generation: scope.ScopeGeneration{GenerationID: "gen-apc"},
	}
	start := time.Now()
	queue.wakeAckWaitsAfterCommitWithin(ctx, work, time.Now().UTC(), 50*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("post-commit re-wake took %v, want it bounded by its timeout", elapsed)
	}
	if !fake.sawDeadline {
		t.Fatal("post-commit re-wake statement ran without a deadline")
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	assertCounterPresentWithLabels(t, rm, "eshu_dp_reducer_waiting_wake_total",
		map[string]string{"wake_site": "ack_post_commit", "outcome": "error"})
	if AckPostCommitWakeTimeout <= 0 || AckPostCommitWakeTimeout >= DefaultWaitingAuditInterval {
		t.Fatalf("AckPostCommitWakeTimeout = %v, want positive and below the %v audit interval",
			AckPostCommitWakeTimeout, DefaultWaitingAuditInterval)
	}
}
