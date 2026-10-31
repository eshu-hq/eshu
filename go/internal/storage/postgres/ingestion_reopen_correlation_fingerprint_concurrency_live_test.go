// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8283 concurrency: four maintenance passes on separate connections over gated items finish with no error and the exact changed/unchanged split; 40 rounds of a stale claim's success write ordered before, after and concurrently with a reclaim plus the fresh claim's write always end on the fresh claim's row with the fresh write never rejected; 40 rounds of a success write racing the retention cascade never deadlock and never leave a fingerprint row behind its work item; disposable database from ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN; 2.9 s on local PostgreSQL 18.6 on 2026-10-17, also run under -race; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// The #8283 concurrency proofs. Every racing party runs on its own
// single-connection handle, so the statements truly interleave in Postgres.
// The gate itself takes no lock (it reads the fingerprint table); the success
// write locks the one work-item row FOR NO KEY UPDATE, then the fingerprint
// row, which is the order a retention cascade takes them too.

// fingerprintGateRounds is the number of independent races each proof runs.
const fingerprintGateRounds = 40

// TestDeployableUnitReopenFingerprintConcurrentPassesLive runs four
// maintenance passes at once over unchanged and changed items: no pass errors,
// every changed item ends pending, and every unchanged item stays succeeded.
func TestDeployableUnitReopenFingerprintConcurrentPassesLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8283 reopen gate proof")
	}
	ctx := context.Background()
	db, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)
	w := seedFingerprintGateWorld(t, ctx, db)
	const items = 30
	for i := range items {
		repo := fmt.Sprintf("repo-n%02d", i)
		succeedFingerprintGateItem(t, ctx, db, fmt.Sprintf("duc-n%02d", i), w.a, []string{repo}, w.at)
		if i%3 == 0 {
			upsertFingerprintGateDeploysFrom(t, ctx, db, w.b.genID, "repo-b", repo)
		}
	}
	conns := make([]*sql.DB, 4)
	for i := range conns {
		conns[i] = openReducerFairnessClaimerDB(t, ctx, targetDSN)
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(conns))
	start := make(chan struct{})
	for _, conn := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			store := NewIngestionStore(SQLDB{DB: conn})
			store.Now = func() time.Time { return w.at.Add(time.Minute) }
			errs <- store.RunDeferredRelationshipMaintenance(ctx, nil, nil)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent maintenance pass error = %v", err)
		}
	}
	for i := range items {
		want := "succeeded"
		if i%3 == 0 {
			want = "pending"
		}
		if got := workItemStatus(t, ctx, db, fmt.Sprintf("duc-n%02d", i)); got != want {
			t.Errorf("duc-n%02d status after concurrent passes = %q, want %q", i, got, want)
		}
	}
}

