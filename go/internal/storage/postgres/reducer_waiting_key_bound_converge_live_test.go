// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11256 bounded narrowed key under contention: many waiters with more than 16 blockers each, activations in sorted, reverse and random order concurrent with parks and re-parks through the production Service loop, converge on the wake path alone with no backstop, no 40P01 and no attempt burn
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

// remainingBlockers is the retried handler's re-check: the verdict-set scopes
// still incomplete (the same predicate the fence and the audit use).
func remainingBlockers(ctx context.Context, db *sql.DB, verdict []string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
SELECT s.scope_id FROM ingestion_scopes AS s
WHERE s.scope_id = ANY ($1::text[]) AND`+incompleteScopeRelationshipGenerationsPredicate+`
ORDER BY s.scope_id COLLATE "C"`, array.StringArray(verdict))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// recheckExecutor stands in for the corpus-fenced handler: it succeeds when no
// blocker of the item's verdict remains, and otherwise defers again with the
// blockers that do, exactly the retried-handler behaviour the wake relies on.
type recheckExecutor struct {
	db       *sql.DB
	verdicts map[string][]string
	mu       sync.Mutex
	runs     map[string]int
}

func (e *recheckExecutor) Execute(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	e.mu.Lock()
	e.runs[intent.IntentID]++
	verdict := e.verdicts[intent.IntentID]
	e.mu.Unlock()
	remaining, err := remainingBlockers(ctx, e.db, verdict)
	if err != nil {
		return reducercontract.Result{}, err
	}
	if len(remaining) > 0 {
		return reducercontract.Result{}, wideWaitCause{blocking: remaining}
	}
	return reducercontract.Result{
		IntentID: intent.IntentID, Domain: intent.Domain,
		Status: reducercontract.ResultStatusSucceeded, CompletedAt: time.Now().UTC(),
	}, nil
}

// TestWideVerdictWaitersConvergeUnderContentionLive pins the bounded key under
// the intended concurrency (#11256). Twelve waiters each wait on 20 to 40
// blockers (more than the 16 a key names) through the production Service loop
// with four workers, while activators commit the blockers concurrently in
// sorted order (the named prefix goes first), reverse order (the last blockers
// are outside the prefix when a waiter parks), and random order. Every waiter
// must finish on the wake path alone: far inside the 5 minute audit and the 10
// minute backstop, with no wait row left, no deadlock (40P01), no burned
// attempt, and Service.Run returning no error.
func TestWideVerdictWaitersConvergeUnderContentionLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #11256 key-bound proof")
	}
	for _, order := range []string{"sorted", "reverse", "random"} {
		t.Run(order, func(t *testing.T) {
			ctx := context.Background()
			db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
			db.SetMaxOpenConns(40)
			at := time.Now().UTC().Truncate(time.Microsecond)
			blockers := realShapeScopeIDs(t, 40)
			for _, id := range blockers {
				seedNarrowWaitScope(t, ctx, db, id, "gen-"+id, "pending", at)
			}
			rng := rand.New(rand.NewPCG(11256, uint64(len(order))))
			exec := &recheckExecutor{db: db, verdicts: map[string][]string{}, runs: map[string]int{}}
			var itemIDs []string
			for w := range 12 {
				id := fmt.Sprintf("conv-%02d", w)
				scope := "git-repository-scope:repository:r_conv" + id
				// The waiter's own relationship generation is active (the own
				// gate passed before the restricted read deferred), which the
				// narrowed wait's satisfaction predicate requires.
				seedNarrowWaitScope(t, ctx, db, scope, "gen-"+id, "active", at.Add(-time.Hour))
				addOwnGateItem(t, ctx, db, id, scope, "gen-"+id, reducer.DomainWorkloadMaterialization, at.Add(-time.Minute))
				size := 20 + rng.IntN(21)
				verdict := slices.Clone(blockers)
				rng.Shuffle(len(verdict), func(i, j int) { verdict[i], verdict[j] = verdict[j], verdict[i] })
				verdict = verdict[:size]
				slices.Sort(verdict)
				exec.verdicts[id] = verdict
				itemIDs = append(itemIDs, id)
			}
			clock := at
			queue := newOwnGateQueue(db, &clock, reducer.DomainWorkloadMaterialization)
			queue.Now = time.Now
			queue.MaxAttempts = 3
			service := reducer.Service{
				PollInterval: 5 * time.Millisecond, Workers: 4, WorkSource: queue, WorkSink: queue, Executor: exec,
			}
			runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- service.Run(runCtx) }()

			order2 := slices.Clone(blockers)
			switch order {
			case "reverse":
				slices.Reverse(order2)
			case "random":
				rng.Shuffle(len(order2), func(i, j int) { order2[i], order2[j] = order2[j], order2[i] })
			}
			store := NewRelationshipStore(SQLDB{DB: db})
			var actWG sync.WaitGroup
			actErrs := make(chan error, len(order2))
			sem := make(chan struct{}, 3)
			delays := make([]time.Duration, len(order2))
			for i := range delays {
				delays[i] = time.Duration(rng.IntN(15)) * time.Millisecond
			}
			for i, id := range order2 {
				actWG.Add(1)
				sem <- struct{}{}
				go func() {
					defer actWG.Done()
					defer func() { <-sem }()
					time.Sleep(delays[i])
					if err := store.ActivateResolutionGeneration(ctx, "gen-"+id, id); err != nil {
						actErrs <- fmt.Errorf("activate %s: %w", id, err)
					}
				}()
			}
			actWG.Wait()
			close(actErrs)
			for err := range actErrs {
				t.Fatalf("activation error (40P01 or lock timeout stops this proof): %v", err)
			}

			deadline := time.Now().Add(60 * time.Second)
			for {
				var open int
				if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM fact_work_items WHERE work_item_id LIKE 'conv-%' AND status <> 'succeeded'`).Scan(&open); err != nil {
					t.Fatalf("count open items: %v", err)
				}
				if open == 0 {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("Service.Run returned %v with %d items open", err, open)
				default:
				}
				if time.Now().After(deadline) {
					dumpOpenConvergenceItems(t, ctx, db, exec)
					t.Fatalf("%d of 12 items still open after 60s: a wake was lost and only the audit or backstop would reopen them", open)
				}
				time.Sleep(25 * time.Millisecond)
			}
			cancel()
			if err := <-done; err != nil {
				t.Fatalf("Service.Run = %v, want nil", err)
			}
			for _, id := range itemIDs {
				if got := countNarrowWaits(t, ctx, db, id); got != 0 {
					t.Fatalf("%s left %d wait rows", id, got)
				}
				var attempts int
				if err := db.QueryRowContext(ctx, `SELECT attempt_count FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&attempts); err != nil {
					t.Fatalf("read attempt count for %s: %v", id, err)
				}
				if attempts != 1 {
					t.Fatalf("%s attempt_count = %d, want 1: the readiness wake must not burn the attempt budget", id, attempts)
				}
			}
			reopened, _, _, err := AuditWaitingItems(ctx, SQLDB{DB: db}, nil, time.Now().UTC(), DefaultWaitingAuditInterval, 100)
			if err != nil || reopened != 0 {
				t.Fatalf("audit after convergence = reopened %d, err %v, want 0, nil", reopened, err)
			}
		})
	}
}

// dumpOpenConvergenceItems prints, for every item still open, its queue row,
// the blockers its wait row names and which of its verdict set remain.
func dumpOpenConvergenceItems(t *testing.T, ctx context.Context, db *sql.DB, exec *recheckExecutor) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT work_item_id, status, attempt_count, visible_at,
       (SELECT count(*) FROM reducer_waiting_items w WHERE w.work_item_id = f.work_item_id),
       (SELECT coalesce(jsonb_array_length(w.waits_for_key->'blocking_scope_ids'), 0) FROM reducer_waiting_items w WHERE w.work_item_id = f.work_item_id LIMIT 1)
FROM fact_work_items f WHERE work_item_id LIKE 'conv-%' AND status <> 'succeeded' ORDER BY 1`)
	if err != nil {
		t.Logf("dump: %v", err)
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, status string
		var attempts, waits int
		var named sql.NullInt64
		var visible sql.NullTime
		if err := rows.Scan(&id, &status, &attempts, &visible, &waits, &named); err != nil {
			t.Logf("dump scan: %v", err)
			return
		}
		remaining, _ := remainingBlockers(ctx, db, exec.verdicts[id])
		t.Logf("open %s: status=%s attempts=%d visible_at=%v wait_rows=%d named=%d verdict=%d remaining=%d runs=%d",
			id, status, attempts, visible.Time.Format(time.RFC3339), waits, named.Int64, len(exec.verdicts[id]), len(remaining), exec.runs[id])
	}
}
