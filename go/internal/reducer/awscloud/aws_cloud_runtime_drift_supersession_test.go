// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
)

// errTestDriftProbe is the sick-backend sentinel for the nil-ledger terminal
// fallback test: past the bound, the gate must commit without consulting it.
var errTestDriftProbe = errors.New("state snapshot probe backend is down")

// statePendingMemoryLedger is an in-memory crossscope.ReadinessWaitLedger
// with the store's earliest-anchor rule and epoch fence, mirroring
// memoryWaitLedger in iamcan's wait tests. The SQL half of the same contract
// is proven against real Postgres by the wait store's live tests.
type statePendingMemoryLedger struct {
	mu      sync.Mutex
	rows    map[string]crossscope.ReadinessWait
	upserts int
	clears  int
}

func newStatePendingMemoryLedger() *statePendingMemoryLedger {
	return &statePendingMemoryLedger{rows: make(map[string]crossscope.ReadinessWait)}
}

func statePendingLedgerKey(scopeID string, domain reducercontract.Domain) string {
	return scopeID + "|" + string(domain)
}

func (l *statePendingMemoryLedger) GetReadinessWait(_ context.Context, scopeID string, domain reducercontract.Domain) (crossscope.ReadinessWait, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	row, ok := l.rows[statePendingLedgerKey(scopeID, domain)]
	return row, ok, nil
}

func (l *statePendingMemoryLedger) UpsertReadinessWait(_ context.Context, wait crossscope.ReadinessWait) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.upserts++
	key := statePendingLedgerKey(wait.ScopeID, wait.Domain)
	if existing, ok := l.rows[key]; ok {
		if wait.AnchorEpoch < existing.AnchorEpoch {
			return nil
		}
		if wait.AnchorEpoch == existing.AnchorEpoch && existing.FirstDeferredAt.Before(wait.FirstDeferredAt) {
			wait.FirstDeferredAt = existing.FirstDeferredAt
		}
		wait.RowVersion = existing.RowVersion + 1
	}
	wait.ClearedAt = time.Time{}
	l.rows[key] = wait
	return nil
}

func (l *statePendingMemoryLedger) ClearReadinessWait(_ context.Context, wait crossscope.ReadinessWait) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clears++
	key := statePendingLedgerKey(wait.ScopeID, wait.Domain)
	existing, ok := l.rows[key]
	if !ok || existing.AnchorEpoch != wait.AnchorEpoch || existing.RowVersion != wait.RowVersion {
		return nil
	}
	l.rows[key] = crossscope.ReadinessWait{
		ScopeID: wait.ScopeID, Domain: wait.Domain,
		FirstDeferredAt:         wait.ClearedAt,
		AnchorEpoch:             existing.AnchorEpoch + 1,
		RowVersion:              existing.RowVersion + 1,
		CommittedGenerationID:   wait.CommittedGenerationID,
		CommittedCycleStartedAt: wait.CommittedCycleStartedAt,
		ClearedAt:               wait.ClearedAt,
		UpdatedAt:               wait.ClearedAt,
	}
	return nil
}

const statePendingSupersessionScope = "aws:123456789012:us-east-1"

// driftSupersessionHandler builds the production Handle path with an
// always-pending state checker, one orphaned candidate row, a fixed clock,
// and the given ledger: every evaluation below goes through the real Handle,
// so none of them can pass against a re-implementation.
func driftSupersessionHandler(ledger crossscope.ReadinessWaitLedger, now *time.Time) (AWSCloudRuntimeDriftHandler, *stubAWSCloudRuntimeDriftFindingWriter) {
	loader := &stubAWSCloudRuntimeDriftEvidenceLoader{
		rows: []cloudruntime.AddressedRow{awsCloudRuntimeDriftOrphanedEvidenceRow("arn:aws:lambda:us-east-1:123456789012:function:x")},
	}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     loader,
		Writer:             writer,
		FencingTokenIssuer: &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{1, 2, 3, 4}},
		ReadinessChecker:   &stubAWSCloudRuntimeDriftReadinessChecker{pending: true},
		ReadinessWaits:     ledger,
		Now:                func() time.Time { return *now },
	}
	return handler, writer
}

func driftSupersessionIntent(generation string, anchor time.Time) reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:       "intent-" + generation,
		ScopeID:        statePendingSupersessionScope,
		GenerationID:   generation,
		SourceSystem:   "aws",
		Domain:         reducercontract.DomainAWSCloudRuntimeDrift,
		Cause:          "aws runtime resource facts observed",
		EnqueuedAt:     anchor,
		CycleStartedAt: anchor,
	}
}

