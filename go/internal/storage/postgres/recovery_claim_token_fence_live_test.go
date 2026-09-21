// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestReducerExactClaimFenceRejectsSameOwnerStaleAttempt proves lease_owner is
// not a sufficient claim token. A process may reclaim an expired row under the
// same configured owner and the injected clock may return the same instant;
// last_attempt_at must still advance and fence every side effect from the old
// execution without disturbing the current claim.
func TestReducerExactClaimFenceRejectsSameOwnerStaleAttempt(t *testing.T) {
	database, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, generationID, _ := refinalizeResetScope(t, ctx, database, suffix)
	fixedNow := time.Now().UTC().Truncate(time.Microsecond).Add(5 * time.Second)
	workItemID := seedClaimTokenDeploymentWork(t, ctx, database, scopeID, generationID, "same-owner", fixedNow)

	queue := NewReducerQueue(SQLDB{DB: database}, "same-owner-worker", time.Minute)
	queue.ClaimDomain = reducer.DomainDeploymentMapping
	queue.Now = func() time.Time { return fixedNow }
	stale := claimTokenClaimOne(t, ctx, queue)
	expireClaimTokenLease(t, ctx, database, workItemID)
	current := claimTokenClaimOne(t, ctx, queue)

	if stale.ClaimedAt == nil || current.ClaimedAt == nil {
		t.Fatalf("claim timestamps = stale:%v current:%v, want both populated", stale.ClaimedAt, current.ClaimedAt)
	}
	if got, want := *current.ClaimedAt, stale.ClaimedAt.Add(time.Microsecond); !got.Equal(want) {
		t.Fatalf("same-clock takeover ClaimedAt = %s, want stale ClaimedAt + 1us = %s", got, want)
	}

	wantCurrent := readClaimTokenWorkState(t, ctx, database, workItemID)
	assertRejectedWithoutClaimMutation := func(name string, wantErr error, operation func() error) {
		t.Helper()
		err := operation()
		if !errors.Is(err, wantErr) {
			t.Fatalf("%s error = %v, want %v", name, err, wantErr)
		}
		if got := readClaimTokenWorkState(t, ctx, database, workItemID); got != wantCurrent {
			t.Fatalf("%s mutated current claim:\n got: %+v\nwant: %+v", name, got, wantCurrent)
		}
	}

	assertRejectedWithoutClaimMutation("stale Heartbeat", ErrReducerClaimRejected, func() error {
		return queue.Heartbeat(ctx, stale)
	})
	assertRejectedWithoutClaimMutation("stale Ack", ErrReducerClaimRejected, func() error {
		return queue.Ack(ctx, stale, reducer.Result{})
	})
	assertRejectedWithoutClaimMutation("stale retry Fail", ErrReducerClaimRejected, func() error {
		return queue.Fail(ctx, stale, claimTokenRetryableError{})
	})
	assertRejectedWithoutClaimMutation("stale dead-letter Fail", ErrReducerClaimRejected, func() error {
		return queue.Fail(ctx, stale, errors.New("permanent reducer failure"))
	})

	store := NewRelationshipStore(SQLDB{DB: database})
	assertRejectedWithoutClaimMutation("stale claim-fenced activation", reducer.ErrExecutionClaimRejected, func() error {
		return store.ActivateResolutionGenerationForClaim(
			ctx, generationID, scopeID, workItemID, stale.ClaimedAt.UTC(),
		)
	})
	assertRelationshipGenerationAbsent(t, ctx, database, generationID)

	if err := store.ActivateResolutionGenerationForClaim(
		ctx, generationID, scopeID, workItemID, current.ClaimedAt.UTC(),
	); err != nil {
		t.Fatalf("current claim activation error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = database.ExecContext(context.Background(),
			`DELETE FROM relationship_generations WHERE generation_id = $1`, generationID)
	})
}

