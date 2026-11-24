// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// mustStandbyHarnessConnect opens a pgx session or fails the test.
func mustStandbyHarnessConnect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect fixture: %v", err)
	}
	return conn
}

// seedStandbyHarnessProbe creates one small probe table plus a four-row scope
// census beside it. Batches read one scope at a time (the per-ecosystem
// chunking analog); nothing in the harness reads fact_records.
func seedStandbyHarnessProbe(t *testing.T, ctx context.Context, writerDSN string) (table, censusTable string, scopes []StagedAuditCensusItem) {
	t.Helper()
	uid := fmt.Sprintf("%d%d", os.Getpid(), time.Now().UnixNano()%100000)
	table = "eshu_11353_probe_" + uid
	censusTable = "eshu_11353_scopes_" + uid
	quoted, quotedCensus := pgx.Identifier{table}.Sanitize(), pgx.Identifier{censusTable}.Sanitize()
	primary := mustStandbyHarnessConnect(t, ctx, writerDSN)
	defer func() { _ = primary.Close(context.Background()) }()
	if _, err := primary.Exec(ctx, "CREATE TABLE "+quoted+
		" (scope_id text NOT NULL, gen text NOT NULL, payload text NOT NULL)"); err != nil {
		t.Fatalf("create probe table: %v", err)
	}
	if _, err := primary.Exec(ctx, "CREATE TABLE "+quotedCensus+
		" (scope_id text PRIMARY KEY, generation_id text NOT NULL)"); err != nil {
		t.Fatalf("create scope census table: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cleanup, err := pgx.Connect(dropCtx, writerDSN)
		if err != nil {
			t.Errorf("cleanup connect: %v", err)
			return
		}
		defer func() { _ = cleanup.Close(dropCtx) }()
		if _, err := cleanup.Exec(dropCtx, "DROP TABLE IF EXISTS "+quoted); err != nil {
			t.Errorf("drop probe table: %v", err)
		}
		if _, err := cleanup.Exec(dropCtx, "DROP TABLE IF EXISTS "+quotedCensus); err != nil {
			t.Errorf("drop census table: %v", err)
		}
	})
	for _, scope := range []string{"eco-1", "eco-2", "eco-3", "eco-4"} {
		if _, err := primary.Exec(ctx, "INSERT INTO "+quotedCensus+" (scope_id, generation_id) VALUES ($1, 'g1')", scope); err != nil {
			t.Fatalf("seed census: %v", err)
		}
		if _, err := primary.Exec(ctx, "INSERT INTO "+quoted+" (scope_id, gen, payload) SELECT $1, 'g1', repeat('p', 200) FROM generate_series(1, $2)",
			scope, standbyHarnessRowsPerScope); err != nil {
			t.Fatalf("seed probe scope %s: %v", scope, err)
		}
		scopes = append(scopes, StagedAuditCensusItem{ScopeID: scope, GenerationID: "g1"})
	}
	return table, censusTable, scopes
}

