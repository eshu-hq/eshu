// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wait_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/readiness/wait"
)

// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres/readiness/wait -run Live -count=1 -v
func readinessWaitLiveStore(t *testing.T) (wait.Store, *sql.DB, context.Context) {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the readiness-wait ledger live proofs")
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	schemaCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := postgres.ApplyBootstrap(schemaCtx, postgres.SQLDB{DB: sqlDB}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	ctx, cancelTest := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancelTest)
	return wait.Store{DB: postgres.SQLDB{DB: sqlDB}}, sqlDB, ctx
}

func liveWait(scopeID string, anchor time.Time, keys ...string) crossscope.ReadinessWait {
	return crossscope.ReadinessWait{
		ScopeID: scopeID, Domain: reducercontract.DomainWorkloadCloudRelationshipMaterialization,
		FirstDeferredAt: anchor, MissingKeys: keys, MissingCount: len(keys),
		MissingFingerprint: fmt.Sprint(keys), CommittedGenerationID: "gen-1",
		CommittedCycleStartedAt: anchor, CommittedFingerprint: fmt.Sprint(keys), UpdatedAt: anchor,
	}
}

// TestReadinessWaitConcurrentUpsertsKeepEarliestAnchorLive is §4.5 item 6: two
// concurrent upserts of one (scope, domain) keep the earlier first_deferred_at
// whichever commits last, and replaying an upsert leaves the row unchanged.
func TestReadinessWaitConcurrentUpsertsKeepEarliestAnchorLive(t *testing.T) {
	store, _, ctx := readinessWaitLiveStore(t)
	scopeID := fmt.Sprintf("aws:readiness-wait-live-%d", time.Now().UnixNano())
	early := time.Date(2026, time.September, 19, 9, 0, 0, 0, time.UTC)
	late := early.Add(10 * time.Minute)

	for round := 0; round < 20; round++ {
		roundScope := fmt.Sprintf("%s-%d", scopeID, round)
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for _, anchor := range []time.Time{late, early} {
			wg.Add(1)
			go func(anchor time.Time) {
				defer wg.Done()
				errs <- store.UpsertReadinessWait(ctx, liveWait(roundScope, anchor, "a"))
			}(anchor)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("round %d upsert: %v", round, err)
			}
		}
		got, found, err := store.GetReadinessWait(ctx, roundScope, reducercontract.DomainWorkloadCloudRelationshipMaterialization)
		if err != nil || !found {
			t.Fatalf("round %d get: found %v err %v", round, found, err)
		}
		if !got.FirstDeferredAt.Equal(early) {
			t.Fatalf("round %d first_deferred_at = %v, want the earlier %v", round, got.FirstDeferredAt, early)
		}
	}

	replayScope := scopeID + "-replay"
	row := liveWait(replayScope, early, "a", "b")
	for i := 0; i < 2; i++ {
		if err := store.UpsertReadinessWait(ctx, row); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}
	got, _, err := store.GetReadinessWait(ctx, replayScope, row.Domain)
	if err != nil {
		t.Fatalf("get replay row: %v", err)
	}
	if !got.FirstDeferredAt.Equal(early) || got.MissingCount != 2 || fmt.Sprint(got.MissingKeys) != "[a b]" ||
		!got.CommittedCycleStartedAt.Equal(early) || got.Settled() || !got.UpdatedAt.Equal(early) {
		t.Fatalf("replayed upsert changed the row: %+v", got)
	}
}

// TestReadinessWaitResetAnchorSettleAndClearLive proves a higher anchor epoch
// overrides the earliest-anchor rule, settled_at round-trips, and Clear leaves
// one tombstone at the next epoch (a replayed clear and a clear of an absent
// row are no-ops).
func TestReadinessWaitResetAnchorSettleAndClearLive(t *testing.T) {
	store, _, ctx := readinessWaitLiveStore(t)
	scopeID := fmt.Sprintf("aws:readiness-wait-reset-%d", time.Now().UnixNano())
	early := time.Date(2026, time.September, 19, 9, 0, 0, 0, time.UTC)
	settled := liveWait(scopeID, early, "a")
	settled.SettledAt = early.Add(30 * time.Minute)
	if err := store.UpsertReadinessWait(ctx, settled); err != nil {
		t.Fatalf("upsert settled: %v", err)
	}
	got, _, err := store.GetReadinessWait(ctx, scopeID, settled.Domain)
	if err != nil || !got.SettledAt.Equal(settled.SettledAt) {
		t.Fatalf("settled_at = %v (err %v), want %v", got.SettledAt, err, settled.SettledAt)
	}

	restart := liveWait(scopeID, early.Add(time.Hour), "a", "c")
	restart.AnchorEpoch = settled.AnchorEpoch + 1
	if err := store.UpsertReadinessWait(ctx, restart); err != nil {
		t.Fatalf("upsert restart: %v", err)
	}
	got, _, err = store.GetReadinessWait(ctx, scopeID, settled.Domain)
	if err != nil || !got.FirstDeferredAt.Equal(restart.FirstDeferredAt) || got.Settled() {
		t.Fatalf("restart row = %+v (err %v), want the new anchor and settled_at cleared", got, err)
	}

	// A clear carries the epoch and row version its evaluation read, so it
	// clears from the row just read, as crossscope.DecideWait does.
	for i := 0; i < 2; i++ {
		clear := got
		clear.ClearedAt = early.Add(2 * time.Hour)
		if err := store.ClearReadinessWait(ctx, clear); err != nil {
			t.Fatalf("clear %d: %v", i, err)
		}
	}
	got, found, err := store.GetReadinessWait(ctx, scopeID, settled.Domain)
	if err != nil || !found || !got.Cleared() || got.MissingCount != 0 || got.AnchorEpoch != restart.AnchorEpoch+1 {
		t.Fatalf("after clear: row %+v found %v err %v, want one tombstone at the next epoch (a replayed clear is a no-op)", got, found, err)
	}
	if err := store.ClearReadinessWait(ctx, liveWait(scopeID+"-absent", early, "a")); err != nil {
		t.Fatalf("clear absent row: %v", err)
	}
}
