// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// migration169ReaderBound is the #7961 contract: no read of content_files may
// wait longer than this while migration 169 runs through the tracked migrator.
const migration169ReaderBound = time.Second

// The fixture size flags let an operator rerun the proof at measurement scale
// (157,422 rows matches the #7961 fixture) without a new environment variable.
var (
	migration169ProofRows   = flag.Int("eshu.migration169.rows", 8000, "content_files rows the #7961 reader-bound proof seeds")
	migration169ProofTokens = flag.Int("eshu.migration169.tokens", 320, "identifier tokens per seeded content_files body")
)

// migration169ProductionIndexes is the content_files index set that migrations
// 001-168 build; the proof refuses to run on a reduced schema.
var migration169ProductionIndexes = []string{
	"content_files_artifact_type_idx",
	"content_files_content_trgm_idx",
	"content_files_iac_relevant_idx",
	"content_files_language_repo_idx",
	"content_files_pkey",
	"content_files_relative_path_trgm_idx",
	"content_files_repo_path_idx",
	"content_files_repo_path_pattern_idx",
	"content_files_template_dialect_idx",
}

// TestMigration169PhasedBackfillReaderBoundLive applies migrations 001-168
// through the real tracked migrator, seeds content_files on the production
// index set, then applies 169 while a reader loop and a pg_locks sampler run.
// It fails if any read waits longer than migration169ReaderBound, if ACCESS
// EXCLUSIVE appears during the backfill, or if the one-message control (the
// pre-#7946 execution) does NOT block a reader, which would mean the fixture
// is too small for the bound to prove anything.
func TestMigration169PhasedBackfillReaderBoundLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableDatabase(t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		30*time.Minute,
	)
	database.SetMaxOpenConns(8)
	definitions := BootstrapDefinitions()
	boundary := slices.IndexFunc(definitions, func(def Definition) bool {
		return path.Base(def.Path) == "169_content_files_generation_id.sql"
	})
	if boundary < 0 {
		t.Fatal("bootstrap definitions omit 169_content_files_generation_id.sql")
	}
	through169 := definitions[:boundary+1]
	quiet := slog.New(slog.DiscardHandler)
	if err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, definitions[:boundary], quiet, schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("apply migrations before 169: %v", err)
	}
	assertMigration169ProductionIndexSet(t, ctx, database)
	rows := *migration169ProofRows
	seedMigration169Fixture(t, ctx, database, rows, *migration169ProofTokens)

	// Phased run through the tracked migrator: the #7961 contract.
	phases := &migration169PhaseRecorder{}
	walBefore := currentWALLSN(t, ctx, database)
	probe := startMigration169Probe(ctx, database)
	applyStarted := time.Now()
	applyErr := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, through169, slog.New(phases), schemaBootstrapCoordination{})
	applyElapsed := time.Since(applyStarted)
	reads, locks, probeErr := probe.stop()
	if probeErr != nil {
		t.Fatalf("reader probe during phased 169: %v", probeErr)
	}
	if applyErr != nil {
		t.Fatalf("apply migration 169: %v", applyErr)
	}
	walBytes := walDiffBytes(t, ctx, database, walBefore)
	if worst := maxReadWait(reads); worst > migration169ReaderBound {
		t.Fatalf("a content_files read waited %s during migration 169 (%d reads), bound %s", worst, len(reads), migration169ReaderBound)
	}
	backfill, ok := phases.window("backfill")
	if !ok {
		t.Fatal("tracked migrator did not report the backfill phase window")
	}
	duringBackfill := readsWithin(reads, backfill)
	if len(duringBackfill) < 3 {
		t.Fatalf("only %d reads ran during the %s backfill; raise -eshu.migration169.rows", len(duringBackfill), backfill.duration())
	}
	assertBackfillLockModes(t, locks, backfill)
	addColumnWindow, _ := phases.window("add_column")
	addColumn := addColumnWindow.duration()
	if addColumn > migration169ReaderBound {
		t.Fatalf("add_column phase held its lock for %s, bound %s", addColumn, migration169ReaderBound)
	}
	if tagged := countTaggedContentFiles(t, ctx, database); tagged != rows {
		t.Fatalf("tagged rows = %d, want %d (every seeded row is eligible)", tagged, rows)
	}
	migrationPath := definitions[boundary].Path
	assertMigration169Receipts(t, ctx, database, migrationPath, 1)
	t.Logf("phased 169: rows=%d tokens=%d apply=%s add_column=%s backfill=%s analyze=%s wal_bytes=%d reads=%d reads_in_backfill=%d max_read_wait=%s max_read_wait_in_backfill=%s lock_samples=%d lock_modes_in_add_column=%v lock_modes_in_backfill=%v",
		rows, *migration169ProofTokens, applyElapsed, addColumn, backfill.duration(), phases.duration("analyze"),
		walBytes, len(reads), len(duringBackfill), maxReadWait(reads), maxReadWait(duringBackfill), len(locks),
		lockModesWithin(locks, addColumnWindow), lockModesWithin(locks, backfill))

	// Idempotent re-run: a recorded receipt skips; a cleared receipt replays
	// every phase, and the NULL guard rewrites no row (every ctid is unchanged).
	before := contentFilesTupleFingerprint(t, ctx, database)
	if err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, through169, quiet, schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("re-apply with receipt: %v", err)
	}
	assertMigrationLedgerCount(t, ctx, database, boundary+1)
	assertMigration169Receipts(t, ctx, database, migrationPath, 1)
	if _, err := database.ExecContext(ctx, "DELETE FROM eshu_schema_migrations WHERE path = $1", migrationPath); err != nil {
		t.Fatalf("clear 169 receipt: %v", err)
	}
	rerun := &migration169PhaseRecorder{}
	if err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, through169, slog.New(rerun), schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("replay 169 without receipt: %v", err)
	}
	assertMigrationLedgerCount(t, ctx, database, boundary+1)
	assertMigration169Receipts(t, ctx, database, migrationPath, 1)
	if after := contentFilesTupleFingerprint(t, ctx, database); after != before {
		t.Fatal("receipt-free replay rewrote content_files rows; the generation_id IS NULL guard must match none")
	}
	t.Logf("replayed 169: backfill=%s, rows rewritten=0 (tuple fingerprint unchanged)", rerun.duration("backfill"))

	// Interruption at scale: terminate the migrator's backend during the
	// backfill. The committed ALTER stays, the UPDATE rolls back, no receipt
	// is written, and a rerun completes with exactly one receipt.
	interruptMigration169Backfill(t, ctx, database, through169, migrationPath)
	if tagged := countTaggedContentFiles(t, ctx, database); tagged != rows {
		t.Fatalf("tagged rows after interrupted rerun = %d, want %d", tagged, rows)
	}

	// Control: the same SQL as one simple-protocol message, as the migrator
	// sent it before #7946. A reader must block past the bound; otherwise the
	// fixture is too small for the bound above to mean anything.
	if _, err := database.ExecContext(ctx, "ALTER TABLE content_files DROP COLUMN generation_id"); err != nil {
		t.Fatalf("reset generation tag for control: %v", err)
	}
	control := startMigration169Probe(ctx, database)
	_, controlErr := database.ExecContext(ctx, definitions[boundary].SQL)
	controlReads, _, controlProbeErr := control.stop()
	if controlProbeErr != nil {
		t.Fatalf("reader probe during one-message control: %v", controlProbeErr)
	}
	if controlErr != nil {
		t.Fatalf("one-message control: %v", controlErr)
	}
	worst := maxReadWait(controlReads)
	if worst <= migration169ReaderBound {
		t.Fatalf("one-message control max read wait %s did not exceed %s; raise -eshu.migration169.rows so the bound is meaningful", worst, migration169ReaderBound)
	}
	t.Logf("one-message control: max_read_wait=%s (blocked behind ACCESS EXCLUSIVE, as expected)", worst)
}