// waitStandbyHarnessReplay blocks until the standby has replayed the seed.
func waitStandbyHarnessReplay(t *testing.T, ctx context.Context, readerDSN, table string, want int) {
	t.Helper()
	quoted := pgx.Identifier{table}.Sanitize()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := pgx.Connect(ctx, readerDSN)
		if err == nil {
			var got int
			if qerr := conn.QueryRow(ctx, "SELECT count(*) FROM "+quoted).Scan(&got); qerr == nil && got == want {
				_ = conn.Close(ctx)
				return
			}
			_ = conn.Close(ctx)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("standby never replayed the seed: %v", ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
	t.Fatalf("standby never replayed %d seed rows", want)
}

// standbyHarnessSnapshotConflicts reads the standby's confl_snapshot counter.
func standbyHarnessSnapshotConflicts(t *testing.T, ctx context.Context, readerDSN string) int64 {
	t.Helper()
	conn := mustStandbyHarnessConnect(t, ctx, readerDSN)
	defer func() { _ = conn.Close(context.Background()) }()
	var snapshot, deadlock, tablespace, lock, bufferpin int64
	if err := conn.QueryRow(ctx, `SELECT confl_snapshot, confl_deadlock, confl_tablespace, confl_lock, confl_bufferpin
		FROM pg_stat_database_conflicts WHERE datname = current_database()`).
		Scan(&snapshot, &deadlock, &tablespace, &lock, &bufferpin); err != nil {
		t.Fatalf("read conflict counters: %v", err)
	}
	t.Logf("conflict counters: snapshot=%d deadlock=%d tablespace=%d lock=%d bufferpin=%d",
		snapshot, deadlock, tablespace, lock, bufferpin)
	return snapshot
}

// standbyHarnessReplayLag reports how far the standby trails the primary.
func standbyHarnessReplayLag(ctx context.Context, readerDSN string) time.Duration {
	conn, err := pgx.Connect(ctx, readerDSN)
	if err != nil {
		return -1
	}
	defer func() { _ = conn.Close(context.Background()) }()
	var lag sql.NullFloat64
	if err := conn.QueryRow(ctx, "SELECT EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp()))").
		Scan(&lag); err != nil || !lag.Valid {
		return -1
	}
	return time.Duration(lag.Float64 * float64(time.Second))
}

// raceStandbyHarnessSnapshot holds one long per-scope snapshot read on the
// standby while the primary churns that scope, and returns the read outcome:
// nil when the read survived (no conflict reproduced).
func raceStandbyHarnessSnapshot(t *testing.T, ctx context.Context, topology *standbyHarnessTopology, table, scope string) error {
	t.Helper()
	quoted := pgx.Identifier{table}.Sanitize()
	longRead := "SELECT count(*) FROM (SELECT payload, pg_sleep(0.004) FROM " + quoted +
		" WHERE scope_id=$1 LIMIT 20000) s"
	done := make(chan error, 1)
	go func() {
		conn, err := pgx.Connect(context.Background(), topology.readerDSN)
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = conn.Close(context.Background()) }()
		readCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		var n int64
		done <- conn.QueryRow(readCtx, longRead, scope).Scan(&n)
	}()
	// Let the scan pin its snapshot before the first replay wave arrives.
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
	}
	for range 4 {
		churnStandbyHarnessScope(ctx, topology.writerDSN, table, scope, 1)
		select {
		case err := <-done:
			return err
		case <-time.After(time.Second):
		}
	}
	return <-done
}

// churnStandbyHarnessScope rewrites every row of one scope and vacuums the
// table so replay on the standby must remove versions a pinned snapshot may
// still need. Each statement is its own implicit transaction.
func churnStandbyHarnessScope(ctx context.Context, writerDSN, table, scope string, rounds int) {
	conn, err := pgx.Connect(ctx, writerDSN)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close(context.Background()) }()
	quoted := pgx.Identifier{table}.Sanitize()
	for i := range rounds {
		if ctx.Err() != nil {
			return
		}
		tag := fmt.Sprintf("churn-%d-%d", time.Now().UnixNano(), i)
		if _, err := conn.Exec(ctx, "UPDATE "+quoted+" SET payload = md5(payload || $1) WHERE scope_id = $2", tag, scope); err != nil {
			return
		}
		if _, err := conn.Exec(ctx, "VACUUM "+quoted); err != nil {
			return
		}
		_, _ = conn.Exec(ctx, "CHECKPOINT")
	}
}

