// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"slices"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// TestDeferredBackfillBlockContract pins the closed block value set the
// eshu_dp_deferred_backfill_block_duration_seconds label may carry (#8082), the
// single block dimension key, and that NewInstruments registers the histogram.
func TestDeferredBackfillBlockContract(t *testing.T) {
	t.Parallel()

	got := []string{
		DeferredBackfillBlockCatalogLoad,
		DeferredBackfillBlockParamsBuild,
		DeferredBackfillBlockPartitionList,
		DeferredBackfillBlockMemoGate,
		DeferredBackfillBlockFactLoad,
		DeferredBackfillBlockDiscover,
		DeferredBackfillBlockWriteBatches,
		DeferredBackfillBlockFanIn,
		DeferredBackfillBlockReopen,
	}
	want := []string{
		"catalog_load", "params_build", "partition_list", "memo_gate",
		"fact_load", "discover", "write_batches", "fanin", "reopen",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("deferred backfill block values = %v, want the closed set %v", got, want)
	}
	logNames := []string{
		DeferredBackfillBlockLogCompleted,
		DeferredBackfillBlockLogFailed,
		DeferredBackfillBlockLogDegraded,
	}
	wantLogNames := []string{
		"deferred_backfill_block_completed",
		"deferred_backfill_block_failed",
		"deferred_backfill_block_degraded",
	}
	if !slices.Equal(logNames, wantLogNames) {
		t.Fatalf("deferred backfill block log line names = %v, want the closed set %v", logNames, wantLogNames)
	}
	if MetricDimensionBlock != "block" {
		t.Fatalf("MetricDimensionBlock = %q, want %q", MetricDimensionBlock, "block")
	}
	if !slices.Contains(MetricDimensionKeys(), MetricDimensionBlock) {
		t.Fatalf("MetricDimensionKeys() missing %q", MetricDimensionBlock)
	}

	provider := sdkmetric.NewMeterProvider()
	instruments, err := NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	if instruments.DeferredBackfillBlockDuration == nil {
		t.Fatal("DeferredBackfillBlockDuration histogram not registered")
	}
}
