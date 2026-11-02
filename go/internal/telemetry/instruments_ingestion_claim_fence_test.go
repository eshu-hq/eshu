// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"slices"
	"sort"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestIngestionClaimFenceSpanRegistered pins the claim-plane commit fence
// span name (#9376 D9) in the frozen span registry.
func TestIngestionClaimFenceSpanRegistered(t *testing.T) {
	t.Parallel()

	if SpanIngestionCommitClaimFence != "ingestion.commit.claim_fence" {
		t.Fatalf("SpanIngestionCommitClaimFence = %q", SpanIngestionCommitClaimFence)
	}
	if !slices.Contains(SpanNames(), SpanIngestionCommitClaimFence) {
		t.Fatalf("SpanNames() missing %q", SpanIngestionCommitClaimFence)
	}
}

// TestIngestionClaimFenceInstrumentsExportBoundedLabels records through the
// registered instruments and reads them back: the rejection counter carries
// only reason, the re-pin counter only outcome, the slack histogram no label
// and seconds as its unit. No claim, generation, or scope id is a label.
func TestIngestionClaimFenceInstrumentsExportBoundedLabels(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("claim-fence-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	ctx := context.Background()
	inst.IngestionClaimFenceRejections.Add(ctx, 1, metric.WithAttributes(attribute.String(MetricDimensionReason, "fence_token")))
	inst.IngestionCommitRepins.Add(ctx, 1, metric.WithAttributes(attribute.String(MetricDimensionOutcome, "ok")))
	inst.IngestionCommitPinSlack.Record(ctx, 42.5)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	found := map[string]metricdata.Metrics{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			found[m.Name] = m
		}
	}
	for name, wantKeys := range map[string][]string{
		"eshu_dp_ingestion_claim_fence_rejections_total": {"reason"},
		"eshu_dp_ingestion_commit_repins_total":          {"outcome"},
	} {
		m, ok := found[name]
		if !ok {
			t.Fatalf("metric %s not exported", name)
		}
		sum, ok := m.Data.(metricdata.Sum[int64])
		if !ok || !sum.IsMonotonic || len(sum.DataPoints) != 1 {
			t.Fatalf("%s data = %#v, want one monotonic int64 sum point", name, m.Data)
		}
		var keys []string
		for _, kv := range sum.DataPoints[0].Attributes.ToSlice() {
			keys = append(keys, string(kv.Key))
		}
		sort.Strings(keys)
		if !slices.Equal(keys, wantKeys) {
			t.Fatalf("%s label keys = %v, want %v", name, keys, wantKeys)
		}
	}
	slack, ok := found["eshu_dp_ingestion_commit_pin_slack_seconds"]
	if !ok {
		t.Fatal("eshu_dp_ingestion_commit_pin_slack_seconds not exported")
	}
	if slack.Unit != "s" {
		t.Fatalf("slack unit = %q, want s", slack.Unit)
	}
	hist, ok := slack.Data.(metricdata.Histogram[float64])
	if !ok || len(hist.DataPoints) != 1 || hist.DataPoints[0].Attributes.Len() != 0 {
		t.Fatalf("slack data = %#v, want one unlabeled float64 histogram point", slack.Data)
	}
}
