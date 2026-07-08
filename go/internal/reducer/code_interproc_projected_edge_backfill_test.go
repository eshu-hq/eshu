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

// fakeBackfillReader satisfies codeInterprocBackfillQuerier for tests.
type fakeBackfillReader struct {
	count       int64
	countErr    error
	enumerateFn func(context.Context, []string) ([]ProjectedTaintEdgeRow, error)
}

func (f fakeBackfillReader) CountTaintFlowsToEdges(ctx context.Context) (int64, error) {
	if f.countErr != nil {
		return 0, f.countErr
	}
	return f.count, nil
}

func (f fakeBackfillReader) EnumerateProjectedTaintEdges(
	ctx context.Context, evidenceSources []string,
) ([]ProjectedTaintEdgeRow, error) {
	if f.enumerateFn != nil {
		return f.enumerateFn(ctx, evidenceSources)
	}
	return nil, nil
}

// fakeBackfillLedger satisfies CodeInterprocProjectedEdgeLedger and records
// RecordProjectedEdges calls for assertion.
type fakeBackfillLedger struct {
	recorded   map[string][]string // key: evidenceSource|scopeID|generationID
	hasRows    map[string]bool
	hasRowsErr error
	recordErr  error
}

func newFakeBackfillLedger() *fakeBackfillLedger {
	return &fakeBackfillLedger{
		recorded: make(map[string][]string),
		hasRows:  make(map[string]bool),
	}
}

func (l *fakeBackfillLedger) RecordProjectedEdges(
	_ context.Context,
	evidenceSource, scopeID, generationID string,
	sourceFunctionUIDs []string,
	_ time.Time,
) error {
	if l.recordErr != nil {
		return l.recordErr
	}
	key := evidenceSource + "|" + scopeID + "|" + generationID
	l.recorded[key] = append([]string(nil), sourceFunctionUIDs...)
	return nil
}

func (l *fakeBackfillLedger) LedgerHasRowsForSource(
	_ context.Context,
	evidenceSource string,
) (bool, error) {
	if l.hasRowsErr != nil {
		return false, l.hasRowsErr
	}
	return l.hasRows[evidenceSource], nil
}

// recordedCalls returns recorded RecordProjectedEdges calls, sorted by key.
func (l *fakeBackfillLedger) recordedCalls() []recordedLedgerCall {
	var calls []recordedLedgerCall
	for key, uids := range l.recorded {
		parts := splitPipeKey(key)
		calls = append(calls, recordedLedgerCall{
			evidenceSource:     parts[0],
			scopeID:            parts[1],
			generationID:       parts[2],
			sourceFunctionUIDs: uids,
		})
	}
	sort.Slice(calls, func(i, j int) bool {
		ki := calls[i].evidenceSource + "|" + calls[i].scopeID + "|" + calls[i].generationID
		kj := calls[j].evidenceSource + "|" + calls[j].scopeID + "|" + calls[j].generationID
		return ki < kj
	})
	return calls
}

type recordedLedgerCall struct {
	evidenceSource     string
	scopeID            string
	generationID       string
	sourceFunctionUIDs []string
}

func splitPipeKey(key string) [3]string {
	var parts [3]string
	si := 0
	for i := 0; i < 3; i++ {
		next := indexByteIn(key, '|', si)
		if next < 0 {
			parts[i] = key[si:]
			return parts
		}
		parts[i] = key[si:next]
		si = next + 1
	}
	return parts
}

