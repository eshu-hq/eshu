// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"reflect"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// This file pins the L7 re-key operator signal (#10408, criterion 3.3): the
// writer reports the pass's genuine group-identity change
// (reportRekeyIdentity) and the handler counts one outcome per pass that
// changed identity (merge, split, member_only, retire_only), staying silent
// on ordinary passes. The classifier cases mirror the L1 writer shapes
// (writer_rekey_test.go): a merge retires dead keys while survivors commit,
// a split drops coverage without retirements, a member-only refresh rewrites
// the same keys, and a retire-only pass writes nothing. Production-shape
// silence cases (EPSS-only refresh, admission, bootstrap, same-key rewrite)
// prove the counter never fires on commit shape alone (F1).

func TestClassifyRekeyOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rep      RekeyReport
		orphaned int
		want     RekeyOutcome
		wantBool bool
	}{
		{
			name:     "silent report stays silent",
			rep:      RekeyReport{},
			orphaned: 0,
			want:     "",
			wantBool: false,
		},
		{
			name:     "silent report with orphaned coverage is a split",
			rep:      RekeyReport{},
			orphaned: 1,
			want:     RekeyOutcomeSplit,
			wantBool: true,
		},
		{
			name:     "retire-only writes nothing",
			rep:      RekeyReport{Outcome: RekeyOutcomeRetireOnly, Groups: []string{"CVE-2026-0604"}, Tombstoned: 2, Count: true},
			orphaned: 0,
			want:     RekeyOutcomeRetireOnly,
			wantBool: true,
		},
		{
			name:     "retire-only keeps dropped coverage visible",
			rep:      RekeyReport{Outcome: RekeyOutcomeRetireOnly, Groups: []string{"CVE-2026-0604"}, Tombstoned: 2, Count: true},
			orphaned: 1,
			want:     RekeyOutcomeRetireOnly,
			wantBool: true,
		},
		{
			name:     "merge retires dead keys while survivors commit",
			rep:      RekeyReport{Outcome: RekeyOutcomeMerge, Groups: []string{"CVE-2026-0601", "CVE-2026-0602"}, Findings: 1, Tombstoned: 1, Count: true},
			orphaned: 0,
			want:     RekeyOutcomeMerge,
			wantBool: true,
		},
		{
			name:     "dropped coverage wins over the merge shape",
			rep:      RekeyReport{Outcome: RekeyOutcomeMerge, Groups: []string{"CVE-2026-0601", "CVE-2026-0602"}, Findings: 1, Tombstoned: 1, Count: true},
			orphaned: 1,
			want:     RekeyOutcomeSplit,
			wantBool: true,
		},
		{
			name:     "split drops coverage without retirements",
			rep:      RekeyReport{Outcome: RekeyOutcomeSplit, Groups: []string{"CVE-2026-0301", "CVE-2026-0302"}, Findings: 2, Count: true},
			orphaned: 1,
			want:     RekeyOutcomeSplit,
			wantBool: true,
		},
		{
			name:     "member-only rewrites the same keys",
			rep:      RekeyReport{Outcome: RekeyOutcomeMemberOnly, Groups: []string{"CVE-2026-0501"}, Findings: 1, Count: true},
			orphaned: 0,
			want:     RekeyOutcomeMemberOnly,
			wantBool: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, count := classifyRekeyOutcome(tc.rep, tc.orphaned)
			if got != tc.want || count != tc.wantBool {
				t.Fatalf("classifyRekeyOutcome(%+v, orphaned=%d) = (%q, %v), want (%q, %v)",
					tc.rep, tc.orphaned, got, count, tc.want, tc.wantBool)
			}
		})
	}
}

