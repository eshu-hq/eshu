// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8294 completion-wake contention on the full bootstrap schema: lock_timeout gives up as classified retryable 55P03 behind a held row and succeeds after release, the locking subquery's EvalPlanQual recheck drops a row a concurrent maintenance reopen moved to pending (sentinel reopened_at preserved), wake/maintenance-reopen/production-claim races over 20 changed items escape no raw deadlock and leave none succeeded, and wake outcomes emit eshu_dp_reducer_completion_wake_total (reopened then noop) plus eshu_dp_correlation_reopened_total; disposable database from ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN; about 12 s on local PostgreSQL 18 on 2026-10-19; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestCompletionWakeLockTimeoutClassifiedLive is the F2 bound proof: behind
// a held row lock the wake gives up at its lock_timeout as a retryable
// CompletionWakeContentionError, and succeeds once the holder releases.
func TestCompletionWakeLockTimeoutClassifiedLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8294 completion wake proof")
	}
	ctx := context.Background()
	db, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)
	w := seedFingerprintGateWorld(t, ctx, db)
	upsertFingerprintGateDeploysFrom(t, ctx, db, w.b.genID, "repo-b-worker", "repo-a")

	holder := openReducerFairnessClaimerDB(t, ctx, targetDSN)
	holderTx, err := holder.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holderTx.Rollback() }()
	var held string
	if err := holderTx.QueryRowContext(ctx,
		`SELECT work_item_id FROM fact_work_items WHERE work_item_id = 'duc-a' FOR UPDATE`).Scan(&held); err != nil {
		t.Fatalf("hold duc-a row lock: %v", err)
	}

	store := completionWakeStore(openReducerFairnessClaimerDB(t, ctx, targetDSN))
	store.completionWakeLockTimeout = 300 * time.Millisecond
	started := time.Now()
	_, err = store.ReopenDeployableUnitCorrelationsForRepos(ctx, []string{"repo-a", "repo-b-worker"})
	elapsed := time.Since(started)
	var contention CompletionWakeContentionError
	if !errors.As(err, &contention) || !contention.Retryable() {
		t.Fatalf("wake behind held row = %v, want retryable CompletionWakeContentionError", err)
	}
	if elapsed < 250*time.Millisecond || elapsed > 5*time.Second {
		t.Fatalf("wake gave up after %s, want about the 300ms lock_timeout", elapsed)
	}
	if err := holderTx.Commit(); err != nil {
		t.Fatalf("release holder: %v", err)
	}
	if _, err := store.ReopenDeployableUnitCorrelationsForRepos(ctx, []string{"repo-a", "repo-b-worker"}); err != nil {
		t.Fatalf("wake after release: %v", err)
	}
	if got := workItemStatus(t, ctx, db, "duc-a"); got != "pending" {
		t.Fatalf("duc-a status after wake = %q, want pending", got)
	}
}

// TestCompletionWakeEvalPlanQualDropsReopenedRowLive is the F2 lock-recheck
// proof: a row the maintenance reopen moves to pending while the wake waits
// on its lock is dropped by the locking subquery's EvalPlanQual recheck —
// the wake neither rewrites it nor counts it.
func TestCompletionWakeEvalPlanQualDropsReopenedRowLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8294 completion wake proof")
	}
	ctx := context.Background()
	db, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)
	w := seedFingerprintGateWorld(t, ctx, db)
	upsertFingerprintGateDeploysFrom(t, ctx, db, w.b.genID, "repo-b-worker", "repo-a")

	holder := openReducerFairnessClaimerDB(t, ctx, targetDSN)
	holderTx, err := holder.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holderTx.Rollback() }()
	var held string
	if err := holderTx.QueryRowContext(ctx,
		`SELECT work_item_id FROM fact_work_items WHERE work_item_id = 'duc-a' FOR UPDATE`).Scan(&held); err != nil {
		t.Fatalf("hold duc-a row lock: %v", err)
	}

	store := completionWakeStore(openReducerFairnessClaimerDB(t, ctx, targetDSN))
	store.completionWakeLockTimeout = 10 * time.Second
	done := make(chan struct {
		reopened int64
		err      error
	}, 1)
	go func() {
		woken, err := store.ReopenDeployableUnitCorrelationsForRepos(ctx, []string{"repo-a", "repo-b-worker"})
		done <- struct {
			reopened int64
			err      error
		}{woken.Reopened, err}
	}()
	waitForCompletionWakeLockWaiter(t, ctx, db)

	// The holder performs the maintenance reopen itself (the production
	// per-row reopen statement) with a sentinel timestamp, then commits:
	// the blocked wake must recheck and drop the now-pending row.
	sentinel := w.at.Add(42 * time.Minute)
	if _, err := holderTx.ExecContext(ctx, reopenSucceededReducerWorkQuery, sentinel, "duc-a"); err != nil {
		t.Fatalf("holder maintenance reopen: %v", err)
	}
	if err := holderTx.Commit(); err != nil {
		t.Fatalf("commit holder: %v", err)
	}
	result := <-done
	if result.err != nil {
		t.Fatalf("wake after holder commit: %v", result.err)
	}
	if result.reopened != 0 {
		t.Fatalf("wake reopened = %d, want 0 (EPQ dropped the reopened row)", result.reopened)
	}
	var reopenedAt time.Time
	if err := db.QueryRowContext(ctx,
		`SELECT reopened_at FROM fact_work_items WHERE work_item_id = 'duc-a'`).Scan(&reopenedAt); err != nil {
		t.Fatalf("read duc-a reopened_at: %v", err)
	}
	if !reopenedAt.Equal(sentinel) {
		t.Fatalf("duc-a reopened_at = %s, want the holder's sentinel %s (wake must not rewrite it)", reopenedAt, sentinel)
	}
	if got := workItemStatus(t, ctx, db, "duc-a"); got != "pending" {
		t.Fatalf("duc-a status = %q, want pending", got)
	}
}

