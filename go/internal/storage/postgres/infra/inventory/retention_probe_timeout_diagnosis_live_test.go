// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// retentionTimeoutDiagnosisLockKey is the advisory lock key the diagnosis
// unit test parks a blocked session on. Derive locks hash repository names,
// so a small integer key cannot collide with them.
const retentionTimeoutDiagnosisLockKey = 82850001

// retentionTimeoutDiagnosisTables are the tables whose bloat and analyze
// state the timeout diagnosis reports: the ones the retention seed fills and
// the retention statements read and write.
var retentionTimeoutDiagnosisTables = []string{
	"fact_records", "scope_generations", "ingestion_scopes",
	"content_entities", "content_files", "infra_resource_entities",
}

// retentionTimeoutDiagnosisBudget bounds the diagnosis itself: a wedged
// server must not wedge the failure report. The prune's own context is
// already expired when this runs, so it carries a fresh budget.
const retentionTimeoutDiagnosisBudget = 15 * time.Second

// snapshotRetentionTimeoutDiagnosis captures pg_stat_activity, ungranted
// pg_locks with their blockers, and table bloat state for the database sqlDB
// is open against, logs every row with a RETENTION_DIAGNOSIS prefix, and
// returns a one-line summary. Call it when a retention prune fails with its
// context expired (#8285): the failure then names the blocker (a lock wait
// with the blocker pid and query) or its absence (a CPU-bound plan or a
// stalled host, with the running query and wait event) instead of a bare
// context deadline.
func snapshotRetentionTimeoutDiagnosis(t *testing.T, sqlDB *sql.DB, phase string) string {
	t.Helper()
	// The prune context is expired when this runs, so the snapshot carries a
	// fresh budget instead of inheriting it.
	snapCtx, cancel := context.WithTimeout(context.Background(), retentionTimeoutDiagnosisBudget)
	defer cancel()
	type activityRow struct {
		pid         int
		elapsed     float64
		state       string
		waitType    sql.NullString
		waitEvent   sql.NullString
		backendType string
		query       string
	}
	var oldest activityRow
	haveOldest := false
	active := 0
	rows, err := sqlDB.QueryContext(snapCtx, `
SELECT pid, EXTRACT(EPOCH FROM clock_timestamp() - query_start),
       state, wait_event_type, wait_event, backend_type, left(query, 120)
FROM pg_stat_activity
WHERE datname = current_database() AND pid <> pg_backend_pid()
ORDER BY query_start LIMIT 20`)
	if err != nil {
		summary := fmt.Sprintf("diagnosis phase=%s snapshot_error=%v", phase, err)
		t.Logf("RETENTION_DIAGNOSIS %s", summary)
		return summary
	}
	for rows.Next() {
		var row activityRow
		if err := rows.Scan(&row.pid, &row.elapsed, &row.state, &row.waitType, &row.waitEvent, &row.backendType, &row.query); err != nil {
			_ = rows.Close()
			summary := fmt.Sprintf("diagnosis phase=%s snapshot_scan_error=%v", phase, err)
			t.Logf("RETENTION_DIAGNOSIS %s", summary)
			return summary
		}
		active++
		// The stuck prune session is active; an idle pooled connection's
		// last query must not win the oldest slot over it.
		if row.state == "active" && (!haveOldest || row.elapsed > oldest.elapsed) {
			oldest = row
			haveOldest = true
		}
		t.Logf("RETENTION_DIAGNOSIS phase=%s activity pid=%d state=%s elapsed_s=%.1f wait=%s:%s backend=%s query=%q",
			phase, row.pid, row.state, row.elapsed, nullString(row.waitType), nullString(row.waitEvent), row.backendType, row.query)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		summary := fmt.Sprintf("diagnosis phase=%s snapshot_rows_error=%v", phase, err)
		t.Logf("RETENTION_DIAGNOSIS %s", summary)
		return summary
	}

	blocked := 0
	var blockers []string
	lockRows, err := sqlDB.QueryContext(snapCtx, `
SELECT blocked.pid, blocking.pid, left(blocked_act.query, 120), left(blocking_act.query, 120)
FROM pg_locks AS blocked
JOIN pg_locks AS blocking ON blocking.locktype = blocked.locktype
  AND blocking.database IS NOT DISTINCT FROM blocked.database
  AND blocking.relation IS NOT DISTINCT FROM blocked.relation
  AND blocking.page IS NOT DISTINCT FROM blocked.page
  AND blocking.tuple IS NOT DISTINCT FROM blocked.tuple
  AND blocking.transactionid IS NOT DISTINCT FROM blocked.transactionid
  AND blocking.classid IS NOT DISTINCT FROM blocked.classid
  AND blocking.objid IS NOT DISTINCT FROM blocked.objid
  AND blocking.objsubid IS NOT DISTINCT FROM blocked.objsubid
  AND blocking.pid <> blocked.pid
JOIN pg_stat_activity AS blocked_act ON blocked_act.pid = blocked.pid
JOIN pg_stat_activity AS blocking_act ON blocking_act.pid = blocking.pid
WHERE NOT blocked.granted
  AND blocked_act.datname = current_database()
  AND blocking_act.datname = current_database()
LIMIT 20`)
	if err != nil {
		summary := fmt.Sprintf("diagnosis phase=%s snapshot_locks_error=%v", phase, err)
		t.Logf("RETENTION_DIAGNOSIS %s", summary)
		return summary
	}
	for lockRows.Next() {
		var blockedPID, blockerPID int
		var blockedQuery, blockerQuery string
		if err := lockRows.Scan(&blockedPID, &blockerPID, &blockedQuery, &blockerQuery); err != nil {
			_ = lockRows.Close()
			summary := fmt.Sprintf("diagnosis phase=%s snapshot_locks_scan_error=%v", phase, err)
			t.Logf("RETENTION_DIAGNOSIS %s", summary)
			return summary
		}
		blocked++
		blockers = append(blockers, fmt.Sprintf("blocked=%d blocker=%d blocked_query=%q blocker_query=%q",
			blockedPID, blockerPID, blockedQuery, blockerQuery))
		t.Logf("RETENTION_DIAGNOSIS phase=%s lock %s", phase, blockers[len(blockers)-1])
	}
	_ = lockRows.Close()
	if err := lockRows.Err(); err != nil {
		summary := fmt.Sprintf("diagnosis phase=%s snapshot_locks_rows_error=%v", phase, err)
		t.Logf("RETENTION_DIAGNOSIS %s", summary)
		return summary
	}

	var tableNotes []string
	for _, table := range retentionTimeoutDiagnosisTables {
		var live, dead sql.NullInt64
		var lastVacuum, lastAnalyze sql.NullString
		if err := sqlDB.QueryRowContext(snapCtx, `
SELECT n_live_tup, n_dead_tup, last_autovacuum::text, last_autoanalyze::text
FROM pg_stat_user_tables WHERE relname = $1`, table).Scan(&live, &dead, &lastVacuum, &lastAnalyze); err != nil {
			tableNotes = append(tableNotes, table+":error")
			continue
		}
		if !live.Valid {
			continue
		}
		tableNotes = append(tableNotes, fmt.Sprintf("%s:live=%d,dead=%d,vacuum=%s,analyze=%s",
			table, live.Int64, dead.Int64, nullString(lastVacuum), nullString(lastAnalyze)))
	}
	var vacuumWorkers int
	_ = sqlDB.QueryRowContext(snapCtx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND backend_type = 'autovacuum worker'`).Scan(&vacuumWorkers)

	oldestWait := "none:none"
	oldestQuery := ""
	oldestElapsed := 0.0
	if haveOldest {
		oldestWait = nullString(oldest.waitType) + ":" + nullString(oldest.waitEvent)
		oldestQuery = oldest.query
		oldestElapsed = oldest.elapsed
	}
	summary := fmt.Sprintf("diagnosis phase=%s sessions=%d oldest_active_elapsed_s=%.1f oldest_active_wait=%s oldest_active_query=%q blocked=%d autovacuum_workers=%d tables=[%s] blockers=[%s]",
		phase, active, oldestElapsed, oldestWait, oldestQuery, blocked, vacuumWorkers,
		strings.Join(tableNotes, " "), strings.Join(blockers, " | "))
	t.Logf("RETENTION_DIAGNOSIS %s", summary)
	return summary
}

// nullString renders an invalid sql.NullString as an empty marker instead of
// the zero value, so missing wait events stay distinguishable.
func nullString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

// retentionTimeoutReport appends the timeout diagnosis to a prune failure,
// and only then: when the round context is still alive the prune error names
// its own cause and a snapshot would only add noise. The round context in
// runRetentionProbeRound is the test's named bounded wait (3m per round); a
// failure with an expired context gets the pg_stat_activity, lock-blocker,
// and bloat snapshot that names what the prune was stuck on (#8285).
func retentionTimeoutReport(t *testing.T, ctx context.Context, sqlDB *sql.DB, phase string) string {
	t.Helper()
	if ctx.Err() == nil {
		return ""
	}
	return " " + snapshotRetentionTimeoutDiagnosis(t, sqlDB, phase)
}

// TestRetentionLiveTimeoutDiagnosisNamesBlocker plants a blocked session and
// requires the timeout diagnosis to name its pid, its wait, and the blocker,
// then requires a clean database to report no blockers. It is the seeded
// RED/GREEN pair for the prune-timeout diagnostic: without the helper the
// summary is empty and both assertions fail.
func TestRetentionLiveTimeoutDiagnosisNamesBlocker(t *testing.T) {
	sqlDB, ctx := liveDB(t)

	holder, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("open holder conn: %v", err)
	}
	defer func() { _ = holder.Close() }()
	holderTx, err := holder.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder tx: %v", err)
	}
	defer func() { _ = holderTx.Rollback() }()
	if _, err := holderTx.ExecContext(ctx,
		fmt.Sprintf("SELECT pg_advisory_xact_lock(%d)", retentionTimeoutDiagnosisLockKey)); err != nil {
		t.Fatalf("take advisory lock: %v", err)
	}

	blockedDone := make(chan error, 1)
	go func() {
		blocked, err := sqlDB.Conn(context.Background())
		if err != nil {
			blockedDone <- err
			return
		}
		defer func() { _ = blocked.Close() }()
		waitCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, err = blocked.ExecContext(waitCtx,
			fmt.Sprintf("SELECT pg_advisory_xact_lock(%d)", retentionTimeoutDiagnosisLockKey))
		blockedDone <- err
	}()
	waitBlocked(t, ctx, sqlDB)

	summary := snapshotRetentionTimeoutDiagnosis(t, sqlDB, "test-blocker")
	for _, want := range []string{
		"blocked=1",
		"pg_advisory_xact_lock",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("blocked summary %q missing %q", summary, want)
		}
	}

	if err := holderTx.Rollback(); err != nil {
		t.Fatalf("release holder tx: %v", err)
	}
	select {
	case err := <-blockedDone:
		if err != nil {
			t.Fatalf("blocked session after release: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("blocked session did not finish after the holder released")
	}

	clean := snapshotRetentionTimeoutDiagnosis(t, sqlDB, "test-clean")
	if !strings.Contains(clean, "blocked=0") {
		t.Errorf("clean summary %q missing blocked=0", clean)
	}

	// retentionTimeoutReport stays silent while the round is alive and
	// appends the snapshot once the bounded wait expires.
	live, stopLive := context.WithTimeout(context.Background(), time.Minute)
	defer stopLive()
	if got := retentionTimeoutReport(t, live, sqlDB, "test-report"); got != "" {
		t.Errorf("live-context report = %q, want empty", got)
	}
	expired, stopExpired := context.WithCancel(context.Background())
	stopExpired()
	expiredReport := retentionTimeoutReport(t, expired, sqlDB, "test-report")
	for _, want := range []string{"diagnosis phase=test-report", "blocked=0"} {
		if !strings.Contains(expiredReport, want) {
			t.Errorf("expired-context report %q missing %q", expiredReport, want)
		}
	}
}

// waitBlocked polls pg_locks until the parked session holds an ungranted
// advisory lock, so the snapshot below cannot race its block.
func waitBlocked(t *testing.T, ctx context.Context, sqlDB *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var ungranted int
		if err := sqlDB.QueryRowContext(ctx, `
SELECT count(*) FROM pg_locks AS blocked
JOIN pg_stat_activity AS blocked_act ON blocked_act.pid = blocked.pid
WHERE blocked.locktype = 'advisory' AND NOT blocked.granted
  AND blocked_act.datname = current_database()`).Scan(&ungranted); err != nil {
			t.Fatalf("poll pg_locks: %v", err)
		}
		if ungranted > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("parked session never blocked on the advisory lock")
}