// TestAWSCloudRuntimeDriftLedgerKeepsFirstDeferAnchorAcrossSupersession pins
// the anchor half of #6814 for the state-pending gate: generation N defers,
// generation N+1 supersedes it before the bound with a state scope still
// pending, and the wait still defers — anchored at N's first defer.
func TestAWSCloudRuntimeDriftLedgerKeepsFirstDeferAnchorAcrossSupersession(t *testing.T) {
	t.Parallel()

	firstWait := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	now := firstWait
	ledger := newStatePendingMemoryLedger()
	handler, writer := driftSupersessionHandler(ledger, &now)

	if _, err := handler.Handle(context.Background(), driftSupersessionIntent("gen-n", firstWait)); err == nil {
		t.Fatal("gen N: want a deferred error on the first wait, got nil")
	}
	if writer.calls != 0 {
		t.Fatalf("writer.calls = %d, want 0 (a deferred pass must not write anything)", writer.calls)
	}

	now = firstWait.Add(15 * time.Minute)
	if _, err := handler.Handle(context.Background(), driftSupersessionIntent("gen-n-plus-1", firstWait.Add(10*time.Minute))); err == nil {
		t.Fatal("gen N+1: want a deferred error 15m after the first wait, got nil")
	}
	if writer.calls != 0 {
		t.Fatalf("writer.calls = %d, want 0 (still within the bound, must not write)", writer.calls)
	}

	row, found, err := ledger.GetReadinessWait(context.Background(), statePendingSupersessionScope, reducercontract.DomainAWSCloudRuntimeDrift)
	if err != nil {
		t.Fatalf("GetReadinessWait() error = %v", err)
	}
	if !found {
		t.Fatal("want a ledger row after two defers, found none: the wait was never recorded")
	}
	if !row.FirstDeferredAt.Equal(firstWait) {
		t.Fatalf("FirstDeferredAt = %v, want %v: the anchor must survive supersession", row.FirstDeferredAt, firstWait)
	}
}

// TestAWSCloudRuntimeDriftLedgerSettlesWithinBoundOfFirstWait is the #6814
// regression for the state-pending gate: generation N waits, generation N+1
// supersedes it before the bound, the state scope stays pending, and the
// domain still reaches its bounded outcome — commit the best-available
// verdict — within MaxWait of the FIRST wait. A per-row anchor defers here
// because N+1's own row is only 10 minutes old.
func TestAWSCloudRuntimeDriftLedgerSettlesWithinBoundOfFirstWait(t *testing.T) {
	t.Parallel()

	firstWait := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	now := firstWait
	ledger := newStatePendingMemoryLedger()
	handler, writer := driftSupersessionHandler(ledger, &now)

	if _, err := handler.Handle(context.Background(), driftSupersessionIntent("gen-n", firstWait)); err == nil {
		t.Fatal("gen N: want a deferred error on the first wait, got nil")
	}

	now = firstWait.Add(35 * time.Minute)
	if _, err := handler.Handle(context.Background(), driftSupersessionIntent("gen-n-plus-1", firstWait.Add(25*time.Minute))); err != nil {
		t.Fatalf("gen N+1: err = %v, want nil (settle 35m after the first wait, past the 30m bound)", err)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1 (past the bound, Handle must write its best-available verdict)", writer.calls)
	}

	row, found, err := ledger.GetReadinessWait(context.Background(), statePendingSupersessionScope, reducercontract.DomainAWSCloudRuntimeDrift)
	if err != nil {
		t.Fatalf("GetReadinessWait() error = %v", err)
	}
	if !found || !row.Settled() {
		t.Fatalf("want a settled ledger row after the bound, got found=%v row=%+v", found, row)
	}
}

// TestAWSCloudRuntimeDriftLedgerNilSkipsProbePastBound pins the terminal
// fallback: with no ledger wired and the row already past the bound, the
// gate commits WITHOUT probing the backend, so a sick probe cannot fail a
// row the pre-ledger code committed. The unanchored evaluation must only ever
// run with a wired ledger behind it.
func TestAWSCloudRuntimeDriftLedgerNilSkipsProbePastBound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	handler, writer := driftSupersessionHandler(nil, &now)
	handler.ReadinessWaits = nil
	handler.ReadinessChecker = &stubAWSCloudRuntimeDriftReadinessChecker{pending: true, err: errTestDriftProbe}

	aged := driftSupersessionIntent("gen-1", now.Add(-2*awsCloudRuntimeDriftStatePendingMaxWait))
	if _, err := handler.Handle(context.Background(), aged); err != nil {
		t.Fatalf("aged row: err = %v, want nil (terminal fallback must not probe)", err)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1 (past the bound, must write)", writer.calls)
	}
	if got := handler.ReadinessChecker.(*stubAWSCloudRuntimeDriftReadinessChecker).calls; got != 0 {
		t.Fatalf("probe calls = %d, want 0 (the backend must not be consulted past the bound)", got)
	}
}

// TestAWSCloudRuntimeDriftLedgerNilMatchesRowBound pins nil-ledger parity:
// with no ledger wired, the ledger path enforces exactly the per-row
// elapsed-time bound the unwired gate always had.
func TestAWSCloudRuntimeDriftLedgerNilMatchesRowBound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 29, 15, 0, 0, 0, time.UTC)
	handler, writer := driftSupersessionHandler(nil, &now)
	handler.ReadinessWaits = nil

	fresh := driftSupersessionIntent("gen-1", now.Add(-time.Minute))
	if _, err := handler.Handle(context.Background(), fresh); err == nil {
		t.Fatal("fresh row: want a deferred error well within the bound, got nil")
	}
	if writer.calls != 0 {
		t.Fatalf("writer.calls = %d, want 0 (still within the bound, must not write)", writer.calls)
	}

	aged := driftSupersessionIntent("gen-1", now.Add(-2*awsCloudRuntimeDriftStatePendingMaxWait))
	if _, err := handler.Handle(context.Background(), aged); err != nil {
		t.Fatalf("aged row: err = %v, want nil (row past the bound must commit, as before)", err)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1 (past the bound, must write)", writer.calls)
	}
}