// standbyHarnessTickPlan wires the probe into the staged-audit runner: the
// census enumerates the four scope rows and each batch reads one scope with
// a row bound, so no statement scans the whole table.
func standbyHarnessTickPlan(table, censusTable string, scopes []StagedAuditCensusItem, conflictScope string, batchStarted chan<- string, onVerify func()) StagedAuditTickPlan {
	quoted, quotedCensus := pgx.Identifier{table}.Sanitize(), pgx.Identifier{censusTable}.Sanitize()
	longRead := "SELECT payload FROM (SELECT payload, pg_sleep(0.004) FROM " + quoted +
		" WHERE scope_id=$1 LIMIT 20000) s"
	fastRead := "SELECT payload FROM " + quoted + " WHERE scope_id=$1 LIMIT 20000"
	return StagedAuditTickPlan{
		Census: func(ctx context.Context, reader db.Queryer) ([]StagedAuditCensusItem, error) {
			rows, err := reader.QueryContext(ctx, "SELECT scope_id, generation_id FROM "+quotedCensus+" ORDER BY scope_id")
			if err != nil {
				return nil, err
			}
			defer func() { _ = rows.Close() }()
			var items []StagedAuditCensusItem
			for rows.Next() {
				var item StagedAuditCensusItem
				if err := rows.Scan(&item.ScopeID, &item.GenerationID); err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			return items, rows.Err()
		},
		FetchBatch: func(ctx context.Context, reader db.Queryer, item StagedAuditCensusItem) (StagedAuditBatchResult, error) {
			query := fastRead
			if item.ScopeID == conflictScope {
				query = longRead
				select {
				case batchStarted <- item.ScopeID:
				default:
				}
			}
			rows, err := reader.QueryContext(ctx, query, item.ScopeID)
			if err != nil {
				return StagedAuditBatchResult{}, err
			}
			defer func() { _ = rows.Close() }()
			var result StagedAuditBatchResult
			for rows.Next() {
				var payload string
				if err := rows.Scan(&payload); err != nil {
					return StagedAuditBatchResult{}, err
				}
				result.Facts++
				result.DistinctKeys++
			}
			return result, rows.Err()
		},
		FetchStoredWinners: func(ctx context.Context, reader db.Queryer) (int, error) {
			rows, err := reader.QueryContext(ctx, "SELECT count(*) FROM "+quoted+" WHERE scope_id=$1 LIMIT 20000", scopes[len(scopes)-1].ScopeID)
			if err != nil {
				return 0, err
			}
			defer func() { _ = rows.Close() }()
			var n int
			if rows.Next() {
				if err := rows.Scan(&n); err != nil {
					return 0, err
				}
			}
			return n, rows.Err()
		},
		Verify: func(context.Context) (int64, error) {
			onVerify()
			return 0, nil
		},
	}
}

// standbyHarnessFleet is a StagedAuditFleet backed by live standby
// connections: one member, one short-lived connection per statement, so no
// cross-batch transaction pins a standby snapshot.
type standbyHarnessFleet struct {
	readerDSN  string
	member     string
	statements []string
}

// PinMember pins the single standby member for the tick.
func (f *standbyHarnessFleet) PinMember(context.Context) (string, error) {
	return f.member, nil
}

// ReplayLag reports the standby replay lag behind the primary.
func (f *standbyHarnessFleet) ReplayLag(ctx context.Context, _ string) (time.Duration, error) {
	if lag := standbyHarnessReplayLag(ctx, f.readerDSN); lag >= 0 {
		return lag, nil
	}
	return 0, nil
}

// RunOnMember runs one statement on a fresh standby connection.
func (f *standbyHarnessFleet) RunOnMember(ctx context.Context, member string, fn func(context.Context, db.Queryer) error) error {
	if member != f.member {
		return fmt.Errorf("standby harness fleet has no member %q", member)
	}
	conn, err := pgx.Connect(ctx, f.readerDSN)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()
	return fn(ctx, &standbyHarnessQueryer{conn: conn, fleet: f})
}

// standbyHarnessQueryer adapts a pgx connection to the runner's db.Queryer
// and records every statement for the bounded-scan assertion.
type standbyHarnessQueryer struct {
	conn  *pgx.Conn
	fleet *standbyHarnessFleet
}

// QueryContext runs one read and records its SQL.
func (q *standbyHarnessQueryer) QueryContext(ctx context.Context, sql string, args ...any) (db.Rows, error) {
	q.fleet.statements = append(q.fleet.statements, sql)
	rows, err := q.conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return standbyHarnessRows{rows: rows}, nil
}

// standbyHarnessRows adapts pgx.Rows to db.Rows.
type standbyHarnessRows struct {
	rows pgx.Rows
}

// Next advances the cursor.
func (r standbyHarnessRows) Next() bool { return r.rows.Next() }

// Scan reads the current row.
func (r standbyHarnessRows) Scan(dest ...any) error { return r.rows.Scan(dest...) }

// Err reports the iteration error.
func (r standbyHarnessRows) Err() error { return r.rows.Err() }

// Close releases the rows.
func (r standbyHarnessRows) Close() error { r.rows.Close(); return r.rows.Err() }

// isStagedAuditDeferredAs reports whether err aborts the tick with reason.
func isStagedAuditDeferredAs(err error, reason StagedAuditDeferralReason, out **StagedAuditDeferredError) bool {
	if err == nil {
		return false
	}
	var deferred *StagedAuditDeferredError
	if !errors.As(err, &deferred) || deferred.Reason != reason {
		return false
	}
	*out = deferred
	return true
}

// assertStandbyHarnessBounded proves the tick read per-scope chunks: every
// recorded statement names a scope predicate, carries a row bound or reads
// the four-row census, and none touches fact_records.
func assertStandbyHarnessBounded(t *testing.T, statements []string) {
	t.Helper()
	if len(statements) == 0 {
		t.Fatal("no statements were recorded on the standby fleet")
	}
	for _, stmt := range statements {
		lowered := strings.ToLower(stmt)
		if strings.Contains(lowered, "fact_records") {
			t.Fatalf("harness scanned fact_records: %q", stmt)
		}
		if !strings.Contains(lowered, "scope_id") {
			t.Fatalf("unscoped standby statement: %q", stmt)
		}
		if strings.Contains(lowered, "from") && !strings.Contains(lowered, "limit") &&
			!strings.Contains(lowered, "eshu_11353_scopes_") {
			t.Fatalf("unbounded standby statement: %q", stmt)
		}
	}
	t.Logf("bounded-statement guard: %d standby statements, all scoped and row-bound", len(statements))
}
