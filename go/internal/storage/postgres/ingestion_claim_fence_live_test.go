// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9376 disposable PostgreSQL 18 proof of the claim-plane commit fence with a fake pinner: a reaped writer at a lower token is refused after the reclaimed writer commits (no stale facts, terminal fence_token), a commit inside the pin-deadline margin rolls back, a stale re-pin cancels the commit, a re-pin hung on a stalled plane does not hold a durable commit, an equal-token replay is idempotent, an equal token under another claim is refused, and two terraform-state shaped work items fence one generation independently while pinning their planned candidate identity; bootstraps a throwaway schema; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

// Live proofs of the #9376 claim-plane commit fence against a disposable
// Postgres with the full bootstrap schema (set ESHU_POSTGRES_DSN). Each test
// builds its own throwaway schema. The claim plane is a fake pinner; the
// fence, the generation, the facts, and the projector queue are real.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/claim/fence"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// claimFenceLiveClock is a store clock tests can move from another goroutine.
type claimFenceLiveClock struct{ nanos atomic.Int64 }

func newClaimFenceLiveClock(at time.Time) *claimFenceLiveClock {
	clock := &claimFenceLiveClock{}
	clock.nanos.Store(at.UnixNano())
	return clock
}

func (c *claimFenceLiveClock) now() time.Time   { return time.Unix(0, c.nanos.Load()).UTC() }
func (c *claimFenceLiveClock) set(at time.Time) { c.nanos.Store(at.UnixNano()) }

// claimFenceLive is one live test's store, schema, clock, and metric reader.
type claimFenceLive struct {
	t        *testing.T
	database *sql.DB
	schema   string
	clock    *claimFenceLiveClock
	reader   *sdkmetric.ManualReader
	inst     *telemetry.Instruments
}

func newClaimFenceLive(t *testing.T) *claimFenceLive {
	t.Helper()
	dsn := ingestionTxLockSplitProofDSN(t)
	database, schema := openIngestionTxLockSplitProofSchema(t, dsn)
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("claim-fence-live"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return &claimFenceLive{
		t: t, database: database, schema: schema,
		clock:  newClaimFenceLiveClock(time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)),
		reader: reader, inst: inst,
	}
}

// store returns a plane-path store over handle with a pinner that never
// ticks unless ticks is set.
func (l *claimFenceLive) store(handle *sql.DB, pinner fence.Pinner, ticks chan time.Time) IngestionStore {
	store := NewIngestionStore(SQLDB{DB: handle})
	store.Now = l.clock.now
	store.Instruments = l.inst
	if ticks == nil {
		ticks = make(chan time.Time)
	}
	store.ClaimPlane = &ClaimPlaneFence{
		Pinner: pinner, Code: claimFenceTestCode, PinBackoff: time.Microsecond,
		Ticks: func(time.Duration) (<-chan time.Time, func()) { return ticks, func() {} },
	}
	return store
}

func (l *claimFenceLive) commit(store IngestionStore, mutation workflow.ClaimMutation, scopeID, generationID string, factIDs ...string) error {
	envelopes := make([]facts.Envelope, 0, len(factIDs))
	for _, factID := range factIDs {
		envelopes = append(envelopes, facts.Envelope{
			FactID: factID, ScopeID: scopeID, GenerationID: generationID,
			FactKind: "pagerduty_service", StableFactKey: factID, SourceRef: facts.Ref{SourceSystem: "pagerduty", FactKey: factID},
			FencingToken: mutation.FencingToken, ObservedAt: l.clock.now(),
			Payload: map[string]any{"writer_token": float64(mutation.FencingToken)},
		})
	}
	mutation.ObservedAt = l.clock.now()
	return store.CommitClaimedScopeGeneration(context.Background(), mutation,
		claimFenceTestScope(scopeID), claimFenceTestGeneration(scopeID, generationID, l.clock.now()),
		testfixtures.FactChannel(envelopes))
}

// count runs one verification query on the test's own schema. It first
// refuses a connection whose current schema is not the proof schema, so a
// verification can never silently resolve against public (which on a shared
// database may hold its own empty core tables and count 0).
func (l *claimFenceLive) count(query string, args ...any) int {
	l.t.Helper()
	var current string
	if err := l.database.QueryRowContext(context.Background(), `SELECT current_schema()`).Scan(&current); err != nil {
		l.t.Fatalf("read current_schema(): %v", err)
	}
	if current != l.schema {
		l.t.Fatalf("verification connection resolves schema %q, want proof schema %q", current, l.schema)
	}
	var n int
	if err := l.database.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		l.t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// rejections returns eshu_dp_ingestion_claim_fence_rejections_total by reason.
func (l *claimFenceLive) rejections() map[string]int64 {
	l.t.Helper()
	var rm metricdata.ResourceMetrics
	if err := l.reader.Collect(context.Background(), &rm); err != nil {
		l.t.Fatalf("Collect() error = %v", err)
	}
	out := map[string]int64{}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "eshu_dp_ingestion_claim_fence_rejections_total" {
				continue
			}
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				reason, _ := point.Attributes.Value("reason")
				out[reason.AsString()] += point.Value
			}
		}
	}
	return out
}

