// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// fakeStagedAuditFleet pins one member and records every per-batch borrow, so
// the tests prove member affinity and per-statement connection release without
// a live standby.
type fakeStagedAuditFleet struct {
	mu       sync.Mutex
	member   string
	lag      time.Duration
	borrows  []string
	releases int
	pins     int
	// blockNext hangs the call whose 1-based sequence reaches blockFrom.
	blockNext chan struct{}
	blockFrom int
}

func (f *fakeStagedAuditFleet) PinMember(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pins++
	return f.member, nil
}

func (f *fakeStagedAuditFleet) ReplayLag(context.Context, string) (time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lag, nil
}

func (f *fakeStagedAuditFleet) RunOnMember(ctx context.Context, member string, fn func(context.Context) error) error {
	f.mu.Lock()
	f.borrows = append(f.borrows, member)
	call := len(f.borrows)
	var block chan struct{}
	if f.blockNext != nil && call >= f.blockFrom {
		block = f.blockNext
		f.blockNext = nil
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.releases++
		f.mu.Unlock()
	}()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fn(ctx)
}

func (f *fakeStagedAuditFleet) usedMembers() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.borrows...)
}

func newLaneCFleet() *fakeStagedAuditFleet {
	return &fakeStagedAuditFleet{member: "read-0", lag: 250 * time.Millisecond}
}

func okLaneCPlan(factsPerBatch int) StagedAuditTickPlan {
	return StagedAuditTickPlan{
		Census: func(context.Context) ([]StagedAuditCensusItem, error) {
			return []StagedAuditCensusItem{{ScopeID: "s1", GenerationID: "g1"}, {ScopeID: "s2", GenerationID: "g1"}}, nil
		},
		FetchBatch: func(context.Context, StagedAuditCensusItem) (StagedAuditBatchResult, error) {
			return StagedAuditBatchResult{Facts: factsPerBatch, DistinctKeys: factsPerBatch}, nil
		},
		FetchStoredWinners: func(context.Context) (int, error) { return 10, nil },
	}
}

// TestStagedAuditRecoveryConflictDefers pins the #10451 classifier on the
// captured live text: SQLSTATE 40001 plus "conflict with recovery" defers.
func TestStagedAuditRecoveryConflictDefers(t *testing.T) {
	live := errors.New("ERROR:  40001: canceling statement due to conflict with recovery\nDETAIL:  User query might have needed to see row versions that must be removed.")
	if !IsRecoveryConflict(live) {
		t.Fatal("live fetch-only cancellation text must classify as recovery conflict")
	}
	pgErr := &pgconn.PgError{Code: "40001", Message: "canceling statement due to conflict with recovery"}
	if !IsRecoveryConflict(pgErr) {
		t.Fatal("pgconn 40001 with recovery message must classify as recovery conflict")
	}
	reason, deferTick := ClassifyStagedAuditError(live)
	if !deferTick || reason != StagedAuditDeferConflictExhausted {
		t.Fatalf("live conflict must defer as conflict-exhausted, got reason=%q defer=%v", reason, deferTick)
	}
}

// TestStagedAuditNonConflictErrorsDoNotDefer pins the negative half: 40001
// alone (genuine serialization failure) and the phrase alone are insufficient,
// and a 57014 statement timeout must never read as a conflict.
func TestStagedAuditNonConflictErrorsDoNotDefer(t *testing.T) {
	serial := &pgconn.PgError{Code: "40001", Message: "could not serialize access due to concurrent update"}
	if IsRecoveryConflict(serial) {
		t.Fatal("bare 40001 without recovery message must not classify as conflict")
	}
	phrase := errors.New("canceling statement due to conflict with recovery")
	if IsRecoveryConflict(phrase) {
		t.Fatal("recovery phrase without SQLSTATE 40001 must not classify as conflict")
	}
	timeout := &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}
	if IsRecoveryConflict(timeout) {
		t.Fatal("57014 statement timeout must not classify as conflict")
	}
	// A 57014 is still a failed batch, so the tick defers — but as a
	// batch-abort, never as a conflict.
	if reason, deferTick := ClassifyStagedAuditError(timeout); !deferTick || reason != StagedAuditDeferBatchAbort {
		t.Fatalf("57014 must defer as batch-abort, got reason=%q defer=%v", reason, deferTick)
	}
	if IsRecoveryConflict(nil) {
		t.Fatal("nil error must not classify as conflict")
	}
}