// TestDeployableUnitReopenFingerprintWriteRacesReclaimLive orders a stale
// claim's success write before, after, and concurrently with a reclaim plus
// the new claim's write, one order per round in rotation. Whatever the order, the stored row ends with the new claim's token, the new
// claim's write is never rejected, and the stale write either lands first (and
// is then replaced) or is rejected.
func TestDeployableUnitReopenFingerprintWriteRacesReclaimLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8283 reopen gate proof")
	}
	ctx := context.Background()
	db, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)
	a := fingerprintGateScope{scopeID: "git:8283-a", genID: "gen-a-1", repoID: "repo-a"}
	seedFingerprintGateScope(t, ctx, db, a)
	staleDB := openReducerFairnessClaimerDB(t, ctx, targetDSN)
	freshDB := openReducerFairnessClaimerDB(t, ctx, targetDSN)
	fingerprint := readFingerprintGateInput(t, ctx, db, []string{"repo-a"})
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)

	staleWon := 0
	for round := range fingerprintGateRounds {
		id := fmt.Sprintf("duc-race-%02d", round)
		t1 := base.Add(time.Duration(round) * time.Minute)
		t2 := t1.Add(time.Second)
		stale := claimFingerprintGateItem(t, ctx, db, id, a.scopeID, a.genID, reducer.DomainDeployableUnitCorrelation, t1)

		staleWrite := func() error { return recordFingerprintGateInput(ctx, staleDB, stale, fingerprint) }
		reclaimAndWrite := func() error {
			if _, err := freshDB.ExecContext(ctx, `UPDATE fact_work_items
SET last_attempt_at = $2, attempt_count = attempt_count + 1 WHERE work_item_id = $1`, id, t2); err != nil {
				return err
			}
			return recordFingerprintGateInput(ctx, freshDB, fingerprintGateClaim{workItemID: id, claimedAt: t2}, fingerprint)
		}
		var staleErr, freshErr error
		switch round % 3 {
		case 0: // the stale write lands before the reclaim
			staleErr = staleWrite()
			freshErr = reclaimAndWrite()
		case 1: // the stale write lands after the fresh claim recorded
			freshErr = reclaimAndWrite()
			staleErr = staleWrite()
		default: // both start together on separate connections
			var wg sync.WaitGroup
			start := make(chan struct{})
			wg.Add(2)
			go func() { defer wg.Done(); <-start; staleErr = staleWrite() }()
			go func() { defer wg.Done(); <-start; freshErr = reclaimAndWrite() }()
			close(start)
			wg.Wait()
		}

		if freshErr != nil {
			t.Fatalf("round %d: fresh-claim write error = %v, want nil", round, freshErr)
		}
		if staleErr == nil {
			staleWon++
		} else if !errors.Is(staleErr, reducercontract.ErrExecutionClaimRejected) {
			t.Fatalf("round %d: stale-claim write error = %v, want nil or ErrExecutionClaimRejected", round, staleErr)
		}
		var claimedAt time.Time
		if err := db.QueryRowContext(ctx, `SELECT claimed_at FROM deployable_unit_reopen_fingerprints
WHERE work_item_id = $1`, id).Scan(&claimedAt); err != nil {
			t.Fatalf("round %d: read stored claim: %v", round, err)
		}
		if !claimedAt.Equal(t2) {
			t.Fatalf("round %d: stored claimed_at = %s, want the fresh claim %s", round, claimedAt, t2)
		}
		// End the round's claim: the scope holds one live reducer lease.
		ackFingerprintGateItem(t, ctx, db, id, t2)
	}
	t.Logf("stale write landed first in %d of %d rounds, rejected in the rest", staleWon, fingerprintGateRounds)
}

// TestDeployableUnitReopenFingerprintWriteRacesRetentionLive races a success
// write against the retention delete of the item's scope generation, whose
// cascade deletes the work item and then its fingerprint row. Both take the
// work-item row before the fingerprint row, so no round deadlocks, and no
// fingerprint row outlives its work item.
func TestDeployableUnitReopenFingerprintWriteRacesRetentionLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8283 reopen gate proof")
	}
	ctx := context.Background()
	db, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)
	writerDB := openReducerFairnessClaimerDB(t, ctx, targetDSN)
	pruneDB := openReducerFairnessClaimerDB(t, ctx, targetDSN)
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)

	for round := range fingerprintGateRounds {
		scope := fingerprintGateScope{
			scopeID: fmt.Sprintf("git:8283-r%02d", round), genID: fmt.Sprintf("gen-r%02d", round), repoID: "repo-r",
		}
		seedFingerprintGateScope(t, ctx, db, scope)
		id := fmt.Sprintf("duc-prune-%02d", round)
		claim := claimFingerprintGateItem(t, ctx, db, id, scope.scopeID, scope.genID, reducer.DomainDeployableUnitCorrelation, base)
		fingerprint := readFingerprintGateInput(t, ctx, db, []string{"repo-r"})
		if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = $1`,
			scope.scopeID); err != nil {
			t.Fatalf("round %d: detach generation: %v", round, err)
		}

		var wg sync.WaitGroup
		var writeErr, pruneErr error
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			writeErr = recordFingerprintGateInput(ctx, writerDB, claim, fingerprint)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, pruneErr = pruneDB.ExecContext(ctx, deleteScopeGenerationsForRetentionQuery, []string{scope.genID})
		}()
		close(start)
		wg.Wait()

		if pruneErr != nil {
			t.Fatalf("round %d: retention delete error = %v", round, pruneErr)
		}
		if writeErr != nil && !errors.Is(writeErr, reducercontract.ErrExecutionClaimRejected) {
			t.Fatalf("round %d: success write error = %v, want nil or ErrExecutionClaimRejected", round, writeErr)
		}
		if got := fingerprintRowCount(t, ctx, db, id); got != 0 {
			t.Fatalf("round %d: fingerprint rows after the work item was pruned = %d, want 0", round, got)
		}
	}
}
