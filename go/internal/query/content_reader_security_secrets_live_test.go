// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

func openSecretProofDatabase(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		10*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	return ctx, db
}

func secretProofIndex(t *testing.T, files []secretProofFile, path string) int {
	t.Helper()
	for i, file := range files {
		if file.path == path {
			return i
		}
	}
	t.Fatalf("fixture has no %s", path)
	return -1
}

// TestHardcodedSecretSideTableDifferentialLive proves #7125's row-equivalence
// claim: the side-table reader returns byte-identical rows, in identical order,
// to the frozen legacy corpus scan for 20 argument sets plus the 8 unscoped
// sweep sets, after the initial load and after every write path that can
// change content_files (changed upsert, unchanged re-upsert, language-only
// update, tombstone delete, primary-key move, set-based delete, re-create).
// After each state the side table must also equal the derivation recomputed
// from content_files, EXCEPT ALL both ways. Set
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN and
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 to run it.
func TestHardcodedSecretSideTableDifferentialLive(t *testing.T) {
	ctx, db := openSecretProofDatabase(t)
	reader := NewContentReader(db)
	writer := newSecretProofWriter(t, ctx, db)

	files := append(secretProofHandFiles(), secretProofBulkFiles(240)...)
	writer.write(files, false)
	assertSecretState(t, ctx, db, reader, "initial load")
	assertSecretCorpusCoverage(t, ctx, db)

	// Changed upsert: findings added, removed, and reclassified.
	kinds := secretProofIndex(t, files, "src/kinds.go")
	clean := secretProofIndex(t, files, "src/clean.go")
	placeholders := secretProofIndex(t, files, "src/placeholders.go")
	files[kinds].body = "package kinds\nslack: xoxb-9999999999-changed\n"
	files[clean].body += "password = \"nowhasapassword\"\n"
	files[placeholders].body = "package placeholders\n"
	changed := []secretProofFile{files[kinds], files[clean], files[placeholders]}
	for i := len(files) - 10; i < len(files); i++ {
		files[i].body += "\nclient_secret = \"appended-secret-" + files[i].path + "\"\n"
		changed = append(changed, files[i])
	}
	writer.write(changed, false)
	assertSecretState(t, ctx, db, reader, "changed upsert")

	// Unchanged re-upsert: the derived rows must not be rewritten at all.
	before, err := sideRowVersions(ctx, db)
	if err != nil {
		t.Fatalf("snapshot side row versions: %v", err)
	}
	writer.write(files[:60], false)
	after, err := sideRowVersions(ctx, db)
	if err != nil {
		t.Fatalf("snapshot side row versions: %v", err)
	}
	if !maps.Equal(before, after) {
		t.Fatalf("unchanged re-upsert rewrote side rows: %d rows before, %d after", len(before), len(after))
	}
	assertSecretState(t, ctx, db, reader, "unchanged re-upsert")

	// Language-only update: same body, language changes (including to and
	// from NULL), so only the denormalised language column can go stale.
	swap := map[string]string{"go": "python", "python": "", "": "go", "yaml": "hcl"}
	languageOnly := make([]secretProofFile, 0, 60)
	for i := 0; i < 60; i++ {
		files[i].language = swap[files[i].language]
		languageOnly = append(languageOnly, files[i])
	}
	writer.write(languageOnly, false)
	assertSecretState(t, ctx, db, reader, "language-only update")

	// Tombstone delete through the writer's batch delete: the FK cascade.
	deleted := slices.Clone(files[10:40])
	writer.write(deleted, true)
	assertSecretState(t, ctx, db, reader, "tombstone delete")

	// Primary-key move: ON UPDATE CASCADE then re-derive under the new key.
	if _, err := db.ExecContext(ctx,
		`UPDATE content_files SET relative_path = relative_path || '.moved'
		 WHERE repo_id = 'repo-1' AND relative_path LIKE 'src/%'`); err != nil {
		t.Fatalf("move relative_path: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE content_files SET repo_id = 'repo-9' WHERE repo_id = 'repo-3'`); err != nil {
		t.Fatalf("move repo_id: %v", err)
	}
	assertSecretState(t, ctx, db, reader, "primary-key move")

	// Set-based delete shaped like the retention prune. The real
	// pruneContentFilesForGenerationsQuery cannot run here: the generation
	// retention store's row-count query names a table that does not exist in the
	// bootstrapped schema (#6809). The real prune statement is proven against the
	// side table in go/internal/storage/postgres
	// (TestContentFileSecretLinesRetentionPruneCascadesLive).
	res, err := db.ExecContext(ctx, `
DELETE FROM content_files AS file
USING (SELECT repo_id, relative_path FROM content_files
       WHERE repo_id IN ('repo-0', 'repo-4') AND relative_path LIKE '%f_00%') AS candidate
WHERE file.repo_id = candidate.repo_id AND file.relative_path = candidate.relative_path`)
	if err != nil {
		t.Fatalf("set-based delete: %v", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		t.Fatal("set-based delete removed no rows; the step is vacuous")
	}
	assertSecretState(t, ctx, db, reader, "set-based delete")

	// Re-create the tombstoned files: the insert trigger path after a cascade.
	writer.write(deleted, false)
	assertSecretState(t, ctx, db, reader, "re-create after delete")

	t.Run("seeded divergence is detected", func(t *testing.T) {
		assertHarnessCatchesDroppedSideRow(t, ctx, db, reader)
		assertHarnessCatchesDisabledUpdateTrigger(t, ctx, db, reader, writer, files[kinds])
	})
}

// assertSecretCorpusCoverage proves the fixture is not vacuous: every kind is
// present, suppressed and unsuppressed rows both exist, and the sk_live line
// that matches the pattern but classifies as empty never reaches the table.
func assertSecretCorpusCoverage(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT finding_kind FROM content_file_secret_lines`)
	if err != nil {
		t.Fatalf("read kinds: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var kinds []string
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	want := []string{"api_token", "aws_access_key", "password_literal", "private_key", "secret_literal", "slack_token"}
	if !slices.Equal(kinds, want) {
		t.Fatalf("side table kinds = %v, want %v", kinds, want)
	}
	var suppressed, live, dropped int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FILTER (WHERE suppressed), count(*) FILTER (WHERE NOT suppressed),
       count(*) FILTER (WHERE line_text LIKE '%sk_live_%')
FROM content_file_secret_lines`).Scan(&suppressed, &live, &dropped); err != nil {
		t.Fatalf("read coverage: %v", err)
	}
	if suppressed == 0 || live == 0 {
		t.Fatalf("suppressed=%d unsuppressed=%d, want both nonzero", suppressed, live)
	}
	if dropped != 0 {
		t.Fatalf("%d sk_live rows reached the side table; the legacy query drops them", dropped)
	}
}

// assertHarnessCatchesDroppedSideRow is the seeded RED for the differential:
// deleting one derived row must make the comparison and the parity check fail.
func assertHarnessCatchesDroppedSideRow(t *testing.T, ctx context.Context, db *sql.DB, reader *ContentReader) {
	t.Helper()
	res, err := db.ExecContext(ctx, `
DELETE FROM content_file_secret_lines
WHERE (repo_id, relative_path, line_number) = (
  SELECT repo_id, relative_path, line_number FROM content_file_secret_lines
  WHERE NOT suppressed ORDER BY repo_id, relative_path, line_number LIMIT 1)`)
	if err != nil {
		t.Fatalf("drop one side row: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("dropped %d side rows, want 1", n)
	}
	diffs, _, err := secretDifferentialDiffs(ctx, db, reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) == 0 {
		t.Fatal("differential harness did not notice a dropped side row")
	}
	if missing, _, err := secretParityCounts(ctx, db); err != nil || missing != 1 {
		t.Fatalf("parity missing = %d (err %v), want 1", missing, err)
	}
	restoreSecretSideTable(t, ctx, db)
	assertSecretState(t, ctx, db, reader, "after restoring the dropped row")
}

// assertHarnessCatchesDisabledUpdateTrigger is the second seeded RED: with the
// UPDATE trigger disabled, a changed upsert leaves stale side rows and the
// harness must fail.
func assertHarnessCatchesDisabledUpdateTrigger(
	t *testing.T, ctx context.Context, db *sql.DB, reader *ContentReader,
	writer secretProofWriter, file secretProofFile,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `ALTER TABLE content_files DISABLE TRIGGER content_files_secret_lines_update`); err != nil {
		t.Fatalf("disable update trigger: %v", err)
	}
	file.body = "token = \"changedwithtriggeroff\"\n" + strings.Repeat("x := 1\n", 3)
	writer.write([]secretProofFile{file}, false)
	if _, err := db.ExecContext(ctx, `ALTER TABLE content_files ENABLE TRIGGER content_files_secret_lines_update`); err != nil {
		t.Fatalf("enable update trigger: %v", err)
	}
	diffs, _, err := secretDifferentialDiffs(ctx, db, reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) == 0 {
		t.Fatal("differential harness did not notice a disabled update trigger")
	}
	restoreSecretSideTable(t, ctx, db)
	assertSecretState(t, ctx, db, reader, "after re-enabling the update trigger")
}

// restoreSecretSideTable rebuilds the side table from the derivation.
func restoreSecretSideTable(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		`DELETE FROM content_file_secret_lines`,
		`INSERT INTO content_file_secret_lines (repo_id, relative_path, line_number, language, finding_kind, line_text)
		 SELECT f.repo_id, f.relative_path, d.line_number, coalesce(f.language, ''), d.finding_kind, d.line_text
		 FROM content_files f CROSS JOIN LATERAL eshu_secret_line_findings(f.content) d`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("restore side table: %v", err)
		}
	}
}
