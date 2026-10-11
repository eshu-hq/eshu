// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestBootstrapInterruptedDefinitionRollsBackLive exercises the real tracked
// migration and advisory-lock path on an isolated, disposable PostgreSQL 18
// database. It intentionally makes no DDL-plus-receipt atomicity claim: the
// interruption occurs before the migration SQL completes.
func TestBootstrapInterruptedDefinitionRollsBackLive(t *testing.T) {
	dsn := os.Getenv("ESHU_7033_ROLLBACK_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_7033_ROLLBACK_TEST_DSN for an owned loopback PostgreSQL 18 fixture")
	}
	if err := assertLoopbackRollbackDSN(dsn); err != nil {
		t.Fatal(err)
	}
	expectedID := os.Getenv("ESHU_TEST_POSTGRES_SYSTEM_IDENTIFIER")
	for _, mode := range []string{"terminate_backend", "cancel_context"} {
		t.Run(mode, func(t *testing.T) {
			bootstrapInterruptedDefinitionCase(t, dsn, expectedID, mode)
		})
	}
}

func bootstrapInterruptedDefinitionCase(t *testing.T, dsn, expectedID, mode string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	runner, err := openOwnedFixtureDB(ctx, dsn, func() string { return expectedID }, ownedFixtureRollback, fixtureSession{})
	if err != nil {
		t.Fatalf("open and validate fixture runner: %v", err)
	}
	runner.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = runner.Close() })
	observer, err := openOwnedFixtureDB(ctx, dsn, func() string { return expectedID }, ownedFixtureRollback, fixtureSession{})
	if err != nil {
		t.Fatalf("open and validate fixture observer: %v", err)
	}
	t.Cleanup(func() { _ = observer.Close() })
	if err := assertOwnedRollbackFixture(ctx, runner); err != nil {
		t.Fatal(err)
	}
	if err := assertOwnedRollbackFixture(ctx, observer); err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	preTable := "eshu_7033_pre_" + suffix
	partialTable := "eshu_7033_partial_" + suffix
	prePath := "test/7033_pre_" + suffix + ".sql"
	partialPath := "test/7033_partial_" + suffix + ".sql"
	pre := Definition{
		Name: "completed_before_interruption",
		Path: prePath,
		SQL:  fmt.Sprintf("CREATE TABLE %s (id INT PRIMARY KEY); INSERT INTO %s (id) VALUES (7)", preTable, preTable),
	}
	partial := Definition{
		Name: "interrupted_multistatement",
		Path: partialPath,
		SQL: fmt.Sprintf("CREATE TABLE %s (id INT PRIMARY KEY); "+
			"INSERT INTO %s (id) VALUES (11); SELECT pg_sleep(30); "+
			"ALTER TABLE %s ADD COLUMN completed BOOLEAN", partialTable, partialTable, partialTable),
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	apply := func(applyCtx context.Context, defs []Definition) error {
		return applyBootstrapDefinitionsWith(applyCtx, SQLDB{DB: runner}, defs, logger, schemaBootstrapCoordination{})
	}
	if err := apply(ctx, []Definition{pre}); err != nil {
		t.Fatalf("apply preceding migration: %v", err)
	}
	assertRollbackTableAndReceipt(t, ctx, observer, preTable, prePath, 7, true)

	applicationName := "eshu_7033_rollback_" + mode + "_" + suffix
	identity := captureRollbackBackend(t, ctx, runner, applicationName)
	applyCtx, stopApply := context.WithCancel(ctx)
	defer stopApply()
	result := make(chan error, 1)
	go func() { result <- apply(applyCtx, []Definition{partial}) }()
	waitForRollbackSleep(t, ctx, observer, identity, partialTable)
	started := time.Now()
	switch mode {
	case "terminate_backend":
		terminateOwnedRollbackBackend(t, ctx, observer, identity, partialTable)
	case "cancel_context":
		stopApply()
	default:
		t.Fatalf("unsupported interruption mode %q", mode)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("interrupted migration unexpectedly succeeded")
		}
		message := strings.ReplaceAll(err.Error(), dsn, "[owned-fixture-dsn]")
		if len(message) > 512 {
			message = message[:512] + "[truncated]"
		}
		t.Logf("interrupted migration returned: %s", message)
		if mode == "cancel_context" && !errors.Is(err, context.Canceled) {
			t.Log("cancellation returned an error other than context.Canceled")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("interrupted migration did not return within 8 seconds")
	}
	if elapsed := time.Since(started); elapsed > 8*time.Second {
		t.Fatalf("interruption returned after %s, want at most 8 seconds", elapsed)
	}
	waitForRollbackCleanup(t, ctx, observer, identity)
	assertRollbackTableAndReceipt(t, ctx, observer, preTable, prePath, 7, true)
	assertRollbackTableAndReceipt(t, ctx, observer, partialTable, partialPath, 11, false)
	assertFreshRollbackOwnership(t, ctx, observer)

	// Re-run the exact SQL, including its sleep, so the migration checksum and
	// recorded receipt are the same as for the interrupted attempt.
	fullRetryCtx, fullRetryCancel := context.WithTimeout(ctx, 45*time.Second)
	defer fullRetryCancel()
	if err := apply(fullRetryCtx, []Definition{partial}); err != nil {
		t.Fatalf("retry exact interrupted migration: %v", err)
	}
	assertRollbackTableAndReceipt(t, ctx, observer, partialTable, partialPath, 11, true)
	if err := apply(ctx, []Definition{partial}); err != nil {
		t.Fatalf("idempotent reapply: %v", err)
	}
	assertRollbackTableAndReceipt(t, ctx, observer, partialTable, partialPath, 11, true)
}

