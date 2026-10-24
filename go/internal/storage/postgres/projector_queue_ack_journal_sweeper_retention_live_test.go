// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8932 seeded RED/GREEN of the sweeper-versus-retention orphan: a journal pass paused after its sweeper insert, an Ack that supersedes the swept generation and commits, then a retention prune at a clock that makes it eligible; the pass must hold the generation until it commits, so retention skips it and no activation row is orphaned; disposable database from ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN (env Skip); about 3 s locally on PostgreSQL 18.6; enrolled in the live-postgres-readiness runner; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// ajSweeperRetentionPolicy makes any superseded generation eligible at once
// when the store's clock runs two hours ahead, as the contention proof does.
func ajSweeperRetentionPolicy() GenerationRetentionPolicy {
	return GenerationRetentionPolicy{
		MinSupersededGenerations: 1, MaxSupersededAge: time.Minute,
		HardMaxSupersededAge: time.Minute, BatchGenerationLimit: 20, BatchRowLimit: 1_000_000,
	}
}

// TestSweeperActivationSurvivesRetentionPrune is the deterministic form of
// the race behind #8932. The scope's active generation g0 has no journal row.
//
//  1. A journal pass runs its sweeper insert for g0 and is paused before it
//     commits: the row is written and not visible to other sessions.
//  2. An Ack activates g1, which supersedes g0, and commits. It misses the
//     journal gate (the pass holds the key), so it writes no row.
//  3. Retention prunes at a clock that makes g0 eligible.
//  4. The pass is released and commits.
//
// Required: no activation row names a generation that no longer exists. The
// sweeper's lock on g0 makes retention skip it in step 3; the next prune,
// after the commit, deletes g0 together with its now visible row.
func TestSweeperActivationSurvivesRetentionPrune(t *testing.T) {
	d := openAckJournalDB(t, 8)
	ctx := context.Background()
	ajSeedScope(t, d, "swr")
	work := ajPending(t, d, "swr", "swr-g1", 1)

	paused := make(chan struct{})
	release := make(chan struct{})
	hookDB := newAJHookDB(d, func(context.Context, db.Transaction) error {
		close(paused)
		<-release
		return nil
	})
	passDone := make(chan error, 1)
	go func() {
		// Zero backfill scopes: only the sweeper journals g0.
		_, err := linksfreshnessstore.NewJournalStore(hookDB).Journal(ctx, 0)
		passDone <- err
	}()
	select {
	case <-paused:
	case err := <-passDone:
		t.Fatalf("journal pass ended before it paused after the sweeper insert: %v", err)
	case <-time.After(time.Minute):
		t.Fatal("journal pass never reached the statement after the sweeper insert")
	}
	released := false
	releasePass := func() {
		if !released {
			released = true
			close(release)
		}
	}
	defer releasePass()

	if err := ajQueue(SQLDB{DB: d}, true).Ack(ctx, work, runtime.Result{}); err != nil {
		t.Fatalf("Ack of swr-g1: %v", err)
	}
	if n := ajCount(t, d, `SELECT count(*) FROM scope_generations WHERE generation_id = 'swr-g0' AND status = 'superseded'`); n != 1 {
		t.Fatalf("swr-g0 is not superseded after the Ack")
	}

	retention := NewGenerationRetentionStore(SQLDB{DB: d})
	retention.Now = func() time.Time { return time.Now().UTC().Add(2 * time.Hour) }
	heldPrune, err := retention.PruneSupersededGenerations(ctx, ajSweeperRetentionPolicy())
	if err != nil {
		t.Fatalf("prune while the pass is held: %v", err)
	}

	releasePass()
	if err := <-passDone; err != nil {
		t.Fatalf("journal pass: %v", err)
	}
	orphans, err := linksfreshnessstore.NewJournalStore(SQLDB{DB: d}).Orphans(ctx)
	if err != nil {
		t.Fatalf("orphans: %v", err)
	}
	t.Logf("pruned while the pass was held: %d; orphans after the pass committed: %+v", heldPrune.GenerationsPruned, orphans)
	if orphans.Activations != 0 || orphans.Links != 0 || orphans.HeadlessBucketGroups != 0 {
		t.Fatalf("orphans after the sweeper committed: %+v (pruned while held: %d)", orphans, heldPrune.GenerationsPruned)
	}
	if heldPrune.GenerationsPruned != 0 {
		t.Fatalf("retention pruned swr-g0 while the sweeper held it: %d", heldPrune.GenerationsPruned)
	}

	later, err := retention.PruneSupersededGenerations(ctx, ajSweeperRetentionPolicy())
	if err != nil {
		t.Fatalf("prune after the pass committed: %v", err)
	}
	if later.GenerationsPruned != 1 {
		t.Fatalf("prune after the commit pruned %d generations, want swr-g0", later.GenerationsPruned)
	}
	if n := ajCount(t, d, `SELECT count(*) FROM changed_since_activations WHERE scope_id = 'swr' AND generation_id = 'swr-g0'`); n != 0 {
		t.Fatalf("the activation row of the pruned swr-g0 remains (%d)", n)
	}
	after, err := linksfreshnessstore.NewJournalStore(SQLDB{DB: d}).Orphans(ctx)
	if err != nil || after.Activations != 0 || after.Links != 0 || after.HeadlessBucketGroups != 0 {
		t.Fatalf("orphans after the second prune: %+v, %v", after, err)
	}
}