// TestReducerExactClaimFenceAckBatchRejectsPartialStaleSet proves one current
// row cannot hide another row's stale claim token in a batch acknowledgement.
// The current row may commit, but the aggregate result must still reject and
// the same-owner takeover must remain claimed.
func TestReducerExactClaimFenceAckBatchRejectsPartialStaleSet(t *testing.T) {
	database, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeA, generationA, _ := refinalizeResetScope(t, ctx, database, suffix+"-a")
	scopeB, generationB, _ := refinalizeResetScope(t, ctx, database, suffix+"-b")
	fixedNow := time.Now().UTC().Truncate(time.Microsecond).Add(5 * time.Second)
	idA := seedClaimTokenDeploymentWork(t, ctx, database, scopeA, generationA, "batch-current", fixedNow)
	idB := seedClaimTokenDeploymentWork(t, ctx, database, scopeB, generationB, "batch-takeover", fixedNow)

	queue := NewReducerQueue(SQLDB{DB: database}, "batch-same-owner", time.Minute)
	queue.ClaimDomain = reducer.DomainDeploymentMapping
	queue.Now = func() time.Time { return fixedNow }
	first := claimTokenClaimMany(t, ctx, queue, 2)
	firstByID := claimTokenIntentsByID(t, first, idA, idB)
	expireClaimTokenLease(t, ctx, database, idB)
	takeover := claimTokenClaimOne(t, ctx, queue)
	if takeover.IntentID != idB {
		t.Fatalf("takeover intent = %q, want %q", takeover.IntentID, idB)
	}

	err := queue.AckBatch(
		ctx,
		[]reducer.Intent{firstByID[idA], firstByID[idB]},
		[]reducer.Result{{}, {}},
	)
	if !errors.Is(err, ErrReducerClaimRejected) {
		t.Fatalf("AckBatch(partial stale set) error = %v, want %v", err, ErrReducerClaimRejected)
	}
	if got := readClaimTokenWorkState(t, ctx, database, idA).status; got != "succeeded" {
		t.Fatalf("current batch row status = %q, want succeeded", got)
	}
	stateB := readClaimTokenWorkState(t, ctx, database, idB)
	if stateB.status != "claimed" || !stateB.lastAttemptAt.Equal(*takeover.ClaimedAt) {
		t.Fatalf("takeover row after partial AckBatch = %+v, want current claimed token %s", stateB, takeover.ClaimedAt)
	}
}

// TestRecoveryClaimFenceRecoveryWinsAgainstActivation proves recovery's
// EXCLUSIVE table fence blocks claim-fenced publication's ROW SHARE lock. Once
// recovery commits retirement, the expired execution must resume and reject.
func TestRecoveryClaimFenceRecoveryWinsAgainstActivation(t *testing.T) {
	database, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, generationID, _ := refinalizeResetScope(t, ctx, database, suffix)
	workItemID := seedClaimTokenDeploymentWork(t, ctx, database, scopeID, generationID, "recovery-wins", time.Now().UTC())
	queue := NewReducerQueue(SQLDB{DB: database}, "recovery-wins-worker", time.Minute)
	queue.ClaimDomain = reducer.DomainDeploymentMapping
	claim := claimTokenClaimOne(t, ctx, queue)
	expireClaimTokenLease(t, ctx, database, workItemID)
	seedActiveRelationshipGeneration(t, ctx, database, generationID, scopeID)

	locked := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRecovery := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseRecovery)
	recoveryDone := make(chan error, 1)
	go func() {
		_, err := NewRecoveryStore(&refinalizeTableLockPauseDB{
			SQLDB: SQLDB{DB: database}, locked: locked, release: release,
		}).RefinalizeScopeProjections(
			ctx, recovery.RefinalizeFilter{ScopeIDs: []string{scopeID}}, time.Now().UTC(),
		)
		recoveryDone <- err
	}()
	claimTokenAwaitSignal(t, locked, recoveryDone, "recovery EXCLUSIVE fence")

	activationConn := claimTokenOpenConn(t, ctx, database)
	activationPID := claimTokenBackendPID(t, ctx, activationConn)
	activationDone := make(chan error, 1)
	go func() {
		activationDone <- NewRelationshipStore(claimTokenSQLConn{Conn: activationConn}).
			ActivateResolutionGenerationForClaim(
				ctx, generationID, scopeID, workItemID, claim.ClaimedAt.UTC(),
			)
	}()
	assertBackendWaitingOnFactWorkItemsLock(t, ctx, database, activationPID, "RowShareLock")

	releaseRecovery()
	if err := claimTokenAwaitError(t, recoveryDone, "recovery"); err != nil {
		t.Fatalf("recovery error = %v", err)
	}
	if err := claimTokenAwaitError(t, activationDone, "activation"); !errors.Is(err, reducer.ErrExecutionClaimRejected) {
		t.Fatalf("post-recovery activation error = %v, want %v", err, reducer.ErrExecutionClaimRejected)
	}
	if got := relationshipGenerationStatus(t, ctx, database, generationID); got != "superseded" {
		t.Fatalf("relationship generation status = %q, want superseded", got)
	}
}

