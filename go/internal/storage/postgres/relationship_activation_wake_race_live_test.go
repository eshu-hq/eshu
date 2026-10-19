// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7597 activation-wake races: an activation waits for an in-flight final Fail and wakes it, a final Fail that waited on the activation lock is reopened by its own post-dead-letter wake, and a held consumer row makes the activation fail with a retryable 55P03 that rolls back and the retry wakes; isolated schema per test, about 5 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The #7597 activation-wake race proofs. A consumer's final counted Fail can
// race the producer's activation; neither order may leave it dead-lettered
// once the generation is active.

// producerDeadError is a real own-gate producer-terminal failure.
func producerDeadError(scope, gen string) error {
	return crossrepo.EvaluateOwnGate(context.Background(),
		func(string) (bool, error) { return false, nil },
		func(context.Context, string, string) (crossrepo.ProducerState, error) {
			return crossrepo.ProducerState{
				ScopeActiveGenerationID: gen, WorkItemID: "dm-1", Status: "dead_letter", FailureClass: "retry_exhausted",
			}, nil
		},
		reducercontract.Intent{ScopeID: scope, GenerationID: gen, Domain: reducer.DomainDeployableUnitCorrelation},
		true, time.Now()).Err
}

// raceFixture seeds a scope generation with a claimed deployment_mapping row
// and a deployable-unit consumer claimed on its last attempt.
type raceFixture struct {
	scope, gen string
	dm         reducercontract.Intent
	consumer   reducercontract.Intent
}

func seedRaceFixture(t *testing.T, ctx context.Context, db *sql.DB, clock *time.Time) raceFixture {
	t.Helper()
	f := raceFixture{scope: "git-repository-scope:acme/app", gen: "gen-1"}
	seedOwnGateScope(t, ctx, db, f.scope, f.gen, clock.Add(-time.Hour))
	addOwnGateItem(t, ctx, db, "dm-1", f.scope, f.gen, reducer.DomainDeploymentMapping, clock.Add(-time.Minute))
	addOwnGateItem(t, ctx, db, "c-1", f.scope, f.gen, reducer.DomainDeployableUnitCorrelation, clock.Add(-time.Minute))
	addOwnGateItem(t, ctx, db, "c-2", f.scope, f.gen, reducer.DomainWorkloadMaterialization, clock.Add(-time.Minute))
	setWakeRow(t, ctx, db, "c-1", "pending", "", 2)
	setWakeRow(t, ctx, db, "c-2", "dead_letter", crossrepo.WorkloadMaterializationProducerDeadLetteredFailureClass, 3)
	var ok bool
	var err error
	if f.dm, ok, err = newOwnGateQueue(db, clock, reducer.DomainDeploymentMapping).Claim(ctx); err != nil || !ok {
		t.Fatalf("claim dm: ok=%v err=%v", ok, err)
	}
	if f.consumer, ok, err = newOwnGateQueue(db, clock, reducer.DomainDeployableUnitCorrelation).Claim(ctx); err != nil || !ok {
		t.Fatalf("claim consumer: ok=%v err=%v", ok, err)
	}
	if f.consumer.AttemptCount != 3 {
		t.Fatalf("consumer attempt = %d, want its last attempt 3", f.consumer.AttemptCount)
	}
	return f
}

