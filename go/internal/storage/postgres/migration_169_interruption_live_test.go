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
	"testing"
	"time"
)

const (
	migration169PauseLockClass = 5318
	migration169PauseLockID    = 169
)

// TestMigration169InterruptedBackfillLive proves that interruption cannot
// record a partial migration or undo the already committed catalog phase.
func TestMigration169InterruptedBackfillLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_MIGRATION169_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_MIGRATION169_TEST_DSN for a disposable loopback PostgreSQL fixture")
	}
	expectedID := os.Getenv("ESHU_TEST_POSTGRES_SYSTEM_IDENTIFIER")
	openCtx, cancelOpen := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelOpen()
	admin, err := openOwnedFixtureDB(openCtx, dsn, func() string { return expectedID }, ownedFixtureMigration169, fixtureSession{})
	if err != nil {
		t.Fatalf("open and validate migration 169 fixture admin: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	definition := migration169Definition(t)
	for _, mode := range []string{"cancel_context", "terminate_backend"} {
		t.Run(mode, func(t *testing.T) {
			testMigration169InterruptedBackfill(t, admin, dsn, expectedID, definition, mode)
		})
	}
}

func testMigration169InterruptedBackfill(
	t *testing.T,
	admin *sql.DB,
	dsn string,
	expectedID string,
	definition Definition,
	mode string,
) {
	t.Helper()
	schema := fmt.Sprintf("eshu_169_interrupt_%s_%d", mode, time.Now().UnixNano())
	setupCtx, setupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer setupCancel()
	if _, err := admin.ExecContext(setupCtx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create migration 169 interruption schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE"); err != nil {
			t.Errorf("drop migration 169 interruption schema: %v", err)
		}
	})

	openScoped := func(application string) *sql.DB {
		t.Helper()
		openCtx, cancelOpen := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelOpen()
		database, err := openOwnedFixtureDB(openCtx, dsn, func() string { return expectedID },
			ownedFixtureMigration169, fixtureSession{searchPath: schema + ",public", applicationName: application})
		if err != nil {
			t.Fatalf("open and validate migration 169 %s pool: %v", application, err)
		}
		database.SetMaxOpenConns(3)
		t.Cleanup(func() { _ = database.Close() })
		return database
	}
	observer := openScoped("eshu169-interrupt-observer-" + mode)
	applyName := "eshu169-interrupt-apply-" + mode
	applier := openScoped(applyName)
	if _, err := observer.ExecContext(setupCtx, `
CREATE TABLE content_files (repo_id text, relative_path text, indexed_at timestamptz, marker text);
CREATE TABLE ingestion_scopes (scope_id text, scope_kind text, source_key text, active_generation_id text);
CREATE TABLE scope_generations (scope_id text, generation_id text, status text, activated_at timestamptz);
INSERT INTO ingestion_scopes VALUES ('scope', 'repository', 'repo', 'generation');
INSERT INTO scope_generations VALUES ('scope', 'generation', 'active', now());
INSERT INTO content_files VALUES ('repo', 'manifest', now() - interval '1 minute', 'original');
CREATE FUNCTION eshu_169_pause() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.relative_path = 'manifest' AND NEW.generation_id = 'generation' THEN
        PERFORM pg_advisory_xact_lock(5318, 169);
    END IF;
    RETURN NEW;
END; $$;
CREATE TRIGGER eshu_169_pause AFTER UPDATE ON content_files
FOR EACH ROW EXECUTE FUNCTION eshu_169_pause();`); err != nil {
		t.Fatalf("seed migration 169 interruption fixture: %v", err)
	}
	holder, err := observer.Conn(setupCtx)
	if err != nil {
		t.Fatalf("acquire pause-lock holder: %v", err)
	}
	t.Cleanup(func() { _ = holder.Close() })
	if _, err := holder.ExecContext(setupCtx, "SELECT pg_advisory_lock($1, $2)", migration169PauseLockClass, migration169PauseLockID); err != nil {
		t.Fatalf("hold migration 169 trigger barrier: %v", err)
	}
	released := false
	release := func() {
		t.Helper()
		if released {
			return
		}
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer releaseCancel()
		var unlocked bool
		if err := holder.QueryRowContext(releaseCtx, "SELECT pg_advisory_unlock($1, $2)",
			migration169PauseLockClass, migration169PauseLockID).Scan(&unlocked); err != nil || !unlocked {
			t.Errorf("release migration 169 trigger barrier: unlocked=%t err=%v", unlocked, err)
		}
		released = true
	}
	t.Cleanup(release)

	gate := newMigration169PhaseGate()
	t.Cleanup(gate.release)
	logger := slog.New(slog.NewTextHandler(gate, nil))
	applyCtx, cancelApply := context.WithCancel(context.Background())
	t.Cleanup(cancelApply)
	applyResult := make(chan error, 1)
	go func() {
		applyResult <- applyBootstrapDefinitionsWith(applyCtx, SQLDB{DB: applier}, []Definition{definition}, logger,
			schemaBootstrapCoordination{ownershipWait: 3 * time.Second, lockRetryBudget: 3 * time.Second})
	}()
	select {
	case <-gate.afterAlter:
	case err := <-applyResult:
		t.Fatalf("migration ended before committed ALTER barrier: %v", err)
	case <-time.After(6 * time.Second):
		t.Fatal("migration did not reach committed ALTER barrier")
	}
	assertMigration169InterruptedState(t, observer, schema, 0, false)
	gate.release()
	backendPID, backendStart := waitForMigration169TriggerBarrier(t, observer, applyName)
	interruptedAt := time.Now()
	if mode == "cancel_context" {
		cancelApply()
	} else {
		terminateCtx, terminateCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer terminateCancel()
		var killed bool
		err := observer.QueryRowContext(terminateCtx, `
SELECT CASE WHEN EXISTS (
    SELECT 1 FROM pg_stat_activity
    WHERE pid = $1 AND backend_start = $2 AND datname = current_database()
      AND application_name = $3 AND wait_event_type = 'Lock'
      AND wait_event = 'advisory' AND query LIKE '%UPDATE content_files AS f%'
) THEN pg_terminate_backend($1) ELSE false END`, backendPID, backendStart, applyName).Scan(&killed)
		if err != nil || !killed {
			t.Fatalf("fenced migration backend termination: killed=%t err=%v", killed, err)
		}
	}
	select {
	case err := <-applyResult:
		if err == nil {
			t.Fatal("interrupted migration unexpectedly succeeded")
		}
		if mode == "cancel_context" && !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled migration error = %v, want context cancellation", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("interrupted migration did not return within eight seconds")
	}
	if elapsed := time.Since(interruptedAt); elapsed > 8*time.Second {
		t.Fatalf("interrupted migration returned after %s, want at most eight seconds", elapsed)
	}
	if gate.countEventPhase("bootstrap.postgres.migration.phase_applied", "analyze") != 0 {
		t.Fatalf("interrupted migration reported completed ANALYZE: %v", gate.linesCopy())
	}
	// All observations after cancellation use a fresh context. The original
	// UPDATE was already in its AFTER trigger, yet none of its row effects may
	// survive the statement rollback.
	assertMigration169InterruptedState(t, observer, schema, 0, false)
	waitForMigration169OwnerCleanup(t, observer, backendPID, backendStart, applyName, mode == "terminate_backend")
	release()

	retry := openScoped("eshu169-interrupt-retry-" + mode)
	retryCtx, retryCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer retryCancel()
	if err := applyBootstrapDefinitionsWith(retryCtx, SQLDB{DB: retry}, []Definition{definition}, slog.Default(),
		schemaBootstrapCoordination{ownershipWait: 3 * time.Second, lockRetryBudget: 3 * time.Second}); err != nil {
		t.Fatalf("fresh owner retry unchanged migration 169: %v", err)
	}
	assertMigration169InterruptedState(t, observer, schema, 1, true)
	if _, err := observer.ExecContext(retryCtx, `
CREATE FUNCTION eshu_169_replay_fail() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'completed migration replayed UPDATE'; END; $$;
CREATE TRIGGER eshu_169_replay_fail BEFORE UPDATE ON content_files
FOR EACH ROW EXECUTE FUNCTION eshu_169_replay_fail();`); err != nil {
		t.Fatalf("install completed-migration replay guard: %v", err)
	}
	if err := applyBootstrapDefinitionsWith(retryCtx, SQLDB{DB: retry}, []Definition{definition}, slog.Default(),
		schemaBootstrapCoordination{ownershipWait: 3 * time.Second, lockRetryBudget: 3 * time.Second}); err != nil {
		t.Fatalf("completed migration did not skip unchanged replay: %v", err)
	}
	assertMigration169InterruptedState(t, observer, schema, 1, true)
}

