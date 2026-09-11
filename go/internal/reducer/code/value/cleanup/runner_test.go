// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cleanup

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/taint"
)

func TestCodeValueFlowStaleCleanupRunnerSweepsBothEvidenceFamilies(t *testing.T) {
	reader := &fakeCodeValueFlowCurrentGenerationReader{
		rows: []CurrentGeneration{
			{ScopeID: "scope-a", GenerationID: "gen-current-a"},
			{ScopeID: "scope-b", GenerationID: "gen-current-b"},
		},
	}
	taintSweeper := &recordingCodeValueFlowTaintSweeper{}
	interproc := &recordingCodeValueFlowInterprocSweeper{}
	leaseManager := &fakeCodeValueFlowLeaseManager{claimResults: []bool{true}}
	runner := &Runner{
		CurrentGenerations: reader,
		TaintEvidence:      taintSweeper,
		InterprocEvidence:  interproc,
		LeaseManager:       leaseManager,
		Config: RunnerConfig{
			LeaseOwner:       "value-flow-owner",
			LeaseTTL:         2 * time.Minute,
			ScopeBatchLimit:  25,
			DeleteBatchLimit: 50,
		},
	}

	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}
	if !result.LeaseAcquired {
		t.Fatal("LeaseAcquired = false, want true")
	}
	if got, want := result.ScopesScanned, 2; got != want {
		t.Fatalf("ScopesScanned = %d, want %d", got, want)
	}
	if got, want := result.TaintSweeps, 2; got != want {
		t.Fatalf("TaintSweeps = %d, want %d", got, want)
	}
	if got, want := result.InterprocSweeps, 2; got != want {
		t.Fatalf("InterprocSweeps = %d, want %d", got, want)
	}
	if !result.CursorExhausted {
		t.Fatal("CursorExhausted = false, want true after a partial page")
	}
	if got := len(taintSweeper.calls); got != 2 {
		t.Fatalf("taintSweeper calls = %d, want 2", got)
	}
	if got := len(interproc.calls); got != 2 {
		t.Fatalf("interproc calls = %d, want 2", got)
	}
	if call := taintSweeper.calls[0]; call.scopeID != "scope-a" ||
		call.generationID != "gen-current-a" ||
		call.evidenceSource != taint.CodeTaintEvidenceSource() ||
		call.limit != 50 {
		t.Fatalf("first taintSweeper call = %+v, want current scope/generation/source/limit", call)
	}
	if call := interproc.calls[1]; call.scopeID != "scope-b" ||
		call.generationID != "gen-current-b" ||
		call.evidenceSource != taint.CodeInterprocEvidenceSource() ||
		call.limit != 50 {
		t.Fatalf("second interproc call = %+v, want current scope/generation/source/limit", call)
	}
	if got := reader.afterScopeIDs; len(got) != 1 || got[0] != "" {
		t.Fatalf("after scope ids = %v, want one first-page read", got)
	}
	if leaseManager.releaseCalls != 1 {
		t.Fatalf("release calls = %d, want 1", leaseManager.releaseCalls)
	}
}

func TestCodeValueFlowStaleCleanupRunnerSkipsWhenLeaseUnavailable(t *testing.T) {
	reader := &fakeCodeValueFlowCurrentGenerationReader{
		rows: []CurrentGeneration{{ScopeID: "scope-a", GenerationID: "gen-a"}},
	}
	taintSweeper := &recordingCodeValueFlowTaintSweeper{}
	interproc := &recordingCodeValueFlowInterprocSweeper{}
	leaseManager := &fakeCodeValueFlowLeaseManager{claimResults: []bool{false}}
	runner := &Runner{
		CurrentGenerations: reader,
		TaintEvidence:      taintSweeper,
		InterprocEvidence:  interproc,
		LeaseManager:       leaseManager,
		Config:             RunnerConfig{LeaseOwner: "value-flow-owner"},
	}

	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}
	if result.LeaseAcquired {
		t.Fatal("LeaseAcquired = true, want false")
	}
	if len(reader.afterScopeIDs) != 0 {
		t.Fatalf("reader calls = %d, want 0 without a lease", len(reader.afterScopeIDs))
	}
	if len(taintSweeper.calls) != 0 || len(interproc.calls) != 0 {
		t.Fatalf("sweeper calls = %d/%d, want 0/0 without a lease", len(taintSweeper.calls), len(interproc.calls))
	}
	if leaseManager.releaseCalls != 0 {
		t.Fatalf("release calls = %d, want 0 without a claimed lease", leaseManager.releaseCalls)
	}
}