// TestStagedAuditTickHoldsOneMember proves affinity: census, every fact
// batch, and the stored-winners fetch run on the single pinned member, and
// every borrow returns its connection before the next statement.
func TestStagedAuditTickHoldsOneMember(t *testing.T) {
	fleet := newLaneCFleet()
	telemetry := NewStagedAuditTelemetry()
	runner := NewStagedAuditRunner(fleet, telemetry, StagedAuditRunnerOptions{})
	result, err := runner.RunTick(context.Background(), okLaneCPlan(100))
	if err != nil {
		t.Fatalf("RunTick: %v", err)
	}
	if result.MemberID != "read-0" {
		t.Fatalf("tick member = %q, want read-0", result.MemberID)
	}
	members := fleet.usedMembers()
	// census + 2 batches + stored winners = 4 member-pinned statements.
	if len(members) != 4 {
		t.Fatalf("pinned statements = %d, want 4 (census, 2 batches, stored winners)", len(members))
	}
	for _, member := range members {
		if member != "read-0" {
			t.Fatalf("statement ran on %q, want every statement on read-0", member)
		}
	}
	fleet.mu.Lock()
	defer fleet.mu.Unlock()
	if fleet.pins != 1 {
		t.Fatalf("PinMember calls = %d, want exactly 1 per tick", fleet.pins)
	}
	if fleet.releases != len(members) {
		t.Fatalf("connection releases = %d, want %d (one per statement)", fleet.releases, len(members))
	}
}

// TestStagedAuditBatchAbortTouchesNoWriter proves the abort rule: a failed
// batch discards staged state, records a batch-abort deferral, and never
// reaches the writer-side verify.
func TestStagedAuditBatchAbortTouchesNoWriter(t *testing.T) {
	fleet := newLaneCFleet()
	telemetry := NewStagedAuditTelemetry()
	runner := NewStagedAuditRunner(fleet, telemetry, StagedAuditRunnerOptions{})
	boom := errors.New("connection reset by peer")
	verified := false
	plan := okLaneCPlan(100)
	plan.FetchBatch = func(context.Context, StagedAuditCensusItem) (StagedAuditBatchResult, error) {
		return StagedAuditBatchResult{}, boom
	}
	plan.Verify = func(context.Context) (int64, error) {
		verified = true
		return 0, nil
	}
	_, err := runner.RunTick(context.Background(), plan)
	if err == nil {
		t.Fatal("failed batch must abort the tick")
	}
	var deferred *StagedAuditDeferredError
	if !errors.As(err, &deferred) {
		t.Fatalf("abort error type = %T, want *StagedAuditDeferredError", err)
	}
	if deferred.Reason != StagedAuditDeferBatchAbort {
		t.Fatalf("abort reason = %q, want batch-abort", deferred.Reason)
	}
	if verified {
		t.Fatal("aborted tick must not contact the writer-side verify")
	}
	if got := telemetry.Deferrals(StagedAuditDeferBatchAbort); got != 1 {
		t.Fatalf("batch-abort deferrals = %d, want 1", got)
	}
	if got := telemetry.Ticks(); got != 0 {
		t.Fatalf("completed ticks = %d, want 0 after abort", got)
	}
}

// TestStagedAuditOverBudgetBatchDefers pins the fail-closed caps: a batch
// past 20,000 rows aborts the tick as a budget deferral with no writer
// contact.
func TestStagedAuditOverBudgetBatchDefers(t *testing.T) {
	fleet := newLaneCFleet()
	telemetry := NewStagedAuditTelemetry()
	runner := NewStagedAuditRunner(fleet, telemetry, StagedAuditRunnerOptions{})
	verified := false
	plan := okLaneCPlan(StagedAuditMaxRowsPerBatch + 1)
	plan.Verify = func(context.Context) (int64, error) {
		verified = true
		return 0, nil
	}
	_, err := runner.RunTick(context.Background(), plan)
	if err == nil {
		t.Fatal("over-budget batch must abort the tick")
	}
	var deferred *StagedAuditDeferredError
	if !errors.As(err, &deferred) {
		t.Fatalf("abort error type = %T, want *StagedAuditDeferredError", err)
	}
	if deferred.Reason != StagedAuditDeferBudget {
		t.Fatalf("over-budget reason = %q, want budget", deferred.Reason)
	}
	if verified {
		t.Fatal("over-budget tick must not contact the writer-side verify")
	}
}

