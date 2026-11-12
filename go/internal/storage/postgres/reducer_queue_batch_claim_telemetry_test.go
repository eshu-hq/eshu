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

// TestClaimBatchRecordsBatchClaimSize is the #10565 claim-telemetry gate:
// every batch claim records the admitted batch size on the shared
// BatchClaimSize histogram, so claim admission stays observable on the same
// metric boundaries the before/after comparison uses. It fails until
// ClaimBatch records.
func TestClaimBatchRecordsBatchClaimSize(t *testing.T) {
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
			if len(histogram.DataPoints) != 1 {
				t.Fatalf("BatchClaimSize data points = %d, want 1", len(histogram.DataPoints))
			}
			point := histogram.DataPoints[0]
			if point.Count != 1 || point.Sum != 0 {
				t.Fatalf("BatchClaimSize point = {Count:%d Sum:%d}, want {Count:1 Sum:0}",
					point.Count, point.Sum)
			}
			return
		}
	}
	t.Fatal("BatchClaimSize histogram recorded nothing for the claim")
}
