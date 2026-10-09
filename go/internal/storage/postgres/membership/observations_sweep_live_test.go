// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore_test

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	membershipstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/membership"
)

// TestObservationStoreSweepLive proves the expired-observation sweep (#7774)
// against real Postgres:
//
//  1. Only rows whose evaluated_at plus their own liveness window plus the
//     grace is strictly before now are deleted. A live row, a row inside the
//     grace, a row exactly at the boundary, and a row a 1h window would
//     expire but its own 48h window keeps are kept.
//  2. A backlog drains in batches: 1,203 rows in one call, and a
//     backlog past the batch cap stops at the cap and finishes next call.
//  3. Four sweeps released together over one backlog delete each row once,
//     with no error and no deadlock.
//  4. A row an evaluation holds locked is skipped, not waited on, and is
//     kept once the evaluation renews it.
//  5. An evaluation that collides with an in-flight delete waits for it,
//     then inserts a fresh row, so a racing evaluation is never lost.
//  6. not_listed rows are never deleted, however old, so a selector whose
//     rows all expired still evaluates instead of tripping the mass-miss
//     guard on scopes it had already confirmed missing.
//
// It runs in the live-postgres-readiness runner. Run locally with a disposable
// PostgreSQL 18 administrative database:
//
//	ESHU_GENERATION_RETENTION_PROOF_DSN=postgresql://postgres:postgres@localhost:<port>/postgres?sslmode=disable \
//	ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE=1 \
//	  go test ./internal/storage/postgres/membership -run 'ObservationStoreSweepLive' -count=1
func TestObservationStoreSweepLive(t *testing.T) {
	ctx, sqlDB, store := openLiveStore(t)
	now := time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC)
	grace := membership.ExpiredObservationGrace
	const window = 3600
	expired := now.Add(-time.Duration(window)*time.Second - grace)

	seedObservations(ctx, t, sqlDB, "sel:gone", 1203, expired.Add(-time.Hour), window)
	seedObservations(ctx, t, sqlDB, "sel:grace", 3, expired.Add(time.Minute), window)
	seedObservations(ctx, t, sqlDB, "sel:boundary", 2, expired, window)
	seedObservations(ctx, t, sqlDB, "sel:live", 4, now.Add(-10*time.Minute), window)
	seedObservations(ctx, t, sqlDB, "sel:long", 2, now.Add(-grace-24*time.Hour), int((48*time.Hour)/time.Second))

	deleted, err := store.DeleteExpiredObservations(ctx, now, grace)
	if err != nil {
		t.Fatalf("DeleteExpiredObservations() error = %v", err)
	}
	if deleted != 1203 {
		t.Fatalf("deleted = %d, want the 1,203 rows past window plus grace", deleted)
	}
	want := map[string]int{"sel:grace": 3, "sel:boundary": 2, "sel:live": 4, "sel:long": 2}
	if got := countBySelector(ctx, t, sqlDB); !maps.Equal(got, want) {
		t.Fatalf("remaining rows by selector = %v, want %v", got, want)
	}

	backlog := membershipstore.ExpiredSweepMaxBatches*membershipstore.ExpiredSweepBatchSize + 7
	seedObservations(ctx, t, sqlDB, "sel:backlog", backlog, expired.Add(-time.Hour), window)
	first, err := store.DeleteExpiredObservations(ctx, now, grace)
	if err != nil || first != int64(backlog-7) {
		t.Fatalf("first backlog sweep = (%d, %v), want (%d, nil): one call stops at the batch cap", first, err, backlog-7)
	}
	second, err := store.DeleteExpiredObservations(ctx, now, grace)
	if err != nil || second != 7 {
		t.Fatalf("second backlog sweep = (%d, %v), want (7, nil)", second, err)
	}

	assertConcurrentSweepsDeleteEachRowOnce(ctx, t, sqlDB, store, now, grace, expired)
	assertLockedRowIsSkippedThenKept(ctx, t, sqlDB, store, now, grace, expired)
	assertRacingEvaluationIsNotLost(ctx, t, sqlDB, store, now, expired)
	assertNotListedHistoryKeepsGuardClear(ctx, t, sqlDB, store, now, grace, expired)
}

