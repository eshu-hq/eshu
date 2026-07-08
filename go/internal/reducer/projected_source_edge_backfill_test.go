// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeProjectedSourceEdgeBackfillReader satisfies
// projectedSourceEdgeBackfillQuerier for tests.
type fakeProjectedSourceEdgeBackfillReader struct {
	enumerateFn func(context.Context, []string) ([]ProjectedSourceEdgeRow, error)
}

func (f fakeProjectedSourceEdgeBackfillReader) EnumerateProjectedSourceEdges(
	ctx context.Context, evidenceSources []string,
) ([]ProjectedSourceEdgeRow, error) {
	if f.enumerateFn != nil {
		return f.enumerateFn(ctx, evidenceSources)
	}
	return nil, nil
}

// fakeProjectedSourceEdgeLedger satisfies ProjectedSourceLedger and records
// RecordProjectedSources calls for assertion. It is a dedicated fixture for
// this backfiller's tests (distinct from fakeProjectedSourceLedger and
// statefulProjectedSourceLedger in projected_source_ledger_test.go) because it
// needs per-call error injection that those fixtures do not model.
type fakeProjectedSourceEdgeLedger struct {
	recorded  map[string][]string // key: evidenceSource|scopeID|generationID
	recordErr error
}

func newFakeProjectedSourceEdgeLedger() *fakeProjectedSourceEdgeLedger {
	return &fakeProjectedSourceEdgeLedger{recorded: make(map[string][]string)}
}

func (l *fakeProjectedSourceEdgeLedger) RecordProjectedSources(
	_ context.Context,
	evidenceSource, scopeID, generationID string,
	sourceUIDs []string,
	_ time.Time,
) error {
	if l.recordErr != nil {
		return l.recordErr
	}
	key := evidenceSource + "|" + scopeID + "|" + generationID
	l.recorded[key] = append([]string(nil), sourceUIDs...)
	return nil
}

func (l *fakeProjectedSourceEdgeLedger) ListSourceUIDsForScopes(
	_ context.Context, _ string, _ []string,
) ([]string, error) {
	return nil, nil
}

func (l *fakeProjectedSourceEdgeLedger) PruneForScopes(
	_ context.Context, _ string, _ []string,
) error {
	return nil
}

// recordedCalls returns recorded RecordProjectedSources calls, sorted by key.
func (l *fakeProjectedSourceEdgeLedger) recordedCalls() []recordedProjectedSourceEdgeCall {
	var calls []recordedProjectedSourceEdgeCall
	for key, uids := range l.recorded {
		parts := splitPipeKey(key)
		calls = append(calls, recordedProjectedSourceEdgeCall{
			evidenceSource: parts[0],
			scopeID:        parts[1],
			generationID:   parts[2],
			sourceUIDs:     uids,
		})
	}
	sort.Slice(calls, func(i, j int) bool {
		ki := calls[i].evidenceSource + "|" + calls[i].scopeID + "|" + calls[i].generationID
		kj := calls[j].evidenceSource + "|" + calls[j].scopeID + "|" + calls[j].generationID
		return ki < kj
	})
	return calls
}

