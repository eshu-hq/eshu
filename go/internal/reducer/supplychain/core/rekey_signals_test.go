// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// This file pins the L7 re-key operator signal (#10408, criterion 3.3): the
// handler classifies every pass that touched an advisory group into one
// closed outcome (merge, split, member_only, retire_only), counts it, and
// stays silent when the touched set is empty. The classifier cases mirror
// the L1 writer shapes (writer_rekey_test.go): a merge retires dead keys
// while survivors commit, a split drops coverage without retirements, a
// member-only refresh rewrites the same keys, and a retire-only pass writes
// nothing.

func TestClassifyRekeyOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		touched   []string
		written   int
		retracted int
		orphaned  int
		want      RekeyOutcome
		wantCount bool
	}{
		{
			name:      "no group change stays silent",
			touched:   nil,
			written:   3,
			retracted: 0,
			orphaned:  0,
			want:      "",
			wantCount: false,
		},
		{
			name:      "empty touched set stays silent",
			touched:   []string{},
			written:   0,
			retracted: 0,
			orphaned:  0,
			want:      "",
			wantCount: false,
		},
		{
			name:      "retire-only writes nothing",
			touched:   []string{"CVE-2026-0604"},
			written:   0,
			retracted: 2,
			orphaned:  0,
			want:      RekeyOutcomeRetireOnly,
			wantCount: true,
		},
		{
			name:      "retire-only keeps dropped coverage visible",
			touched:   []string{"CVE-2026-0604"},
			written:   0,
			retracted: 1,
			orphaned:  1,
			want:      RekeyOutcomeRetireOnly,
			wantCount: true,
		},
		{
			name:      "merge retires dead keys while survivors commit",
			touched:   []string{"CVE-2026-0601", "CVE-2026-0602"},
			written:   1,
			retracted: 1,
			orphaned:  0,
			want:      RekeyOutcomeMerge,
			wantCount: true,
		},
		{
			name:      "dropped coverage wins over the merge shape",
			touched:   []string{"CVE-2026-0601", "CVE-2026-0602"},
			written:   1,
			retracted: 1,
			orphaned:  1,
			want:      RekeyOutcomeSplit,
			wantCount: true,
		},
		{
			name:      "split drops coverage without retirements",
			touched:   []string{"CVE-2026-0301", "CVE-2026-0302"},
			written:   2,
			retracted: 0,
			orphaned:  1,
			want:      RekeyOutcomeSplit,
			wantCount: true,
		},
		{
			name:      "member-only rewrites the same keys",
			touched:   []string{"CVE-2026-0501", "CVE-2026-0502", "CVE-2026-0503"},
			written:   1,
			retracted: 0,
			orphaned:  0,
			want:      RekeyOutcomeMemberOnly,
			wantCount: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, count := classifyRekeyOutcome(tc.touched, tc.written, tc.retracted, tc.orphaned)
			if got != tc.want || count != tc.wantCount {
				t.Fatalf("classifyRekeyOutcome(%v, written=%d, retracted=%d, orphaned=%d) = (%q, %v), want (%q, %v)",
					tc.touched, tc.written, tc.retracted, tc.orphaned, got, count, tc.want, tc.wantCount)
			}
		})
	}
}

// TestEmitRekeyCountsOutcome proves the counter fires with the right outcome
// on a re-key pass and stays silent on a no-group-change pass, through the
// real Instruments registry (not a copy of its shape).
func TestEmitRekeyCountsOutcome(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	h := SupplyChainImpactHandler{Instruments: inst}

	subSignals := map[string]float64{}
	h.emitRekey(ctx, SupplyChainImpactWriteResult{
		FactsWritten:    1,
		FactsRetracted:  1,
		TouchedGroupIDs: []string{"CVE-2026-0601", "CVE-2026-0602"},
	}, suppressionCarryTally{carried: 1}, subSignals)

	counts := rekeyCounts(t, reader)
	if got := counts[string(RekeyOutcomeMerge)]; got != 1 {
		t.Fatalf("rekey_total{outcome=merge} = %d, want 1: %v", got, counts)
	}
	if got := carriedCounts(t, reader)[string(RekeyOutcomeMerge)]; got != 1 {
		t.Fatalf("carried_via_member_total{outcome=merge} = %d, want 1", got)
	}
	if got := subSignals[rekeyGroupsTouchedSubSignal]; got != 2 {
		t.Fatalf("sub-signal %q = %v, want 2", rekeyGroupsTouchedSubSignal, got)
	}
	if got := subSignals[rekeyFindingsRekeyedSubSignal]; got != 1 {
		t.Fatalf("sub-signal %q = %v, want 1", rekeyFindingsRekeyedSubSignal, got)
	}
	if got := subSignals[rekeyRowsTombstonedSubSignal]; got != 1 {
		t.Fatalf("sub-signal %q = %v, want 1", rekeyRowsTombstonedSubSignal, got)
	}

	// A no-group-change pass counts nothing and reports zero magnitudes.
	silent := map[string]float64{}
	h.emitRekey(ctx, SupplyChainImpactWriteResult{
		FactsWritten:   4,
		FactsRetracted: 0,
	}, suppressionCarryTally{}, silent)
	after := rekeyCounts(t, reader)
	if len(after) != len(counts) {
		t.Fatalf("silent pass changed the counter: before %v, after %v", counts, after)
	}
	for outcome, before := range counts {
		if after[outcome] != before {
			t.Fatalf("silent pass moved outcome %q: before %d, after %d", outcome, before, after[outcome])
		}
	}
	if got := silent[rekeyGroupsTouchedSubSignal]; got != 0 {
		t.Fatalf("silent sub-signal %q = %v, want 0", rekeyGroupsTouchedSubSignal, got)
	}
}

// TestEmitRekeyCountsSplitOrphan proves the split outcome carries the
// orphaned-by-split instrument with the same outcome label.
func TestEmitRekeyCountsSplitOrphan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	h := SupplyChainImpactHandler{Instruments: inst}

	h.emitRekey(ctx, SupplyChainImpactWriteResult{
		FactsWritten:    2,
		FactsRetracted:  0,
		TouchedGroupIDs: []string{"CVE-2026-0301", "CVE-2026-0302"},
	}, suppressionCarryTally{orphaned: 1}, map[string]float64{})

	if got := rekeyCounts(t, reader)[string(RekeyOutcomeSplit)]; got != 1 {
		t.Fatalf("rekey_total{outcome=split} = %d, want 1", got)
	}
	if got := orphanedCounts(t, reader)[string(RekeyOutcomeSplit)]; got != 1 {
		t.Fatalf("orphaned_by_split_total{outcome=split} = %d, want 1", got)
	}
}

func rekeyCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	return rekeyInstrumentCounts(t, reader, "eshu_dp_supply_chain_impact_rekey_total")
}

func carriedCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	return rekeyInstrumentCounts(t, reader, "eshu_dp_supply_chain_impact_suppression_carried_via_member_total")
}

func orphanedCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	return rekeyInstrumentCounts(t, reader, "eshu_dp_supply_chain_impact_suppression_orphaned_by_split_total")
}

func rekeyInstrumentCounts(t *testing.T, reader *sdkmetric.ManualReader, name string) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				outcome, _ := dp.Attributes.Value("outcome")
				out[outcome.AsString()] += dp.Value
			}
		}
	}
	return out
}