func indexByteIn(s string, b byte, start int) int {
	for i := start; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// Satisfy the full CodeInterprocProjectedEdgeLedger interface.
func (l *fakeBackfillLedger) ListSourceUIDsForScopes(_ context.Context, _ string, _ []string) ([]string, error) {
	return nil, nil
}

func (l *fakeBackfillLedger) ListSourceUIDsForSource(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (l *fakeBackfillLedger) ListStaleSourceUIDs(_ context.Context, _ string, _ string, _ string, _ int) ([]string, error) {
	return nil, nil
}

func (l *fakeBackfillLedger) PruneForScopes(_ context.Context, _ string, _ []string) error {
	return nil
}

func (l *fakeBackfillLedger) PruneForSource(_ context.Context, _ string) error { return nil }

func (l *fakeBackfillLedger) PruneStaleForUIDs(_ context.Context, _ string, _ string, _ string, _ []string) error {
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestCodeInterprocProjectedEdgeBackfillerCountZeroNoOp proves that when the
// graph has zero TAINT_FLOWS_TO edges, the backfiller returns nil without
// calling EnumerateProjectedTaintEdges or RecordProjectedEdges.
func TestCodeInterprocProjectedEdgeBackfillerCountZeroNoOp(t *testing.T) {
	t.Parallel()

	ledger := newFakeBackfillLedger()
	b := CodeInterprocProjectedEdgeBackfiller{
		Reader:          fakeBackfillReader{count: 0},
		Ledger:          ledger,
		EvidenceSources: []string{codeInterprocEvidenceSource, codeInterprocFixpointEvidenceSource},
		Now:             time.Now,
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(ledger.recordedCalls()) != 0 {
		t.Fatalf("RecordProjectedEdges calls = %d, want 0", len(ledger.recordedCalls()))
	}
}

// TestCodeInterprocProjectedEdgeBackfillerCountPositiveLedgerEmptyBackfills proves
// that when count > 0 and the ledger has no rows for a source, the backfiller
// enumerates and records grouped rows.
func TestCodeInterprocProjectedEdgeBackfillerCountPositiveLedgerEmptyBackfills(t *testing.T) {
	t.Parallel()

	reader := fakeBackfillReader{
		count: 5,
		enumerateFn: func(_ context.Context, sources []string) ([]ProjectedTaintEdgeRow, error) {
			return []ProjectedTaintEdgeRow{
				{EvidenceSource: codeInterprocEvidenceSource, ScopeID: "scope-1", GenerationID: "gen-1", SourceFunctionUID: "uid-a"},
				{EvidenceSource: codeInterprocEvidenceSource, ScopeID: "scope-1", GenerationID: "gen-1", SourceFunctionUID: "uid-b"},
				{EvidenceSource: codeInterprocEvidenceSource, ScopeID: "scope-2", GenerationID: "gen-2", SourceFunctionUID: "uid-c"},
				{EvidenceSource: codeInterprocFixpointEvidenceSource, ScopeID: "scope-1", GenerationID: "gen-1", SourceFunctionUID: "uid-d"},
			}, nil
		},
	}
	ledger := newFakeBackfillLedger()

	b := CodeInterprocProjectedEdgeBackfiller{
		Reader:          reader,
		Ledger:          ledger,
		EvidenceSources: []string{codeInterprocEvidenceSource, codeInterprocFixpointEvidenceSource},
		Now:             time.Now,
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	calls := ledger.recordedCalls()
	if len(calls) != 3 {
		t.Fatalf("RecordProjectedEdges calls = %d, want 3", len(calls))
	}

	// Keys sorted alphabetically by evidence source.
	checkCall(t, calls[0], codeInterprocFixpointEvidenceSource, "scope-1", "gen-1", []string{"uid-d"})
	checkCall(t, calls[1], codeInterprocEvidenceSource, "scope-1", "gen-1", []string{"uid-a", "uid-b"})
	checkCall(t, calls[2], codeInterprocEvidenceSource, "scope-2", "gen-2", []string{"uid-c"})
}

// TestCodeInterprocProjectedEdgeBackfillerSkipsSourcesWithExistingLedgerRows proves
// that when count > 0 but the ledger already has rows for a source, that
// source is skipped (idempotent once).
func TestCodeInterprocProjectedEdgeBackfillerSkipsSourcesWithExistingLedgerRows(t *testing.T) {
	t.Parallel()

	reader := fakeBackfillReader{
		count: 5,
		enumerateFn: func(_ context.Context, sources []string) ([]ProjectedTaintEdgeRow, error) {
			return []ProjectedTaintEdgeRow{
				{EvidenceSource: codeInterprocFixpointEvidenceSource, ScopeID: "scope-1", GenerationID: "gen-1", SourceFunctionUID: "uid-x"},
			}, nil
		},
	}
	ledger := newFakeBackfillLedger()
	ledger.hasRows[codeInterprocEvidenceSource] = true          // skip
	ledger.hasRows[codeInterprocFixpointEvidenceSource] = false // backfill

	b := CodeInterprocProjectedEdgeBackfiller{
		Reader:          reader,
		Ledger:          ledger,
		EvidenceSources: []string{codeInterprocEvidenceSource, codeInterprocFixpointEvidenceSource},
		Now:             time.Now,
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	calls := ledger.recordedCalls()
	if len(calls) != 1 {
		t.Fatalf("RecordProjectedEdges calls = %d, want 1 (only fixpoint source)", len(calls))
	}
	checkCall(t, calls[0], codeInterprocFixpointEvidenceSource, "scope-1", "gen-1", []string{"uid-x"})
}

// TestCodeInterprocProjectedEdgeBackfillerBothSourcesAlreadyBackfilledSkipsAll proves
// that when both sources already have ledger rows, enumeration is skipped entirely.
func TestCodeInterprocProjectedEdgeBackfillerBothSourcesAlreadyBackfilledSkipsAll(t *testing.T) {
	t.Parallel()

	enumerateCalled := false
	reader := fakeBackfillReader{
		count: 5,
		enumerateFn: func(_ context.Context, sources []string) ([]ProjectedTaintEdgeRow, error) {
			enumerateCalled = true
			return nil, nil
		},
	}
	ledger := newFakeBackfillLedger()
	ledger.hasRows[codeInterprocEvidenceSource] = true
	ledger.hasRows[codeInterprocFixpointEvidenceSource] = true

	b := CodeInterprocProjectedEdgeBackfiller{
		Reader:          reader,
		Ledger:          ledger,
		EvidenceSources: []string{codeInterprocEvidenceSource, codeInterprocFixpointEvidenceSource},
		Now:             time.Now,
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if enumerateCalled {
		t.Fatalf("EnumerateProjectedTaintEdges was called but both sources already backfilled")
	}
	if len(ledger.recordedCalls()) != 0 {
		t.Fatalf("RecordProjectedEdges calls = %d, want 0", len(ledger.recordedCalls()))
	}
}

// TestCodeInterprocProjectedEdgeBackfillerNilReaderNoOp proves that a nil
// Reader results in a no-op (returns nil).
func TestCodeInterprocProjectedEdgeBackfillerNilReaderNoOp(t *testing.T) {
	t.Parallel()

	ledger := newFakeBackfillLedger()
	b := CodeInterprocProjectedEdgeBackfiller{
		Reader:          nil,
		Ledger:          ledger,
		EvidenceSources: []string{codeInterprocEvidenceSource},
		Now:             time.Now,
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("Run error with nil Reader: %v", err)
	}
}

// TestCodeInterprocProjectedEdgeBackfillerNilLedgerNoOp proves that a nil
// Ledger results in a no-op (returns nil).
func TestCodeInterprocProjectedEdgeBackfillerNilLedgerNoOp(t *testing.T) {
	t.Parallel()

	reader := fakeBackfillReader{count: 5}
	b := CodeInterprocProjectedEdgeBackfiller{
		Reader:          reader,
		Ledger:          nil,
		EvidenceSources: []string{codeInterprocEvidenceSource},
		Now:             time.Now,
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("Run error with nil Ledger: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Fake backfill state marker
// ---------------------------------------------------------------------------
type fakeBackfillStateMarker struct {
	complete     map[string]bool
	markComplete map[string]time.Time
}

func newFakeBackfillStateMarker() *fakeBackfillStateMarker {
	return &fakeBackfillStateMarker{
		complete:     make(map[string]bool),
		markComplete: make(map[string]time.Time),
	}
}

func (m *fakeBackfillStateMarker) IsComplete(_ context.Context, key string) (bool, error) {
	return m.complete[key], nil
}

func (m *fakeBackfillStateMarker) MarkComplete(_ context.Context, key string, at time.Time) error {
	m.markComplete[key] = at
	return nil
}

// ---------------------------------------------------------------------------
// StateMarker-driven backfill tests
// ---------------------------------------------------------------------------

// TestCodeInterprocProjectedEdgeBackfillerStateMarkerNotCompleteBackfills proves
// that when count > 0 and the marker says the source is NOT complete, the
// backfiller enumerates, records, and calls MarkComplete.
func TestCodeInterprocProjectedEdgeBackfillerStateMarkerNotCompleteBackfills(t *testing.T) {
	t.Parallel()

	reader := fakeBackfillReader{
		count: 5,
		enumerateFn: func(_ context.Context, sources []string) ([]ProjectedTaintEdgeRow, error) {
			return []ProjectedTaintEdgeRow{
				{EvidenceSource: codeInterprocEvidenceSource, ScopeID: "scope-1", GenerationID: "gen-1", SourceFunctionUID: "uid-a"},
			}, nil
		},
	}
	ledger := newFakeBackfillLedger()
	marker := newFakeBackfillStateMarker()

	b := CodeInterprocProjectedEdgeBackfiller{
		Reader:          reader,
		Ledger:          ledger,
		StateMarker:     marker,
		EvidenceSources: []string{codeInterprocEvidenceSource},
		Now:             time.Now,
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if len(ledger.recordedCalls()) != 1 {
		t.Fatalf("RecordProjectedEdges calls = %d, want 1", len(ledger.recordedCalls()))
	}

	key := codeInterprocBackfillKey(codeInterprocEvidenceSource)
	if _, ok := marker.markComplete[key]; !ok {
		t.Fatalf("MarkComplete was NOT called for key %q", key)
	}
}

// TestCodeInterprocProjectedEdgeBackfillerStateMarkerCompleteSkips proves that
// when count > 0 but the marker says the source IS complete, the backfiller
// skips enumeration entirely.
func TestCodeInterprocProjectedEdgeBackfillerStateMarkerCompleteSkips(t *testing.T) {
	t.Parallel()

	enumerateCalled := false
	reader := fakeBackfillReader{
		count: 5,
		enumerateFn: func(_ context.Context, sources []string) ([]ProjectedTaintEdgeRow, error) {
			enumerateCalled = true
			return nil, nil
		},
	}
	ledger := newFakeBackfillLedger()
	marker := newFakeBackfillStateMarker()
	marker.complete[codeInterprocBackfillKey(codeInterprocEvidenceSource)] = true

	b := CodeInterprocProjectedEdgeBackfiller{
		Reader:          reader,
		Ledger:          ledger,
		StateMarker:     marker,
		EvidenceSources: []string{codeInterprocEvidenceSource},
		Now:             time.Now,
	}

	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if enumerateCalled {
		t.Fatalf("EnumerateProjectedTaintEdges was called but source is already marked complete")
	}
	if len(ledger.recordedCalls()) != 0 {
		t.Fatalf("RecordProjectedEdges calls = %d, want 0", len(ledger.recordedCalls()))
	}
}

// TestCodeInterprocProjectedEdgeBackfillerRecordErrorSkipsMarkComplete proves
// that when RecordProjectedEdges fails, the error is returned and MarkComplete
// is NOT called (so the next startup re-runs the backfill).
func TestCodeInterprocProjectedEdgeBackfillerRecordErrorSkipsMarkComplete(t *testing.T) {
	t.Parallel()

	reader := fakeBackfillReader{
		count: 5,
		enumerateFn: func(_ context.Context, sources []string) ([]ProjectedTaintEdgeRow, error) {
			return []ProjectedTaintEdgeRow{
				{EvidenceSource: codeInterprocEvidenceSource, ScopeID: "scope-1", GenerationID: "gen-1", SourceFunctionUID: "uid-a"},
			}, nil
		},
	}
	ledger := newFakeBackfillLedger()
	ledger.recordErr = errors.New("record failed")
	marker := newFakeBackfillStateMarker()

	b := CodeInterprocProjectedEdgeBackfiller{
		Reader:          reader,
		Ledger:          ledger,
		StateMarker:     marker,
		EvidenceSources: []string{codeInterprocEvidenceSource},
		Now:             time.Now,
	}

	err := b.Run(context.Background())
	if err == nil {
		t.Fatal("Run error = nil, want record-failed error")
	}

	key := codeInterprocBackfillKey(codeInterprocEvidenceSource)
	if _, ok := marker.markComplete[key]; ok {
		t.Fatalf("MarkComplete WAS called for key %q, want skipped after record error", key)
	}
}

func checkCall(t *testing.T, call recordedLedgerCall, wantSource, wantScope, wantGen string, wantUIDs []string) {
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
	if !stringSlicesEqual(call.sourceFunctionUIDs, wantUIDs) {
		t.Errorf("sourceFunctionUIDs = %v, want %v", call.sourceFunctionUIDs, wantUIDs)
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
