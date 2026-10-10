// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestMigration169PhasedTrackedLive checks the actual tracked bootstrap route
// on an empty, explicitly named disposable PostgreSQL database. It does not
// accept an ops-qa or other remotely hosted DSN.
func TestMigration169PhasedTrackedLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_MIGRATION169_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_MIGRATION169_TEST_DSN for a disposable loopback PostgreSQL fixture")
	}
	address, err := url.Parse(dsn)
	if err != nil || address.Hostname() != "127.0.0.1" || address.Path != "/eshu169fixture" {
		t.Fatalf("migration 169 fixture requires loopback host and eshu169fixture database")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open migration 169 fixture: %v", err)
	}
	database.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := database.ExecContext(ctx, `
CREATE TABLE content_files (repo_id text, relative_path text, indexed_at timestamptz, generation_id text);
ALTER TABLE content_files DROP COLUMN generation_id;
CREATE TABLE ingestion_scopes (scope_id text, scope_kind text, source_key text, active_generation_id text);
CREATE TABLE scope_generations (scope_id text, generation_id text, status text, activated_at timestamptz);
INSERT INTO ingestion_scopes VALUES ('scope', 'repository', 'repo', 'generation');
INSERT INTO scope_generations VALUES ('scope', 'generation', 'active', now());
INSERT INTO content_files VALUES ('repo', 'manifest', now() - interval '1 minute');`); err != nil {
		t.Fatalf("seed migration 169 fixture: %v", err)
	}
	def := migration169Definition(t)
	apply := func(runCtx context.Context) error {
		return applyBootstrapDefinitionsWith(runCtx, SQLDB{DB: database}, []Definition{def}, slog.Default(), schemaBootstrapCoordination{
			ownershipWait: 3 * time.Second, lockRetryBudget: 3 * time.Second,
		})
	}

	// A failed UPDATE must not roll back the already committed catalog change.
	if _, err := database.ExecContext(ctx, `
CREATE FUNCTION eshu_169_fail() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'injected backfill interruption'; END; $$;
CREATE TRIGGER eshu_169_fail BEFORE UPDATE ON content_files
FOR EACH ROW EXECUTE FUNCTION eshu_169_fail();`); err != nil {
		t.Fatalf("install backfill fault: %v", err)
	}
	if err := apply(ctx); err == nil || !strings.Contains(err.Error(), "injected backfill interruption") {
		t.Fatalf("faulted migration error = %v, want injected backfill error", err)
	}
	assertMigration169ColumnAndReceipt(t, ctx, database, true, 0)
	if _, err := database.ExecContext(ctx, "DROP TRIGGER eshu_169_fail ON content_files; DROP FUNCTION eshu_169_fail()"); err != nil {
		t.Fatalf("remove backfill fault: %v", err)
	}
	if err := apply(ctx); err != nil {
		t.Fatalf("retry interrupted migration: %v", err)
	}
	assertMigration169ColumnAndReceipt(t, ctx, database, true, 1)
	var tag sql.NullString
	if err := database.QueryRowContext(ctx, "SELECT generation_id FROM content_files WHERE relative_path = 'manifest'").Scan(&tag); err != nil || tag.String != "generation" {
		t.Fatalf("retried backfill tag = %v, err=%v, want generation", tag, err)
	}

	// A recorded full variant skips even while a reader holds ACCESS SHARE.
	reader, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("start reader: %v", err)
	}
	var rows int
	if err := reader.QueryRowContext(ctx, "SELECT count(*) FROM content_files").Scan(&rows); err != nil {
		_ = reader.Rollback()
		t.Fatalf("hold reader lock: %v", err)
	}
	replayCtx, replayCancel := context.WithTimeout(ctx, time.Second)
	err = apply(replayCtx)
	replayCancel()
	_ = reader.Rollback()
	if err != nil {
		t.Fatalf("completed receipt replay while reader active: %v", err)
	}
	assertMigration169ColumnAndReceipt(t, ctx, database, true, 1)

	// Re-arm the migration. A slow row trigger keeps the UPDATE in flight so
	// a read and a legacy NULL-tag writer can prove they are not held behind
	// the ALTER's ACCESS EXCLUSIVE lock.
	if _, err := database.ExecContext(ctx,
		"DELETE FROM eshu_schema_migrations WHERE path = $1 AND variant = 'full'", def.Path,
	); err != nil {
		t.Fatalf("remove receipt for overlap replay: %v", err)
	}
	if _, err := database.ExecContext(ctx, "ALTER TABLE content_files DROP COLUMN generation_id"); err != nil {
		t.Fatalf("remove generation tag for overlap replay: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
CREATE FUNCTION eshu_169_delay() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM pg_sleep(3); RETURN NEW; END; $$;
CREATE TRIGGER eshu_169_delay BEFORE UPDATE ON content_files
FOR EACH ROW EXECUTE FUNCTION eshu_169_delay();`); err != nil {
		t.Fatalf("install backfill delay: %v", err)
	}
	result := make(chan error, 1)
	go func() { result <- apply(ctx) }()
	waitForMigration169Backfill(t, ctx, database)
	readCtx, readCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	readErr := database.QueryRowContext(readCtx, "SELECT count(*) FROM content_files").Scan(&rows)
	readCancel()
	if readErr != nil {
		t.Fatalf("read blocked during migration 169 UPDATE: %v", readErr)
	}
	writeCtx, writeCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	_, writeErr := database.ExecContext(writeCtx,
		"INSERT INTO content_files (repo_id, relative_path, indexed_at) VALUES ('repo', 'legacy', now() - interval '1 minute')")
	writeCancel()
	if writeErr != nil {
		t.Fatalf("mixed-version NULL-tag writer blocked during UPDATE: %v", writeErr)
	}
	contenderCtx, contenderCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	contenderErr := apply(contenderCtx)
	contenderCancel()
	if contenderErr == nil {
		t.Fatal("concurrent bootstrap unexpectedly bypassed schema ownership")
	}
	if err := <-result; err != nil {
		t.Fatalf("overlap migration: %v", err)
	}
	assertMigration169ColumnAndReceipt(t, ctx, database, true, 1)
	if err := database.QueryRowContext(ctx, "SELECT generation_id FROM content_files WHERE relative_path = 'legacy'").Scan(&tag); err != nil || tag.Valid {
		t.Fatalf("mixed-version writer tag = %v, err=%v, want NULL", tag, err)
	}
	if _, err := database.ExecContext(ctx, "DROP TRIGGER eshu_169_delay ON content_files; DROP FUNCTION eshu_169_delay()"); err != nil {
		t.Fatalf("remove backfill delay: %v", err)
	}
	if _, err := database.ExecContext(ctx, "DELETE FROM content_files WHERE relative_path = 'legacy'"); err != nil {
		t.Fatalf("remove overlap writer row: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		"DELETE FROM eshu_schema_migrations WHERE path = $1 AND variant = 'full'", def.Path,
	); err != nil {
		t.Fatalf("remove receipt for retry proof: %v", err)
	}
	if _, err := database.ExecContext(ctx, "ALTER TABLE content_files DROP COLUMN generation_id"); err != nil {
		t.Fatalf("remove generation tag for retry proof: %v", err)
	}

	// Block after the committed ALTER and hold only a content row. The first
	// UPDATE attempt gets 55P03; the same phase retries after the holder
	// releases it, without repeating the ALTER or recording a partial receipt.
	gate := newMigration169PhaseGate()
	defer gate.release()
	logger := slog.New(slog.NewTextHandler(gate, nil))
	result = make(chan error, 1)
	go func() {
		result <- (SQLDB{DB: database}).withSchemaBootstrapLock(ctx, logger, 3*time.Second, func(locked db.Executor) error {
			tracker, ok := locked.(schemaMigrationTracker)
			if !ok {
				return sql.ErrConnDone
			}
			return tracker.applyTrackedDefinitions(ctx, []Definition{def}, schemaStatementBounds{
				lockTimeout: 100 * time.Millisecond, lockRetryBudget: 3 * time.Second,
			}, logger)
		})
	}()
	select {
	case <-gate.afterAlter:
	case <-ctx.Done():
		t.Fatal("migration 169 ALTER phase did not finish")
	}
	holder, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin backfill row holder: %v", err)
	}
	var lockedPath string
	if err := holder.QueryRowContext(ctx,
		"SELECT relative_path FROM content_files WHERE relative_path = 'manifest' FOR UPDATE",
	).Scan(&lockedPath); err != nil || lockedPath != "manifest" {
		_ = holder.Rollback()
		t.Fatalf("hold backfill row = %q, err=%v", lockedPath, err)
	}
	gate.release()
	select {
	case <-gate.lockWait:
	case <-ctx.Done():
		_ = holder.Rollback()
		t.Fatal("migration 169 UPDATE did not report its bounded lock retry")
	}
	if err := holder.Commit(); err != nil {
		t.Fatalf("release backfill row: %v", err)
	}
	if err := <-result; err != nil {
		t.Fatalf("retry only interrupted backfill phase: %v", err)
	}
	assertMigration169ColumnAndReceipt(t, ctx, database, true, 1)
	if gate.countEventPhase("bootstrap.postgres.migration.phase_applying", "add_column") != 1 ||
		gate.countEventPhase("bootstrap.postgres.migration.phase_applying", "backfill") != 1 ||
		gate.count("event_name=bootstrap.postgres.migration.lock_wait") != 1 {
		t.Fatalf("phase retry events did not show one ALTER, one UPDATE phase, one lock retry: %v", gate.linesCopy())
	}
}

type migration169PhaseGate struct {
	mu         sync.Mutex
	releaseOne sync.Once
	lines      []string
	afterAlter chan struct{}
	proceed    chan struct{}
	lockWait   chan struct{}
}

func (gate *migration169PhaseGate) release() {
	gate.releaseOne.Do(func() { close(gate.proceed) })
}

func newMigration169PhaseGate() *migration169PhaseGate {
	return &migration169PhaseGate{
		afterAlter: make(chan struct{}, 1),
		proceed:    make(chan struct{}),
		lockWait:   make(chan struct{}, 1),
	}
}

func (gate *migration169PhaseGate) Write(p []byte) (int, error) {
	line := string(p)
	gate.mu.Lock()
	gate.lines = append(gate.lines, line)
	gate.mu.Unlock()
	if strings.Contains(line, "event_name=bootstrap.postgres.migration.phase_applied") && strings.Contains(line, "phase=add_column") {
		select {
		case gate.afterAlter <- struct{}{}:
		default:
		}
		<-gate.proceed
	}
	if strings.Contains(line, "event_name=bootstrap.postgres.migration.lock_wait") {
		select {
		case gate.lockWait <- struct{}{}:
		default:
		}
	}
	return len(p), nil
}

func (gate *migration169PhaseGate) count(needle string) int {
	count := 0
	for _, line := range gate.linesCopy() {
		if strings.Contains(line, needle) {
			count++
		}
	}
	return count
}

func (gate *migration169PhaseGate) countEventPhase(event, phase string) int {
	count := 0
	for _, line := range gate.linesCopy() {
		if strings.Contains(line, "event_name="+event) && strings.Contains(line, "phase="+phase) {
			count++
		}
	}
	return count
}

func (gate *migration169PhaseGate) linesCopy() []string {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return append([]string(nil), gate.lines...)
}

func assertMigration169ColumnAndReceipt(t *testing.T, ctx context.Context, database *sql.DB, wantColumn bool, wantReceipts int) {
	t.Helper()
	var exists bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS (
SELECT 1 FROM information_schema.columns
WHERE table_schema = 'public' AND table_name = 'content_files' AND column_name = 'generation_id')`).Scan(&exists); err != nil || exists != wantColumn {
		t.Fatalf("generation_id column exists=%t err=%v, want %t", exists, err, wantColumn)
	}
	var receipts, matching int
	if err := database.QueryRowContext(ctx,
		"SELECT count(*), count(*) FILTER (WHERE checksum_sha256 = $2) FROM eshu_schema_migrations WHERE path = $1 AND variant = 'full'",
		contentGenerationMigrationPath, contentGenerationMigrationSHA,
	).Scan(&receipts, &matching); err != nil || receipts != wantReceipts || matching != wantReceipts {
		t.Fatalf("migration 169 receipts=%d matching=%d err=%v, want %d", receipts, matching, err, wantReceipts)
	}
}

func waitForMigration169Backfill(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	for {
		var active int
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity
WHERE pid <> pg_backend_pid() AND state = 'active'
AND query LIKE '%UPDATE content_files AS f%'`).Scan(&active); err != nil {
			t.Fatalf("inspect migration 169 backfill activity: %v", err)
		}
		if active > 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("migration 169 backfill never started")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
