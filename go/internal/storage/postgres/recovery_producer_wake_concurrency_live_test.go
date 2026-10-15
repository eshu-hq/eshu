// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// The #7597 wake concurrency proof. Each round seeds several scopes, each with
// a dead-lettered deployment_mapping producer and two producer-terminal
// consumers, then races replays on independent connections:
//
//   - two identical targeted replays (duplicate operator request);
//   - a targeted producer replay against a broad stage-wide replay that
//     selects the consumers directly, in both scope orders;
//   - a consumer-domain claimer polling throughout.
//
// The proof requires no statement error (in particular no 40P01 deadlock or
// lock failure from the replays or the claimer), and every seeded row
// reopened exactly once across all replays: never twice, and never reopened
// without being reported.
func TestReplayWakeConcurrentReplaysAreIdempotentLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #7597 wake concurrency proof")
	}
	ctx := context.Background()
	db, schema := openReducerFairnessDBWithSchema(t, ctx, dsn)
	const workers, scopes, rounds = 4, 6, 15
	conns := make([]*sql.DB, workers+1)
	for i := range conns {
		conns[i] = openReducerFairnessClaimerDB(t, ctx, dsn, schema)
	}

	wokenTotal := 0
	for round := 0; round < rounds; round++ {
		scopeIDs, seeded := seedWakeRound(t, ctx, db, round, scopes)
		filters := []recovery.ReplayFilter{
			{Stage: recovery.StageReducer, ScopeIDs: scopeIDs, FailureClass: "retry_exhausted"},
			{Stage: recovery.StageReducer, ScopeIDs: scopeIDs, FailureClass: "retry_exhausted"},
			{Stage: recovery.StageReducer},
			{Stage: recovery.StageReducer, ScopeIDs: reversed(scopeIDs), Limit: 3},
		}
		var (
			mu       sync.Mutex
			reopened = map[string]int{}
			errs     []error
			start    = make(chan struct{})
			wg       sync.WaitGroup
		)
		now := time.Now().UTC()
		for i, filter := range filters {
			wg.Add(1)
			go func(conn *sql.DB, filter recovery.ReplayFilter) {
				defer wg.Done()
				<-start
				result, err := NewRecoveryStore(SQLDB{DB: conn}).ReplayFailedWorkItems(ctx, filter, now)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errs = append(errs, err)
					return
				}
				wokenTotal += len(result.WokenConsumerIDs)
				for _, id := range append(result.WorkItemIDs, result.WokenConsumerIDs...) {
					reopened[id]++
				}
			}(conns[i], filter)
		}
		claimErr, replaysDone := make(chan error, 1), make(chan struct{})
		go func() {
			<-start
			claimErr <- claimNothingDeadLettered(ctx, conns[workers], seeded, replaysDone)
		}()
		close(start)
		wg.Wait()
		close(replaysDone)
		if err := <-claimErr; err != nil {
			t.Fatalf("round %d claimer: %v", round, err)
		}
		if len(errs) > 0 {
			t.Fatalf("round %d replay errors (a deadlock or lock failure would show here): %v", round, errs)
		}
		// The bounded replay may stop short; a final drain must find nothing
		// left of the producer set, and nothing may be reopened twice.
		for id := range seeded {
			if reopened[id] > 1 {
				t.Fatalf("round %d: %s reopened %d times, want at most once", round, id, reopened[id])
			}
			var status string
			if err := db.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&status); err != nil {
				t.Fatalf("round %d read %s: %v", round, id, err)
			}
			if (status == "dead_letter") == (reopened[id] == 1) {
				t.Fatalf("round %d: %s status=%s reopened=%d; status and reported reopen disagree", round, id, status, reopened[id])
			}
		}
	}
	t.Logf("%d rounds: %d consumers reopened by the wake, the rest by a broad replay", rounds, wokenTotal)
	if wokenTotal == 0 {
		t.Fatal("no round reopened a consumer through the wake; the race never exercised it")
	}
}

// seedWakeRound seeds one round's producers and consumers, all dead-lettered.
func seedWakeRound(t *testing.T, ctx context.Context, db *sql.DB, round, scopes int) ([]string, map[string]bool) {
	t.Helper()
	at := time.Now().UTC().Add(-time.Hour)
	scopeIDs := make([]string, 0, scopes)
	seeded := map[string]bool{}
	classes := map[reducer.Domain]string{}
	for domain, class := range map[reducer.Domain]string{
		reducer.DomainDeploymentMapping:         "retry_exhausted",
		reducer.DomainDeployableUnitCorrelation: "deployable_unit_correlation_producer_dead_lettered",
		reducer.DomainWorkloadMaterialization:   "workload_materialization_producer_dead_lettered",
	} {
		classes[domain] = class
	}
	for s := 0; s < scopes; s++ {
		scope, gen := fmt.Sprintf("scope-r%d-s%d", round, s), fmt.Sprintf("gen-r%d-s%d", round, s)
		seedOwnGateScope(t, ctx, db, scope, gen, at)
		scopeIDs = append(scopeIDs, scope)
		for domain, class := range classes {
			id := fmt.Sprintf("%s-%s", scope, domain)
			addOwnGateItem(t, ctx, db, id, scope, gen, domain, at)
			if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status = 'dead_letter', failure_class = $2,
attempt_count = 3 WHERE work_item_id = $1`, id, class); err != nil {
				t.Fatalf("seed %s: %v", id, err)
			}
			seeded[id] = true
		}
	}
	return scopeIDs, seeded
}

// claimNothingDeadLettered polls a consumer-domain claim while the replays
// race, so the claim's FOR UPDATE SKIP LOCKED contends with the woken CTE's
// row locks. It fails on any claim error, and hands each claimed row back to
// pending so the replay accounting stays exact.
func claimNothingDeadLettered(ctx context.Context, conn *sql.DB, seeded map[string]bool, done <-chan struct{}) error {
	clock := time.Now().UTC().Add(time.Hour)
	q := newOwnGateQueue(conn, &clock, reducer.DomainDeployableUnitCorrelation, reducer.DomainWorkloadMaterialization)
	for {
		select {
		case <-done:
			return nil
		default:
		}
		intent, ok, err := q.Claim(ctx)
		if err != nil {
			return fmt.Errorf("claim: %w", err)
		}
		if !ok {
			continue
		}
		if !seeded[intent.IntentID] {
			continue
		}
		if _, err := conn.ExecContext(ctx, `UPDATE fact_work_items SET status = 'pending', lease_owner = NULL,
claim_until = NULL WHERE work_item_id = $1`, intent.IntentID); err != nil {
			return err
		}
	}
}

func reversed(values []string) []string {
	out := append([]string(nil), values...)
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}