// assertNotListedHistoryKeepsGuardClear seeds a github_org selector whose 30
// rows all expired past the grace: 20 confirmed not_listed and 10 selected.
// The sweep deletes only the 10 selected rows. A listing that still misses
// the 20 then evaluates. Had their history been deleted they would count as
// newly unlisted, above the max(10, ceil(10% of 30)) threshold, and the guard
// would trip on every cycle.
func assertNotListedHistoryKeepsGuardClear(ctx context.Context, t *testing.T, sqlDB *sql.DB, store membershipstore.ObservationStore, now time.Time, grace time.Duration, expired time.Time) {
	t.Helper()
	selector := membership.NewGitHubOrgSelector("githubOrg", "acme", nil, false, membership.GitHubAppPrincipal("9", "10"))
	known := make([]membership.KnownScope, 30)
	listing := membership.Listing{Complete: true}
	for i := range known {
		known[i] = membership.KnownScope{ScopeID: fmt.Sprintf("scope:guard-%02d", i), Slug: fmt.Sprintf("acme/guard-%02d", i)}
		state := membership.StateNotListed
		if i >= 20 {
			state = membership.StateSelected
			listing.Repositories = append(listing.Repositories, membership.ListedRepository{
				ScopeID: known[i].ScopeID, Slug: known[i].Slug, GitHubID: int64(500 + i), State: membership.StateSelected,
			})
		}
		if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO repository_selection_observations (scope_id, selector_id, selector_kind, selector_owner, state,
    state_since, state_cycle_count, evaluated_at, liveness_window_seconds)
VALUES ($1, $2, 'github_org', 'acme', $3, $4, 5, $4, 3600)`, known[i].ScopeID, selector.ID, string(state), expired.Add(-time.Hour)); err != nil {
			t.Fatalf("seed %s: %v", known[i].ScopeID, err)
		}
	}
	if deleted, err := store.DeleteExpiredObservations(ctx, now, grace); err != nil || deleted != 10 {
		t.Fatalf("sweep = (%d, %v), want (10, nil): only the expired selected rows", deleted, err)
	}
	prior := readByScope(ctx, t, store, selector.ID)
	if len(prior) != 20 {
		t.Fatalf("rows left = %d, want the 20 expired not_listed rows kept", len(prior))
	}
	result := membership.Evaluate(membership.Input{
		Selector: selector, Now: now, LivenessWindow: time.Hour, Listing: listing, Known: known, Prior: slices.Collect(maps.Values(prior)),
	})
	if result.Outcome != membership.OutcomeEvaluated {
		t.Fatalf("outcome = %q with %d newly unlisted (threshold %d), want evaluated", result.Outcome, result.Counts.NewlyUnlisted, result.GuardThreshold)
	}
}

// assertConcurrentSweepsDeleteEachRowOnce releases four sweeps at once over a
// 3,000-row backlog: SKIP LOCKED hands them disjoint rows, so together they
// delete every row exactly once, with no error and no deadlock.
func assertConcurrentSweepsDeleteEachRowOnce(ctx context.Context, t *testing.T, sqlDB *sql.DB, store membershipstore.ObservationStore, now time.Time, grace time.Duration, expired time.Time) {
	t.Helper()
	seedObservations(ctx, t, sqlDB, "sel:contended", 3000, expired.Add(-time.Hour), 3600)
	start := make(chan struct{})
	var wg sync.WaitGroup
	deleted := make([]int64, 4)
	errs := make([]error, 4)
	for i := range 4 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			deleted[i], errs[i] = store.DeleteExpiredObservations(ctx, now, grace)
		}(i)
	}
	close(start)
	wg.Wait()
	var total int64
	for i := range 4 {
		if errs[i] != nil {
			t.Fatalf("concurrent sweep %d error = %v", i, errs[i])
		}
		total += deleted[i]
	}
	if total != 3000 {
		t.Fatalf("concurrent sweeps deleted %d rows in total (%v), want each of the 3,000 rows once", total, deleted)
	}
	if got := countBySelector(ctx, t, sqlDB)["sel:contended"]; got != 0 {
		t.Fatalf("contended rows left = %d, want 0", got)
	}
}

// assertLockedRowIsSkippedThenKept holds an expired row inside an open
// renewal, sweeps, then commits the renewal and sweeps again.
func assertLockedRowIsSkippedThenKept(ctx context.Context, t *testing.T, sqlDB *sql.DB, store membershipstore.ObservationStore, now time.Time, grace time.Duration, expired time.Time) {
	t.Helper()
	seedObservations(ctx, t, sqlDB, "sel:renewed", 1, expired.Add(-time.Hour), 3600)
	renewal, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin renewal: %v", err)
	}
	defer func() { _ = renewal.Rollback() }()
	if _, err := renewal.ExecContext(ctx,
		`UPDATE repository_selection_observations SET evaluated_at = $1 WHERE selector_id = 'sel:renewed'`, now); err != nil {
		t.Fatalf("renew row: %v", err)
	}
	sweepCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if deleted, err := store.DeleteExpiredObservations(sweepCtx, now, grace); err != nil || deleted != 0 {
		t.Fatalf("sweep during renewal = (%d, %v), want (0, nil) without waiting on the lock", deleted, err)
	}
	if err := renewal.Commit(); err != nil {
		t.Fatalf("commit renewal: %v", err)
	}
	if deleted, err := store.DeleteExpiredObservations(ctx, now, grace); err != nil || deleted != 0 {
		t.Fatalf("sweep after renewal = (%d, %v), want (0, nil)", deleted, err)
	}
	if got := countBySelector(ctx, t, sqlDB)["sel:renewed"]; got != 1 {
		t.Fatalf("renewed rows = %d, want the renewed row kept", got)
	}
}

// assertRacingEvaluationIsNotLost deletes an expired row in an open
// transaction, starts an evaluation of the same row, commits the delete, and
// checks the evaluation's row exists.
func assertRacingEvaluationIsNotLost(ctx context.Context, t *testing.T, sqlDB *sql.DB, store membershipstore.ObservationStore, now, expired time.Time) {
	t.Helper()
	selector := membership.NewGitHubOrgSelector("githubOrg", "acme", nil, false, membership.GitHubAppPrincipal("7", "8"))
	scopeID := "scope:race-00000"
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO repository_selection_observations (scope_id, selector_id, selector_kind, selector_owner, state,
    state_since, state_cycle_count, evaluated_at, liveness_window_seconds)
VALUES ($1, $2, 'github_org', 'acme', 'not_listed', $3, 5, $3, 3600)`, scopeID, selector.ID, expired.Add(-time.Hour)); err != nil {
		t.Fatalf("seed racing row: %v", err)
	}
	sweep, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin sweep: %v", err)
	}
	defer func() { _ = sweep.Rollback() }()
	if _, err := sweep.ExecContext(ctx, `DELETE FROM repository_selection_observations WHERE scope_id = $1`, scopeID); err != nil {
		t.Fatalf("delete racing row: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- store.UpsertObservations(ctx, membership.Batch{
			Selector: selector, EvaluatedAt: now, LivenessWindow: time.Hour,
			Rows: []membership.Row{{ScopeID: scopeID, State: membership.StateSelected}},
		})
	}()
	waitForLockWait(ctx, t, sqlDB)
	if err := sweep.Commit(); err != nil {
		t.Fatalf("commit sweep: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("racing UpsertObservations() error = %v", err)
	}
	got := readByScope(ctx, t, store, selector.ID)[scopeID]
	if got.State != membership.StateSelected || !got.EvaluatedAt.Equal(now) || got.StateCycleCount != 1 {
		t.Fatalf("racing evaluation row = %+v, want a fresh selected row at %v", got, now)
	}
}

