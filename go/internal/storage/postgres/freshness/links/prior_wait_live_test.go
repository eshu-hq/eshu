// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// pausedLockQuery derives the shipped generation lock with a pause between
// its snapshot and its row lock: an InitPlan in the qual runs when the scan
// reaches the row, after the statement took its snapshot and before LockRows
// locks it. Deriving it from the shipped constant keeps it from drifting.
func pausedLockQuery(t *testing.T, pause time.Duration) string {
	t.Helper()
	const anchor = "WHERE generation_id = $1"
	if !strings.Contains(linksfreshnessstore.LockGenerationQueryForTest, anchor) {
		t.Fatalf("lock statement has no %q; the paused variant cannot be derived", anchor)
	}
	return strings.Replace(linksfreshnessstore.LockGenerationQueryForTest, anchor,
		fmt.Sprintf("WHERE (SELECT pg_sleep(%g)) IS NOT NULL AND generation_id = $1", pause.Seconds()), 1)
}

// TestGenerationLockOldSnapshotWaitsOnRetention pins the S4d shape found for
// #7127 PR-3e (evidence file, "The #7115 shape on the lock statement"). The
// generation lock (the activating generation's and the prior's) takes its
// snapshot, a non-key update of the generation commits, and retention locks
// and deletes the new version and holds its transaction. Locking the old
// version then follows the update chain, which does not honour SKIP LOCKED:
// the lock waits for retention's commit and then returns no row
// (generation_locked). The control, without the committed update, skips at
// once. This is the documented bounded wait (arbiter decision pending:
// arb-7127-3e-wait); it is not a deadlock, because retention never waits on
// the link.
func TestGenerationLockOldSnapshotWaitsOnRetention(t *testing.T) {
	const pause, hold = time.Second, 3 * time.Second
	for _, tc := range []struct {
		name          string
		update        bool
		wantAtLeast   time.Duration
		wantUnderHold bool
	}{
		{"control: retention holds, no committed update", false, 0, true},
		{"committed non-key update after the snapshot, retention holds", true, hold, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := openLedgerDB(t)
			l.seedScope(t, "wt")
			l.seedGeneration(t, "wt", "wt0", false, "superseded", fixtureEpoch, fixtureEpoch.Add(time.Hour))
			lock := l.begin(t)
			defer func() { _ = lock.Rollback() }()
			type outcome struct {
				rows    int
				elapsed time.Duration
				err     error
			}
			done := make(chan outcome, 1)
			paused := pausedLockQuery(t, pause)
			start := time.Now()
			go func() {
				rows, err := lock.QueryContext(l.ctx, paused, "wt0", "wt")
				if err != nil {
					done <- outcome{err: err}
					return
				}
				n := 0
				for rows.Next() {
					n++
				}
				err = rows.Err()
				_ = rows.Close()
				done <- outcome{rows: n, elapsed: time.Since(start), err: err}
			}()
			time.Sleep(pause / 4)
			if tc.update {
				l.exec(t, `UPDATE scope_generations SET ingested_at = ingested_at + interval '1 second' WHERE generation_id = 'wt0'`)
			}
			retention := l.begin(t)
			defer func() { _ = retention.Rollback() }()
			l.execTx(t, retention, `SELECT 1 FROM scope_generations WHERE generation_id = 'wt0' FOR UPDATE SKIP LOCKED`)
			l.execTx(t, retention, `DELETE FROM scope_generations WHERE generation_id = 'wt0'`)
			time.Sleep(hold - time.Since(start))
			if err := retention.Commit(); err != nil {
				t.Fatalf("commit retention: %v", err)
			}
			got := <-done
			if got.err != nil {
				t.Fatalf("lock: %v", got.err)
			}
			t.Logf("%s: lock returned %d rows after %s (pause %s, retention held until %s)", tc.name, got.rows, got.elapsed, pause, hold)
			if got.rows != 0 {
				t.Fatalf("lock returned %d rows, want 0 (generation_locked)", got.rows)
			}
			if tc.wantUnderHold && got.elapsed >= hold {
				t.Fatalf("control lock took %s, want a skip before retention commits at %s", got.elapsed, hold)
			}
			if got.elapsed < tc.wantAtLeast {
				t.Fatalf("lock took %s, want it to wait for retention (%s): the chain-walk wait is gone, update the docs", got.elapsed, tc.wantAtLeast)
			}
		})
	}
}
