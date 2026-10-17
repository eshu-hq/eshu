// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

import (
	"context"
	"database/sql"
	"errors"
	"math/rand"
	"sync"
	"testing"
	"time"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
)

var errLiveLoad = errors.New("live test load failure")

// failingDeriver returns a Deriver whose step 2 fails after delay, so the
// derive goes straight to its failure-only update.
func failingDeriver(t *testing.T, db *sql.DB, delay time.Duration) *artifacts.Deriver {
	t.Helper()
	deriver := newLiveDeriver(t, db, testExtractorVersion)
	artifacts.SetDeriveHooks(deriver, artifacts.DeriveHooks{Load: func(context.Context, contentartifacts.Reader, contentartifacts.Repository, []contentartifacts.File, bool, int) ([]map[string]any, contentartifacts.ReadStats, error) {
		time.Sleep(delay)
		return nil, contentartifacts.ReadStats{}, errLiveLoad
	}})
	return deriver
}

// advisoryKeys returns the 64-bit keys of the advisory locks of this database
// with the given grant state.
func advisoryKeys(ctx context.Context, t *testing.T, db *sql.DB, granted bool) []int64 {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT (classid::bigint << 32) | objid::bigint FROM pg_locks
WHERE locktype = 'advisory' AND granted = $1
  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`, granted)
	if err != nil {
		t.Fatalf("read advisory keys: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var keys []int64
	for rows.Next() {
		var key int64
		if err := rows.Scan(&key); err != nil {
			t.Fatalf("scan advisory key: %v", err)
		}
		keys = append(keys, key)
	}
	return keys
}

// TestDeriveFailureRecordWaitsOnTheWriteLockLive: while a successful derive
// of a newer version holds the repository lock, a failing derive's
// failure-only update waits on that same lock (the waiter's key equals the
// holder's, and both equal hashtextextended(RepoLockKey(repo), 0)). After
// the write commits, the failure counts itself and leaves the newer
// content_version and rows as they are.
func TestDeriveFailureRecordWaitsOnTheWriteLockLive(t *testing.T) {
	ctx, db := openDatabase(t)
	putFile(ctx, t, db, "repo-a", "docker-compose.yml", composeBody(1))
	if got := newLiveDeriver(t, db, testExtractorVersion).DeriveRepository(ctx, artifacts.Request{RepoID: "repo-a"}).Outcome; got != artifacts.OutcomeOK {
		t.Fatalf("first derive = %q", got)
	}
	putFile(ctx, t, db, "repo-a", "docker-compose.yml", composeBody(2))
	current := versionOf(ctx, t, db, "repo-a")

	release, held := heldDerive(ctx, t, db, "repo-a")
	failed := make(chan artifacts.Result, 1)
	failing := failingDeriver(t, db, 0)
	go func() {
		failed <- failing.DeriveRepository(ctx, artifacts.Request{RepoID: "repo-a", Trigger: artifacts.TriggerRepair})
	}()
	waitForAdvisoryWaiter(ctx, t, db)

	var want int64
	if err := db.QueryRowContext(ctx, `SELECT hashtextextended($1, 0)`, artifacts.RepoLockKey("repo-a")).Scan(&want); err != nil {
		t.Fatalf("hash lock key: %v", err)
	}
	holders, waiters := advisoryKeys(ctx, t, db, true), advisoryKeys(ctx, t, db, false)
	if len(holders) != 1 || len(waiters) != 1 || holders[0] != want || waiters[0] != want {
		t.Fatalf("advisory keys: held %v, waiting %v; want both [%d]", holders, waiters, want)
	}
	release()
	if result := <-held; result.Outcome != artifacts.OutcomeOK {
		t.Fatalf("held derive = %+v, want ok", result)
	}
	if result := <-failed; result.Outcome != artifacts.OutcomeError || !result.FailureRecorded {
		t.Fatalf("failing derive = %+v, want error with the failure recorded", result)
	}
	state := stateOf(ctx, t, db, "repo-a")
	if state.ContentVersion == nil || *state.ContentVersion != current || state.FailureCount != 1 || *state.RowCount != 1 {
		t.Fatalf("state = %+v, want content_version %d, row_count 1, failure_count 1", state, current)
	}
	if got := rowGeneration(t, storedRows(ctx, t, db, "repo-a")); got != 2 {
		t.Fatalf("stored generation = %d, want 2", got)
	}
}

// TestDeriveFailureRecordRacingNewerWritesLive: each round writes new content
// and races a successful derive against a failing derive. Whatever the order,
// the failure-only update never lowers content_version, never changes the
// rows, and the state ends at the current version with that round's rows.
func TestDeriveFailureRecordRacingNewerWritesLive(t *testing.T) {
	ctx, db := openDatabase(t)
	rng := rand.New(rand.NewSource(8155))
	// Odd rounds let the write usually win; even rounds hold the write back
	// after its version read so the failure usually records first. Both
	// orders must occur.
	prompt := newLiveDeriver(t, db, testExtractorVersion)
	delayed := newLiveDeriver(t, db, testExtractorVersion)
	artifacts.SetDeriveHooks(delayed, artifacts.DeriveHooks{AfterVersionRead: func(context.Context) { time.Sleep(30 * time.Millisecond) }})
	var last int64 = -1
	failuresAfterSuccess := 0
	for round := 1; round <= 30; round++ {
		putFile(ctx, t, db, "repo-a", "docker-compose.yml", composeBody(round))
		succeeding, failing := prompt, failingDeriver(t, db, time.Duration(rng.Intn(20000))*time.Microsecond)
		if round%2 == 0 {
			succeeding, failing = delayed, failingDeriver(t, db, time.Duration(rng.Intn(2000))*time.Microsecond)
		}
		var wg sync.WaitGroup
		var ok, failed artifacts.Result
		wg.Add(2)
		go func() { defer wg.Done(); ok = succeeding.DeriveRepository(ctx, artifacts.Request{RepoID: "repo-a"}) }()
		go func() { defer wg.Done(); failed = failing.DeriveRepository(ctx, artifacts.Request{RepoID: "repo-a"}) }()
		wg.Wait()
		if ok.Outcome != artifacts.OutcomeOK || failed.Outcome != artifacts.OutcomeError || !failed.FailureRecorded {
			t.Fatalf("round %d: success %+v, failure %+v", round, ok, failed)
		}
		state := stateOf(ctx, t, db, "repo-a")
		current := versionOf(ctx, t, db, "repo-a")
		if state.ContentVersion == nil || *state.ContentVersion != current || *state.ContentVersion <= last {
			t.Fatalf("round %d: state %+v, version %d, previous %d; want the current, advancing version", round, state, current, last)
		}
		last = *state.ContentVersion
		if got := rowGeneration(t, storedRows(ctx, t, db, "repo-a")); got != round || *state.RowCount != 1 {
			t.Fatalf("round %d: stored generation %d, row_count %d", round, got, *state.RowCount)
		}
		if state.FailureCount == 1 {
			failuresAfterSuccess++
		}
	}
	t.Logf("30 rounds: the failure update landed after the write in %d, before it in %d", failuresAfterSuccess, 30-failuresAfterSuccess)
	if failuresAfterSuccess < 5 || 30-failuresAfterSuccess < 5 {
		t.Fatalf("orders: %d after, %d before; want at least 5 of each", failuresAfterSuccess, 30-failuresAfterSuccess)
	}
}
