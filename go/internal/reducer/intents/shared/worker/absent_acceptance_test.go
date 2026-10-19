// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// stubAbsentGenerationReader is a stubSharedIntentReader that opts into the
// #7853 drain by reporting a fixed gone set from AbsentGenerationIDs.
type stubAbsentGenerationReader struct {
	stubSharedIntentReader
	gone       map[string]struct{}
	probeCalls int
	probeErr   error
}

func (s *stubAbsentGenerationReader) AbsentGenerationIDs(
	_ context.Context, generationIDs []string,
) (map[string]struct{}, error) {
	s.probeCalls++
	if s.probeErr != nil {
		return nil, s.probeErr
	}
	gone := make(map[string]struct{}, len(generationIDs))
	for _, id := range generationIDs {
		if _, ok := s.gone[id]; ok {
			gone[id] = struct{}{}
		}
	}
	return gone, nil
}

// absentAcceptanceLookup answers the healthy generation for the healthy scope
// and reports every other key absent, so orphan rows land in the
// absent-acceptance set instead of active or stale.
func absentAcceptanceLookup(
	healthyScope, healthyUnit, healthyRun, healthyGen string,
) sharedintent.AcceptedGenerationLookup {
	return func(key sharedintent.AcceptanceKey) (string, bool) {
		if key.ScopeID == healthyScope && key.AcceptanceUnitID == healthyUnit && key.SourceRunID == healthyRun {
			return healthyGen, true
		}
		return "", false
	}
}

func TestAbsentAcceptanceRowsRecoversDroppedRows(t *testing.T) {
	t.Parallel()

	active := selectionTestRow(reducercontract.DomainWorkloadDependency, "scope-active", "unit-active", "gen-active", 1)
	stale := selectionTestRow(reducercontract.DomainWorkloadDependency, "scope-stale", "unit-stale", "gen-stale", 2)
	orphan := selectionTestRow(reducercontract.DomainWorkloadDependency, "scope-orphan", "unit-orphan", "gen-orphan", 3)
	keyless := sharedintent.Row{IntentID: "intent-keyless", ProjectionDomain: reducercontract.DomainWorkloadDependency}
	all := []sharedintent.Row{active, stale, orphan, keyless}

	got := AbsentAcceptanceRows(all, []sharedintent.Row{active}, []string{stale.IntentID})
	if len(got) != 1 || got[0].IntentID != orphan.IntentID {
		t.Fatalf("AbsentAcceptanceRows() = %v, want only the orphan row", got)
	}
}

func TestSplitAbsentAcceptanceRowsDrainsOnlyGenerationGone(t *testing.T) {
	t.Parallel()

	domain := reducercontract.DomainWorkloadDependency
	gone := selectionTestRow(domain, "scope-gone", "unit-gone", "gen-gone", 1)
	present := selectionTestRow(domain, "scope-present", "unit-present", "gen-present", 2)
	emptyGen := selectionTestRow(domain, "scope-empty", "unit-empty", "", 3)
	absent := []sharedintent.Row{gone, present, emptyGen}

	reader := &stubAbsentGenerationReader{gone: map[string]struct{}{"gen-gone": {}}}
	drainable, kept, err := SplitAbsentAcceptanceRows(context.Background(), reader, absent)
	if err != nil {
		t.Fatalf("SplitAbsentAcceptanceRows() error = %v", err)
	}
	if len(drainable) != 1 || drainable[0].IntentID != gone.IntentID {
		t.Fatalf("drainable = %v, want only the generation-gone row", drainable)
	}
	if len(kept) != 2 {
		t.Fatalf("kept = %v, want the generation-present and empty-generation rows", kept)
	}
	if reader.probeCalls != 1 {
		t.Fatalf("probe calls = %d, want exactly 1 bounded lookup", reader.probeCalls)
	}
}