func TestCodeValueFlowStaleCleanupRunnerCursorPagesWithoutWrappingHot(t *testing.T) {
	reader := &fakeCodeValueFlowCurrentGenerationReader{
		pages: [][]CurrentGeneration{
			{
				{ScopeID: "scope-a", GenerationID: "gen-a"},
				{ScopeID: "scope-b", GenerationID: "gen-b"},
			},
			nil,
			{{ScopeID: "scope-a", GenerationID: "gen-a"}},
		},
	}
	taintSweeper := &recordingCodeValueFlowTaintSweeper{}
	interproc := &recordingCodeValueFlowInterprocSweeper{}
	runner := &Runner{
		CurrentGenerations: reader,
		TaintEvidence:      taintSweeper,
		InterprocEvidence:  interproc,
		Config: RunnerConfig{
			ScopeBatchLimit:  2,
			DeleteBatchLimit: 10,
		},
	}

	first, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("first RunOnce() error = %v", err)
	}
	if first.CursorExhausted {
		t.Fatal("first CursorExhausted = true, want false for a full page")
	}
	second, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("second RunOnce() error = %v", err)
	}
	if !second.CursorExhausted {
		t.Fatal("second CursorExhausted = false, want true at end of cursor")
	}
	third, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("third RunOnce() error = %v", err)
	}
	if got := third.ScopesScanned; got != 1 {
		t.Fatalf("third ScopesScanned = %d, want restart from first page after exhaustion", got)
	}
	if got, want := reader.afterScopeIDs, []string{"", "scope-b", ""}; !equalCodeValueFlowStringSlices(got, want) {
		t.Fatalf("after scope ids = %v, want %v", got, want)
	}
}

func TestCodeValueFlowStaleCleanupRunnerValidation(t *testing.T) {
	runner := &Runner{}

	_, err := runner.RunOnce(context.Background())

	if err == nil {
		t.Fatal("RunOnce() error = nil, want validation error")
	}
	if !errors.Is(err, ErrCurrentGenerationsRequired) {
		t.Fatalf("RunOnce() error = %v, want ErrCurrentGenerationsRequired", err)
	}
}

type fakeCodeValueFlowCurrentGenerationReader struct {
	rows          []CurrentGeneration
	pages         [][]CurrentGeneration
	afterScopeIDs []string
	limits        []int
}

func (r *fakeCodeValueFlowCurrentGenerationReader) ListCurrentCodeValueFlowGenerations(
	_ context.Context,
	afterScopeID string,
	limit int,
) ([]CurrentGeneration, error) {
	r.afterScopeIDs = append(r.afterScopeIDs, afterScopeID)
	r.limits = append(r.limits, limit)
	if len(r.pages) > 0 {
		page := r.pages[0]
		r.pages = r.pages[1:]
		return page, nil
	}
	return r.rows, nil
}

type codeValueFlowSweepCall struct {
	scopeID        string
	generationID   string
	evidenceSource string
	limit          int
}

type recordingCodeValueFlowTaintSweeper struct {
	mu    sync.Mutex
	calls []codeValueFlowSweepCall
}

func (w *recordingCodeValueFlowTaintSweeper) RetractStaleCodeTaintEvidence(
	_ context.Context,
	scopeID string,
	generationID string,
	evidenceSource string,
	limit int,
) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, codeValueFlowSweepCall{
		scopeID:        scopeID,
		generationID:   generationID,
		evidenceSource: evidenceSource,
		limit:          limit,
	})
	return nil
}

type recordingCodeValueFlowInterprocSweeper struct {
	calls       []codeValueFlowSweepCall
	byUIDsCalls []codeValueFlowSweepCall
}