// assertMigration169ProductionIndexSet refuses a reduced schema: the proof is
// only meaningful when every content_files index and the secret-lines
// triggers that the backfill UPDATE maintains are present.
func assertMigration169ProductionIndexSet(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	got := queryStrings(t, ctx, database,
		"SELECT indexname FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'content_files' ORDER BY 1")
	if !slices.Equal(got, migration169ProductionIndexes) {
		t.Fatalf("content_files indexes = %v, want the production set %v", got, migration169ProductionIndexes)
	}
	triggers := queryStrings(t, ctx, database,
		"SELECT tgname FROM pg_trigger WHERE tgrelid = 'content_files'::regclass AND NOT tgisinternal ORDER BY 1")
	want := []string{"content_files_secret_lines_insert", "content_files_secret_lines_update"}
	if !slices.Equal(triggers, want) {
		t.Fatalf("content_files triggers = %v, want %v", triggers, want)
	}
}

// seedMigration169Fixture loads the #7961 fixture shape: 782 repository
// scopes with one activated generation each and rows content_files bodies of
// tokens identifier-like words, every row eligible for the 169 backfill.
func seedMigration169Fixture(t *testing.T, ctx context.Context, database *sql.DB, rows, tokens int) {
	t.Helper()
	if rows < 1 || tokens < 1 {
		t.Fatalf("invalid fixture size rows=%d tokens=%d", rows, tokens)
	}
	seed := fmt.Sprintf(`
SET eshu.secret_lines_derive = 'deferred';
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
                              observed_at, ingested_at, status, active_generation_id)
SELECT 'scope:' || g, 'repository', 'git', 'repo-' || g, 'git', 'p', now(), now(), 'active', 'gen-' || g
FROM generate_series(1, 782) g;
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
SELECT 'gen-' || g, 'scope:' || g, 'snapshot', now() - interval '2 days', now() - interval '2 days', 'active', now() - interval '1 day'
FROM generate_series(1, 782) g;
CREATE TEMP TABLE vocab AS
SELECT i, substr(md5(i::text), 1, 4 + (i %% 9)) || CASE WHEN i %% 5 = 0 THEN '_handler' WHEN i %% 7 = 0 THEN 'Service' ELSE '' END AS w
FROM generate_series(1, 4000) i;
CREATE INDEX ON vocab (i);
INSERT INTO content_files (repo_id, relative_path, commit_sha, content, content_hash, line_count, language,
                           indexed_at, artifact_type, template_dialect, iac_relevant)
SELECT 'repo-' || (1 + (n %% 782)), 'src/pkg' || (n %% 97) || '/file_' || n || '.go', md5(n::text),
       body, md5(body), 30, (ARRAY['go','python','typescript','hcl','yaml'])[1 + n %% 5],
       now() - interval '2 days', NULL, NULL, (n %% 11 = 0)
FROM (
  SELECT n, (SELECT string_agg(v.w, CASE WHEN k %% 12 = 0 THEN E'\n' ELSE ' ' END)
             FROM generate_series(1, %d) k
             JOIN vocab v ON v.i = 1 + ((n::bigint * 7919 + k * 104729) %% 4000)) AS body
  FROM generate_series(1, %d) n
) s;
DROP TABLE vocab;
RESET eshu.secret_lines_derive;
ANALYZE content_files;
ANALYZE ingestion_scopes;
ANALYZE scope_generations;`, tokens, rows)
	if _, err := database.ExecContext(ctx, seed); err != nil {
		t.Fatalf("seed migration 169 fixture: %v", err)
	}
}