// TestActivationWaitsForAnInflightFinalFailLive: the consumer's final Fail is
// mid-transaction when the activation runs. The activation waits on that row,
// then wakes it in the same transaction. RED with the in-flight lock removed:
// the wake's snapshot misses the uncommitted dead letter.
func TestActivationWaitsForAnInflightFinalFailLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #7597 race proof")
	}
	ctx := context.Background()
	base, schema := openReducerFairnessDBWithSchema(t, ctx, dsn)
	clock := time.Now().UTC()
	f := seedRaceFixture(t, ctx, base, &clock)

	failConn := openReducerFairnessClaimerDB(t, ctx, dsn, schema)
	tx, err := failConn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A failed assertion must not leave the transaction holding locks that
	// the schema cleanup would wait on.
	t.Cleanup(func() { _ = tx.Rollback() })
	failQueue := newOwnGateQueue(failConn, &clock, reducer.DomainDeployableUnitCorrelation)
	failQueue.database = SQLTx{Tx: tx}
	if err := failQueue.Fail(ctx, f.consumer, producerDeadError(f.scope, f.gen)); err != nil {
		t.Fatalf("final Fail inside the open transaction: %v", err)
	}

	activated := make(chan error, 1)
	go func() {
		activated <- NewRelationshipStore(SQLDB{DB: openReducerFairnessClaimerDB(t, ctx, dsn, schema)}).
			ActivateResolutionGenerationForClaim(ctx, f.gen, f.scope, f.dm.IntentID, *f.dm.ClaimedAt)
	}()
	select {
	case err := <-activated:
		t.Fatalf("activation finished (%v) while the consumer's Fail held its row; it must wait", err)
	case <-time.After(400 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit final Fail: %v", err)
	}
	if err := <-activated; err != nil {
		t.Fatalf("activation: %v", err)
	}
	for _, id := range []string{"c-1", "c-2"} {
		if got := readWakeRow(t, ctx, base, id); got.status != "pending" || !got.reopened.Valid {
			t.Fatalf("%s = %+v, want woken by the activation that waited for its Fail", id, got)
		}
	}
}

// pausedActivationDB holds the activation transaction open after its
// in-flight lock statement until release.
type pausedActivationDB struct {
	SQLDB
	locked  chan struct{}
	release chan struct{}
	once    sync.Once
}

func (d *pausedActivationDB) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &pausedActivationTx{SQLTx: SQLTx{Tx: tx}, owner: d}, nil
}

type pausedActivationTx struct {
	SQLTx
	owner *pausedActivationDB
}

func (tx *pausedActivationTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	rows, err := tx.SQLTx.QueryContext(ctx, query, args...)
	if err == nil && query == lockInflightOwnGateConsumersSQL {
		tx.owner.once.Do(func() { close(tx.owner.locked) })
	}
	return rows, err
}

func (tx *pausedActivationTx) Commit() error {
	<-tx.owner.release
	return tx.SQLTx.Commit()
}

// TestFinalFailAfterTheActivationLockIsWokenLive: the consumer's final Fail
// starts after the activation locked its row. The Fail waits, dead-letters
// after the activation commits, and its own post-dead-letter wake reopens it.
// RED with wakeAfterProducerTerminalDeadLetter removed: the row stays dead.
func TestFinalFailAfterTheActivationLockIsWokenLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #7597 race proof")
	}
	ctx := context.Background()
	base, schema := openReducerFairnessDBWithSchema(t, ctx, dsn)
	clock := time.Now().UTC()
	f := seedRaceFixture(t, ctx, base, &clock)

	paused := &pausedActivationDB{
		SQLDB:  SQLDB{DB: openReducerFairnessClaimerDB(t, ctx, dsn, schema)},
		locked: make(chan struct{}), release: make(chan struct{}),
	}
	t.Cleanup(func() {
		select {
		case <-paused.release:
		default:
			close(paused.release)
		}
	})
	activated := make(chan error, 1)
	go func() {
		activated <- NewRelationshipStore(paused).
			ActivateResolutionGenerationForClaim(ctx, f.gen, f.scope, f.dm.IntentID, *f.dm.ClaimedAt)
	}()
	select {
	case <-paused.locked:
	case err := <-activated:
		t.Fatalf("activation returned (%v) before locking the in-flight consumer", err)
	case <-time.After(10 * time.Second):
		t.Fatal("activation never reached its in-flight lock")
	}
	failed := make(chan error, 1)
	go func() {
		q := newOwnGateQueue(openReducerFairnessClaimerDB(t, ctx, dsn, schema), &clock, reducer.DomainDeployableUnitCorrelation)
		failed <- q.Fail(ctx, f.consumer, producerDeadError(f.scope, f.gen))
	}()
	select {
	case err := <-failed:
		t.Fatalf("final Fail finished (%v) while the activation held the row; it must wait", err)
	case <-time.After(400 * time.Millisecond):
	}
	close(paused.release)
	if err := <-activated; err != nil {
		t.Fatalf("activation: %v", err)
	}
	if err := <-failed; err != nil {
		t.Fatalf("final Fail: %v", err)
	}
	if got := readWakeRow(t, ctx, base, "c-1"); got.status != "pending" || !got.reopened.Valid {
		t.Fatalf("c-1 = %+v, want reopened by its own post-dead-letter wake", got)
	}
}

