// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestServiceBatchClaimRecordsSingleBatchClaimSize is the #10565 F4
// service-side gate: runBatchConcurrent records eshu_dp_reducer_batch_claim_size
// exactly once per ClaimBatch call with queue=reducer (one two-item batch
// plus the terminal empty poll, whose Wait fails closed), so claim admission
// stays observable on the same boundaries the before/after comparison uses
// without the storage layer double-counting it. It fails if the service
// records zero or two observations for one claim.
func TestServiceBatchClaimRecordsSingleBatchClaimSize(t *testing.T) {
	t.Parallel()

	instruments, reader := batchTelemetry(t)
	source := &fakeBatchWorkSource{intents: makeTestIntents(2)}
	service := Service{
		PollInterval:   time.Millisecond,
		WorkSource:     source,
		Executor:       &countingExecutor{},
		WorkSink:       &fakeBatchWorkSink{},
		Workers:        2,
		BatchClaimSize: 2,
		Wait:           func(context.Context, time.Duration) error { return context.Canceled },
		Instruments:    instruments,
	}
	if err := service.runMainLoop(context.Background()); err != nil {
		t.Fatalf("runMainLoop() error = %v", err)
	}
	claims := len(source.claimLimits())
	if claims == 0 {
		t.Fatal("fake source saw no ClaimBatch calls")
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var (
		count uint64
		sum   int64
	)
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_reducer_batch_claim_size" {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[int64])
			if !ok {
				t.Fatalf("batch claim size data = %T, want Histogram[int64]", m.Data)
			}
			for _, point := range histogram.DataPoints {
				value, ok := point.Attributes.Value(attribute.Key("queue"))
				if !ok || value.AsString() != "reducer" {
					t.Fatalf("batch claim size point missing queue=reducer: %+v", point.Attributes)
				}
				count += point.Count
				sum += int64(point.Sum)
			}
		}
	}
	if count != uint64(claims) {
		t.Fatalf("batch claim size observations = %d, want one per ClaimBatch call (%d)", count, claims)
	}
	if sum != 2 {
		t.Fatalf("batch claim size sum = %d, want 2 (one two-item batch plus the terminal empty poll)", sum)
	}
}