func assertLoopbackRollbackDSN(dsn string) error {
	_, err := parseOwnedFixtureEndpoint(dsn, ownedFixtureRollback)
	return err
}

func TestRollbackFixtureDSNGuard(t *testing.T) {
	good := "postgres://postgres@127.0.0.1:15432/eshu_7033_rollback?sslmode=disable"
	if err := assertLoopbackRollbackDSN(good); err != nil {
		t.Fatalf("valid owned loopback DSN rejected: %v", err)
	}
	for _, bad := range []string{
		"postgres://postgres@10.0.0.1:15432/eshu_7033_rollback?sslmode=disable",
		"postgres://postgres@127.0.0.1:15432/eshu?sslmode=disable",
		"postgres://eshu@127.0.0.1:15432/eshu_7033_rollback?sslmode=disable",
		"postgres://postgres@127.0.0.1:15432/eshu_7033_rollback?sslmode=disable&host=10.0.0.1",
		"postgres://postgres@127.0.0.1:15432/eshu_7033_rollback?sslmode=require",
		"postgres://postgres@127.0.0.1:0/eshu_7033_rollback?sslmode=disable",
	} {
		if err := assertLoopbackRollbackDSN(bad); err == nil {
			t.Errorf("non-owned DSN accepted: %s", bad)
		}
	}
}

func assertOwnedRollbackFixture(ctx context.Context, db *sql.DB) error {
	var name string
	var version int
	if err := db.QueryRowContext(ctx, `SELECT current_database(),
current_setting('server_version_num')::int`).Scan(&name, &version); err != nil {
		return fmt.Errorf("inspect rollback fixture: %w", err)
	}
	if name != rollbackTestDatabase || version/10000 != 18 {
		return fmt.Errorf("refuse non-owned fixture: database=%q major=%d", name, version/10000)
	}
	return nil
}

type rollbackBackendIdentity struct {
	pid             int
	applicationName string
	startedAt       time.Time
}

func captureRollbackBackend(t *testing.T, ctx context.Context, db *sql.DB, applicationName string) rollbackBackendIdentity {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("reserve rollback backend: %v", err)
	}
	defer func() { _ = conn.Close() }()
	var identity rollbackBackendIdentity
	identity.applicationName = applicationName
	if err := conn.QueryRowContext(ctx, `SELECT set_config('application_name', $1, false)`, applicationName).Scan(new(string)); err != nil {
		t.Fatalf("label rollback backend: %v", err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT pid, backend_start FROM pg_stat_activity WHERE pid = pg_backend_pid()`).Scan(
		&identity.pid, &identity.startedAt,
	); err != nil {
		t.Fatalf("capture rollback backend identity: %v", err)
	}
	return identity
}

func waitForRollbackSleep(t *testing.T, ctx context.Context, observer *sql.DB, identity rollbackBackendIdentity, table string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var active bool
		err := observer.QueryRowContext(ctx, `SELECT EXISTS (
SELECT 1 FROM pg_stat_activity
WHERE pid = $1 AND datname = $2 AND application_name = $3 AND backend_start = $4
  AND state = 'active' AND wait_event = 'PgSleep' AND query LIKE $5
)`, identity.pid, rollbackTestDatabase, identity.applicationName, identity.startedAt,
			"%"+table+"%pg_sleep(30)%").Scan(&active)
		if err != nil {
			t.Fatalf("observe fenced pg_sleep: %v", err)
		}
		if active {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for fenced pg_sleep: %v", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatal("owned backend never reached PgSleep within 8 seconds")
}

func terminateOwnedRollbackBackend(t *testing.T, ctx context.Context, observer *sql.DB, identity rollbackBackendIdentity, table string) {
	t.Helper()
	var terminated bool
	err := observer.QueryRowContext(ctx, `SELECT pg_terminate_backend(pid, 5000)
FROM pg_stat_activity
WHERE pid = $1 AND datname = $2 AND application_name = $3 AND backend_start = $4
  AND state = 'active' AND wait_event = 'PgSleep' AND query LIKE $5`,
		identity.pid, rollbackTestDatabase, identity.applicationName, identity.startedAt,
		"%"+table+"%pg_sleep(30)%").Scan(&terminated)
	if err != nil || !terminated {
		t.Fatalf("terminate only fenced rollback backend: terminated=%t err=%v", terminated, err)
	}
	var sameBackend int
	if err := observer.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity
WHERE pid = $1 AND datname = $2 AND application_name = $3 AND backend_start = $4`,
		identity.pid, rollbackTestDatabase, identity.applicationName, identity.startedAt,
	).Scan(&sameBackend); err != nil || sameBackend != 0 {
		t.Fatalf("terminated backend still present: count=%d err=%v", sameBackend, err)
	}
}