// TestRecoveryClaimFenceActivationWinsBeforeRecovery proves the inverse lock
// order. A valid activation takes ROW SHARE and publishes first; once its lease
// expires, recovery waits for that transaction, then acquires EXCLUSIVE and
// retires the now-committed generation.
func TestRecoveryClaimFenceActivationWinsBeforeRecovery(t *testing.T) {
	database, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, generationID, _ := refinalizeResetScope(t, ctx, database, suffix)
	workItemID := seedClaimTokenDeploymentWork(t, ctx, database, scopeID, generationID, "activation-wins", time.Now().UTC())
	queue := NewReducerQueue(SQLDB{DB: database}, "activation-wins-worker", time.Minute)
	queue.ClaimDomain = reducer.DomainDeploymentMapping
	claim := claimTokenClaimOne(t, ctx, queue)
	seedActiveRelationshipGeneration(t, ctx, database, generationID, scopeID)
	setClaimTokenLeaseTTL(t, ctx, database, workItemID, time.Second)

	activationConn := claimTokenOpenConn(t, ctx, database)
	activated := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseActivation := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseActivation)
	activationDone := make(chan error, 1)
	go func() {
		activationDone <- NewRelationshipStore(&claimTokenActivationPauseDB{
			claimTokenSQLConn: claimTokenSQLConn{Conn: activationConn},
			activated:         activated,
			release:           release,
		}).ActivateResolutionGenerationForClaim(
			ctx, generationID, scopeID, workItemID, claim.ClaimedAt.UTC(),
		)
	}()
	claimTokenAwaitSignal(t, activated, activationDone, "claim-fenced activation")
	claimTokenAwaitLeaseExpiry(t, ctx, database, workItemID)

	recoveryConn := claimTokenOpenConn(t, ctx, database)
	recoveryPID := claimTokenBackendPID(t, ctx, recoveryConn)
	recoveryDone := make(chan error, 1)
	go func() {
		_, err := NewRecoveryStore(claimTokenSQLConn{Conn: recoveryConn}).
			RefinalizeScopeProjections(
				ctx, recovery.RefinalizeFilter{ScopeIDs: []string{scopeID}}, time.Now().UTC(),
			)
		recoveryDone <- err
	}()
	assertBackendWaitingOnFactWorkItemsLock(t, ctx, database, recoveryPID, "ExclusiveLock")

	releaseActivation()
	if err := claimTokenAwaitError(t, activationDone, "activation"); err != nil {
		t.Fatalf("activation error = %v", err)
	}
	if err := claimTokenAwaitError(t, recoveryDone, "recovery"); err != nil {
		t.Fatalf("recovery error = %v", err)
	}
	if got := relationshipGenerationStatus(t, ctx, database, generationID); got != "superseded" {
		t.Fatalf("relationship generation status = %q, want superseded", got)
	}
}

type claimTokenRetryableError struct{}

func (claimTokenRetryableError) Error() string   { return "retryable reducer failure" }
func (claimTokenRetryableError) Retryable() bool { return true }

type claimTokenWorkState struct {
	status        string
	leaseOwner    string
	claimUntil    sql.NullTime
	lastAttemptAt time.Time
	attemptCount  int
	visibleAt     sql.NullTime
	failureClass  sql.NullString
	failureText   sql.NullString
	updatedAt     time.Time
}

func seedClaimTokenDeploymentWork(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	scopeID, generationID, entityKey string,
	now time.Time,
) string {
	t.Helper()
	queue := NewReducerQueue(SQLDB{DB: database}, "claim-token-seed", time.Minute)
	queue.Now = func() time.Time { return now }
	intent := runtime.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainDeploymentMapping,
		EntityKey:    entityKey,
		Reason:       "exact claim token fence proof",
		FactID:       "fact-" + entityKey,
		SourceSystem: "git",
	}
	if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{intent}); err != nil {
		t.Fatalf("seed deployment work: %v", err)
	}
	return reducerWorkItemID(intent)
}

func claimTokenClaimOne(t *testing.T, ctx context.Context, queue ReducerQueue) reducer.Intent {
	t.Helper()
	intents := claimTokenClaimMany(t, ctx, queue, 1)
	if len(intents) != 1 {
		t.Fatalf("ClaimBatch() returned %d intents, want 1", len(intents))
	}
	return intents[0]
}