// TestReportRekeyIdentityProductionShapes pins the F1 production-shape
// contract end to end: ordinary passes report no identity change however
// many rows they wrote or retracted, while genuine re-keys report the
// right outcome with magnitudes limited to the identity-changed subset.
func TestReportRekeyIdentityProductionShapes(t *testing.T) {
	t.Parallel()

	mkRow := func(factID, key, digest, cve string, members ...string) changedRow {
		return changedRow{factID: factID, stableKey: key, digest: digest, cveID: cve, members: members}
	}
	mkStored := func(factID, gen, key, digest, cve string, members ...string) storedFinding {
		return storedFinding{factID: factID, generationID: gen, stableKey: key, digest: digest, cveID: cve, fencingToken: 5, members: members}
	}

	tests := []struct {
		name       string
		rows       []changedRow
		stored     []storedFinding
		gen        string
		retract    bool
		want       RekeyOutcome
		wantCount  bool
		wantGroups []string
	}{
		{
			name: "EPSS-only refresh stays silent",
			rows: []changedRow{
				mkRow("new-a", "k-a", "d-new", "CVE-2026-0601", "CVE-2026-0601"),
			},
			stored: []storedFinding{
				mkStored("old-a", "gen-old", "k-a", "d-old", "CVE-2026-0601", "CVE-2026-0601"),
			},
			gen: "gen-new", retract: true,
			want: "", wantCount: false,
		},
		{
			name: "new admission stays silent",
			rows: []changedRow{
				mkRow("new-a", "k-a", "d-new", "CVE-2026-0601", "CVE-2026-0601"),
				mkRow("new-b", "k-b", "d-new", "CVE-2026-0900", "CVE-2026-0900"),
			},
			stored: []storedFinding{
				mkStored("old-a", "gen-old", "k-a", "d-new", "CVE-2026-0601", "CVE-2026-0601"),
			},
			gen: "gen-new", retract: true,
			want: "", wantCount: false,
		},
		{
			name: "bootstrap first pass stays silent",
			rows: []changedRow{
				mkRow("new-a", "k-a", "d-new", "CVE-2026-0601", "CVE-2026-0601"),
			},
			stored: nil,
			gen:    "gen-1", retract: true,
			want: "", wantCount: false,
		},
		{
			name: "generation rollover rewrite stays silent",
			rows: []changedRow{
				mkRow("new-a", "k-a", "d-same", "CVE-2026-0601", "CVE-2026-0601"),
			},
			stored: []storedFinding{
				mkStored("old-a", "gen-old", "k-a", "d-same", "CVE-2026-0601", "CVE-2026-0601"),
			},
			gen: "gen-new", retract: true,
			want: "", wantCount: false,
		},
		{
			name: "member-only refresh counts member_only",
			rows: []changedRow{
				mkRow("new-a", "k-a", "d-new", "CVE-2026-0601", "CVE-2026-0601", "CVE-2026-0603"),
			},
			stored: []storedFinding{
				mkStored("old-a", "gen-old", "k-a", "d-old", "CVE-2026-0601", "CVE-2026-0601", "CVE-2026-0602"),
			},
			gen: "gen-new", retract: true,
			want: RekeyOutcomeMemberOnly, wantCount: true,
			wantGroups: []string{"CVE-2026-0601"},
		},
		{
			name: "two-group merge counts merge",
			rows: []changedRow{
				mkRow("new-a", "k-a", "d-new", "CVE-2026-0601", "CVE-2026-0601", "CVE-2026-0602"),
			},
			stored: []storedFinding{
				mkStored("old-a", "gen-old", "k-a", "d-old", "CVE-2026-0601", "CVE-2026-0601"),
				mkStored("dead-b", "gen-old", "k-b", "d-old", "CVE-2026-0602", "CVE-2026-0602"),
			},
			gen: "gen-new", retract: true,
			want: RekeyOutcomeMerge, wantCount: true,
			wantGroups: []string{"CVE-2026-0601", "CVE-2026-0602"},
		},
		{
			name: "alias-removal split counts split",
			rows: []changedRow{
				mkRow("new-a", "k-a", "d-new", "CVE-2026-0601", "CVE-2026-0601"),
				mkRow("new-h", "k-h", "d-new", "CVE-2026-0602", "CVE-2026-0602"),
			},
			stored: []storedFinding{
				mkStored("old-a", "gen-old", "k-a", "d-old", "CVE-2026-0601", "CVE-2026-0601", "CVE-2026-0602"),
			},
			gen: "gen-new", retract: true,
			want: RekeyOutcomeSplit, wantCount: true,
			wantGroups: []string{"CVE-2026-0601", "CVE-2026-0602"},
		},
		{
			name: "withdrawal with no survivors counts retire_only",
			rows: nil,
			stored: []storedFinding{
				mkStored("dead-b", "gen-old", "k-b", "d-old", "CVE-2026-0602", "CVE-2026-0602"),
			},
			gen: "gen-new", retract: true,
			want: RekeyOutcomeRetireOnly, wantCount: true,
			wantGroups: []string{"CVE-2026-0602"},
		},
		{
			name: "partial pass that drops a group reports no retirement",
			rows: []changedRow{
				mkRow("new-a", "k-a", "d-same", "CVE-2026-0601", "CVE-2026-0601"),
			},
			stored: []storedFinding{
				mkStored("old-a", "gen-old", "k-a", "d-same", "CVE-2026-0601", "CVE-2026-0601"),
				mkStored("old-b", "gen-old", "k-b", "d-old", "CVE-2026-0602", "CVE-2026-0602"),
			},
			gen: "gen-new", retract: false,
			want: "", wantCount: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := reportRekeyIdentity(tc.rows, tc.stored, tc.gen, 7, tc.retract)
			if got.Outcome != tc.want || got.Count != tc.wantCount {
				t.Fatalf("reportRekeyIdentity() = (%q, count=%v), want (%q, count=%v)",
					got.Outcome, got.Count, tc.want, tc.wantCount)
			}
			if tc.wantCount && !reflect.DeepEqual(got.Groups, tc.wantGroups) {
				t.Fatalf("reportRekeyIdentity().Groups = %v, want %v", got.Groups, tc.wantGroups)
			}
			if !tc.wantCount && (len(got.Groups) != 0 || got.Findings != 0 || got.Tombstoned != 0) {
				t.Fatalf("silent report carries magnitudes: %+v", got)
			}
		})
	}
}

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
		FactsWritten:   1,
		FactsRetracted: 1,
		Rekey: RekeyReport{
			Outcome:    RekeyOutcomeMerge,
			Groups:     []string{"CVE-2026-0601", "CVE-2026-0602"},
			Findings:   1,
			Tombstoned: 1,
			Count:      true,
		},
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

	// A no-identity-change pass counts nothing and reports zero magnitudes,
	// however many rows it wrote: the counter reads the writer's genuine
	// subset, never commit shape (F1).
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
		FactsWritten:   2,
		FactsRetracted: 0,
		Rekey: RekeyReport{
			Outcome:  RekeyOutcomeSplit,
			Groups:   []string{"CVE-2026-0301", "CVE-2026-0302"},
			Findings: 2,
			Count:    true,
		},
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
