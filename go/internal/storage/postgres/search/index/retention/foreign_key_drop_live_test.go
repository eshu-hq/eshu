// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

// termsForeignKeyDrop is the bootstrap definition that drops the terms
// generation foreign key (#7427 PR3). The file number lives only in the
// migration's filename.
const termsForeignKeyDrop = "eshu_search_index_terms_drop_generation_fk"

// termsForeignKeyNames are the two names the terms generation foreign key has
// in the field: a fresh install creates it with the partitioned table
// (003b), and an install that went through the 039a cutover kept the shadow
// table's name on the parent and on all 64 leaves.
var termsForeignKeyNames = []string{
	"eshu_search_index_terms_generation_id_fkey",
	"eshu_search_index_terms_shadow_generation_id_fkey",
}

// termsGenerationForeignKeysSQL lists every foreign key from the terms parent
// or any of its leaves to scope_generations, inherited or not.
const termsGenerationForeignKeysSQL = `
SELECT constraint_row.conrelid::regclass::text || '.' || constraint_row.conname
FROM pg_constraint AS constraint_row
WHERE constraint_row.contype = 'f'
  AND constraint_row.confrelid = to_regclass('scope_generations')
  AND (constraint_row.conrelid = to_regclass('eshu_search_index_terms')
       OR constraint_row.conrelid IN (
           SELECT inhrelid FROM pg_inherits WHERE inhparent = to_regclass('eshu_search_index_terms')))
ORDER BY 1`

// foreignKeyCount counts the foreign keys of table (and, with leaves, of its
// partitions) that reference referenced.
const foreignKeyCountSQL = `
SELECT count(*)
FROM pg_constraint AS constraint_row
WHERE constraint_row.contype = 'f'
  AND constraint_row.confrelid = to_regclass($2)
  AND (constraint_row.conrelid = to_regclass($1)
       OR constraint_row.conrelid IN (SELECT inhrelid FROM pg_inherits WHERE inhparent = to_regclass($1)))`

func (f *liveFixture) termsGenerationForeignKeys(t *testing.T) []string {
	t.Helper()
	rows, err := f.db.QueryContext(f.ctx, termsGenerationForeignKeysSQL)
	if err != nil {
		t.Fatalf("list terms generation foreign keys: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan terms generation foreign key: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read terms generation foreign keys: %v", err)
	}
	return names
}

func (f *liveFixture) foreignKeyCount(t *testing.T, table, referenced string) int {
	t.Helper()
	var count int
	if err := f.db.QueryRowContext(f.ctx, foreignKeyCountSQL, table, referenced).Scan(&count); err != nil {
		t.Fatalf("count %s foreign keys to %s: %v", table, referenced, err)
	}
	return count
}

// restoreTermsForeignKey puts the pre-#7427-PR3 foreign key back under name,
// so a test can run the drop migration against it again.
func (f *liveFixture) restoreTermsForeignKey(t *testing.T, name string) {
	t.Helper()
	f.exec(t, `ALTER TABLE eshu_search_index_terms ADD CONSTRAINT `+name+
		` FOREIGN KEY (generation_id) REFERENCES scope_generations(generation_id) ON DELETE CASCADE`)
	if got := len(f.termsGenerationForeignKeys(t)); got != 65 {
		t.Fatalf("restored terms foreign keys = %d, want 65 (the parent and its 64 leaves)", got)
	}
}

