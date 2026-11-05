// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

type generationOutcomeKey struct{ collectorKind, outcome, reason string }

func newGenerationOutcomeInstruments(t *testing.T) (*Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := NewInstruments(provider.Meter("generation-outcomes"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

func readGenerationOutcomes(t *testing.T, reader *sdkmetric.ManualReader) map[generationOutcomeKey]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := map[generationOutcomeKey]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_ingestion_generation_outcomes_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("data type = %T, want Int64 sum", m.Data)
			}
			for _, point := range sum.DataPoints {
				key := generationOutcomeKey{}
				for _, kv := range point.Attributes.ToSlice() {
					switch string(kv.Key) {
					case MetricDimensionCollectorKind:
						key.collectorKind = kv.Value.AsString()
					case MetricDimensionOutcome:
						key.outcome = kv.Value.AsString()
					case MetricDimensionReason:
						key.reason = kv.Value.AsString()
					default:
						t.Fatalf("unexpected label %q on generation outcomes counter", kv.Key)
					}
				}
				out[key] += point.Value
			}
		}
	}
	return out
}

func TestRecordGenerationOutcomeMovesOnMintAndSkip(t *testing.T) {
	inst, reader := newGenerationOutcomeInstruments(t)
	kev := string(scope.CollectorVulnerabilityIntelligence)

	inst.RecordGenerationOutcome(context.Background(), kev, GenerationOutcomeMinted, "")
	inst.RecordGenerationOutcome(context.Background(), kev, GenerationOutcomeSkipped, RefreshSkipReasonUnchanged)
	inst.RecordGenerationOutcome(context.Background(), kev, GenerationOutcomeSkipped, RefreshSkipReasonUnchanged)

	got := readGenerationOutcomes(t, reader)
	want := map[generationOutcomeKey]int64{
		{kev, GenerationOutcomeMinted, GenerationReasonNone}:        1,
		{kev, GenerationOutcomeSkipped, RefreshSkipReasonUnchanged}: 2,
	}
	if len(got) != len(want) {
		t.Fatalf("series = %v, want %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("%+v = %d, want %d (all: %v)", key, got[key], value, got)
		}
	}
}

func TestRecordGenerationOutcomeNilSafe(t *testing.T) {
	var nilInst *Instruments
	nilInst.RecordGenerationOutcome(context.Background(), "git", GenerationOutcomeMinted, "")
	(&Instruments{}).RecordGenerationOutcome(context.Background(), "git", GenerationOutcomeMinted, "")
}

// TestRecordGenerationOutcomeLabelSetIsBounded feeds hostile collector kinds,
// outcomes, and reasons (a scope id leaking into any of them would show up as
// thousands of series) and proves the series count stays inside the closed
// product of known collector kinds, outcomes, and reasons.
func TestRecordGenerationOutcomeLabelSetIsBounded(t *testing.T) {
	inst, reader := newGenerationOutcomeInstruments(t)
	for i := 0; i < 2000; i++ {
		kind := fmt.Sprintf("vuln-intel://osv/scope-%d", i)
		reason := fmt.Sprintf("reason-%d", i)
		inst.RecordGenerationOutcome(context.Background(), kind, fmt.Sprintf("outcome-%d", i), reason)
		inst.RecordGenerationOutcome(context.Background(), kind, GenerationOutcomeSkipped, reason)
		inst.RecordGenerationOutcome(context.Background(), kind, GenerationOutcomeMinted, reason)
	}
	got := readGenerationOutcomes(t, reader)
	kinds := len(scope.AllCollectorKinds()) + 1 // plus the "other" bucket
	reasons := len(RefreshSkipReasons()) + 1    // skipped reasons plus "none" for minted
	if bound := kinds * reasons; len(got) > bound {
		t.Fatalf("series = %d, want at most %d", len(got), bound)
	}
	if len(got) > 3 {
		t.Fatalf("hostile input spread across %d series, want it folded into the other bucket: %v", len(got), got)
	}
	for key := range got {
		if key.collectorKind != GenerationCollectorKindOther {
			t.Fatalf("unknown collector kind leaked as label %q", key.collectorKind)
		}
	}
}

func TestGenerationOutcomeClosedSets(t *testing.T) {
	if got := GenerationOutcomes(); len(got) != 2 || got[0] != GenerationOutcomeMinted || got[1] != GenerationOutcomeSkipped {
		t.Fatalf("GenerationOutcomes() = %v, want [minted skipped]", got)
	}
}

func BenchmarkRecordGenerationOutcome(b *testing.B) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := NewInstruments(provider.Meter("bench"))
	if err != nil {
		b.Fatalf("NewInstruments() error = %v", err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst.RecordGenerationOutcome(ctx, "vulnerability_intelligence", GenerationOutcomeSkipped, RefreshSkipReasonUnchanged)
	}
}