func queryStrings(t *testing.T, ctx context.Context, database *sql.DB, query string) []string {
	t.Helper()
	result, err := database.QueryContext(ctx, query)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer func() { _ = result.Close() }()
	var out []string
	for result.Next() {
		var value string
		if err := result.Scan(&value); err != nil {
			t.Fatalf("scan %q: %v", query, err)
		}
		out = append(out, value)
	}
	if err := result.Err(); err != nil {
		t.Fatalf("iterate %q: %v", query, err)
	}
	return out
}

func countTaggedContentFiles(t *testing.T, ctx context.Context, database *sql.DB) int {
	t.Helper()
	var tagged int
	if err := database.QueryRowContext(ctx,
		"SELECT count(*) FROM content_files WHERE generation_id IS NOT NULL").Scan(&tagged); err != nil {
		t.Fatalf("count tagged content_files: %v", err)
	}
	return tagged
}

func currentWALLSN(t *testing.T, ctx context.Context, database *sql.DB) string {
	t.Helper()
	var lsn string
	if err := database.QueryRowContext(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&lsn); err != nil {
		t.Fatalf("read WAL LSN: %v", err)
	}
	return lsn
}

func walDiffBytes(t *testing.T, ctx context.Context, database *sql.DB, before string) int64 {
	t.Helper()
	var bytes int64
	if err := database.QueryRowContext(ctx,
		"SELECT pg_wal_lsn_diff(pg_current_wal_lsn(), $1::pg_lsn)::bigint", before).Scan(&bytes); err != nil {
		t.Fatalf("diff WAL LSN: %v", err)
	}
	return bytes
}