func requireRejection(t *testing.T, err error, reason fence.Reason, terminal bool) *fence.Rejection {
	t.Helper()
	var rejection *fence.Rejection
	if !errors.As(err, &rejection) || rejection.Reason != reason || rejection.TerminalFailure() != terminal {
		t.Fatalf("commit error = %v, want %s rejection (terminal=%v)", err, reason, terminal)
	}
	return rejection
}

// TestClaimPlaneFenceRefusesReclaimedStaleWriterLive is T1, the #9376
// regression: writer 2 holds the reclaimed claim at token 5 and commits the
// generation; writer 1, reaped at token 4, then commits the same generation.
// Without the fence row writer 1's commit succeeds and its own fact lands
// under the generation. With it, writer 1 is refused terminally and leaves
// nothing behind.
func TestClaimPlaneFenceRefusesReclaimedStaleWriterLive(t *testing.T) {
	live := newClaimFenceLive(t)
	store := live.store(live.database, &claimFenceFakePinner{}, nil)
	const scopeID, generationID = "scope-t1", "gen-t1"

	if err := live.commit(store, claimFenceTestMutation(scopeID, generationID, "claim-w2", 5), scopeID, generationID,
		"fact-common", "fact-w2-only"); err != nil {
		t.Fatalf("writer 2 commit error = %v", err)
	}
	queueBefore := live.count(`SELECT count(*) FROM fact_work_items WHERE generation_id = $1`, generationID)

	err := live.commit(store, claimFenceTestMutation(scopeID, generationID, "claim-w1", 4), scopeID, generationID,
		"fact-common", "fact-w1-only")
	rejection := requireRejection(t, err, fence.ReasonFenceToken, true)
	if rejection.RequestedToken != 4 || rejection.StoredToken != 5 {
		t.Fatalf("tokens = (%d, %d), want (4, 5)", rejection.RequestedToken, rejection.StoredToken)
	}
	if n := live.count(`SELECT count(*) FROM fact_records WHERE generation_id = $1 AND fencing_token = 4`, generationID); n != 0 {
		t.Fatalf("facts at stale token 4 = %d, want 0", n)
	}
	if n := live.count(`SELECT count(*) FROM fact_records WHERE fact_id = 'fact-w1-only'`); n != 0 {
		t.Fatal("stale writer's own fact landed under the generation")
	}
	if n := live.count(`SELECT count(*) FROM generation_commit_fences
		WHERE generation_id = $1 AND fencing_token = 5 AND claim_id = 'claim-w2'`, generationID); n != 1 {
		t.Fatalf("fence rows for writer 2 = %d, want 1", n)
	}
	if n := live.count(`SELECT count(*) FROM fact_work_items WHERE generation_id = $1`, generationID); n != queueBefore {
		t.Fatalf("projector queue rows = %d, want %d unchanged", n, queueBefore)
	}
	if got := live.rejections()["fence_token"]; got != 1 {
		t.Fatalf("fence_token rejections = %d, want 1", got)
	}
}

// TestClaimPlaneFenceDeadlineRollsBackLive is T2: the clock reaches the pin
// deadline minus the margin plus 1 ns before COMMIT, so the commit rolls back
// with a retryable deadline refusal and nothing persists.
func TestClaimPlaneFenceDeadlineRollsBackLive(t *testing.T) {
	live := newClaimFenceLive(t)
	start := live.clock.now()
	pinner := &clockMovingPinner{clock: live.clock, to: start.Add(time.Minute - DefaultClaimFenceMargin + time.Nanosecond)}
	store := live.store(live.database, pinner, nil)

	err := live.commit(store, claimFenceTestMutation("scope-t2", "gen-t2", "claim-t2", 1), "scope-t2", "gen-t2", "fact-t2")
	requireRejection(t, err, fence.ReasonDeadline, false)
	for table, query := range map[string]string{
		"scope_generations":        `SELECT count(*) FROM scope_generations WHERE generation_id = 'gen-t2'`,
		"generation_commit_fences": `SELECT count(*) FROM generation_commit_fences WHERE generation_id = 'gen-t2'`,
		"fact_records":             `SELECT count(*) FROM fact_records WHERE generation_id = 'gen-t2'`,
	} {
		if n := live.count(query); n != 0 {
			t.Fatalf("%s rows = %d after a deadline refusal, want 0", table, n)
		}
	}
	if got := live.rejections()["deadline"]; got != 1 {
		t.Fatalf("deadline rejections = %d, want 1", got)
	}
}