func claimTokenClaimMany(t *testing.T, ctx context.Context, queue ReducerQueue, limit int) []reducer.Intent {
	t.Helper()
	intents, err := queue.ClaimBatch(ctx, limit)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	return intents
}

func claimTokenIntentsByID(t *testing.T, intents []reducer.Intent, ids ...string) map[string]reducer.Intent {
	t.Helper()
	byID := make(map[string]reducer.Intent, len(intents))
	for _, intent := range intents {
		byID[intent.IntentID] = intent
	}
	for _, id := range ids {
		if _, ok := byID[id]; !ok {
			t.Fatalf("ClaimBatch() intents = %#v, missing %q", intents, id)
		}
	}
	return byID
}

func readClaimTokenWorkState(t *testing.T, ctx context.Context, database *sql.DB, workItemID string) claimTokenWorkState {
	t.Helper()
	var state claimTokenWorkState
	err := database.QueryRowContext(ctx, `
SELECT status, COALESCE(lease_owner, ''), claim_until, last_attempt_at,
       attempt_count, visible_at, failure_class, failure_message, updated_at
FROM fact_work_items
WHERE work_item_id = $1`, workItemID).Scan(
		&state.status,
		&state.leaseOwner,
		&state.claimUntil,
		&state.lastAttemptAt,
		&state.attemptCount,
		&state.visibleAt,
		&state.failureClass,
		&state.failureText,
		&state.updatedAt,
	)
	if err != nil {
		t.Fatalf("read work item %q: %v", workItemID, err)
	}
	return state
}

func expireClaimTokenLease(t *testing.T, ctx context.Context, database *sql.DB, workItemID string) {
	t.Helper()
	setClaimTokenLeaseTTL(t, ctx, database, workItemID, -time.Second)
}

func setClaimTokenLeaseTTL(t *testing.T, ctx context.Context, database *sql.DB, workItemID string, ttl time.Duration) {
	t.Helper()
	if _, err := database.ExecContext(ctx,
		`UPDATE fact_work_items SET claim_until = clock_timestamp() + $2::interval WHERE work_item_id = $1`,
		workItemID, ttl.String(),
	); err != nil {
		t.Fatalf("set claim lease TTL: %v", err)
	}
}

func assertRelationshipGenerationAbsent(t *testing.T, ctx context.Context, database *sql.DB, generationID string) {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM relationship_generations WHERE generation_id = $1`, generationID,
	).Scan(&count); err != nil {
		t.Fatalf("count relationship generation: %v", err)
	}
	if count != 0 {
		t.Fatalf("relationship generation rows = %d, want 0", count)
	}
}

type claimTokenSQLConn struct{ *sql.Conn }

func (c claimTokenSQLConn) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return c.Conn.QueryContext(ctx, query, args...)
}

func (c claimTokenSQLConn) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := c.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return SQLTx{Tx: tx}, nil
}

type claimTokenActivationPauseDB struct {
	claimTokenSQLConn
	activated chan<- struct{}
	release   <-chan struct{}
	once      sync.Once
}

func (d *claimTokenActivationPauseDB) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	if !strings.Contains(query, "WITH claimant AS MATERIALIZED") {
		return d.claimTokenSQLConn.ExecContext(ctx, query, args...)
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	d.once.Do(func() { close(d.activated) })
	select {
	case <-d.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func claimTokenOpenConn(t *testing.T, ctx context.Context, database *sql.DB) *sql.Conn {
	t.Helper()
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated connection: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func claimTokenBackendPID(t *testing.T, ctx context.Context, conn *sql.Conn) int {
	t.Helper()
	var pid int
	if err := conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("read backend pid: %v", err)
	}
	return pid
}

func claimTokenAwaitSignal(t *testing.T, signal <-chan struct{}, failed <-chan error, label string) {
	t.Helper()
	select {
	case <-signal:
	case err := <-failed:
		t.Fatalf("%s completed before pause: %v", label, err)
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func claimTokenAwaitError(t *testing.T, done <-chan error, label string) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
		return nil
	}
}

func claimTokenAwaitLeaseExpiry(t *testing.T, ctx context.Context, database *sql.DB, workItemID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var expired bool
		if err := database.QueryRowContext(ctx,
			`SELECT claim_until <= clock_timestamp() FROM fact_work_items WHERE work_item_id = $1`, workItemID,
		).Scan(&expired); err != nil {
			t.Fatalf("read claim lease expiry: %v", err)
		}
		if expired {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatal("claim lease did not expire within 5s")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
