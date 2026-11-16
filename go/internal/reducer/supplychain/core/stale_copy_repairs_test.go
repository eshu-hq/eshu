// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// This file pins the stale-copy repair signal (#10765). A finding's copy in
// one holder scope can read active while another scope has re-derived the same
// finding as advisory_retracted, and the default view ranks by priority score
// first, so the stale copy can win until its scope re-derives. The one-hop
// holder fan-out is the repair. Each time a pass rewrites a copy whose stored
// suppression state differs in retraction from the state it now derives, the
// counter adds one. That includes the originating scope's own flip, so it is a
// count of flips written, not of stale copies left.

func TestStaleCopyRepairsCountOnlyRetractionFlips(t *testing.T) {
	t.Parallel()

	row := func(factID, key, state string) changedRow {
		return changedRow{factID: factID, stableKey: key, digest: "new", cveID: "CVE-2026-1", suppressionState: state}
	}
	own := func(factID, key, state string) storedFinding {
		return storedFinding{factID: factID, generationID: "g", stableKey: key, digest: "old", cveID: "CVE-2026-1", fencingToken: 1, suppressionState: state}
	}
	prior := func(factID, key, state string) storedFinding {
		return storedFinding{factID: factID, generationID: "g-prior", stableKey: key, digest: "old", cveID: "CVE-2026-1", fencingToken: 1, suppressionState: state}
	}
	const retracted, active = "advisory_retracted", "active"

	tests := []struct {
		name   string
		rows   []changedRow
		stored []storedFinding
		want   StaleCopyRepairs
	}{
		{"a rerun that retracts an active copy", []changedRow{row("f1", "k1", retracted)}, []storedFinding{own("f1", "k1", active)}, StaleCopyRepairs{ActiveToRetracted: 1}},
		{"a rerun that restores a retracted copy", []changedRow{row("f1", "k1", active)}, []storedFinding{own("f1", "k1", retracted)}, StaleCopyRepairs{RetractedToActive: 1}},
		{"a rerun that keeps the state counts nothing", []changedRow{row("f1", "k1", retracted)}, []storedFinding{own("f1", "k1", retracted)}, StaleCopyRepairs{}},
		{"a first pass of a generation is judged against the previous generation", []changedRow{row("f2", "k1", retracted)}, []storedFinding{prior("f1", "k1", active)}, StaleCopyRepairs{ActiveToRetracted: 1}},
		{"an operator decision that becomes a retraction counts as leaving active", []changedRow{row("f1", "k1", retracted)}, []storedFinding{own("f1", "k1", "accepted_risk")}, StaleCopyRepairs{ActiveToRetracted: 1}},
		{"a retracted copy that becomes an operator decision counts as leaving retracted", []changedRow{row("f1", "k1", "accepted_risk")}, []storedFinding{own("f1", "k1", retracted)}, StaleCopyRepairs{RetractedToActive: 1}},
		{"a change between two non-retracted states is not a repair", []changedRow{row("f1", "k1", "accepted_risk")}, []storedFinding{own("f1", "k1", active)}, StaleCopyRepairs{}},
		{"a new retracted copy with no stored baseline is not a repair", []changedRow{row("f9", "k9", retracted)}, []storedFinding{own("f1", "k1", active)}, StaleCopyRepairs{}},
		{"a rerun ignores the previous generation's rows", []changedRow{row("f1", "k1", retracted)}, []storedFinding{own("f1", "k1", retracted), prior("f0", "k1", active)}, StaleCopyRepairs{}},
		{"a stored row the pass no longer derives is not a repair", nil, []storedFinding{own("f1", "k1", active)}, StaleCopyRepairs{}},
		{
			"each flipped copy counts once",
			[]changedRow{row("f1", "k1", retracted), row("f2", "k2", retracted), row("f3", "k3", active)},
			[]storedFinding{own("f1", "k1", active), own("f2", "k2", active), own("f3", "k3", retracted)},
			StaleCopyRepairs{ActiveToRetracted: 2, RetractedToActive: 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := staleCopyRepairsOf(tc.rows, tc.stored, "g"); got != tc.want {
				t.Fatalf("staleCopyRepairsOf() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestWriteReportsTheStaleCopiesItRepaired drives the production writer over a
// stored active copy and a pass that derives the same finding retracted. The
// stored state comes from the same read the change judgement uses.
func TestWriteReportsTheStaleCopiesItRepaired(t *testing.T) {
	t.Parallel()

	finding := changedTestFinding("CVE-2026-0001")
	prior := changedTestWrite(finding)
	retracted := finding
	retracted.Suppression = SupplyChainSuppressionDecision{State: SupplyChainSuppressionStateAdvisoryRetracted, Reason: "nvd rejected CVE-2026-0001"}
	write := changedTestWrite(retracted)
	writer, beginner := changedTestWriter()
	beginner.state.stored = []fakeStoredFinding{storedLike(prior, finding)}

	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if want := (StaleCopyRepairs{ActiveToRetracted: 1}); result.StaleCopyRepairs != want {
		t.Fatalf("StaleCopyRepairs = %+v, want %+v", result.StaleCopyRepairs, want)
	}

	// The same retraction written over an already retracted copy repairs
	// nothing: the first pass already counted it.
	again, beginnerAgain := changedTestWriter()
	stored := storedLike(write, retracted)
	stored.suppressionState = string(SupplyChainSuppressionStateAdvisoryRetracted)
	beginnerAgain.state.stored = []fakeStoredFinding{stored}
	result, err = again.WriteSupplyChainImpactFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if want := (StaleCopyRepairs{}); result.StaleCopyRepairs != want {
		t.Fatalf("StaleCopyRepairs on an already retracted copy = %+v, want none", result.StaleCopyRepairs)
	}
}

// TestHandlerCountsStaleCopyRepairsByTransition proves the whole reducer pass:
// the repairs the writer reports reach the counter by closed transition and the
// result sub-signals, and a pass with no repair adds nothing.
func TestHandlerCountsStaleCopyRepairsByTransition(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	writer := &recordingSupplyChainImpactWriter{repairs: StaleCopyRepairs{ActiveToRetracted: 2, RetractedToActive: 1}}
	handler := SupplyChainImpactHandler{
		FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
		FactLoader: &stubSupplyChainImpactFactLoader{scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-live", "CVE-2026-3003", 8.0),
			vulnerabilityAffectedPackageFact("affected-live", "CVE-2026-3003", "pkg:npm/live", "npm", "live", "1.0.0", "1.0.1"),
			packageConsumptionFactWithRange("consume-live", "pkg:npm/live", testImpactRepositoryID, "1.0.0"),
		}},
		Writer:      writer,
		Instruments: inst,
		Now:         func() time.Time { return time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC) },
	}
	intent := reducercontract.Intent{
		IntentID: "intent-10765", ScopeID: "vuln-intel://osv/npm/live", GenerationID: "generation-10765",
		SourceSystem: "vulnerability_intelligence", Domain: reducercontract.DomainSupplyChainImpact,
		Cause: "vulnerability evidence observed",
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	counts := staleCopyRepairCounts(t, reader)
	want := map[string]int64{"active_to_retracted": 2, "retracted_to_active": 1}
	if !reflect.DeepEqual(counts, want) {
		t.Fatalf("stale_copy_repairs_total by transition = %v, want %v", counts, want)
	}
	if got := result.SubSignals["stale_copy_repairs_active_to_retracted"]; got != 2 {
		t.Fatalf("sub-signal stale_copy_repairs_active_to_retracted = %v, want 2", got)
	}
	if got := result.SubSignals["stale_copy_repairs_retracted_to_active"]; got != 1 {
		t.Fatalf("sub-signal stale_copy_repairs_retracted_to_active = %v, want 1", got)
	}

	writer.repairs = StaleCopyRepairs{}
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if after := staleCopyRepairCounts(t, reader); !reflect.DeepEqual(after, want) {
		t.Fatalf("a pass with no repair moved the counter: %v, want %v", after, want)
	}
}

// TestStaleCopyRepairsSurviveALaterFailureAndSkipAFailedWrite pins the emit
// point: the counter is added right after the writer commit, before the group
// projection enqueue and the holder fan-out that can still fail the pass, so a
// retry that finds the copy already repaired cannot lose the count. A writer
// that fails before it commits counted nothing, so it adds nothing.
func TestStaleCopyRepairsSurviveALaterFailureAndSkipAFailedWrite(t *testing.T) {
	t.Parallel()

	repaired := SupplyChainImpactWriteResult{
		ChangedCVEIDs:    []string{"CVE-2026-9001"},
		TouchedGroupIDs:  []string{"CVE-2026-9001"},
		StaleCopyRepairs: StaleCopyRepairs{ActiveToRetracted: 2, RetractedToActive: 1},
	}
	tests := []struct {
		name  string
		build func(h *SupplyChainImpactHandler, port *stubFanOut, writer *changeWriter)
		want  map[string]int64
	}{
		{
			name: "a failed group projection enqueue after the commit",
			build: func(h *SupplyChainImpactHandler, _ *stubFanOut, _ *changeWriter) {
				h.GroupProjectionEnqueuer = &recordingGroupEnqueuer{err: errors.New("queue unavailable")}
			},
			want: map[string]int64{"active_to_retracted": 2, "retracted_to_active": 1},
		},
		{
			name: "a failed holder fan-out after the commit",
			build: func(h *SupplyChainImpactHandler, port *stubFanOut, _ *changeWriter) {
				h.GroupProjectionEnqueuer = &recordingGroupEnqueuer{}
				port.findErr = errors.New("holder read failed")
			},
			want: map[string]int64{"active_to_retracted": 2, "retracted_to_active": 1},
		},
		{
			name: "a writer that fails before it commits",
			build: func(_ *SupplyChainImpactHandler, _ *stubFanOut, writer *changeWriter) {
				writer.err = errors.New("writer transaction rolled back")
			},
			want: map[string]int64{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v", err)
			}
			port := &stubFanOut{holders: []string{"holder-a"}, payload: map[string]any{}}
			writer := &changeWriter{result: repaired}
			handler := fanOutHandler(port, writer, inst)
			tc.build(&handler, port, writer)

			if _, err := handler.Handle(context.Background(), fanOutIntent(1, port.payload)); err == nil {
				t.Fatal("Handle() error = nil, want the injected failure to fail the pass")
			}
			if got := staleCopyRepairCounts(t, reader); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("stale_copy_repairs_total by transition = %v, want %v", got, tc.want)
			}
		})
	}
}

func staleCopyRepairCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_stale_copy_repairs_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				for _, kv := range dp.Attributes.ToSlice() {
					if string(kv.Key) != "transition" {
						t.Fatalf("stale copy repair counter carries label %q, want only transition", kv.Key)
					}
				}
				transition, _ := dp.Attributes.Value("transition")
				if strings.TrimSpace(transition.AsString()) == "" {
					t.Fatalf("stale copy repair point has no transition: %v", dp.Attributes)
				}
				out[transition.AsString()] += dp.Value
			}
		}
	}
	return out
}