func TestSplitAbsentAcceptanceRowsWithoutPortKeepsEverything(t *testing.T) {
	t.Parallel()

	domain := reducercontract.DomainWorkloadDependency
	orphan := selectionTestRow(domain, "scope-orphan", "unit-orphan", "gen-orphan", 1)
	reader := &stubSharedIntentReader{}

	drainable, kept, err := SplitAbsentAcceptanceRows(context.Background(), reader, []sharedintent.Row{orphan})
	if err != nil {
		t.Fatalf("SplitAbsentAcceptanceRows() error = %v", err)
	}
	if len(drainable) != 0 {
		t.Fatalf("drainable = %v, want none without the lookup port", drainable)
	}
	if len(kept) != 1 || kept[0].IntentID != orphan.IntentID {
		t.Fatalf("kept = %v, want the orphan row", kept)
	}
}

func TestSplitAbsentAcceptanceRowsEmptyInputCostsNoQuery(t *testing.T) {
	t.Parallel()

	reader := &stubAbsentGenerationReader{gone: map[string]struct{}{"gen-gone": {}}}
	drainable, kept, err := SplitAbsentAcceptanceRows(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("SplitAbsentAcceptanceRows() error = %v", err)
	}
	if len(drainable) != 0 || len(kept) != 0 {
		t.Fatalf("drainable = %v, kept = %v, want both empty", drainable, kept)
	}
	if reader.probeCalls != 0 {
		t.Fatalf("probe calls = %d, want 0 for empty input", reader.probeCalls)
	}
}

func TestSplitAbsentAcceptanceRowsLookupErrorFailsClosed(t *testing.T) {
	t.Parallel()

	domain := reducercontract.DomainWorkloadDependency
	orphan := selectionTestRow(domain, "scope-orphan", "unit-orphan", "gen-orphan", 1)
	reader := &stubAbsentGenerationReader{probeErr: errors.New("generation probe failed")}

	if _, _, err := SplitAbsentAcceptanceRows(context.Background(), reader, []sharedintent.Row{orphan}); err == nil {
		t.Fatal("SplitAbsentAcceptanceRows() error = nil, want fail-closed lookup error")
	}
}

func TestDrainAbsentAcceptanceMarksDrainableAndClearsMap(t *testing.T) {
	t.Parallel()

	domain := reducercontract.DomainWorkloadDependency
	gone := selectionTestRow(domain, "scope-gone", "unit-gone", "gen-gone", 1)
	present := selectionTestRow(domain, "scope-present", "unit-present", "gen-present", 2)
	reader := &stubAbsentGenerationReader{
		stubSharedIntentReader: stubSharedIntentReader{},
		gone:                   map[string]struct{}{"gen-gone": {}},
	}
	absent := map[string]sharedintent.Row{gone.IntentID: gone, present.IntentID: present}

	drained, keptIDs, err := DrainAbsentAcceptance(
		context.Background(), time.Now().UTC(), AcceptanceTelemetry{},
		"test_runner", domain, reader, absent, nil,
	)
	if err != nil {
		t.Fatalf("DrainAbsentAcceptance() error = %v", err)
	}
	if len(drained) != 1 || drained[0] != gone.IntentID {
		t.Fatalf("drained = %v, want [%q]", drained, gone.IntentID)
	}
	if len(keptIDs) != 1 || keptIDs[0] != present.IntentID {
		t.Fatalf("keptIDs = %v, want [%q] for the generation-present row", keptIDs, present.IntentID)
	}
	if len(reader.completedIDs) != 1 || reader.completedIDs[0] != gone.IntentID {
		t.Fatalf("marked = %v, want [%q]", reader.completedIDs, gone.IntentID)
	}
	if _, ok := absent[gone.IntentID]; ok {
		t.Fatal("drained id still in the absent map; a rescan would re-mark it")
	}
	if _, ok := absent[present.IntentID]; !ok {
		t.Fatal("kept torn-write row missing from the absent map")
	}
}