func (w *recordingCodeValueFlowInterprocSweeper) RetractStaleCodeInterprocEvidence(
	_ context.Context,
	scopeID string,
	generationID string,
	evidenceSource string,
	limit int,
) error {
	w.calls = append(w.calls, codeValueFlowSweepCall{
		scopeID:        scopeID,
		generationID:   generationID,
		evidenceSource: evidenceSource,
		limit:          limit,
	})
	return nil
}

func (w *recordingCodeValueFlowInterprocSweeper) RetractStaleCodeInterprocEvidenceByUIDs(
	_ context.Context, sourceUIDs []string, scopeID, generationID, evidenceSource string,
) error {
	w.byUIDsCalls = append(w.byUIDsCalls, codeValueFlowSweepCall{
		scopeID:        scopeID,
		generationID:   generationID,
		evidenceSource: evidenceSource,
		limit:          len(sourceUIDs),
	})
	return nil
}

// CodeInterprocEvidenceWriter methods (not needed for old-path testing, just the by-uids):
func (w *recordingCodeValueFlowInterprocSweeper) WriteCodeInterprocEvidence(context.Context, []map[string]any, string, string, string) error {
	return nil
}

func (w *recordingCodeValueFlowInterprocSweeper) RetractCodeInterprocEvidence(context.Context, []string, string, string) error {
	return nil
}

func (w *recordingCodeValueFlowInterprocSweeper) RetractCodeInterprocEvidenceSource(context.Context, string) error {
	return nil
}

func (w *recordingCodeValueFlowInterprocSweeper) RetractCodeInterprocEvidenceByUIDs(context.Context, []string, []string, string) error {
	return nil
}

func (w *recordingCodeValueFlowInterprocSweeper) RetractCodeInterprocEvidenceSourceByUIDs(context.Context, []string, string) error {
	return nil
}

type fakeCodeValueFlowLeaseManager struct {
	claimResults []bool
	releaseCalls int
}

func (l *fakeCodeValueFlowLeaseManager) ClaimPartitionLease(
	_ context.Context,
	_ string,
	_, _ int,
	_ string,
	_ time.Duration,
) (bool, error) {
	if len(l.claimResults) == 0 {
		return true, nil
	}
	result := l.claimResults[0]
	l.claimResults = l.claimResults[1:]
	return result, nil
}

func (l *fakeCodeValueFlowLeaseManager) ReleasePartitionLease(
	_ context.Context,
	_ string,
	_, _ int,
	_ string,
) error {
	l.releaseCalls++
	return nil
}

// fakeCodeInterprocProjectedEdgeLedger is a local copy, scoped to this
// package's tests, of the reducer root's own fake of the same name
// (codedataflow_evidence_test_helpers_test.go). It satisfies
// taint.CodeInterprocProjectedEdgeLedger with only the fields this package's
// ledger-driven tests need; Go test files cannot share unexported symbols
// across a package boundary (issue #6061).
type fakeCodeInterprocProjectedEdgeLedger struct {
	listStaleUIDs   []string
	pruneStaleCalls int
}

func (f *fakeCodeInterprocProjectedEdgeLedger) RecordProjectedEdges(
	context.Context, string, string, string, []string, time.Time,
) error {
	return nil
}

func (f *fakeCodeInterprocProjectedEdgeLedger) ListSourceUIDsForScopes(
	context.Context, string, []string,
) ([]string, error) {
	return nil, nil
}

func (f *fakeCodeInterprocProjectedEdgeLedger) ListSourceUIDsForSource(
	context.Context, string,
) ([]string, error) {
	return nil, nil
}

func (f *fakeCodeInterprocProjectedEdgeLedger) ListStaleSourceUIDs(
	context.Context, string, string, string, int,
) ([]string, error) {
	return f.listStaleUIDs, nil
}

func (f *fakeCodeInterprocProjectedEdgeLedger) PruneForScopes(context.Context, string, []string) error {
	return nil
}

func (f *fakeCodeInterprocProjectedEdgeLedger) PruneForSource(context.Context, string) error {
	return nil
}