func waitForMigration169TriggerBarrier(t *testing.T, database *sql.DB, application string) (int, time.Time) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	for {
		var pid int
		var started time.Time
		err := database.QueryRowContext(ctx, `
SELECT pid, backend_start FROM pg_stat_activity
WHERE datname = current_database() AND application_name = $1
  AND state = 'active' AND wait_event_type = 'Lock'
  AND wait_event = 'advisory' AND query LIKE '%UPDATE content_files AS f%'
ORDER BY pid LIMIT 1`, application).Scan(&pid, &started)
		if err == nil {
			return pid, started
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("inspect migration 169 trigger barrier: %v", err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("migration 169 UPDATE never reached advisory trigger barrier")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func waitForMigration169OwnerCleanup(
	t *testing.T, database *sql.DB, pid int, started time.Time, application string, requireExit bool,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	for {
		var active, locks int
		err := database.QueryRowContext(ctx, `
SELECT count(*) FILTER (WHERE a.state = 'active'),
       count(*) FILTER (WHERE l.locktype = 'advisory' AND l.classid = $4
                              AND l.objid = $5 AND l.granted)
FROM pg_stat_activity AS a
LEFT JOIN pg_locks AS l ON l.pid = a.pid
WHERE a.pid = $1 AND a.backend_start = $2 AND a.application_name = $3`,
			pid, started, application, schemaBootstrapAdvisoryLockClass, schemaBootstrapAdvisoryLockID,
		).Scan(&active, &locks)
		if err != nil {
			t.Fatalf("inspect interrupted migration ownership: %v", err)
		}
		if active == 0 && locks == 0 {
			if !requireExit {
				return
			}
			var present bool
			if err := database.QueryRowContext(ctx,
				"SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid = $1 AND backend_start = $2 AND application_name = $3)",
				pid, started, application).Scan(&present); err != nil {
				t.Fatalf("inspect terminated migration backend: %v", err)
			}
			if !present {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("interrupted migration owner still active or locked: active=%d locks=%d", active, locks)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func assertMigration169InterruptedState(t *testing.T, database *sql.DB, schema string, receipts int, tagged bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var column bool
	if err := database.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM information_schema.columns
WHERE table_schema = $1 AND table_name = 'content_files' AND column_name = 'generation_id')`, schema).Scan(&column); err != nil || !column {
		t.Fatalf("migration 169 committed column exists=%t err=%v, want true", column, err)
	}
	var count, matching int
	if err := database.QueryRowContext(ctx, `
SELECT count(*), count(*) FILTER (WHERE checksum_sha256 = $2)
FROM eshu_schema_migrations WHERE path = $1 AND variant = 'full'`,
		contentGenerationMigrationPath, contentGenerationMigrationSHA).Scan(&count, &matching); err != nil || count != receipts || matching != receipts {
		t.Fatalf("migration 169 receipts=%d matching=%d err=%v, want %d", count, matching, err, receipts)
	}
	var repo, path, marker string
	var tag sql.NullString
	if err := database.QueryRowContext(ctx, `
SELECT repo_id, relative_path, marker, generation_id FROM content_files`).Scan(&repo, &path, &marker, &tag); err != nil {
		t.Fatalf("read migration 169 source row: %v", err)
	}
	if repo != "repo" || path != "manifest" || marker != "original" || tag.Valid != tagged || (tagged && tag.String != "generation") {
		t.Fatalf("migration 169 source row=(%q,%q,%q,%v), want original row and tagged=%t", repo, path, marker, tag, tagged)
	}
	var rowCount int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM content_files").Scan(&rowCount); err != nil || rowCount != 1 {
		t.Fatalf("migration 169 source rows=%d err=%v, want 1", rowCount, err)
	}
}