// TestStagedAuditHungBatchFailsInsideBudget proves each batch is its own
// short statement: a hung batch fails fast on the configured timeout and the
// error carries the 25s production default.
func TestStagedAuditHungBatchFailsInsideBudget(t *testing.T) {
	if DefaultStagedAuditBatchTimeout != 25*time.Second {
		t.Fatalf("DefaultStagedAuditBatchTimeout = %v, want 25s", DefaultStagedAuditBatchTimeout)
	}
	fleet := newLaneCFleet()
	fleet.blockNext = make(chan struct{}) // never closed: the batch hangs.
	fleet.blockFrom = 2                   // call 1 is the census; call 2 is the batch.
	telemetry := NewStagedAuditTelemetry()
	runner := NewStagedAuditRunner(fleet, telemetry, StagedAuditRunnerOptions{BatchTimeout: 150 * time.Millisecond})
	plan := okLaneCPlan(100)
	plan.Census = func(context.Context) ([]StagedAuditCensusItem, error) {
		return []StagedAuditCensusItem{{ScopeID: "s1", GenerationID: "g1"}}, nil
	}
	started := time.Now()
	_, err := runner.RunTick(context.Background(), plan)
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("hung batch must fail the tick")
	}
	if elapsed > 25*time.Second {
		t.Fatalf("hung batch took %v, want failure inside 25s", elapsed)
	}
	var deferred *StagedAuditDeferredError
	if !errors.As(err, &deferred) {
		t.Fatalf("timeout error type = %T, want *StagedAuditDeferredError", err)
	}
}

// TestStagedAuditTelemetryServes3AM pins the operator surface: per-tick
// batch count and max batch duration, staged-fact and distinct-key gauges,
// member identity with replay lag at tick start and end, and the
// writer-verify discard rate.
func TestStagedAuditTelemetryServes3AM(t *testing.T) {
	fleet := newLaneCFleet()
	telemetry := NewStagedAuditTelemetry()
	runner := NewStagedAuditRunner(fleet, telemetry, StagedAuditRunnerOptions{})
	plan := okLaneCPlan(100)
	plan.Verify = func(context.Context) (int64, error) { return 4, nil }
	result, err := runner.RunTick(context.Background(), plan)
	if err != nil {
		t.Fatalf("RunTick: %v", err)
	}
	if result.Batches != 2 {
		t.Fatalf("tick batches = %d, want 2", result.Batches)
	}
	if result.StagedFacts != 200 || result.DistinctKeys != 200 {
		t.Fatalf("staged = (%d facts, %d keys), want (200, 200)", result.StagedFacts, result.DistinctKeys)
	}
	if telemetry.LastBatchCount() != 2 {
		t.Fatalf("telemetry batch count = %d, want 2", telemetry.LastBatchCount())
	}
	if telemetry.LastMaxBatchDuration() <= 0 {
		t.Fatal("telemetry must record a positive max batch duration")
	}
	if telemetry.LastStagedFacts() != 200 || telemetry.LastDistinctKeys() != 200 {
		t.Fatalf("telemetry staged = (%d, %d), want (200, 200)", telemetry.LastStagedFacts(), telemetry.LastDistinctKeys())
	}
	if telemetry.LastMember() != "read-0" {
		t.Fatalf("telemetry member = %q, want read-0", telemetry.LastMember())
	}
	if telemetry.LastReplayLagStart() != 250*time.Millisecond || telemetry.LastReplayLagEnd() != 250*time.Millisecond {
		t.Fatalf("telemetry replay lag = (%v, %v), want (250ms, 250ms)",
			telemetry.LastReplayLagStart(), telemetry.LastReplayLagEnd())
	}
	if got, want := telemetry.DiscardRate(), 4.0/1.0; got != want {
		t.Fatalf("writer-verify discard rate = %v, want %v (4 discards over 1 verify)", got, want)
	}
	if !strings.Contains(result.String(), "read-0") {
		t.Fatal("tick result must carry member identity")
	}
}
