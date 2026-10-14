// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"sort"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// blockDurationMetric is the per-block histogram of the whole pass (#8082).
const blockDurationMetric = "eshu_dp_deferred_backfill_block_duration_seconds"

// collectBlockDurationPoints returns, per block label value, the number of
// observations eshu_dp_deferred_backfill_block_duration_seconds recorded. It
// fails when a data point carries any attribute other than block, because the
// closed one-label shape is the cardinality contract.
func collectBlockDurationPoints(t *testing.T, reader *sdkmetric.ManualReader) map[string]uint64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	points := map[string]uint64{}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, record := range scopeMetrics.Metrics {
			if record.Name != blockDurationMetric {
				continue
			}
			hist, ok := record.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s data = %T, want metricdata.Histogram[float64]", record.Name, record.Data)
			}
			for _, dp := range hist.DataPoints {
				if got := dp.Attributes.Len(); got != 1 {
					t.Fatalf("%s data point has %d attributes, want exactly the block label", record.Name, got)
				}
				block, found := dp.Attributes.Value("block")
				if !found {
					t.Fatalf("%s data point carries no block attribute", record.Name)
				}
				points[block.AsString()] += dp.Count
			}
		}
	}
	return points
}

// TestRunAllRelationshipEvidenceRecordsOneBlockDurationPointPerBlock pins one
// histogram observation per in-pass block per pass, labeled only by block, with
// exactly the closed in-pass values. The ninth value, reopen, records from the
// root package and is pinned there.
func TestRunAllRelationshipEvidenceRecordsOneBlockDurationPointPerBlock(t *testing.T) {
	t.Parallel()

	store, _ := newBlockPassStore(t)
	reader, instruments := fanInMetricHarness(t)

	if _, err := store.RunAllRelationshipEvidence(context.Background(), nil, instruments); err != nil {
		t.Fatalf("RunAllRelationshipEvidence() error = %v, want nil", err)
	}

	got := collectBlockDurationPoints(t, reader)
	wantBlocks := []string{
		"catalog_load", "params_build", "partition_list", "memo_gate",
		"fact_load", "discover", "write_batches", "fanin",
	}
	var gotBlocks []string
	for block, count := range got {
		gotBlocks = append(gotBlocks, block)
		if count != 1 {
			t.Errorf("block %q observations = %d, want 1 per pass", block, count)
		}
	}
	sort.Strings(gotBlocks)
	want := append([]string(nil), wantBlocks...)
	sort.Strings(want)
	if len(gotBlocks) != len(want) {
		t.Fatalf("block label set = %v, want %v", gotBlocks, want)
	}
	for i := range want {
		if gotBlocks[i] != want[i] {
			t.Fatalf("block label set = %v, want %v", gotBlocks, want)
		}
	}
}