// clockMovingPinner pins successfully and moves the store clock to `to`
// after the pin, so the pre-COMMIT check sees the moved clock.
type clockMovingPinner struct {
	clock *claimFenceLiveClock
	to    time.Time
}

func (p *clockMovingPinner) PinClaimForCommit(_ context.Context, claimID string, _ int64, _, _, _ string, _ time.Time) (string, error) {
	p.clock.set(p.to)
	return "commit-" + claimID, nil
}

// TestClaimPlaneFenceStaleRepinCancelsCommitLive is T3: the second pin (the
// first re-pin) comes back stale while the fact stream is still open, so the
// commit context is cancelled, the transaction rolls back, and the error is
// the terminal stale refusal.
func TestClaimPlaneFenceStaleRepinCancelsCommitLive(t *testing.T) {
	live := newClaimFenceLive(t)
	pinner := &claimFenceFakePinner{results: []error{nil, claimFenceCodedError{"stale_fence"}}}
	ticks := make(chan time.Time)
	store := live.store(live.database, pinner, ticks)
	stream := make(chan facts.Envelope)
	go func() {
		defer close(stream)
		stream <- facts.Envelope{
			FactID: "fact-t3-a", ScopeID: "scope-t3", GenerationID: "gen-t3",
			FactKind: "pagerduty_service", StableFactKey: "a", FencingToken: 1, ObservedAt: live.clock.now(),
		}
		ticks <- time.Time{}
		for len(pinner.snapshot()) < 2 {
			time.Sleep(time.Millisecond)
		}
		time.Sleep(20 * time.Millisecond)
		stream <- facts.Envelope{
			FactID: "fact-t3-b", ScopeID: "scope-t3", GenerationID: "gen-t3",
			FactKind: "pagerduty_service", StableFactKey: "b", FencingToken: 1, ObservedAt: live.clock.now(),
		}
	}()
	mutation := claimFenceTestMutation("scope-t3", "gen-t3", "claim-t3", 1)
	mutation.ObservedAt = live.clock.now()
	err := store.CommitClaimedScopeGeneration(context.Background(), mutation, claimFenceTestScope("scope-t3"),
		claimFenceTestGeneration("scope-t3", "gen-t3", live.clock.now()), stream)
	requireRejection(t, err, fence.ReasonStaleFence, true)
	if n := live.count(`SELECT count(*) FROM scope_generations WHERE generation_id = 'gen-t3'`); n != 0 {
		t.Fatalf("generation rows after a stale re-pin = %d, want 0", n)
	}
}

// TestClaimPlaneFenceReplayAndCrossItemLive covers T4 (same claim and token
// replays idempotently), T5 (same token under another claim is the
// fence_claim_mismatch integrity refusal), and T6 (two terraform-state shaped
// work items at token 1 commit one generation; neither refuses the other).
func TestClaimPlaneFenceReplayAndCrossItemLive(t *testing.T) {
	live := newClaimFenceLive(t)
	store := live.store(live.database, &claimFenceFakePinner{}, nil)
	mutation := claimFenceTestMutation("scope-t4", "gen-t4", "claim-t4", 3)
	for attempt := 1; attempt <= 2; attempt++ {
		if err := live.commit(store, mutation, "scope-t4", "gen-t4", "fact-t4"); err != nil {
			t.Fatalf("T4 attempt %d error = %v, want idempotent accept", attempt, err)
		}
	}
	if n := live.count(`SELECT count(*) FROM generation_commit_fences WHERE generation_id = 'gen-t4' AND fencing_token = 3`); n != 1 {
		t.Fatalf("T4 fence rows = %d, want 1", n)
	}

	other := mutation
	other.ClaimID = "claim-t4-other"
	requireRejection(t, live.commit(store, other, "scope-t4", "gen-t4", "fact-t4-other"), fence.ReasonFenceClaimMismatch, true)
	if n := live.count(`SELECT count(*) FROM fact_records WHERE fact_id = 'fact-t4-other'`); n != 0 {
		t.Fatal("T5 mismatched claim's fact landed")
	}

	// T6 pins on the item's PLANNED identity (the candidate id) and fences
	// the RESOLVED snapshot generation: the two must not be confused.
	tfPinner := &claimFenceFakePinner{}
	tfStore := live.store(live.database, tfPinner, nil)
	for run := 1; run <= 2; run++ {
		item := claimFenceTestMutation("scope-t6", "tfstate-candidate:s3:abc", fmt.Sprintf("claim-run%d", run), 1)
		item.WorkItemID = fmt.Sprintf("terraform_state:i1:run-%d:tfstate-candidate:s3:abc", run)
		if err := live.commit(tfStore, item, "scope-t6", "gen-t6-snapshot", fmt.Sprintf("fact-t6-%d", run)); err != nil {
			t.Fatalf("T6 run %d error = %v, want accept", run, err)
		}
	}
	if n := live.count(`SELECT count(*) FROM generation_commit_fences WHERE generation_id = 'gen-t6-snapshot'`); n != 2 {
		t.Fatalf("T6 fence rows = %d, want 2 (one per work item)", n)
	}
	if n := live.count(`SELECT count(*) FROM generation_commit_fences WHERE generation_id = 'tfstate-candidate:s3:abc'`); n != 0 {
		t.Fatalf("T6 fence rows on the planned candidate id = %d, want 0", n)
	}
	calls := tfPinner.snapshot()
	if len(calls) != 2 {
		t.Fatalf("T6 pin calls = %d, want 2", len(calls))
	}
	for _, call := range calls {
		if call.generationID != "tfstate-candidate:s3:abc" || call.sourceRunID != "tfstate-candidate:s3:abc" {
			t.Fatalf("T6 pin call = %+v, want the planned candidate identity, not the resolved generation", call)
		}
	}
}

