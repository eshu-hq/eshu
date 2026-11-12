// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// stubBatchClaimDB is an empty queue: every claim returns zero rows.
type stubBatchClaimDB struct{}

func (stubBatchClaimDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return &fakeRows{}, nil
}

func (stubBatchClaimDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

// TestClaimBatchDoesNotRecordBatchClaimSize is the #10565 F4 storage-side
// gate: BatchClaimSize is a service-layer emission (runBatchConcurrent
// records it once per ClaimBatch with queue=reducer, including the
// early-return conflict path), so the storage claim must not record it —
// a second recording here would double-count every production claim. It
// fails if ClaimBatch emits the histogram.
func TestClaimBatchDoesNotRecordBatchClaimSize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("batch-claim-size-proof"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}

	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	queue := ReducerQueue{
		database:      stubBatchClaimDB{},
		LeaseOwner:    "batch-claim-size-proof",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
		Instruments:   instruments,
	}
	// Non-vacuous: the histogram this test watches must exist, or its
	// absence would pass for the wrong reason.
	if instruments.BatchClaimSize == nil {
		t.Fatal("BatchClaimSize histogram is not registered")
	}
	intents, err := queue.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("ClaimBatch: %v", err)
	}
	if len(intents) != 0 {
		t.Fatalf("ClaimBatch on an empty queue = %d intents, want 0", len(intents))
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, scope := range collected.ScopeMetrics {
		for _, metric := range scope.Metrics {
			histogram, ok := metric.Data.(metricdata.Histogram[int64])
			if !ok {
				continue
			}
			if len(metric.Name) < len("batch_claim_size") ||
				metric.Name[len(metric.Name)-len("batch_claim_size"):] != "batch_claim_size" {
				continue
			}
			t.Fatalf("storage ClaimBatch recorded BatchClaimSize (Count=%d): the service layer owns that emission (F4)",
				histogram.DataPoints[0].Count)
		}
	}
}