// TestActivationLockTimeoutFailsRetryablyLive: a consumer row held past the
// activation's lock_timeout fails the activation with a retryable 55P03 and
// rolls the generation back; the next attempt activates and wakes.
func TestActivationLockTimeoutFailsRetryablyLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #7597 lock-timeout proof")
	}
	ctx := context.Background()
	base, schema := openReducerFairnessDBWithSchema(t, ctx, dsn)
	clock := time.Now().UTC()
	f := seedRaceFixture(t, ctx, base, &clock)

	holder, err := openReducerFairnessClaimerDB(t, ctx, dsn, schema).BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Rollback() })
	if _, err := holder.ExecContext(ctx, `SELECT 1 FROM fact_work_items WHERE work_item_id = 'c-1' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	store := NewRelationshipStore(SQLDB{DB: openReducerFairnessClaimerDB(t, ctx, dsn, schema)})
	store.activationLockTimeout = 200 * time.Millisecond
	err = store.ActivateResolutionGenerationForClaim(ctx, f.gen, f.scope, f.dm.IntentID, *f.dm.ClaimedAt)
	var contention ActivationWakeContentionError
	if !errors.As(err, &contention) || !reducer.IsRetryable(err) {
		t.Fatalf("activation under a held consumer row = %v, want a retryable ActivationWakeContentionError", err)
	}
	var generations int
	if err := base.QueryRowContext(ctx, `SELECT count(*) FROM relationship_generations WHERE generation_id = $1`, f.gen).Scan(&generations); err != nil {
		t.Fatal(err)
	}
	if generations != 0 {
		t.Fatalf("relationship generation rows = %d after a timed-out activation, want 0 (rolled back)", generations)
	}
	if got := readWakeRow(t, ctx, base, "c-2"); got.status != "dead_letter" {
		t.Fatalf("c-2 = %+v after a rolled-back activation, want still dead_letter", got)
	}
	if err := holder.Rollback(); err != nil {
		t.Fatal(err)
	}

	// The deployment_mapping attempt fails with the retryable contention
	// error, the queue retries it, and the next attempt activates and wakes.
	dmq := newOwnGateQueue(base, &clock, reducer.DomainDeploymentMapping)
	if err := dmq.Fail(ctx, f.dm, err); err != nil {
		t.Fatalf("fail the timed-out deployment_mapping attempt: %v", err)
	}
	st, _, _, _, vis := readProducerTerminalRow(t, ctx, base, "dm-1")
	if st != "retrying" {
		t.Fatalf("deployment_mapping after the contention failure = %s, want retrying", st)
	}
	clock = vis.Add(time.Second)
	retried, ok, err := dmq.Claim(ctx)
	if err != nil || !ok || retried.IntentID != "dm-1" || retried.AttemptCount != 2 {
		t.Fatalf("re-claim deployment_mapping: ok=%v id=%s attempt=%d err=%v", ok, retried.IntentID, retried.AttemptCount, err)
	}
	if err := store.ActivateResolutionGenerationForClaim(ctx, f.gen, f.scope, retried.IntentID, *retried.ClaimedAt); err != nil {
		t.Fatalf("retried activation: %v", err)
	}
	if err := dmq.Ack(ctx, retried, reducer.Result{IntentID: retried.IntentID, Domain: retried.Domain, Status: reducer.ResultStatusSucceeded}); err != nil {
		t.Fatalf("ack the retried deployment_mapping: %v", err)
	}
	if got := readWakeRow(t, ctx, base, "c-2"); got.status != "pending" || !got.reopened.Valid {
		t.Fatalf("c-2 = %+v after the retried deployment_mapping activated, want woken", got)
	}
	if st, _, _, _, _ := readProducerTerminalRow(t, ctx, base, "dm-1"); st != "succeeded" {
		t.Fatalf("retried deployment_mapping = %s, want succeeded", st)
	}
}
