// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// stallFrom is the one replacement that derives the stalled lock of W3 from
// the shipped generation lock: a one-second stall is joined in front of the
// row, so the statement takes its snapshot, sleeps, then reaches and locks
// the row (arbiter ruling arb-7127-3e-wait, W3).
const (
	stallFrom     = "FROM scope_generations"
	stalledFrom   = "FROM (SELECT pg_sleep(1)) AS stall, scope_generations"
	stallDuration = time.Second
)

// stalledLockQuery derives the stalled variant and fails unless the shipped
// statement has exactly one anchor.
func stalledLockQuery(t *testing.T) string {
	t.Helper()
	shipped := linksfreshnessstore.LockGenerationQueryForTest
	if n := strings.Count(shipped, stallFrom); n != 1 {
		t.Fatalf("shipped generation lock has %d %q anchors, want 1; the stalled variant cannot be derived", n, stallFrom)
	}
	return strings.Replace(shipped, stallFrom, stalledFrom, 1)
}

// TestStalledLockDiffersFromShippedOnlyByTheStall is W3's hermetic guard: the
// derived statement is the shipped one with the stall added and nothing
// else, so W3 measures the production lock.
func TestStalledLockDiffersFromShippedOnlyByTheStall(t *testing.T) {
	derived := stalledLockQuery(t)
	if back := strings.Replace(derived, stalledFrom, stallFrom, 1); back != linksfreshnessstore.LockGenerationQueryForTest {
		t.Fatalf("derived lock differs from the shipped one beyond the stall:\n%s", derived)
	}
}

// TestGenerationLockChainWaitRaceShape is W3 of arbiter ruling
// arb-7127-3e-wait, the race shape S4d found for #7127 PR-3e. The stalled
// lock takes its snapshot; during the stall a non-key update of the row
// commits, then a retention-shaped session locks the new version FOR UPDATE,
// deletes it and holds for 3 s. Under the production bound the lock fails
// with 55P03 within 250 ms of the stall's end, in the update-chain walk
// ("locking updated version"). RED without the bound: the lock waits for
// retention's commit and returns no row. The control, without the committed
// update, skips at the stall's end.
func TestGenerationLockChainWaitRaceShape(t *testing.T) {
	const hold = 3 * time.Second
	// Bounds stated before the run: the timeout fires 250 ms after the stall;
	// allow 500 ms of scheduling on a loaded host, far below the 3 s hold.
	const timeoutBy = stallDuration + 250*time.Millisecond + 500*time.Millisecond
	for _, tc := range []struct {
		name     string
		bounded  bool
		update   bool
		want55   bool
		minTime  time.Duration
		maxTime  time.Duration
		wantRows int
	}{
		{"bounded: committed update, retention holds", true, true, true, stallDuration + 250*time.Millisecond, timeoutBy, 0},
		{"unbounded RED: waits for retention's commit", false, true, false, hold, hold + time.Second, 0},
		{"control: no committed update, skips", true, false, false, stallDuration, timeoutBy, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := openLedgerDB(t)
			l.seedScope(t, "wt")
			l.seedGeneration(t, "wt", "wt0", false, "superseded", fixtureEpoch, fixtureEpoch.Add(time.Hour))
			lock := l.begin(t)
			defer func() { _ = lock.Rollback() }()
			if tc.bounded {
				l.execTx(t, lock, linksfreshnessstore.SetGenerationLockTimeoutStatementForTest)
			}
			type outcome struct {
				rows    int
				elapsed time.Duration
				err     error
			}
			done := make(chan outcome, 1)
			stalled := stalledLockQuery(t)
			start := time.Now()
			go func() {
				rows, err := lock.QueryContext(l.ctx, stalled, "wt0", "wt")
				n := 0
				if err == nil {
					for rows.Next() {
						n++
					}
					err = rows.Err()
					_ = rows.Close()
				}
				done <- outcome{rows: n, elapsed: time.Since(start), err: err}
			}()
			time.Sleep(stallDuration / 4)
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
			t.Logf("%s: %d rows, err %v, after %s", tc.name, got.rows, got.err, got.elapsed)
			var pgErr *pgconn.PgError
			if tc.want55 {
				if !errors.As(got.err, &pgErr) || pgErr.Code != "55P03" || !strings.Contains(pgErr.Where, "locking updated version") {
					t.Fatalf("bounded lock = %v, want 55P03 in the update-chain walk", got.err)
				}
			} else if got.err != nil || got.rows != tc.wantRows {
				t.Fatalf("lock = %d rows, %v; want %d rows and no error", got.rows, got.err, tc.wantRows)
			}
			if got.elapsed < tc.minTime || got.elapsed > tc.maxTime {
				t.Fatalf("lock answered after %s, want between %s and %s", got.elapsed, tc.minTime, tc.maxTime)
			}
		})
	}
}
