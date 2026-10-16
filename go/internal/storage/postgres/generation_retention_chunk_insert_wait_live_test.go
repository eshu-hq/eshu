// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// insertWaitBlockedBound caps how long the chunk hook waits for every
// concurrent insert to show up as lock-blocked before it gives up, lets the
// prune finish, and fails the test with the observed count.
const insertWaitBlockedBound = 10 * time.Second

// insertWaitObservation is what the afterChunkTargetLocked hook saw while
// the prune held the chunk target: the tagged inserts already running before
// it started any, the most lock-blocked tagged inserts it counted after, and
// the error that ended the hook early, if any.
type insertWaitObservation struct {
	startedEarly int
	blocked      int
	err          error
}

// TestGenerationRetentionChunkConcurrentInsertWaitsLive proves a fact insert
// into a chunking generation waits out the prune's row locks instead of
// deadlocking or being lost. The five inserts start only after the prune
// holds the chunk target (afterChunkTargetLocked), and the hook returns only
// once pg_stat_activity shows all five waiting on a lock, so the ordering is
// fixed on every run (#8206). An insert that wins the race instead makes the
// prune skip the generation on purpose (SKIP LOCKED, runner.go); that path is
// TestGenerationRetentionChunkLockMissDropsSilentlyLive's proof, not this
// one's. The hook also checks that no tagged insert is running before it
// starts them, so an edit that starts the inserts early fails by assertion.
// After the prune commits, all five inserts commit and the final count is
// exact. None lands in the drained page: they are still uncommitted, blocked
// in their foreign-key check, when the page query runs, so its snapshot
// cannot see them.
func TestGenerationRetentionChunkConcurrentInsertWaitsLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scopeID, generationID = "chunk-insertwait", "chunk-insertwait-g0"
	testfixtures.SeedScope(t, ctx, database, scopeID)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, generationID, now.Add(-10*24*time.Hour))
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
SELECT 'iw-' || lpad(n::text, 5, '0'), $1, $2, 'metric', 'iwk' || n, 'git', 'iwk' || n, now(), now(), '{}'::jsonb
FROM generate_series(1, 1500) AS n`, scopeID, generationID); err != nil {
		t.Fatalf("seed insert-wait facts: %v", err)
	}

	// The tag is unique to this run, so the pg_stat_activity filter cannot
	// count another test's statements on a shared CI database.
	tag := fmt.Sprintf("/* eshu-8206-insert-wait %d */", time.Now().UnixNano())
	insertSQL := tag + `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'metric', $4, 'git', $4, now(), now(), '{}'::jsonb)`

	const inserts = 5
	var wg sync.WaitGroup
	errs := make(chan error, inserts)
	observed := make(chan insertWaitObservation, 1)
	hookCalls := 0
	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	store.afterChunkTargetLocked = func() {
		hookCalls++
		if hookCalls != 1 {
			return
		}
		early, err := countTaggedStatements(ctx, database, tag, false)
		if err != nil || early != 0 {
			observed <- insertWaitObservation{startedEarly: early, err: err}
			return
		}
		for i := range inserts {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, err := database.ExecContext(ctx, insertSQL,
					fmt.Sprintf("iw-new-%d", i), scopeID, generationID, fmt.Sprintf("iwnewk-%d", i))
				errs <- err
			}(i)
		}
		blocked, err := waitForLockBlockedInserts(ctx, database, tag, inserts, insertWaitBlockedBound)
		observed <- insertWaitObservation{blocked: blocked, err: err}
	}

	result, err := store.PruneSupersededGenerations(ctx, chunkDrainPolicy())
	wg.Wait()
	close(errs)
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if hookCalls != 1 {
		t.Fatalf("afterChunkTargetLocked ran %d times, want 1 (one chunked relock)", hookCalls)
	}
	var observation insertWaitObservation
	select {
	case observation = <-observed:
	default:
		t.Fatal("afterChunkTargetLocked reported no observation")
	}
	if observation.err != nil {
		t.Fatalf("afterChunkTargetLocked observation: %v", observation.err)
	}
	if observation.startedEarly != 0 {
		t.Fatalf("tagged inserts running before the chunk target was locked = %d, want 0",
			observation.startedEarly)
	}
	if observation.blocked != inserts {
		t.Fatalf("lock-blocked inserts while the prune held the chunk target = %d, want %d",
			observation.blocked, inserts)
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent insert: %v", err)
		}
	}
	if got := result.Skipped[generationRetentionChunkedProgressSkippedReason]; got != 1 {
		t.Fatalf("Skipped[row_limit_chunked] = %d, want 1", got)
	}
	if got := result.RowsPruned["fact_records"]; got != 1000 {
		t.Fatalf("RowsPruned[fact_records] = %d, want 1000 (one bounded page)", got)
	}
	// One 1000-fact page drained, five inserts committed after it: every row
	// is accounted for, so no insert was lost to the chunk delete and no
	// deadlock stalled either side.
	if facts := countScopeGenerationFacts(t, ctx, database, scopeID, generationID); facts != 1500-1000+inserts {
		t.Fatalf("facts after concurrent inserts = %d, want %d", facts, 1500-1000+inserts)
	}
	var newFacts int
	if err := database.QueryRowContext(ctx,
		`SELECT count(*) FROM fact_records WHERE generation_id = $1 AND fact_id LIKE 'iw-new-%'`,
		generationID).Scan(&newFacts); err != nil {
		t.Fatalf("count inserted facts: %v", err)
	}
	if newFacts != inserts {
		t.Fatalf("inserted facts after the prune = %d, want %d (none in the drained page)", newFacts, inserts)
	}
}

// waitForLockBlockedInserts polls pg_stat_activity on its own pool
// connection until exactly want active statements carrying tag wait on a
// heavyweight lock, or bound expires. It returns the last count it saw, so a
// timeout reports how many inserts were actually blocked. It never fails the
// test itself: the hook calling it runs inside the prune's transaction and
// must return so the prune can commit and release the inserts.
func waitForLockBlockedInserts(
	ctx context.Context, database *sql.DB, tag string, want int, bound time.Duration,
) (int, error) {
	waitCtx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	blocked := 0
	for {
		count, err := countTaggedStatements(waitCtx, database, tag, true)
		if err != nil && waitCtx.Err() == nil {
			return blocked, err
		}
		if err == nil {
			blocked = count
		}
		if err == nil && blocked == want {
			return blocked, nil
		}
		select {
		case <-waitCtx.Done():
			return blocked, fmt.Errorf("saw %d of %d tagged inserts lock-blocked within %v", blocked, want, bound)
		case <-ticker.C:
		}
	}
}

// countTaggedStatements counts the other sessions' active statements whose
// query text carries tag, only those waiting on a heavyweight lock when
// lockWaitOnly is set. The tag is a bind parameter, so this query's own text
// never contains it and cannot count itself.
func countTaggedStatements(ctx context.Context, database *sql.DB, tag string, lockWaitOnly bool) (int, error) {
	var count int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database()
  AND pid <> pg_backend_pid()
  AND state = 'active'
  AND (NOT $2 OR wait_event_type = 'Lock')
  AND position($1 in query) > 0`, tag, lockWaitOnly).Scan(&count); err != nil {
		return 0, fmt.Errorf("poll pg_stat_activity: %w", err)
	}
	return count, nil
}