func TestDrainAbsentAcceptanceEmptyMapCostsNoMark(t *testing.T) {
	t.Parallel()

	reader := &stubAbsentGenerationReader{gone: map[string]struct{}{"gen-gone": {}}}
	drained, keptIDs, err := DrainAbsentAcceptance(
		context.Background(), time.Now().UTC(), AcceptanceTelemetry{},
		"test_runner", reducercontract.DomainWorkloadDependency, reader, map[string]sharedintent.Row{}, nil,
	)
	if err != nil {
		t.Fatalf("DrainAbsentAcceptance() error = %v", err)
	}
	if len(drained) != 0 || len(keptIDs) != 0 {
		t.Fatalf("drained = %v, keptIDs = %v, want both empty", drained, keptIDs)
	}
	if len(reader.completedIDs) != 0 {
		t.Fatalf("marked = %v, want no mark call for empty input", reader.completedIDs)
	}
	if reader.probeCalls != 0 {
		t.Fatalf("probe calls = %d, want 0 for empty input", reader.probeCalls)
	}
}

// warnCountingHandler counts Warn-level records so a test can prove the
// torn-write WARN fires once per row per cycle, not once per drain.
type warnCountingHandler struct {
	warns *int
}

func (h warnCountingHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (h warnCountingHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level == slog.LevelWarn {
		*h.warns++
	}
	return nil
}

func (h warnCountingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h warnCountingHandler) WithGroup(string) slog.Handler { return h }

// TestDrainAbsentAcceptanceReportsTornWriteOnceAcrossDrains reproduces the
// #8509 double-report: the hit-path drain WARNs and counts every torn row in
// the scanned window, then the unit-level drain re-WARNs and re-counts the
// same rows. The first drain simulates the selection window (one
// generation-gone orphan plus one torn write); the second simulates the
// selected unit's reload carrying the same torn row, with the first drain's
// kept ids excluded. The torn row must WARN and count exactly once across
// both drains, while a genuinely new torn row in the unit load is still
// reported and the drained orphan is still marked exactly once.
func TestDrainAbsentAcceptanceReportsTornWriteOnceAcrossDrains(t *testing.T) {
	t.Parallel()

	domain := reducercontract.DomainWorkloadDependency
	gone := selectionTestRow(domain, "scope-gone", "unit-gone", "gen-gone", 1)
	torn := selectionTestRow(domain, "scope-torn", "unit-torn", "gen-present", 2)
	freshTorn := selectionTestRow(domain, "scope-fresh", "unit-fresh", "gen-present", 3)
	reader := &stubAbsentGenerationReader{
		stubSharedIntentReader: stubSharedIntentReader{},
		gone:                   map[string]struct{}{"gen-gone": {}},
	}
	var warns int
	tel := AcceptanceTelemetry{
		Logger: slog.New(warnCountingHandler{warns: &warns}),
	}
	ctx := context.Background()
	now := time.Now().UTC()

	window := map[string]sharedintent.Row{gone.IntentID: gone, torn.IntentID: torn}
	drained, keptIDs, err := DrainAbsentAcceptance(
		ctx, now, tel, "test_runner", domain, reader, window, nil,
	)
	if err != nil {
		t.Fatalf("DrainAbsentAcceptance() error = %v", err)
	}
	if len(drained) != 1 || drained[0] != gone.IntentID {
		t.Fatalf("drained = %v, want [%q]", drained, gone.IntentID)
	}
	if len(keptIDs) != 1 || keptIDs[0] != torn.IntentID {
		t.Fatalf("keptIDs = %v, want [%q]", keptIDs, torn.IntentID)
	}
	if warns != 1 {
		t.Fatalf("warns = %d, want 1 for the selection-window torn row", warns)
	}

	reported := make(map[string]struct{}, len(keptIDs))
	for _, id := range keptIDs {
		reported[id] = struct{}{}
	}
	unit := map[string]sharedintent.Row{torn.IntentID: torn}
	unitDrained, unitKeptIDs, err := DrainAbsentAcceptance(
		ctx, now, tel, "test_runner", domain, reader, unit, reported,
	)
	if err != nil {
		t.Fatalf("DrainAbsentAcceptance() error = %v", err)
	}
	if len(unitDrained) != 0 {
		t.Fatalf("unit drained = %v, want none: the orphan is already completed", unitDrained)
	}
	if len(unitKeptIDs) != 0 {
		t.Fatalf("unit keptIDs = %v, want none: the torn row was already reported", unitKeptIDs)
	}
	if warns != 1 {
		t.Fatalf("warns = %d, want still 1: the unit drain must not re-WARN the same row", warns)
	}

	unitWithFresh := map[string]sharedintent.Row{torn.IntentID: torn, freshTorn.IntentID: freshTorn}
	_, freshKeptIDs, err := DrainAbsentAcceptance(
		ctx, now, tel, "test_runner", domain, reader, unitWithFresh, reported,
	)
	if err != nil {
		t.Fatalf("DrainAbsentAcceptance() error = %v", err)
	}
	if len(freshKeptIDs) != 1 || freshKeptIDs[0] != freshTorn.IntentID {
		t.Fatalf("fresh keptIDs = %v, want [%q]: the exclusion must not swallow new rows", freshKeptIDs, freshTorn.IntentID)
	}
	if warns != 2 {
		t.Fatalf("warns = %d, want 2: the genuinely new torn row still WARNs once", warns)
	}
	if len(reader.completedIDs) != 1 || reader.completedIDs[0] != gone.IntentID {
		t.Fatalf("marked = %v, want exactly [%q]: the drain marks once across all drains", reader.completedIDs, gone.IntentID)
	}
}

func TestSelectPartitionBatchDrainsGenerationGoneOrphans(t *testing.T) {
	t.Parallel()

	domain := reducercontract.DomainWorkloadDependency
	healthy := selectionTestRow(domain, "scope-healthy", "unit-healthy", "gen-healthy", 1)
	orphan := selectionTestRow(domain, "scope-orphan", "unit-orphan", "gen-gone", 2)
	reader := &stubAbsentGenerationReader{
		stubSharedIntentReader: stubSharedIntentReader{pending: []sharedintent.Row{healthy, orphan}},
		gone:                   map[string]struct{}{"gen-gone": {}},
	}

	batch, err := SelectPartitionBatch(
		context.Background(), reader, domain, 0, 1, 1,
		absentAcceptanceLookup("scope-healthy", "unit-healthy", "run-1", "gen-healthy"),
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if len(batch.LatestRows) != 1 || batch.LatestRows[0].IntentID != healthy.IntentID {
		t.Fatalf("LatestRows = %v, want the healthy row", batch.LatestRows)
	}
	if len(batch.AbsentDrainedIDs) != 1 || batch.AbsentDrainedIDs[0] != orphan.IntentID {
		t.Fatalf("AbsentDrainedIDs = %v, want [%q]", batch.AbsentDrainedIDs, orphan.IntentID)
	}
	if len(batch.AbsentKeptRows) != 0 {
		t.Fatalf("AbsentKeptRows = %v, want none", batch.AbsentKeptRows)
	}
}

func TestSelectPartitionBatchKeepsTornWritesPending(t *testing.T) {
	t.Parallel()

	domain := reducercontract.DomainWorkloadDependency
	healthy := selectionTestRow(domain, "scope-healthy", "unit-healthy", "gen-healthy", 1)
	torn := selectionTestRow(domain, "scope-torn", "unit-torn", "gen-present", 2)
	reader := &stubAbsentGenerationReader{
		stubSharedIntentReader: stubSharedIntentReader{pending: []sharedintent.Row{healthy, torn}},
		gone:                   map[string]struct{}{},
	}

	batch, err := SelectPartitionBatch(
		context.Background(), reader, domain, 0, 1, 1,
		absentAcceptanceLookup("scope-healthy", "unit-healthy", "run-1", "gen-healthy"),
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if len(batch.AbsentDrainedIDs) != 0 {
		t.Fatalf("AbsentDrainedIDs = %v, want none: the generation still exists", batch.AbsentDrainedIDs)
	}
	if len(batch.AbsentKeptRows) != 1 || batch.AbsentKeptRows[0].IntentID != torn.IntentID {
		t.Fatalf("AbsentKeptRows = %v, want the torn-write row", batch.AbsentKeptRows)
	}
	if len(reader.completedIDs) != 0 {
		t.Fatalf("marked = %v, want no mark from selection itself", reader.completedIDs)
	}
}
