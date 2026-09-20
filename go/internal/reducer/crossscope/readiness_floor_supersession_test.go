// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossscope

import (
	"context"
	"sync"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// supersessionLedger is an in-memory ReadinessWaitLedger with the store's
// earliest-anchor rule and epoch fence, so handler-level supersession tests
// run hermetically. The SQL half of the same contract is proven against real
// Postgres by the wait store's live tests.
type supersessionLedger struct {
	mu      sync.Mutex
	rows    map[string]ReadinessWait
	upserts int
	clears  int
}

func newSupersessionLedger() *supersessionLedger {
	return &supersessionLedger{rows: make(map[string]ReadinessWait)}
}

func supersessionLedgerKey(scopeID string, domain reducercontract.Domain) string {
	return scopeID + "|" + string(domain)
}

func (l *supersessionLedger) GetReadinessWait(_ context.Context, scopeID string, domain reducercontract.Domain) (ReadinessWait, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	row, ok := l.rows[supersessionLedgerKey(scopeID, domain)]
	return row, ok, nil
}

func (l *supersessionLedger) UpsertReadinessWait(_ context.Context, wait ReadinessWait) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.upserts++
	key := supersessionLedgerKey(wait.ScopeID, wait.Domain)
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

func (l *supersessionLedger) ClearReadinessWait(_ context.Context, wait ReadinessWait) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clears++
	key := supersessionLedgerKey(wait.ScopeID, wait.Domain)
	existing, ok := l.rows[key]
	if !ok || existing.AnchorEpoch != wait.AnchorEpoch || existing.RowVersion != wait.RowVersion {
		return nil
	}
	l.rows[key] = ReadinessWait{
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

// runLedgerFloor drives the ledger-anchored floor the way the handlers do:
// sample readiness pre-load, then apply the post-load decision against the
// ledger. resolved is the whole-batch resolved count credited to the
// consumer's single declared producer.
func runLedgerFloor(
	t *testing.T,
	ledger ReadinessWaitLedger,
	readiness ProducerReadiness,
	intent reducercontract.Intent,
	now time.Time,
	lookupPlanned bool,
	resolved int,
) error {
	t.Helper()

	signal, err := CheckProducerReadinessBeforeLoadWithLedger(
		context.Background(), ledger, readiness, intent, now, lookupPlanned,
	)
	if err != nil {
		return err
	}
	return ApplyProducerReadinessPostLoad(
		context.Background(), nil, ledger, signal,
		SingleProducerResolvedCounts(signal.ProducerDomains, resolved),
		intent, now,
	)
}

func ledgerIntent(domain reducercontract.Domain, scope, generation string, anchor time.Time) reducercontract.Intent {
	return reducercontract.Intent{
		Domain:         domain,
		ScopeID:        scope,
		GenerationID:   generation,
		EnqueuedAt:     anchor,
		CycleStartedAt: anchor,
	}
}

// TestProducerReadinessLedgerKeepsFirstDeferAnchorAcrossSupersession pins the
// anchor half of #6814: generation N defers, generation N+1 supersedes it
// before the bound with the condition persisting, and the wait still defers —
// but anchored at N's first defer, not at N+1's fresh row.
func TestProducerReadinessLedgerKeepsFirstDeferAnchorAcrossSupersession(t *testing.T) {
	t.Parallel()

	const scope = "scope:ledger-anchor"
	ledger := newSupersessionLedger()
	readiness := &fixedProducerReadiness{ready: false}
	consumer := reducercontract.DomainCICDRunCorrelation

	firstWait := testCrossScopeNow
	genN := ledgerIntent(consumer, scope, "gen-n", firstWait)
	if err := runLedgerFloor(t, ledger, readiness, genN, firstWait, true, 0); err == nil {
		t.Fatal("gen N: want a readiness error on the first wait, got nil")
	}

	supersedeAt := firstWait.Add(10 * time.Minute)
	genNPlus1 := ledgerIntent(consumer, scope, "gen-n-plus-1", supersedeAt)
	if err := runLedgerFloor(t, ledger, readiness, genNPlus1, firstWait.Add(15*time.Minute), true, 0); err == nil {
		t.Fatal("gen N+1: want a readiness error 15m after the first wait, got nil")
	}

	row, found, err := ledger.GetReadinessWait(context.Background(), scope, consumer)
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

// TestProducerReadinessLedgerSettlesWithinBoundOfFirstWait is the #6814
// regression for CheckProducerReadinessBeforeLoad: generation N waits,
// generation N+1 supersedes it before the bound, the not-ready condition
// persists, and the domain still reaches its bounded outcome — settle, i.e.
// proceed — within MaxWait of the FIRST wait. A per-row anchor defers here
// because N+1's own row is only 10 minutes old.
func TestProducerReadinessLedgerSettlesWithinBoundOfFirstWait(t *testing.T) {
	t.Parallel()

	const scope = "scope:ledger-settle"
	ledger := newSupersessionLedger()
	readiness := &fixedProducerReadiness{ready: false}
	consumer := reducercontract.DomainCICDRunCorrelation

	firstWait := testCrossScopeNow
	genN := ledgerIntent(consumer, scope, "gen-n", firstWait)
	if err := runLedgerFloor(t, ledger, readiness, genN, firstWait, true, 0); err == nil {
		t.Fatal("gen N: want a readiness error on the first wait, got nil")
	}

	genNPlus1 := ledgerIntent(consumer, scope, "gen-n-plus-1", firstWait.Add(25*time.Minute))
	if err := runLedgerFloor(t, ledger, readiness, genNPlus1, firstWait.Add(35*time.Minute), true, 0); err != nil {
		t.Fatalf("gen N+1: err = %v, want nil (settle 35m after the first wait, past the 30m bound)", err)
	}

	row, found, err := ledger.GetReadinessWait(context.Background(), scope, consumer)
	if err != nil {
		t.Fatalf("GetReadinessWait() error = %v", err)
	}
	if !found || !row.Settled() {
		t.Fatalf("want a settled ledger row after the bound, got found=%v row=%+v", found, row)
	}
}

// TestProducerReadinessLedgerClearsWhenProducerActivates pins the other
// terminal: the condition resolves, the consumer proceeds, and the ledger row
// is tombstoned so a later wait starts a fresh bound.
func TestProducerReadinessLedgerClearsWhenProducerActivates(t *testing.T) {
	t.Parallel()

	const scope = "scope:ledger-clear"
	ledger := newSupersessionLedger()
	consumer := reducercontract.DomainCICDRunCorrelation

	firstWait := testCrossScopeNow
	genN := ledgerIntent(consumer, scope, "gen-n", firstWait)
	if err := runLedgerFloor(t, ledger, &fixedProducerReadiness{ready: false}, genN, firstWait, true, 0); err == nil {
		t.Fatal("gen N: want a readiness error on the first wait, got nil")
	}

	genNPlus1 := ledgerIntent(consumer, scope, "gen-n-plus-1", firstWait.Add(10*time.Minute))
	if err := runLedgerFloor(t, ledger, &fixedProducerReadiness{ready: true}, genNPlus1, firstWait.Add(15*time.Minute), true, 0); err != nil {
		t.Fatalf("gen N+1: err = %v, want nil (producer activated, must proceed)", err)
	}

	row, found, err := ledger.GetReadinessWait(context.Background(), scope, consumer)
	if err != nil {
		t.Fatalf("GetReadinessWait() error = %v", err)
	}
	if !found || !row.Cleared() {
		t.Fatalf("want a cleared ledger tombstone after activation, got found=%v row=%+v", found, row)
	}
}

// TestProducerReadinessLedgerNilMatchesRowBound pins nil-ledger parity: with
// no ledger wired, the WithLedger path enforces exactly the per-row
// elapsed-time bound the unwired floor always had.
func TestProducerReadinessLedgerNilMatchesRowBound(t *testing.T) {
	t.Parallel()

	consumer := reducercontract.DomainCICDRunCorrelation

	fresh := ledgerIntent(consumer, "scope:nil-fresh", "gen-1", testCrossScopeNow.Add(-time.Minute))
	if err := runLedgerFloor(t, nil, &fixedProducerReadiness{ready: false}, fresh, testCrossScopeNow, true, 0); err == nil {
		t.Fatal("fresh row: want a readiness error well within the bound, got nil")
	}

	aged := ledgerIntent(consumer, "scope:nil-aged", "gen-1", testCrossScopeNow.Add(-2*ProducerReadinessMaxWait))
	if err := runLedgerFloor(t, nil, &fixedProducerReadiness{ready: false}, aged, testCrossScopeNow, true, 0); err != nil {
		t.Fatalf("aged row: err = %v, want nil (row past the bound must proceed, as before)", err)
	}
}