func assertRollbackTableAndReceipt(t *testing.T, ctx context.Context, db *sql.DB, table, path string, sentinel int, want bool) {
	t.Helper()
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil || exists != want {
		t.Fatalf("table %s exists=%t err=%v, want %t", table, exists, err, want)
	}
	var receipts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM eshu_schema_migrations WHERE path = $1 AND variant = 'full'`,
		path).Scan(&receipts); err != nil || receipts != map[bool]int{false: 0, true: 1}[want] {
		t.Fatalf("receipt %s count=%d err=%v, want table=%t", path, receipts, err, want)
	}
	if !want {
		return // an absent table cannot retain its inserted sentinel row
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE id = $1`, sentinel).Scan(&count); err != nil || count != 1 {
		t.Fatalf("sentinel %s count=%d err=%v, want 1", table, count, err)
	}
}

func waitForRollbackCleanup(t *testing.T, ctx context.Context, db *sql.DB, identity rollbackBackendIdentity) {
	t.Helper()
	started := time.Now()
	deadline := started.Add(20 * time.Second)
	lastHolder := "none"
	lastActivity := "none"
	for time.Now().Before(deadline) {
		rows, err := db.QueryContext(ctx, `SELECT a.pid, a.application_name, a.backend_start,
  a.state, a.xact_start, a.wait_event
FROM pg_locks l
JOIN pg_stat_activity a ON a.pid = l.pid
WHERE l.locktype = 'advisory' AND l.classid = $1 AND l.objid = $2 AND l.granted
  AND l.database = (SELECT oid FROM pg_database WHERE datname = $3)`,
			schemaBootstrapAdvisoryLockClass, schemaBootstrapAdvisoryLockID, rollbackTestDatabase)
		if err != nil {
			t.Fatalf("inspect schema advisory holder: %v", err)
		}
		held := false
		lastHolder = "none"
		for rows.Next() {
			var pid int
			var applicationName, state string
			var backendStart time.Time
			var xactStart sql.NullTime
			var waitEvent sql.NullString
			if err := rows.Scan(&pid, &applicationName, &backendStart, &state, &xactStart, &waitEvent); err != nil {
				_ = rows.Close()
				t.Fatalf("scan schema advisory holder: %v", err)
			}
			lastHolder = fmt.Sprintf("pid=%d app=%s started=%s state=%s xact=%t wait=%s",
				pid, applicationName, backendStart.UTC().Format(time.RFC3339Nano), state,
				xactStart.Valid, waitEvent.String)
			if pid != identity.pid || applicationName != identity.applicationName || !backendStart.Equal(identity.startedAt) {
				_ = rows.Close()
				t.Fatalf("unexpected schema advisory holder: %s", lastHolder)
			}
			held = true
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("read schema advisory holders: %v", err)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("close schema advisory holder rows: %v", err)
		}

		var state string
		var xactStart sql.NullTime
		err = db.QueryRowContext(ctx, `SELECT state, xact_start FROM pg_stat_activity
WHERE pid = $1 AND datname = $2 AND application_name = $3 AND backend_start = $4`,
			identity.pid, rollbackTestDatabase, identity.applicationName, identity.startedAt).Scan(&state, &xactStart)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("inspect interrupted backend transaction: %v", err)
		}
		active := err == nil && (state == "active" || xactStart.Valid)
		lastActivity = fmt.Sprintf("present=%t state=%s xact=%t", err == nil, state, xactStart.Valid)
		if !held && !active {
			t.Logf("rollback cleanup complete after %s", time.Since(started))
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for rollback cleanup: %v; holder=%s activity=%s", ctx.Err(), lastHolder, lastActivity)
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatalf("rollback cleanup exceeded 20 seconds: holder=%s activity=%s elapsed=%s",
		lastHolder, lastActivity, time.Since(started))
}

func assertFreshRollbackOwnership(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open fresh owner connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	var owned bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1, $2)`,
		schemaBootstrapAdvisoryLockClass, schemaBootstrapAdvisoryLockID).Scan(&owned); err != nil || !owned {
		t.Fatalf("fresh ownership acquired=%t err=%v", owned, err)
	}
	var released bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_advisory_unlock($1, $2)`,
		schemaBootstrapAdvisoryLockClass, schemaBootstrapAdvisoryLockID).Scan(&released); err != nil || !released {
		t.Fatalf("release fresh ownership=%t err=%v", released, err)
	}
}
