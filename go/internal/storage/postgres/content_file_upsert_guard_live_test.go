// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8915 disposable PostgreSQL 18 proof that an unchanged content_files re-upsert (only indexed_at differs) leaves the row version, the secret-line side table, and the content-version fence untouched through both the batch and single-row writers, that each guarded column and a content-only change still update including NULL transitions, and that a changed file re-derives its secret lines; enrolled in the live-postgres-readiness runner; bridges the family ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN as the retention proofs do; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// openContentUpsertGuardDatabase returns a schema-isolated, fully bootstrapped
// database for the #8915 proofs. Run locally with
//
//	ESHU_POSTGRES_TEST_DSN='postgres:///postgres?host=/var/run/postgresql' \
//	  go test ./internal/storage/postgres -run ContentFileUpsertGuard -count=1 -v
func openContentUpsertGuardDatabase(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	bridgeRetentionFamilyDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	t.Cleanup(cancel)
	db, _ := openServiceLineageSchemaLive(ctx, t, "eshu_8915_upsert")
	db.SetMaxOpenConns(16)
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap: %v", err)
	}
	return ctx, db
}

// guardedSecretBody builds a file of about sizeKB kilobytes whose every tenth
// line is a secret-like line, so the migration-138 trigger has findings to
// derive. The seed makes bodies differ.
func guardedSecretBody(seed string, sizeKB int) string {
	var b strings.Builder
	for i := 0; b.Len() < sizeKB*1024; i++ {
		if i%10 == 0 {
			fmt.Fprintf(&b, "password = \"%s-pass-%06d\"\n", seed, i)
			continue
		}
		fmt.Fprintf(&b, "func handler_%s_%06d(request, response) { return nil }\n", seed, i)
	}
	return b.String()
}

// contentFileVersionRow is the physical identity of one content_files row: a
// new row version changes xmin and ctid; a lock-only touch changes neither.
type contentFileVersionRow struct {
	xmin       string
	ctid       string
	indexedAt  time.Time
	content    string
	commitSHA  sql.NullString
	generation sql.NullString
}

