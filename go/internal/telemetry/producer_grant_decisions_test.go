// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"slices"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

func newGrantRecorderForTest(t testing.TB) (*ProducerGrantDecisionRecorder, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	if inst.ProducerGrantDecisions == nil {
		t.Fatal("ProducerGrantDecisions counter was not registered")
	}
	return NewProducerGrantDecisionRecorder(inst), reader
}

// grantDecisionPoints collects every data point of the decision counter as a
// label map plus its value.
func grantDecisionPoints(t testing.TB, reader *sdkmetric.ManualReader) []map[string]string {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var points []map[string]string
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != contract.MetricProducerGrantDecisions {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want Sum[int64]", m.Data)
			}
			for _, dp := range sum.DataPoints {
				labels := map[string]string{"value": string(rune('0' + dp.Value))}
				for _, kv := range dp.Attributes.ToSlice() {
					labels[string(kv.Key)] = kv.Value.AsString()
				}
				points = append(points, labels)
			}
		}
	}
	return points
}

// TestProducerGrantDecisionRecorderLabelsAreClosed proves the counter carries
// exactly decision, stage, reason, and fact_kind, and never a producer id, so
// operator-configured identifiers cannot blow up cardinality.
func TestProducerGrantDecisionRecorderLabelsAreClosed(t *testing.T) {
	t.Parallel()

	recorder, reader := newGrantRecorderForTest(t)
	ctx := context.Background()
	recorder.Record(ctx, contract.ProducerGrantStageEmission, contract.ProducerGrantDecisionDeny, contract.ProducerGrantReasonRevoked, "aws_resource")
	recorder.Record(ctx, contract.ProducerGrantStageInstall, contract.ProducerGrantDecisionAllow, contract.ProducerGrantReasonGranted, "aws_resource")
	recorder.Record(ctx, contract.ProducerGrantStageInstall, contract.ProducerGrantDecisionAllow, contract.ProducerGrantReasonGranted, "aws_resource")

	points := grantDecisionPoints(t, reader)
	if len(points) != 2 {
		t.Fatalf("data points = %v, want 2 distinct label sets", points)
	}
	allowed := []string{"decision", "stage", "reason", "fact_kind", "value"}
	for _, point := range points {
		for key := range point {
			if !slices.Contains(allowed, key) {
				t.Fatalf("label key %q outside allowlist %v", key, allowed)
			}
		}
	}
	found := map[string]string{}
	for _, point := range points {
		found[point["decision"]+"/"+point["stage"]+"/"+point["reason"]+"/"+point["fact_kind"]] = point["value"]
	}
	if found["deny/emission/revoked/aws_resource"] != "1" {
		t.Fatalf("deny point missing or wrong: %v", found)
	}
	if found["allow/install/granted/aws_resource"] != "2" {
		t.Fatalf("allow point missing or wrong (want count 2): %v", found)
	}
}

// TestProducerGrantDecisionRecorderFoldsUnboundedValues proves an out-of-set
// stage, decision, reason, or non-core fact kind folds to a fixed value
// instead of creating a new time series per caller-supplied string.
func TestProducerGrantDecisionRecorderFoldsUnboundedValues(t *testing.T) {
	t.Parallel()

	recorder, reader := newGrantRecorderForTest(t)
	recorder.Record(context.Background(), "op-supplied-stage", "op-supplied-decision", "op-supplied-reason", "dev.example.operator.kind")

	points := grantDecisionPoints(t, reader)
	if len(points) != 1 {
		t.Fatalf("data points = %v, want 1", points)
	}
	point := points[0]
	if point["stage"] != contract.ProducerGrantReasonUnknown ||
		point["decision"] != contract.ProducerGrantReasonUnknown ||
		point["reason"] != contract.ProducerGrantReasonUnknown ||
		point["fact_kind"] != contract.ProducerGrantFactKindOther {
		t.Fatalf("label set = %v, want every out-of-set value folded", point)
	}
}

// TestProducerGrantDecisionRecorderHotPathAddsNothingOverTheSDK is the
// hot-path guard: the per-emission recheck records on every extension result,
// so a warmed-up Record must allocate no more than a bare SDK counter Add with
// a prebuilt option (the SDK's own floor), i.e. the recorder itself adds zero.
func TestProducerGrantDecisionRecorderHotPathAddsNothingOverTheSDK(t *testing.T) {
	recorder, _ := newGrantRecorderForTest(t)
	ctx := context.Background()
	record := func() {
		recorder.Record(ctx, contract.ProducerGrantStageEmission, contract.ProducerGrantDecisionAllow, contract.ProducerGrantReasonGranted, "aws_resource")
	}
	record() // warm the option cache

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
	raw, err := provider.Meter("floor").Int64Counter("floor")
	if err != nil {
		t.Fatalf("Int64Counter() error = %v, want nil", err)
	}
	opt := metric.WithAttributeSet(attribute.NewSet(attribute.String("k", "v")))
	floor := testing.AllocsPerRun(200, func() { raw.Add(ctx, 1, opt) })

	if got := testing.AllocsPerRun(200, record); got > floor {
		t.Fatalf("Record() allocs/op = %v, want <= SDK floor %v on the warmed hot path", got, floor)
	}
}

// TestProducerGrantDecisionRecorderNilSafe proves an absent recorder or
// instruments never panics the emission path.
func TestProducerGrantDecisionRecorderNilSafe(t *testing.T) {
	t.Parallel()

	var nilRecorder *ProducerGrantDecisionRecorder
	nilRecorder.Record(context.Background(), "emission", "allow", "granted", "aws_resource")
	NewProducerGrantDecisionRecorder(nil).Record(context.Background(), "emission", "allow", "granted", "aws_resource")
}

// BenchmarkProducerGrantDecisionRecord bounds the cost of one warmed
// decision record.
func BenchmarkProducerGrantDecisionRecord(b *testing.B) {
	recorder, _ := newGrantRecorderForTest(b)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		recorder.Record(ctx, contract.ProducerGrantStageEmission, contract.ProducerGrantDecisionAllow, contract.ProducerGrantReasonGranted, "aws_resource")
	}
}
