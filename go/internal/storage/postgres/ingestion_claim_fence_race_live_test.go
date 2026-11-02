// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9376 disposable PostgreSQL 18 concurrency proof of the claim-plane commit fence: eight writers race successive tokens of one work item on one generation on their own connections, once with no projector and once with a concurrent projector claim and Ack, while a deferred-maintenance holder takes the exclusive advisory lock; no deadlock inside 30 s, refused writers leave no facts, and the stored fence token equals the highest committed fact token; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/claim/fence"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/lock"
)

// TestClaimPlaneFenceConcurrentWritersAckAndMaintenanceLive is T9: K writers
// race successive fencing tokens of one work item on one generation, each on
// its own connection, while a projector worker claims and acks the
// generation and a deferred-maintenance holder repeatedly takes the
// repository's exclusive advisory lock. Every operation finishes inside the
// bound (no deadlock); every writer either commits, is refused by the fence
// (fence_token), or meets the already-activated generation (finalized skip);
// no refused writer's fact exists; and the stored fence token equals the
// highest token whose fact committed.
func TestClaimPlaneFenceConcurrentWritersAckAndMaintenanceLive(t *testing.T) {
	live := newClaimFenceLive(t)
	dsn := ingestionTxLockSplitProofDSN(t)
	ctx := context.Background()
	const writers = 8
	const scopeID = "scope-race"

	writerStores := make([]IngestionStore, writers)
	for i := range writerStores {
		writerStores[i] = live.store(openIngestionTxLockSplitProofClaimerDB(t, ctx, dsn, live.schema), &claimFenceFakePinner{}, nil)
	}
	queue := NewProjectorQueue(SQLDB{DB: openIngestionTxLockSplitProofClaimerDB(t, ctx, dsn, live.schema)}, "race-acker", time.Minute)
	maintenance := SQLDB{DB: openIngestionTxLockSplitProofClaimerDB(t, ctx, dsn, live.schema)}

	stopMaintenance := make(chan struct{})
	var holds atomic.Int64
	maintenanceDone := runUntil(stopMaintenance, func() bool {
		tx, err := maintenance.Begin(ctx)
		if err != nil {
			t.Errorf("maintenance begin: %v", err)
			return false
		}
		if err := lockstore.AcquireDeferredMaintenanceRepoExclusiveLocks(ctx, tx, []string{scopeID}); err != nil {
			_ = tx.Rollback()
			t.Errorf("maintenance lock: %v", err)
			return false
		}
		time.Sleep(2 * time.Millisecond)
		if err := tx.Commit(); err != nil {
			t.Errorf("maintenance commit: %v", err)
			return false
		}
		holds.Add(1)
		return true
	})
	defer func() { close(stopMaintenance); <-maintenanceDone }()

	// Race 1: no projector worker, so every writer meets a pending
	// generation and only the fence orders them.
	stats := raceClaimFenceWriters(t, live, writerStores, scopeID, "gen-race-fence")
	if stats.refused != writers-stats.committed || stats.committed == 0 {
		t.Fatalf("race 1 committed=%d refused=%d, want every writer committed or fence-refused", stats.committed, stats.refused)
	}

	// Race 2: a projector worker claims and acks concurrently, so later
	// writers may meet an activated generation (finalized skip).
	stopAck := make(chan struct{})
	var acks atomic.Int64
	ackDone := runUntil(stopAck, func() bool {
		work, ok, err := queue.Claim(ctx)
		if err != nil {
			t.Errorf("projector claim: %v", err)
			return false
		}
		if !ok {
			time.Sleep(time.Millisecond)
			return true
		}
		if err := queue.Ack(ctx, work, runtime.Result{}); err == nil {
			acks.Add(1)
		}
		return true
	})
	stats2 := raceClaimFenceWriters(t, live, writerStores, scopeID, "gen-race-ack")
	close(stopAck)
	<-ackDone
	t.Logf("race1 committed=%d refused=%d; race2 committed=%d refused=%d skipped=%d; acks=%d maintenance_holds=%d rejections=%v",
		stats.committed, stats.refused, stats2.committed, stats2.refused, stats2.skipped, acks.Load(), holds.Load(), live.rejections())
}

// runUntil runs step in a goroutine until stop closes or step returns false.
func runUntil(stop <-chan struct{}, step func() bool) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if !step() {
				return
			}
		}
	}()
	return done
}

type claimFenceRaceStats struct{ committed, refused, skipped int }

// raceClaimFenceWriters starts one writer per store at once, token i+1 on
// one work item and generation, waits at most 30 s, and checks the outcome
// invariants: each writer committed, was fence-refused (and left no fact),
// or met an activated generation; the stored fence token equals the highest
// committed fact token.
func raceClaimFenceWriters(t *testing.T, live *claimFenceLive, stores []IngestionStore, scopeID, generationID string) claimFenceRaceStats {
	t.Helper()
	ctx := context.Background()
	results := make([]error, len(stores))
	start := make(chan struct{})
	var writersDone sync.WaitGroup
	for i := range stores {
		writersDone.Add(1)
		go func(token int64) {
			defer writersDone.Done()
			<-start
			results[token-1] = live.commit(stores[token-1],
				claimFenceTestMutation(scopeID, generationID, fmt.Sprintf("claim-%s-%d", generationID, token), token),
				scopeID, generationID, fmt.Sprintf("fact-%s-%d", generationID, token))
		}(int64(i + 1))
	}
	finished := make(chan struct{})
	go func() { writersDone.Wait(); close(finished) }()
	close(start)
	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatalf("%s: writers did not finish within 30s: suspected deadlock", generationID)
	}
	var stats claimFenceRaceStats
	for i, err := range results {
		token := i + 1
		present := live.count(`SELECT count(*) FROM fact_records WHERE fact_id = $1`, fmt.Sprintf("fact-%s-%d", generationID, token)) == 1
		var rejection *fence.Rejection
		switch {
		case err == nil && present:
			stats.committed++
		case err == nil:
			stats.skipped++
		case errors.As(err, &rejection) && rejection.Reason == fence.ReasonFenceToken:
			if present {
				t.Fatalf("%s: writer %d was refused but its fact exists", generationID, token)
			}
			stats.refused++
		default:
			t.Fatalf("%s: writer %d error = %v, want nil or a fence_token refusal", generationID, token, err)
		}
	}
	if stats.committed == 0 {
		t.Fatalf("%s: no writer committed", generationID)
	}
	var storedToken, highestFactToken int64
	if err := live.database.QueryRowContext(ctx, `SELECT fencing_token FROM generation_commit_fences WHERE generation_id = $1`,
		generationID).Scan(&storedToken); err != nil {
		t.Fatalf("%s: read fence: %v", generationID, err)
	}
	if err := live.database.QueryRowContext(ctx, `SELECT max(fencing_token) FROM fact_records WHERE generation_id = $1`,
		generationID).Scan(&highestFactToken); err != nil {
		t.Fatalf("%s: read facts: %v", generationID, err)
	}
	if storedToken != highestFactToken {
		t.Fatalf("%s: stored fence token = %d, highest committed fact token = %d, want equal", generationID, storedToken, highestFactToken)
	}
	return stats
}