// migrationConn opens one connection with the migration runner's 5 s
// lock_timeout and returns it with its backend pid.
func (f *liveFixture) migrationConn(t *testing.T) (*sql.Conn, int) {
	t.Helper()
	conn, err := f.db.Conn(f.ctx)
	if err != nil {
		t.Fatalf("open migration connection: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.ExecContext(f.ctx, "SET lock_timeout = '5s'"); err != nil {
		t.Fatalf("set migration lock_timeout: %v", err)
	}
	var pid int
	if err := conn.QueryRowContext(f.ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatalf("read migration backend pid: %v", err)
	}
	return conn, pid
}

// waitForLockWait polls until backend pid waits on a heavyweight lock.
func (f *liveFixture) waitForLockWait(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := f.db.QueryRowContext(f.ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid = $1 AND NOT granted)`, pid).Scan(&waiting); err != nil {
			t.Fatalf("poll backend %d lock wait: %v", pid, err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("backend %d never waited on a lock: the overlap did not happen", pid)
}

// grantedTermsLocks counts the relation locks backend pid holds on the terms
// parent and its leaves.
func (f *liveFixture) grantedTermsLocks(t *testing.T, pid int) int {
	t.Helper()
	var count int
	if err := f.db.QueryRowContext(f.ctx, `
SELECT count(*) FROM pg_locks AS lock_row
WHERE lock_row.pid = $1 AND lock_row.granted AND lock_row.locktype = 'relation'
  AND (lock_row.relation = to_regclass('eshu_search_index_terms')
       OR lock_row.relation IN (SELECT inhrelid FROM pg_inherits WHERE inhparent = to_regclass('eshu_search_index_terms')))`,
		pid).Scan(&count); err != nil {
		t.Fatalf("read backend %d terms locks: %v", pid, err)
	}
	return count
}

// assertNoTableLock runs the migration in a transaction and fails if it took
// any lock on a relation outside the system catalogs: the local supervisor
// re-applies every definition at each start, so a re-run must be free.
func (f *liveFixture) assertNoTableLock(t *testing.T, migration string) {
	t.Helper()
	tx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatalf("begin no-op re-run: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(f.ctx, migration); err != nil {
		t.Fatalf("no-op re-run of the drop migration: %v", err)
	}
	rows, err := tx.QueryContext(f.ctx, `
SELECT lock_row.relation::regclass::text || ':' || lock_row.mode
FROM pg_locks AS lock_row
JOIN pg_class AS class ON class.oid = lock_row.relation
JOIN pg_namespace AS namespace ON namespace.oid = class.relnamespace
WHERE lock_row.pid = pg_backend_pid() AND lock_row.locktype = 'relation'
  AND namespace.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY 1`)
	if err != nil {
		t.Fatalf("read no-op re-run locks: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var locked []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan no-op re-run lock: %v", err)
		}
		locked = append(locked, name)
	}
	if len(locked) != 0 {
		t.Fatalf("no-op re-run of the drop migration locked %v, want no table lock", locked)
	}
}

// TestTermsForeignKeyDropLiveLeavesNoGenerationForeignKey is the #7427 PR3
// end state on the bootstrapped schema: no foreign key from the terms parent
// or any leaf references scope_generations, so deleting a generation row runs
// no statement on the terms table. The other search index keys stay: the
// terms and documents scope_id keys, and the documents and stats generation
// keys. A terms row for a generation that does not exist is now accepted;
// the census and sweep own such rows.
func TestTermsForeignKeyDropLiveLeavesNoGenerationForeignKey(t *testing.T) {
	f := openLiveFixture(t)
	if got := f.termsGenerationForeignKeys(t); len(got) != 0 {
		t.Fatalf("terms foreign keys to scope_generations after bootstrap = %v, want none", got)
	}
	for _, kept := range []struct {
		table, referenced string
		want              int
	}{
		{"eshu_search_index_terms", "ingestion_scopes", 65},
		{"eshu_search_index_documents", "scope_generations", 1},
		{"eshu_search_index_documents", "ingestion_scopes", 1},
		{"eshu_search_index_stats", "scope_generations", 1},
	} {
		if got := f.foreignKeyCount(t, kept.table, kept.referenced); got != kept.want {
			t.Fatalf("%s foreign keys to %s = %d, want %d", kept.table, kept.referenced, got, kept.want)
		}
	}
	f.exec(t, `INSERT INTO eshu_search_index_terms (scope_id, generation_id, document_id, term_key, term, term_frequency)
VALUES ($1, 'gen-missing-7427', 'doc-0', 'tk-0', 'term-0', 1)`, f.scope)
}

// TestTermsForeignKeyDropLiveHandlesBothConstraintNames: the migration finds
// the key in pg_constraint, so it drops it under either name, and a re-run
// with the key gone takes no table lock.
func TestTermsForeignKeyDropLiveHandlesBothConstraintNames(t *testing.T) {
	f := openLiveFixture(t)
	migration := postgres.MigrationSQL(termsForeignKeyDrop)
	conn, _ := f.migrationConn(t)
	for _, name := range termsForeignKeyNames {
		f.restoreTermsForeignKey(t, name)
		if _, err := conn.ExecContext(f.ctx, migration); err != nil {
			t.Fatalf("drop migration with the key named %s: %v", name, err)
		}
		if got := f.termsGenerationForeignKeys(t); len(got) != 0 {
			t.Fatalf("terms foreign keys after dropping %s = %v, want none", name, got)
		}
		if got := f.foreignKeyCount(t, "eshu_search_index_terms", "ingestion_scopes"); got != 65 {
			t.Fatalf("terms scope_id foreign keys after dropping %s = %d, want 65", name, got)
		}
		f.assertNoTableLock(t, migration)
	}

	// A key one leaf carries on its own (not inherited from the parent).
	f.exec(t, `ALTER TABLE eshu_search_index_terms_p00 ADD CONSTRAINT terms_p00_generation_only_fkey
FOREIGN KEY (generation_id) REFERENCES scope_generations(generation_id)`)
	if got := f.termsGenerationForeignKeys(t); len(got) != 1 {
		t.Fatalf("terms foreign keys with one leaf-only key = %v, want that one", got)
	}
	if _, err := conn.ExecContext(f.ctx, migration); err != nil {
		t.Fatalf("drop migration with a leaf-only key: %v", err)
	}
	if got := f.termsGenerationForeignKeys(t); len(got) != 0 {
		t.Fatalf("terms foreign keys after dropping the leaf-only key = %v, want none", got)
	}
}

// TestTermsForeignKeyDropLiveIsDeadlockFree runs the migration against the
// two lock orders production uses on scope_generations and the terms table.
// A plain ALTER TABLE ... DROP CONSTRAINT takes ACCESS EXCLUSIVE on the terms
// parent and its 64 leaves, then waits for scope_generations while it holds
// them; the retention prune holds scope_generations and then deletes terms,
// so the plain form deadlocks it (SQLSTATE 40P01, which the migration runner
// does not retry). The migration must wait only while it holds nothing.
func TestTermsForeignKeyDropLiveIsDeadlockFree(t *testing.T) {
	f := openLiveFixture(t)
	f.seedIndex(t, f.active, 20, 3)
	f.seedIndex(t, f.old, 20, 3)
	migration := postgres.MigrationSQL(termsForeignKeyDrop)
	conn, pid := f.migrationConn(t)

	// Prune order: scope_generations first, then terms.
	f.restoreTermsForeignKey(t, termsForeignKeyNames[0])
	pruner, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatalf("begin prune-order holder: %v", err)
	}
	defer func() { _ = pruner.Rollback() }()
	if _, err := pruner.ExecContext(f.ctx, `SELECT generation_id FROM scope_generations WHERE generation_id = $1 FOR UPDATE`, f.old); err != nil {
		t.Fatalf("prune-order holder locks the generation row: %v", err)
	}
	migrated := make(chan error, 1)
	go func() {
		_, err := conn.ExecContext(f.ctx, migration)
		migrated <- err
	}()
	f.waitForLockWait(t, pid)
	if held := f.grantedTermsLocks(t, pid); held != 0 {
		t.Fatalf("the waiting migration holds %d terms locks, want 0: it must wait only while it holds nothing", held)
	}
	if _, err := pruner.ExecContext(f.ctx, `DELETE FROM eshu_search_index_terms WHERE scope_id = $1 AND generation_id = $2`, f.scope, f.old); err != nil {
		t.Fatalf("prune-order holder deletes terms while the drop waits: %v", err)
	}
	if err := pruner.Commit(); err != nil {
		t.Fatalf("prune-order holder commit: %v", err)
	}
	if err := <-migrated; err != nil {
		t.Fatalf("drop migration beside the prune-order holder: %v", err)
	}
	if got := f.termsGenerationForeignKeys(t); len(got) != 0 {
		t.Fatalf("terms foreign keys after the drop = %v, want none", got)
	}

	// Writer order: a terms insert, whose key check then holds
	// scope_generations, inside an open transaction.
	f.restoreTermsForeignKey(t, termsForeignKeyNames[0])
	writer, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatalf("begin writer-order holder: %v", err)
	}
	defer func() { _ = writer.Rollback() }()
	insert := `INSERT INTO eshu_search_index_terms (scope_id, generation_id, document_id, term_key, term, term_frequency)
VALUES ($1, $2, $3, 'tk-w', 'term-w', 1)`
	if _, err := writer.ExecContext(f.ctx, insert, f.scope, f.active, "doc-w1"); err != nil {
		t.Fatalf("writer inserts a term: %v", err)
	}
	go func() {
		_, err := conn.ExecContext(f.ctx, migration)
		migrated <- err
	}()
	f.waitForLockWait(t, pid)
	if _, err := writer.ExecContext(f.ctx, insert, f.scope, f.active, "doc-w2"); err != nil {
		t.Fatalf("writer inserts a second term while the drop waits: %v", err)
	}
	if err := writer.Commit(); err != nil {
		t.Fatalf("writer-order holder commit: %v", err)
	}
	if err := <-migrated; err != nil {
		t.Fatalf("drop migration beside the writer-order holder: %v", err)
	}

	// A terms-only holder: scope_generations is free, terms is not. The
	// migration takes terms with NOWAIT and fails at once with 55P03, which
	// the runner retries; nothing changes.
	f.restoreTermsForeignKey(t, termsForeignKeyNames[0])
	reader, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatalf("begin terms-only holder: %v", err)
	}
	defer func() { _ = reader.Rollback() }()
	var count int
	if err := reader.QueryRowContext(f.ctx, `SELECT count(*) FROM eshu_search_index_terms WHERE scope_id = $1`, f.scope).Scan(&count); err != nil {
		t.Fatalf("terms-only holder reads terms: %v", err)
	}
	started := time.Now()
	_, err = conn.ExecContext(f.ctx, migration)
	elapsed := time.Since(started)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" || !coordination.IsLockNotAvailable(err) {
		t.Fatalf("drop migration beside a terms-only holder: err = %v, want SQLSTATE 55P03 for the runner's retry", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("drop migration waited %s beside a terms-only holder, want an immediate 55P03", elapsed)
	}
	if err := reader.Commit(); err != nil {
		t.Fatalf("terms-only holder commit: %v", err)
	}
	if got := len(f.termsGenerationForeignKeys(t)); got != 65 {
		t.Fatalf("terms foreign keys after the refused drop = %d, want the 65 restored", got)
	}
	if _, err := conn.ExecContext(f.ctx, migration); err != nil {
		t.Fatalf("retried drop migration after the holder committed: %v", err)
	}
	if got := f.termsGenerationForeignKeys(t); len(got) != 0 {
		t.Fatalf("terms foreign keys after the retried drop = %v, want none", got)
	}
}
