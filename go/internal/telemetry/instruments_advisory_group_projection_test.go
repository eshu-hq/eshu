// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// advisoryGroupSample is one collected data point reduced to the label set
// and value the operator reads: the instrument name, its attributes, and the
// summed value (counter value, or histogram sum).
type advisoryGroupSample struct {
	name  string
	attrs attribute.Set
	value float64
}

func collectAdvisoryGroupSamples(t *testing.T, reader *sdkmetric.ManualReader) []advisoryGroupSample {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var out []advisoryGroupSample
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, p := range data.DataPoints {
					out = append(out, advisoryGroupSample{m.Name, p.Attributes, float64(p.Value)})
				}
			case metricdata.Histogram[float64]:
				for _, p := range data.DataPoints {
					out = append(out, advisoryGroupSample{m.Name, p.Attributes, p.Sum})
				}
			}
		}
	}
	return out
}

func advisoryGroupSampleValue(samples []advisoryGroupSample, name string, want ...attribute.KeyValue) (float64, bool) {
	wantSet := attribute.NewSet(want...)
	for _, s := range samples {
		if s.name == name && s.attrs.Equals(&wantSet) {
			return s.value, true
		}
	}
	return 0, false
}

// TestRecordAdvisoryGroupWatermarkMarkerLoss pins the #11089 counter: each
// closed reason lands as its own sample labelled with the domain, the prior
// failure class, and that reason, and repeated verdicts accumulate.
func TestRecordAdvisoryGroupWatermarkMarkerLoss(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	inst, err := NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	ctx := context.Background()
	const name = "eshu_dp_advisory_group_projection_watermark_deferral_marker_missing_total"

	inst.RecordAdvisoryGroupWatermarkMarkerLoss(ctx, "advisory_group_projection", "wait_not_ready", AdvisoryGroupWatermarkMarkerLossMissing)
	inst.RecordAdvisoryGroupWatermarkMarkerLoss(ctx, "advisory_group_projection", "wait_not_ready", AdvisoryGroupWatermarkMarkerLossMissing)
	inst.RecordAdvisoryGroupWatermarkMarkerLoss(ctx, "advisory_group_projection", "wait_not_ready", AdvisoryGroupWatermarkMarkerLossUnparsable)
	inst.RecordAdvisoryGroupWatermarkMarkerLoss(ctx, "advisory_group_projection", "wait_stale", AdvisoryGroupWatermarkMarkerLossFutureDated)

	samples := collectAdvisoryGroupSamples(t, reader)
	for _, tc := range []struct {
		class, reason string
		want          float64
	}{
		{"wait_not_ready", AdvisoryGroupWatermarkMarkerLossMissing, 2},
		{"wait_not_ready", AdvisoryGroupWatermarkMarkerLossUnparsable, 1},
		{"wait_stale", AdvisoryGroupWatermarkMarkerLossFutureDated, 1},
	} {
		got, ok := advisoryGroupSampleValue(samples, name,
			AttrDomain("advisory_group_projection"), AttrFailureClass(tc.class), AttrReason(tc.reason))
		if !ok || got != tc.want {
			t.Fatalf("%s{class=%s,reason=%s} = %v (found=%v), want %v", name, tc.class, tc.reason, got, ok, tc.want)
		}
	}
	if _, ok := advisoryGroupSampleValue(samples, name,
		AttrDomain("advisory_group_projection"), AttrFailureClass("wait_stale"), AttrReason(AdvisoryGroupWatermarkMarkerLossMissing)); ok {
		t.Fatal("a class/reason pair that never fired must not appear")
	}
}

// TestAdvisoryGroupRecordHelpersTolerateMissingInstruments pins the sibling
// contract: a nil receiver or an Instruments with no counters registered
// records nothing and never panics.
func TestAdvisoryGroupRecordHelpersTolerateMissingInstruments(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, inst := range map[string]*Instruments{"nil": nil, "unregistered": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			inst.RecordAdvisoryGroupWatermarkMarkerLoss(ctx, "d", "c", AdvisoryGroupWatermarkMarkerLossMissing)
			inst.RecordAdvisoryGroupReconciled(ctx, "d", AdvisoryGroupProjectionOutcomeProjected, 1, 1, 1)
			inst.RecordAdvisoryGroupMemberUnionOversized(ctx, "d")
			inst.RecordAdvisoryGroupEnqueueFailure(ctx, "d")
			inst.RecordAdvisoryGroupReconcileDuration(ctx, "d", 1)
			inst.RecordAdvisoryGroupLag(ctx, "d", 1)
			inst.RecordAdvisoryGroupWatermarkWait(ctx, "d", "c", "1", 1, 1, 1, 1)
		})
	}
}