// hangAfterFirstPinner answers the pre-transaction pin and then blocks every
// re-pin until its context ends, like an HTTP client against a stalled plane.
type hangAfterFirstPinner struct {
	calls   atomic.Int64
	entered chan struct{}
}

func (p *hangAfterFirstPinner) PinClaimForCommit(ctx context.Context, claimID string, _ int64, _, _, _ string, _ time.Time) (string, error) {
	if p.calls.Add(1) == 1 {
		return "commit-" + claimID, nil
	}
	p.entered <- struct{}{}
	<-ctx.Done()
	return "", ctx.Err()
}

// TestClaimPlaneFenceHungRepinDoesNotHoldADurableCommitLive is the #9376
// review P2-1 probe: a re-pin blocked on a stalled plane mid-stream must not
// keep an already-durable commit from returning. The per-attempt bound is
// lifted so only the keeper stop can release the blocked call.
func TestClaimPlaneFenceHungRepinDoesNotHoldADurableCommitLive(t *testing.T) {
	live := newClaimFenceLive(t)
	pinner := &hangAfterFirstPinner{entered: make(chan struct{}, 1)}
	ticks := make(chan time.Time)
	store := live.store(live.database, pinner, ticks)
	store.ClaimPlane.PinAttemptTimeout = time.Hour
	stream := make(chan facts.Envelope)
	go func() {
		defer close(stream)
		stream <- facts.Envelope{
			FactID: "fact-p21-a", ScopeID: "scope-p21", GenerationID: "gen-p21",
			FactKind: "pagerduty_service", StableFactKey: "a", FencingToken: 1, ObservedAt: live.clock.now(),
		}
		ticks <- time.Time{}
		<-pinner.entered
		stream <- facts.Envelope{
			FactID: "fact-p21-b", ScopeID: "scope-p21", GenerationID: "gen-p21",
			FactKind: "pagerduty_service", StableFactKey: "b", FencingToken: 1, ObservedAt: live.clock.now(),
		}
	}()
	mutation := claimFenceTestMutation("scope-p21", "gen-p21", "claim-p21", 1)
	mutation.ObservedAt = live.clock.now()
	done := make(chan error, 1)
	go func() {
		done <- store.CommitClaimedScopeGeneration(context.Background(), mutation, claimFenceTestScope("scope-p21"),
			claimFenceTestGeneration("scope-p21", "gen-p21", live.clock.now()), stream)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("commit error = %v, want nil (the pin deadline still had slack)", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("commit did not return within 10s while a re-pin hung on a stalled plane")
	}
	if n := live.count(`SELECT count(*) FROM generation_commit_fences WHERE generation_id = 'gen-p21' AND fencing_token = 1`); n != 1 {
		t.Fatalf("fence rows = %d, want 1 durable row", n)
	}
	if n := live.count(`SELECT count(*) FROM fact_records WHERE generation_id = 'gen-p21'`); n != 2 {
		t.Fatalf("fact rows = %d, want 2", n)
	}
}