// waitForLockWait polls until a backend in this database waits on a lock.
func waitForLockWait(ctx context.Context, t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := sqlDB.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			t.Fatalf("read lock waits: %v", err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the racing evaluation never waited on the in-flight delete")
}

// seedObservations inserts count selected rows for selectorID, all evaluated
// at evaluatedAt with the given liveness window in seconds.
func seedObservations(ctx context.Context, t *testing.T, sqlDB *sql.DB, selectorID string, count int, evaluatedAt time.Time, windowSeconds int) {
	t.Helper()
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO repository_selection_observations (scope_id, selector_id, selector_kind, selector_owner, state,
    state_since, state_cycle_count, evaluated_at, liveness_window_seconds)
SELECT 'scope:' || $1 || '-' || lpad(g::text, 5, '0'), $1, 'github_org', 'acme', 'selected', $2, 1, $2, $3
FROM generate_series(1, $4::int) AS g`, selectorID, evaluatedAt, windowSeconds, count); err != nil {
		t.Fatalf("seed %d observations for %s: %v", count, selectorID, err)
	}
}

func countBySelector(ctx context.Context, t *testing.T, sqlDB *sql.DB) map[string]int {
	t.Helper()
	rows, err := sqlDB.QueryContext(ctx, `SELECT selector_id, count(*) FROM repository_selection_observations GROUP BY selector_id`)
	if err != nil {
		t.Fatalf("count observations: %v", err)
	}
	defer func() { _ = rows.Close() }()
	counts := map[string]int{}
	for rows.Next() {
		var (
			selectorID string
			n          int
		)
		if err := rows.Scan(&selectorID, &n); err != nil {
			t.Fatalf("count observations: %v", err)
		}
		counts[selectorID] = n
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("count observations: %v", err)
	}
	return counts
}