// TestAdvisoryGroupRecordHelpersRecordLabelledSamples drives every
// advisory-group Record helper through a real meter and asserts the label
// set and value each one writes, including the drop of negative (clock
// skew) wait and lag samples.
func TestAdvisoryGroupRecordHelpersRecordLabelledSamples(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	inst, err := NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	ctx := context.Background()
	const domain = "advisory_group_projection"
	pre := "eshu_dp_advisory_group_projection_"

	inst.RecordAdvisoryGroupReconciled(ctx, domain, AdvisoryGroupProjectionOutcomeProjected, 3, 2, 1)
	inst.RecordAdvisoryGroupReconciled(ctx, domain, AdvisoryGroupProjectionOutcomeFailed, 0, 0, 0)
	inst.RecordAdvisoryGroupMemberUnionOversized(ctx, domain)
	inst.RecordAdvisoryGroupEnqueueFailure(ctx, "supply_chain_impact")
	inst.RecordAdvisoryGroupReconcileDuration(ctx, domain, 0.25)
	inst.RecordAdvisoryGroupLag(ctx, domain, 12)
	inst.RecordAdvisoryGroupLag(ctx, domain, -1) // clock skew: dropped
	inst.RecordAdvisoryGroupWatermarkWait(ctx, domain, "wait_not_ready", "1", 2640, 0, 6300, 3600)
	inst.RecordAdvisoryGroupWatermarkWait(ctx, domain, "wait_stale", "2", -1, -1, -1, -1) // skewed signals dropped, verdict counted

	samples := collectAdvisoryGroupSamples(t, reader)
	dom := AttrDomain(domain)
	for _, tc := range []struct {
		name  string
		attrs []attribute.KeyValue
		want  float64
	}{
		{pre + "groups_total", []attribute.KeyValue{dom, AttrOutcome(AdvisoryGroupProjectionOutcomeProjected)}, 1},
		{pre + "groups_total", []attribute.KeyValue{dom, AttrOutcome(AdvisoryGroupProjectionOutcomeFailed)}, 1},
		{pre + "edges_upserted_total", []attribute.KeyValue{dom}, 3},
		{pre + "edges_retracted_total", []attribute.KeyValue{dom}, 2},
		{pre + "nodes_deleted_total", []attribute.KeyValue{dom}, 1},
		{pre + "member_union_oversized_total", []attribute.KeyValue{dom}, 1},
		{pre + "enqueue_failures_total", []attribute.KeyValue{AttrDomain("supply_chain_impact")}, 1},
		{pre + "reconcile_duration_seconds", []attribute.KeyValue{dom}, 0.25},
		{pre + "lag_seconds", []attribute.KeyValue{dom}, 12},
		{pre + "first_claim_lag_seconds", []attribute.KeyValue{dom}, 2640},
		{pre + "post_claim_deferral_seconds", []attribute.KeyValue{dom}, 0},
		{pre + "watermark_age_seconds", []attribute.KeyValue{dom}, 6300},
		{pre + "watermark_gap_seconds", []attribute.KeyValue{dom}, 3600},
		{pre + "watermark_waits_total", []attribute.KeyValue{dom, AttrFailureClass("wait_not_ready"), AttrAttempt("1")}, 1},
		{pre + "watermark_waits_total", []attribute.KeyValue{dom, AttrFailureClass("wait_stale"), AttrAttempt("2")}, 1},
	} {
		got, ok := advisoryGroupSampleValue(samples, tc.name, tc.attrs...)
		if !ok || got != tc.want {
			t.Errorf("%s%v = %v (found=%v), want %v", tc.name, tc.attrs, got, ok, tc.want)
		}
	}
}