// TestCompletionWakeConcurrentWithMaintenanceAndClaimLive races the wake
// against the maintenance per-row reopen and the production claim over
// changed items: no raw deadlock escapes (wake contention, if any, is the
// classified retryable error), and every changed item ends out of
// succeeded exactly once.
func TestCompletionWakeConcurrentWithMaintenanceAndClaimLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8294 completion wake proof")
	}
	ctx := context.Background()
	db, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)
	w := seedFingerprintGateWorld(t, ctx, db)
	const items = 20
	itemIDs := make([]string, 0, items)
	for i := range items {
		repo := fmt.Sprintf("repo-w%02d", i)
		id := fmt.Sprintf("duc-w%02d", i)
		succeedFingerprintGateItem(t, ctx, db, id, w.a, []string{repo}, w.at)
		upsertFingerprintGateDeploysFrom(t, ctx, db, w.b.genID, "repo-b", repo)
		itemIDs = append(itemIDs, id)
	}
	repos := []string{"repo-b"}
	for i := range items {
		repos = append(repos, fmt.Sprintf("repo-w%02d", i))
	}

	wakeDB := openReducerFairnessClaimerDB(t, ctx, targetDSN)
	maintDB := openReducerFairnessClaimerDB(t, ctx, targetDSN)
	claimDB := openReducerFairnessClaimerDB(t, ctx, targetDSN)
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	start := make(chan struct{})
	wg.Add(3)
	go func() {
		defer wg.Done()
		<-start
		store := completionWakeStore(wakeDB)
		for range 3 {
			if _, err := store.ReopenDeployableUnitCorrelationsForRepos(ctx, repos); err != nil {
				var contention CompletionWakeContentionError
				if !errors.As(err, &contention) {
					errs <- fmt.Errorf("wake: %w", err)
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		queue := NewReducerQueue(SQLDB{DB: maintDB}, "completion-wake-maintenance-proof", time.Minute)
		for _, id := range itemIDs {
			if _, err := queue.ReopenSucceeded(ctx, id); err != nil {
				errs <- fmt.Errorf("maintenance reopen %s: %w", id, err)
			}
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		queue := NewReducerQueue(SQLDB{DB: claimDB}, "completion-wake-claim-proof", time.Minute)
		for range 5 {
			if _, _, err := queue.Claim(ctx); err != nil {
				errs <- fmt.Errorf("claim: %w", err)
			}
		}
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent operation failed: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	// Settle: one final wake leaves no changed item succeeded.
	if _, err := completionWakeStore(db).ReopenDeployableUnitCorrelationsForRepos(ctx, repos); err != nil {
		t.Fatalf("final wake: %v", err)
	}
	for _, id := range itemIDs {
		if got := workItemStatus(t, ctx, db, id); got == "succeeded" {
			t.Errorf("%s still succeeded after wake/maintenance/claim race", id)
		}
	}
}

// completionWakeMetricValue reads one attribute value's count from a named
// Int64 sum, 0 when never counted.
func completionWakeMetricValue(t *testing.T, rm metricdata.ResourceMetrics, name, attrKey, attrValue string) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data type = %T, want Int64 sum", name, m.Data)
			}
			for _, point := range sum.DataPoints {
				v, _ := point.Attributes.Value(attribute.Key(attrKey))
				if v.AsString() == attrValue {
					return point.Value
				}
			}
		}
	}
	return 0
}

// TestCompletionWakeMetricsLive is the F7 operator-signal proof: a wake
// that reopened counts once as reopened (and feeds the shared
// correlation_reopened counter), a settled re-run counts once as noop, so
// never-ran, ran-and-reopened, and ran-empty are distinguishable.
func TestCompletionWakeMetricsLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8294 completion wake proof")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	w := seedFingerprintGateWorld(t, ctx, db)
	upsertFingerprintGateDeploysFrom(t, ctx, db, w.b.genID, "repo-b-worker", "repo-a")

	inst, reader := newDeltaUnionTestInstruments(t)
	store := completionWakeStore(db).WithInstruments(inst)
	if _, err := store.ReopenDeployableUnitCorrelationsForRepos(ctx, []string{"repo-a", "repo-b-worker"}); err != nil {
		t.Fatalf("completion wake: %v", err)
	}
	succeedFingerprintGateItem(t, ctx, db, "duc-a", w.a, []string{"repo-a"}, w.at.Add(time.Minute))
	if _, err := store.ReopenDeployableUnitCorrelationsForRepos(ctx, []string{"repo-a", "repo-b-worker"}); err != nil {
		t.Fatalf("settled completion wake: %v", err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if got := completionWakeMetricValue(t, rm, "eshu_dp_reducer_completion_wake_total", "outcome", "reopened"); got != 1 {
		t.Errorf("completion_wake{reopened} = %d, want 1", got)
	}
	if got := completionWakeMetricValue(t, rm, "eshu_dp_reducer_completion_wake_total", "outcome", "noop"); got != 1 {
		t.Errorf("completion_wake{noop} = %d, want 1", got)
	}
	if got := completionWakeMetricValue(t, rm, "eshu_dp_correlation_reopened_total", "domain", "deployable_unit_correlation"); got < 1 {
		t.Errorf("correlation_reopened{deployable_unit_correlation} = %d, want >= 1", got)
	}
}