func (f *fakeCodeInterprocProjectedEdgeLedger) PruneStaleForUIDs(
	context.Context, string, string, string, []string,
) error {
	f.pruneStaleCalls++
	return nil
}

func (f *fakeCodeInterprocProjectedEdgeLedger) LedgerHasRowsForSource(
	context.Context, string,
) (bool, error) {
	return false, nil
}

// TestCodeValueFlowStaleCleanupRunnerLedgerDrivenInterprocSweep proves when both
// InterprocLedger and InterprocWriter are set, the runner enumerates stale uids
// from the ledger, calls the anchored-delete method, and prunes stale rows.
func TestCodeValueFlowStaleCleanupRunnerLedgerDrivenInterprocSweep(t *testing.T) {
	reader := &fakeCodeValueFlowCurrentGenerationReader{
		rows: []CurrentGeneration{
			{ScopeID: "scope-a", GenerationID: "gen-current-a"},
		},
	}
	taintSweeper := &recordingCodeValueFlowTaintSweeper{}
	interprocWriter := &recordingCodeValueFlowInterprocSweeper{}
	ledger := &fakeCodeInterprocProjectedEdgeLedger{
		listStaleUIDs: []string{"uid-1", "uid-2"},
	}
	leaseManager := &fakeCodeValueFlowLeaseManager{claimResults: []bool{true}}
	runner := &Runner{
		CurrentGenerations: reader,
		TaintEvidence:      taintSweeper,
		InterprocWriter:    interprocWriter,
		InterprocLedger:    ledger,
		LeaseManager:       leaseManager,
		Config: RunnerConfig{
			LeaseOwner:       "value-flow-owner",
			LeaseTTL:         2 * time.Minute,
			ScopeBatchLimit:  25,
			DeleteBatchLimit: 50,
		},
	}

	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}
	if !result.LeaseAcquired {
		t.Fatal("LeaseAcquired = false, want true")
	}
	if got, want := result.InterprocSweeps, 1; got != want {
		t.Fatalf("InterprocSweeps = %d, want %d", got, want)
	}
	// The old RetractStaleCodeInterprocEvidence should NOT be called.
	if len(interprocWriter.calls) != 0 {
		t.Fatalf("old interproc sweeper calls = %d, want 0 (should use by-uids via writer)", len(interprocWriter.calls))
	}
	// The by-uids path should have been called once.
	if len(interprocWriter.byUIDsCalls) != 1 {
		t.Fatalf("byUIDsCalls = %d, want 1", len(interprocWriter.byUIDsCalls))
	}
	if ledger.pruneStaleCalls != 1 {
		t.Fatalf("pruneStaleCalls = %d, want 1", ledger.pruneStaleCalls)
	}
}

// TestCodeValueFlowStaleCleanupRunnerLedgerEmptyUidsNoOp proves when the ledger
// returns empty uids, the anchored delete is a no-op and the scope is still
// counted as swept.
func TestCodeValueFlowStaleCleanupRunnerLedgerEmptyUidsNoOp(t *testing.T) {
	reader := &fakeCodeValueFlowCurrentGenerationReader{
		rows: []CurrentGeneration{
			{ScopeID: "scope-a", GenerationID: "gen-current-a"},
		},
	}
	taintSweeper := &recordingCodeValueFlowTaintSweeper{}
	interprocWriter := &recordingCodeValueFlowInterprocSweeper{}
	ledger := &fakeCodeInterprocProjectedEdgeLedger{
		listStaleUIDs: nil, // no stale uids
	}
	runner := &Runner{
		CurrentGenerations: reader,
		TaintEvidence:      taintSweeper,
		InterprocWriter:    interprocWriter,
		InterprocLedger:    ledger,
		Config: RunnerConfig{
			ScopeBatchLimit:  25,
			DeleteBatchLimit: 50,
		},
	}

	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}
	if got, want := result.InterprocSweeps, 1; got != want {
		t.Fatalf("InterprocSweeps = %d, want %d (empty ledger still counts as swept)", got, want)
	}
}

func equalCodeValueFlowStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
