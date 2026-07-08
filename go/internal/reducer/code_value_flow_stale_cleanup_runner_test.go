// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCodeValueFlowStaleCleanupRunnerSweepsBothEvidenceFamilies(t *testing.T) {
	reader := &fakeCodeValueFlowCurrentGenerationReader{
		rows: []CodeValueFlowCurrentGeneration{
			{ScopeID: "scope-a", GenerationID: "gen-current-a"},
			{ScopeID: "scope-b", GenerationID: "gen-current-b"},
		},
	}
	taint := &recordingCodeValueFlowTaintSweeper{}
	interproc := &recordingCodeValueFlowInterprocSweeper{}
	leaseManager := &fakeCodeValueFlowLeaseManager{claimResults: []bool{true}}
	runner := &CodeValueFlowStaleCleanupRunner{
		CurrentGenerations: reader,
		TaintEvidence:      taint,
		InterprocEvidence:  interproc,
		LeaseManager:       leaseManager,
		Config: CodeValueFlowStaleCleanupRunnerConfig{
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
	if got := len(taint.calls); got != 2 {
		t.Fatalf("taint calls = %d, want 2", got)
	}
	if got := len(interproc.calls); got != 2 {
		t.Fatalf("interproc calls = %d, want 2", got)
	}
	if call := taint.calls[0]; call.scopeID != "scope-a" ||
		call.generationID != "gen-current-a" ||
		call.evidenceSource != codeTaintEvidenceSource ||
		call.limit != 50 {
		t.Fatalf("first taint call = %+v, want current scope/generation/source/limit", call)
	}
	if call := interproc.calls[1]; call.scopeID != "scope-b" ||
		call.generationID != "gen-current-b" ||
		call.evidenceSource != codeInterprocEvidenceSource ||
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
		rows: []CodeValueFlowCurrentGeneration{{ScopeID: "scope-a", GenerationID: "gen-a"}},
	}
	taint := &recordingCodeValueFlowTaintSweeper{}
	interproc := &recordingCodeValueFlowInterprocSweeper{}
	leaseManager := &fakeCodeValueFlowLeaseManager{claimResults: []bool{false}}
	runner := &CodeValueFlowStaleCleanupRunner{
		CurrentGenerations: reader,
		TaintEvidence:      taint,
		InterprocEvidence:  interproc,
		LeaseManager:       leaseManager,
		Config:             CodeValueFlowStaleCleanupRunnerConfig{LeaseOwner: "value-flow-owner"},
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
	if len(taint.calls) != 0 || len(interproc.calls) != 0 {
		t.Fatalf("sweeper calls = %d/%d, want 0/0 without a lease", len(taint.calls), len(interproc.calls))
	}
	if leaseManager.releaseCalls != 0 {
		t.Fatalf("release calls = %d, want 0 without a claimed lease", leaseManager.releaseCalls)
	}
}

func TestCodeValueFlowStaleCleanupRunnerCursorPagesWithoutWrappingHot(t *testing.T) {
	reader := &fakeCodeValueFlowCurrentGenerationReader{
		pages: [][]CodeValueFlowCurrentGeneration{
			{
				{ScopeID: "scope-a", GenerationID: "gen-a"},
				{ScopeID: "scope-b", GenerationID: "gen-b"},
			},
			nil,
			{{ScopeID: "scope-a", GenerationID: "gen-a"}},
		},
	}
	taint := &recordingCodeValueFlowTaintSweeper{}
	interproc := &recordingCodeValueFlowInterprocSweeper{}
	runner := &CodeValueFlowStaleCleanupRunner{
		CurrentGenerations: reader,
		TaintEvidence:      taint,
		InterprocEvidence:  interproc,
		Config: CodeValueFlowStaleCleanupRunnerConfig{
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
	runner := &CodeValueFlowStaleCleanupRunner{}

	_, err := runner.RunOnce(context.Background())

	if err == nil {
		t.Fatal("RunOnce() error = nil, want validation error")
	}
	if !errors.Is(err, ErrCodeValueFlowCurrentGenerationsRequired) {
		t.Fatalf("RunOnce() error = %v, want ErrCodeValueFlowCurrentGenerationsRequired", err)
	}
}

func TestServiceStartsCodeValueFlowStaleCleanupRunner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &fakeCodeValueFlowCurrentGenerationReader{
		rows: []CodeValueFlowCurrentGeneration{{ScopeID: "scope-a", GenerationID: "gen-a"}},
	}
	taint := &recordingCodeValueFlowTaintSweeper{}
	interproc := &recordingCodeValueFlowInterprocSweeper{}
	started := make(chan struct{}, 1)
	runner := &CodeValueFlowStaleCleanupRunner{
		CurrentGenerations: reader,
		TaintEvidence:      taint,
		InterprocEvidence:  interproc,
		Config:             CodeValueFlowStaleCleanupRunnerConfig{PollInterval: time.Hour},
		Wait: func(ctx context.Context, _ time.Duration) error {
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		},
	}
	service := Service{CodeValueFlowStaleCleanupRunner: runner}
	var wg sync.WaitGroup
	var gotErr error
	service.startSideRunners(ctx, &wg, func(err error) {
		if !errors.Is(err, context.Canceled) {
			gotErr = err
		}
	})

	deadline := time.After(time.Second)
	for taint.callCount() != 1 {
		select {
		case <-deadline:
			t.Fatal("taint stale cleanup was not called")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	<-started
	cancel()
	wg.Wait()

	if gotErr != nil {
		t.Fatalf("side runner error = %v, want nil", gotErr)
	}
}

type fakeCodeValueFlowCurrentGenerationReader struct {
	rows          []CodeValueFlowCurrentGeneration
	pages         [][]CodeValueFlowCurrentGeneration
	afterScopeIDs []string
	limits        []int
}

func (r *fakeCodeValueFlowCurrentGenerationReader) ListCurrentCodeValueFlowGenerations(
	_ context.Context,
	afterScopeID string,
	limit int,
) ([]CodeValueFlowCurrentGeneration, error) {
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

func (w *recordingCodeValueFlowTaintSweeper) callCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.calls)
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

// TestCodeValueFlowStaleCleanupRunnerLedgerDrivenInterprocSweep proves when both
// InterprocLedger and InterprocWriter are set, the runner enumerates stale uids
// from the ledger, calls the anchored-delete method, and prunes stale rows.
func TestCodeValueFlowStaleCleanupRunnerLedgerDrivenInterprocSweep(t *testing.T) {
	reader := &fakeCodeValueFlowCurrentGenerationReader{
		rows: []CodeValueFlowCurrentGeneration{
			{ScopeID: "scope-a", GenerationID: "gen-current-a"},
		},
	}
	taint := &recordingCodeValueFlowTaintSweeper{}
	interprocWriter := &recordingCodeValueFlowInterprocSweeper{}
	ledger := &fakeCodeInterprocProjectedEdgeLedger{
		listStaleUIDs: []string{"uid-1", "uid-2"},
	}
	leaseManager := &fakeCodeValueFlowLeaseManager{claimResults: []bool{true}}
	runner := &CodeValueFlowStaleCleanupRunner{
		CurrentGenerations: reader,
		TaintEvidence:      taint,
		InterprocWriter:    interprocWriter,
		InterprocLedger:    ledger,
		LeaseManager:       leaseManager,
		Config: CodeValueFlowStaleCleanupRunnerConfig{
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
		rows: []CodeValueFlowCurrentGeneration{
			{ScopeID: "scope-a", GenerationID: "gen-current-a"},
		},
	}
	taint := &recordingCodeValueFlowTaintSweeper{}
	interprocWriter := &recordingCodeValueFlowInterprocSweeper{}
	ledger := &fakeCodeInterprocProjectedEdgeLedger{
		listStaleUIDs: nil, // no stale uids
	}
	runner := &CodeValueFlowStaleCleanupRunner{
		CurrentGenerations: reader,
		TaintEvidence:      taint,
		InterprocWriter:    interprocWriter,
		InterprocLedger:    ledger,
		Config: CodeValueFlowStaleCleanupRunnerConfig{
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