func readContentFileVersions(t *testing.T, ctx context.Context, db *sql.DB, repoID string) map[string]contentFileVersionRow {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT relative_path, xmin::text, ctid::text, indexed_at, content, commit_sha, generation_id
FROM content_files WHERE repo_id = $1 ORDER BY relative_path`, repoID)
	if err != nil {
		t.Fatalf("read content_files versions: %v", err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]contentFileVersionRow{}
	for rows.Next() {
		var path string
		var row contentFileVersionRow
		if err := rows.Scan(&path, &row.xmin, &row.ctid, &row.indexedAt, &row.content, &row.commitSHA, &row.generation); err != nil {
			t.Fatalf("scan content_files version: %v", err)
		}
		row.indexedAt = row.indexedAt.UTC()
		got[path] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("content_files versions rows: %v", err)
	}
	return got
}

// readContentFenceVersion returns the migration-186 content-version fence for
// a repository, or 0 when no bump has happened yet.
func readContentFenceVersion(t *testing.T, ctx context.Context, db *sql.DB, repoID string) int64 {
	t.Helper()
	var version int64
	err := db.QueryRowContext(ctx, `SELECT version FROM config_artifact_content_versions WHERE repo_id = $1`, repoID).Scan(&version)
	if err == sql.ErrNoRows {
		return 0
	}
	if err != nil {
		t.Fatalf("read content fence version: %v", err)
	}
	return version
}

func guardedRecords(commit string, bodies map[string]string) []content.Record {
	paths := make([]string, 0, len(bodies))
	for path := range bodies {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	records := make([]content.Record, 0, len(paths))
	for _, path := range paths {
		records = append(records, content.Record{
			Path:     path,
			Body:     bodies[path],
			Metadata: map[string]string{"commit_sha": commit, "language": "go"},
		})
	}
	return records
}

// TestContentFileUpsertGuardUnchangedReplayLeavesRowsUntouchedLive is the
// #8915 regression. A same-generation replay of identical content differs only
// in indexed_at. It must not write a new row version (xmin and ctid hold),
// must leave the version fence and the secret-line side table alone, and must
// hold through both writers: the batch ContentWriter and the single-row
// ContentStore.
func TestContentFileUpsertGuardUnchangedReplayLeavesRowsUntouchedLive(t *testing.T) {
	ctx, db := openContentUpsertGuardDatabase(t)
	const repoID = "repo-8915-replay"
	bodies := map[string]string{}
	for i := 0; i < 6; i++ {
		bodies[fmt.Sprintf("src/f%02d.go", i)] = guardedSecretBody(fmt.Sprintf("s%d", i), 24)
	}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := t0
	writer := NewContentWriter(SQLDB{DB: db})
	writer.Now = func() time.Time { return clock }
	store := NewContentStore(SQLDB{DB: db})
	store.Now = func() time.Time { return clock }
	mat := content.Materialization{RepoID: repoID, ScopeID: "scope-8915", GenerationID: "gen-1", Records: guardedRecords("sha-1", bodies)}

	if _, err := writer.Write(ctx, mat); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	before := readContentFileVersions(t, ctx, db, repoID)
	fenceBefore := readContentFenceVersion(t, ctx, db, repoID)
	sideBefore := secretLinesSideRows(t, ctx, db)
	if len(before) != len(bodies) || len(sideBefore) == 0 || fenceBefore == 0 {
		t.Fatalf("seed rows = %d (want %d), side rows = %d (want findings), fence = %d (want a bump)", len(before), len(bodies), len(sideBefore), fenceBefore)
	}

	assertUntouched := func(label string) {
		t.Helper()
		after := readContentFileVersions(t, ctx, db, repoID)
		for path, was := range before {
			now := after[path]
			if now.xmin != was.xmin || now.ctid != was.ctid {
				t.Errorf("%s: %s row version changed (xmin %s->%s, ctid %s->%s), want an untouched row", label, path, was.xmin, now.xmin, was.ctid, now.ctid)
			}
			if !now.indexedAt.Equal(was.indexedAt) {
				t.Errorf("%s: %s indexed_at = %s, want the first write's %s (indexed_at is deliberately not a guarded column)", label, path, now.indexedAt, was.indexedAt)
			}
		}
		if got := readContentFenceVersion(t, ctx, db, repoID); got != fenceBefore {
			t.Errorf("%s: fence version = %d, want %d: an unchanged replay must not bump it", label, got, fenceBefore)
		}
		requireSecretLinesParity(t, ctx, db, label)
		if got := secretLinesSideRows(t, ctx, db); len(got) != len(sideBefore) {
			t.Errorf("%s: secret-line side rows = %d, want %d", label, len(got), len(sideBefore))
		}
	}

	clock = t0.Add(time.Hour)
	if _, err := writer.Write(ctx, mat); err != nil {
		t.Fatalf("replay Write: %v", err)
	}
	assertUntouched("batch writer replay")

	clock = t0.Add(2 * time.Hour)
	if err := store.UpsertFileBatch(ctx, repoID, "gen-1", mat.Records); err != nil {
		t.Fatalf("replay UpsertFileBatch: %v", err)
	}
	assertUntouched("single-row store replay")
}

// TestContentFileUpsertGuardChangedFileStillUpdatesLive proves the guard does
// not swallow real changes: a changed body re-derives its secret lines, a
// commit-only change still writes, an untouched sibling stays untouched, and
// the fence stages the repository. The fence bumps once per touching
// transaction (its contract): the mixed batch writes in two statements (the
// metadata UPDATE for the commit-only row, the upsert for the changed row),
// each its own implicit transaction as before, so the version advances by
// two.
func TestContentFileUpsertGuardChangedFileStillUpdatesLive(t *testing.T) {
	ctx, db := openContentUpsertGuardDatabase(t)
	const repoID = "repo-8915-changed"
	bodies := map[string]string{
		"a.go": guardedSecretBody("a", 8),
		"b.go": guardedSecretBody("b", 8),
		"c.go": guardedSecretBody("c", 8),
	}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := t0
	writer := NewContentWriter(SQLDB{DB: db})
	writer.Now = func() time.Time { return clock }
	if _, err := writer.Write(ctx, content.Materialization{RepoID: repoID, ScopeID: "scope-8915", GenerationID: "gen-1", Records: guardedRecords("sha-1", bodies)}); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	before := readContentFileVersions(t, ctx, db, repoID)
	fenceBefore := readContentFenceVersion(t, ctx, db, repoID)

	changed := map[string]string{"a.go": guardedSecretBody("a-new", 8), "b.go": bodies["b.go"], "c.go": bodies["c.go"]}
	records := guardedRecords("sha-1", changed)
	for i := range records {
		if records[i].Path == "b.go" {
			records[i].Metadata["commit_sha"] = "sha-2"
		}
	}
	clock = t0.Add(time.Hour)
	if _, err := writer.Write(ctx, content.Materialization{RepoID: repoID, ScopeID: "scope-8915", GenerationID: "gen-1", Records: records}); err != nil {
		t.Fatalf("changed Write: %v", err)
	}
	after := readContentFileVersions(t, ctx, db, repoID)
	if after["a.go"].xmin == before["a.go"].xmin || after["a.go"].content != changed["a.go"] || !after["a.go"].indexedAt.Equal(clock) {
		t.Errorf("a.go (body changed) = %+v, want a new row version carrying the new body and indexed_at", after["a.go"])
	}
	if after["b.go"].xmin == before["b.go"].xmin || after["b.go"].commitSHA.String != "sha-2" {
		t.Errorf("b.go (commit changed) = %+v, want a new row version with commit sha-2", after["b.go"])
	}
	if after["c.go"].xmin != before["c.go"].xmin || !after["c.go"].indexedAt.Equal(before["c.go"].indexedAt) {
		t.Errorf("c.go (unchanged sibling) = %+v, want it untouched next to changed rows", after["c.go"])
	}
	if got := readContentFenceVersion(t, ctx, db, repoID); got != fenceBefore+2 {
		t.Errorf("fence version = %d, want %d: the metadata UPDATE and the upsert each bump once", got, fenceBefore+2)
	}
	requireSecretLinesParity(t, ctx, db, "after changed write")
	var newFindings int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM content_file_secret_lines WHERE repo_id = $1 AND relative_path = 'a.go' AND line_text LIKE '%a-new-pass-%'`, repoID).Scan(&newFindings); err != nil || newFindings == 0 {
		t.Errorf("a.go findings from the new body = %d (err %v), want the re-derived secret lines", newFindings, err)
	}
}