type recordedProjectedSourceEdgeCall struct {
	evidenceSource string
	scopeID        string
	generationID   string
	sourceUIDs     []string
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestProjectedSourceEdgeBackfillerNilReaderNoOp proves that a nil Reader
// results in a no-op (returns nil, nothing enumerated or recorded).
func TestProjectedSourceEdgeBackfillerNilReaderNoOp(t *testing.T) {
	t.Parallel()

	ledger := newFakeProjectedSourceEdgeLedger()
	marker := newFakeBackfillStateMarker()
	b := ProjectedSourceEdgeBackfiller{
		Reader:          nil,
		Ledger:          ledger,
		StateMarker:     marker,
		EvidenceSources: []string{awsRelationshipEvidenceSource},
		Now:             time.Now,
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("Run error with nil Reader: %v", err)
	}
	if len(ledger.recordedCalls()) != 0 {
		t.Fatalf("RecordProjectedSources calls = %d, want 0", len(ledger.recordedCalls()))
	}
}

// TestProjectedSourceEdgeBackfillerNilLedgerNoOp proves that a nil Ledger
// results in a no-op (returns nil).
func TestProjectedSourceEdgeBackfillerNilLedgerNoOp(t *testing.T) {
	t.Parallel()

	enumerateCalled := false
	reader := fakeProjectedSourceEdgeBackfillReader{
		enumerateFn: func(_ context.Context, _ []string) ([]ProjectedSourceEdgeRow, error) {
			enumerateCalled = true
			return nil, nil
		},
	}
	marker := newFakeBackfillStateMarker()
	b := ProjectedSourceEdgeBackfiller{
		Reader:          reader,
		Ledger:          nil,
		StateMarker:     marker,
		EvidenceSources: []string{awsRelationshipEvidenceSource},
		Now:             time.Now,
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("Run error with nil Ledger: %v", err)
	}
	if enumerateCalled {
		t.Fatal("EnumerateProjectedSourceEdges was called but Ledger is nil")
	}
}

// TestProjectedSourceEdgeBackfillerNilStateMarkerNoOp proves that a nil
// StateMarker results in a no-op. Unlike the TAINT_FLOWS_TO backfillers,
// CloudResource relationship types are an open vocabulary, so there is no
// cheap bare-type count guard available to fall back on; the StateMarker is
// the ONLY idempotency guard against a full-graph enumeration on every
// reducer startup, so it is required rather than optional.
func TestProjectedSourceEdgeBackfillerNilStateMarkerNoOp(t *testing.T) {
	t.Parallel()

	enumerateCalled := false
	reader := fakeProjectedSourceEdgeBackfillReader{
		enumerateFn: func(_ context.Context, _ []string) ([]ProjectedSourceEdgeRow, error) {
			enumerateCalled = true
			return nil, nil
		},
	}
	ledger := newFakeProjectedSourceEdgeLedger()
	b := ProjectedSourceEdgeBackfiller{
		Reader:          reader,
		Ledger:          ledger,
		StateMarker:     nil,
		EvidenceSources: []string{awsRelationshipEvidenceSource},
		Now:             time.Now,
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("Run error with nil StateMarker: %v", err)
	}
	if enumerateCalled {
		t.Fatal("EnumerateProjectedSourceEdges was called but StateMarker is nil")
	}
	if len(ledger.recordedCalls()) != 0 {
		t.Fatalf("RecordProjectedSources calls = %d, want 0", len(ledger.recordedCalls()))
	}
}

// TestProjectedSourceEdgeBackfillerEnumerateGroupsAndRecords proves the core
// path: for sources not yet marked complete, the backfiller enumerates
// existing graph edges, groups rows by (evidence_source, scope_id,
// generation_id), and records each group into the ledger — seeding it so a
// subsequent ledger-anchored retract can find pre-ledger edges instead of
// treating them as absent (the orphan scenario this backfiller exists to
// prevent).
func TestProjectedSourceEdgeBackfillerEnumerateGroupsAndRecords(t *testing.T) {
	t.Parallel()

	reader := fakeProjectedSourceEdgeBackfillReader{
		enumerateFn: func(_ context.Context, sources []string) ([]ProjectedSourceEdgeRow, error) {
			return []ProjectedSourceEdgeRow{
				{EvidenceSource: awsRelationshipEvidenceSource, ScopeID: "scope-1", GenerationID: "gen-1", SourceUID: "uid-a"},
				{EvidenceSource: awsRelationshipEvidenceSource, ScopeID: "scope-1", GenerationID: "gen-1", SourceUID: "uid-b"},
				{EvidenceSource: awsRelationshipEvidenceSource, ScopeID: "scope-2", GenerationID: "gen-2", SourceUID: "uid-c"},
				{EvidenceSource: observabilityCoverageEvidenceSource, ScopeID: "scope-1", GenerationID: "gen-1", SourceUID: "uid-d"},
			}, nil
		},
	}
	ledger := newFakeProjectedSourceEdgeLedger()
	marker := newFakeBackfillStateMarker()

	b := ProjectedSourceEdgeBackfiller{
		Reader:          reader,
		Ledger:          ledger,
		StateMarker:     marker,
		EvidenceSources: []string{awsRelationshipEvidenceSource, observabilityCoverageEvidenceSource},
		Now:             time.Now,
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	calls := ledger.recordedCalls()
	if len(calls) != 3 {
		t.Fatalf("RecordProjectedSources calls = %d, want 3", len(calls))
	}
	checkProjectedSourceEdgeCall(t, calls[0], awsRelationshipEvidenceSource, "scope-1", "gen-1", []string{"uid-a", "uid-b"})
	checkProjectedSourceEdgeCall(t, calls[1], awsRelationshipEvidenceSource, "scope-2", "gen-2", []string{"uid-c"})
	checkProjectedSourceEdgeCall(t, calls[2], observabilityCoverageEvidenceSource, "scope-1", "gen-1", []string{"uid-d"})

	for _, src := range []string{awsRelationshipEvidenceSource, observabilityCoverageEvidenceSource} {
		key := projectedSourceEdgeBackfillKey(src)
		if _, ok := marker.markComplete[key]; !ok {
			t.Fatalf("MarkComplete was NOT called for key %q", key)
		}
	}
}

// TestProjectedSourceEdgeBackfillerPerSourceCompletionSkipsMarkedSource proves
// that a source already marked complete is excluded from enumeration while a
// sibling source not yet marked complete still backfills.
func TestProjectedSourceEdgeBackfillerPerSourceCompletionSkipsMarkedSource(t *testing.T) {
	t.Parallel()

	var enumeratedSources []string
	reader := fakeProjectedSourceEdgeBackfillReader{
		enumerateFn: func(_ context.Context, sources []string) ([]ProjectedSourceEdgeRow, error) {
			enumeratedSources = append([]string(nil), sources...)
			return []ProjectedSourceEdgeRow{
				{EvidenceSource: gcpRelationshipEvidenceSource, ScopeID: "scope-1", GenerationID: "gen-1", SourceUID: "uid-x"},
			}, nil
		},
	}
	ledger := newFakeProjectedSourceEdgeLedger()
	marker := newFakeBackfillStateMarker()
	marker.complete[projectedSourceEdgeBackfillKey(awsRelationshipEvidenceSource)] = true

	b := ProjectedSourceEdgeBackfiller{
		Reader:          reader,
		Ledger:          ledger,
		StateMarker:     marker,
		EvidenceSources: []string{awsRelationshipEvidenceSource, gcpRelationshipEvidenceSource},
		Now:             time.Now,
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if len(enumeratedSources) != 1 || enumeratedSources[0] != gcpRelationshipEvidenceSource {
		t.Fatalf("enumeratedSources = %v, want [%q]", enumeratedSources, gcpRelationshipEvidenceSource)
	}
	calls := ledger.recordedCalls()
	if len(calls) != 1 {
		t.Fatalf("RecordProjectedSources calls = %d, want 1", len(calls))
	}
	checkProjectedSourceEdgeCall(t, calls[0], gcpRelationshipEvidenceSource, "scope-1", "gen-1", []string{"uid-x"})
}

// TestProjectedSourceEdgeBackfillerAllSourcesCompleteSkipsEnumeration proves
// that when every configured source is already marked complete, enumeration
// never runs at all.
func TestProjectedSourceEdgeBackfillerAllSourcesCompleteSkipsEnumeration(t *testing.T) {
	t.Parallel()

	enumerateCalled := false
	reader := fakeProjectedSourceEdgeBackfillReader{
		enumerateFn: func(_ context.Context, _ []string) ([]ProjectedSourceEdgeRow, error) {
			enumerateCalled = true
			return nil, nil
		},
	}
	ledger := newFakeProjectedSourceEdgeLedger()
	marker := newFakeBackfillStateMarker()
	marker.complete[projectedSourceEdgeBackfillKey(awsRelationshipEvidenceSource)] = true
	marker.complete[projectedSourceEdgeBackfillKey(azureRelationshipEvidenceSource)] = true

	b := ProjectedSourceEdgeBackfiller{
		Reader:          reader,
		Ledger:          ledger,
		StateMarker:     marker,
		EvidenceSources: []string{awsRelationshipEvidenceSource, azureRelationshipEvidenceSource},
		Now:             time.Now,
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if enumerateCalled {
		t.Fatal("EnumerateProjectedSourceEdges was called but all sources already complete")
	}
	if len(ledger.recordedCalls()) != 0 {
		t.Fatalf("RecordProjectedSources calls = %d, want 0", len(ledger.recordedCalls()))
	}
}

// TestProjectedSourceEdgeBackfillerIdempotentOnSecondRun proves that running
// the backfiller twice — the second time with markers already set by the
// first run — is a no-op on the second run (no re-enumeration, no duplicate
// records).
func TestProjectedSourceEdgeBackfillerIdempotentOnSecondRun(t *testing.T) {
	t.Parallel()

	enumerateCalls := 0
	reader := fakeProjectedSourceEdgeBackfillReader{
		enumerateFn: func(_ context.Context, _ []string) ([]ProjectedSourceEdgeRow, error) {
			enumerateCalls++
			return []ProjectedSourceEdgeRow{
				{EvidenceSource: awsRelationshipEvidenceSource, ScopeID: "scope-1", GenerationID: "gen-1", SourceUID: "uid-a"},
			}, nil
		},
	}
	ledger := newFakeProjectedSourceEdgeLedger()
	marker := newFakeBackfillStateMarker()

	b := ProjectedSourceEdgeBackfiller{
		Reader:          reader,
		Ledger:          ledger,
		StateMarker:     marker,
		EvidenceSources: []string{awsRelationshipEvidenceSource},
		Now:             time.Now,
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("first Run error: %v", err)
	}
	if enumerateCalls != 1 {
		t.Fatalf("enumerateCalls after first run = %d, want 1", enumerateCalls)
	}
	if len(ledger.recordedCalls()) != 1 {
		t.Fatalf("recorded calls after first run = %d, want 1", len(ledger.recordedCalls()))
	}

	// fakeBackfillStateMarker tracks MarkComplete calls and IsComplete
	// answers in separate maps (by design, so other tests can assert
	// MarkComplete was invoked independently of IsComplete's answer). A real
	// backing store (CodeValueFlowBackfillStateStore) makes MarkComplete
	// immediately observable to a later IsComplete call, so promote the
	// first run's MarkComplete result into the completion map here to
	// simulate that persistence before driving the second Run.
	key := projectedSourceEdgeBackfillKey(awsRelationshipEvidenceSource)
	if _, ok := marker.markComplete[key]; !ok {
		t.Fatalf("MarkComplete was NOT called for key %q after first run", key)
	}
	marker.complete[key] = true

	// Second run: marker is now set, so this must be a full no-op.
	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("second Run error: %v", err)
	}
	if enumerateCalls != 1 {
		t.Fatalf("enumerateCalls after second run = %d, want 1 (no re-enumeration)", enumerateCalls)
	}
	if len(ledger.recordedCalls()) != 1 {
		t.Fatalf("recorded calls after second run = %d, want 1 (still just the first run's record)", len(ledger.recordedCalls()))
	}
}

// TestProjectedSourceEdgeBackfillerRecordErrorSkipsMarkComplete proves that
// when RecordProjectedSources fails, the error is returned and MarkComplete is
// NOT called, so the next startup retries the backfill instead of silently
// treating a partially-seeded source as done.
func TestProjectedSourceEdgeBackfillerRecordErrorSkipsMarkComplete(t *testing.T) {
	t.Parallel()

	reader := fakeProjectedSourceEdgeBackfillReader{
		enumerateFn: func(_ context.Context, _ []string) ([]ProjectedSourceEdgeRow, error) {
			return []ProjectedSourceEdgeRow{
				{EvidenceSource: awsRelationshipEvidenceSource, ScopeID: "scope-1", GenerationID: "gen-1", SourceUID: "uid-a"},
			}, nil
		},
	}
	ledger := newFakeProjectedSourceEdgeLedger()
	ledger.recordErr = errors.New("record failed")
	marker := newFakeBackfillStateMarker()

	b := ProjectedSourceEdgeBackfiller{
		Reader:          reader,
		Ledger:          ledger,
		StateMarker:     marker,
		EvidenceSources: []string{awsRelationshipEvidenceSource},
		Now:             time.Now,
	}

	err := b.Run(context.Background())
	if err == nil {
		t.Fatal("Run error = nil, want record-failed error")
	}

	key := projectedSourceEdgeBackfillKey(awsRelationshipEvidenceSource)
	if _, ok := marker.markComplete[key]; ok {
		t.Fatalf("MarkComplete WAS called for key %q, want skipped after record error", key)
	}
}

// TestProjectedSourceEdgeBackfillerSkipsRowsWithEmptyFields proves that
// enumerated rows with any empty identity field are dropped rather than
// recorded with a blank key component.
func TestProjectedSourceEdgeBackfillerSkipsRowsWithEmptyFields(t *testing.T) {
	t.Parallel()

	reader := fakeProjectedSourceEdgeBackfillReader{
		enumerateFn: func(_ context.Context, _ []string) ([]ProjectedSourceEdgeRow, error) {
			return []ProjectedSourceEdgeRow{
				{EvidenceSource: awsRelationshipEvidenceSource, ScopeID: "", GenerationID: "gen-1", SourceUID: "uid-a"},
				{EvidenceSource: awsRelationshipEvidenceSource, ScopeID: "scope-1", GenerationID: "gen-1", SourceUID: "uid-b"},
			}, nil
		},
	}
	ledger := newFakeProjectedSourceEdgeLedger()
	marker := newFakeBackfillStateMarker()

	b := ProjectedSourceEdgeBackfiller{
		Reader:          reader,
		Ledger:          ledger,
		StateMarker:     marker,
		EvidenceSources: []string{awsRelationshipEvidenceSource},
		Now:             time.Now,
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	calls := ledger.recordedCalls()
	if len(calls) != 1 {
		t.Fatalf("RecordProjectedSources calls = %d, want 1 (blank-scope row dropped upstream by reader)", len(calls))
	}
	checkProjectedSourceEdgeCall(t, calls[0], awsRelationshipEvidenceSource, "scope-1", "gen-1", []string{"uid-b"})
}

func checkProjectedSourceEdgeCall(
	t *testing.T, call recordedProjectedSourceEdgeCall, wantSource, wantScope, wantGen string, wantUIDs []string,
) {
	t.Helper()
	if call.evidenceSource != wantSource {
		t.Errorf("evidenceSource = %q, want %q", call.evidenceSource, wantSource)
	}
	if call.scopeID != wantScope {
		t.Errorf("scopeID = %q, want %q", call.scopeID, wantScope)
	}
	if call.generationID != wantGen {
		t.Errorf("generationID = %q, want %q", call.generationID, wantGen)
	}
	if !stringSlicesEqual(call.sourceUIDs, wantUIDs) {
		t.Errorf("sourceUIDs = %v, want %v", call.sourceUIDs, wantUIDs)
	}
}
