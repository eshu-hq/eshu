// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8915 disposable PostgreSQL 18 proof of two contracts of the content_files upsert guard: a duplicate path inside one batch is rejected the way it always was (the guard must not turn it into a silent last-write-wins), and a statement whose rows are all skipped still fires the statement-level UPDATE triggers with an empty transition table while the shipped migration-138 and migration-186 UPDATE triggers are statement-level; enrolled in the live-postgres-readiness runner; bridges the family ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN as the retention proofs do; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// TestContentWriterRejectsDuplicatePathInBatchLive pins the batch contract
// the guard could have changed. Before the guard every duplicate path inside
// one multi-row upsert failed (SQLSTATE 21000, "cannot affect row a second
// time"). With the guard alone, a duplicate over a stored row that the first
// copy leaves unchanged is skipped and a differing second copy then wins
// silently. ContentWriter rejects the duplicate up front instead, for new and
// stored paths alike, and leaves the stored row untouched.
func TestContentWriterRejectsDuplicatePathInBatchLive(t *testing.T) {
	ctx, db := openContentUpsertGuardDatabase(t)
	const repoID = "repo-8915-duplicates"
	writer := NewContentWriter(SQLDB{DB: db})
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writer.Now = func() time.Time { return t0 }
	record := func(path, body string) content.Record {
		return content.Record{Path: path, Body: body, Metadata: map[string]string{"commit_sha": "sha-1", "language": "go"}}
	}
	materialize := func(records ...content.Record) content.Materialization {
		return content.Materialization{RepoID: repoID, ScopeID: "scope-8915", GenerationID: "gen-1", Records: records}
	}
	if _, err := writer.Write(ctx, materialize(record("stored.go", "package stored\n"))); err != nil {
		t.Fatalf("seed Write: %v", err)
	}
	before := readContentFileVersions(t, ctx, db, repoID)

	cases := []struct {
		name    string
		records []content.Record
	}{
		{"new path twice with different bodies", []content.Record{record("new.go", "package a\n"), record("new.go", "package b\n")}},
		{"stored path twice, both unchanged", []content.Record{record("stored.go", "package stored\n"), record("stored.go", "package stored\n")}},
		{"stored path, unchanged then different", []content.Record{record("stored.go", "package stored\n"), record("stored.go", "package changed\n")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := writer.Write(ctx, materialize(tc.records...))
			if err == nil || !strings.Contains(err.Error(), "duplicate path") {
				t.Fatalf("Write error = %v, want a duplicate path error", err)
			}
			after := readContentFileVersions(t, ctx, db, repoID)
			if got := after["stored.go"]; got.xmin != before["stored.go"].xmin || got.content != before["stored.go"].content {
				t.Fatalf("stored.go changed by a rejected batch: %+v, want %+v", got, before["stored.go"])
			}
			if _, ok := after["new.go"]; ok {
				t.Fatal("new.go was written by a rejected batch")
			}
		})
	}
}

// TestContentFileUpsertGuardSkippedStatementFiresStatementTriggersLive proves
// what a skipped row does and does not trigger. The migration-138 secret-line
// and migration-186 fence UPDATE triggers are statement-level, so an upsert
// whose rows are all skipped still fires them, with an empty transition table;
// they find no row to stage. A probe trigger records the transition-table row
// count per firing.
func TestContentFileUpsertGuardSkippedStatementFiresStatementTriggersLive(t *testing.T) {
	ctx, db := openContentUpsertGuardDatabase(t)
	for _, stmt := range []string{
		`CREATE TABLE eshu_8915_trigger_probe (changed_rows bigint NOT NULL)`,
		`CREATE FUNCTION eshu_8915_trigger_probe_fn() RETURNS trigger LANGUAGE plpgsql AS $fn$
		 BEGIN INSERT INTO eshu_8915_trigger_probe SELECT count(*) FROM probe_new_rows; RETURN NULL; END $fn$`,
		`CREATE TRIGGER eshu_8915_probe AFTER UPDATE ON content_files REFERENCING NEW TABLE AS probe_new_rows
		 FOR EACH STATEMENT EXECUTE FUNCTION eshu_8915_trigger_probe_fn()`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("probe setup: %v", err)
		}
	}
	rows, err := db.QueryContext(ctx, `
SELECT tgname, (tgtype & 1) = 1 AS row_level, (tgtype & 16) = 16 AS on_update
FROM pg_trigger WHERE tgrelid = 'content_files'::regclass AND NOT tgisinternal
  AND tgname IN ('content_files_secret_lines_update', 'content_files_version_fence_update')`)
	if err != nil {
		t.Fatalf("read trigger levels: %v", err)
	}
	seen := 0
	for rows.Next() {
		var name string
		var rowLevel, onUpdate bool
		if err := rows.Scan(&name, &rowLevel, &onUpdate); err != nil {
			t.Fatalf("scan trigger: %v", err)
		}
		seen++
		if rowLevel || !onUpdate {
			t.Errorf("trigger %s: row_level=%v on_update=%v, want a statement-level UPDATE trigger", name, rowLevel, onUpdate)
		}
	}
	_ = rows.Close()
	if seen != 2 {
		t.Fatalf("found %d of the migration-138 and migration-186 UPDATE triggers, want 2", seen)
	}

	shape := upsertShapes()[1]
	a := newGuardFileArgs("trigger/probe.go")
	if _, err := db.ExecContext(ctx, shape.query, a.args()...); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a.indexedAt = a.indexedAt.Add(time.Hour)
	if _, err := db.ExecContext(ctx, shape.query, a.args()...); err != nil {
		t.Fatalf("unchanged replay: %v", err)
	}
	a.content = "package changed\n"
	a.indexedAt = a.indexedAt.Add(time.Hour)
	if _, err := db.ExecContext(ctx, shape.query, a.args()...); err != nil {
		t.Fatalf("changed replay: %v", err)
	}
	probe, err := db.QueryContext(ctx, `SELECT changed_rows FROM eshu_8915_trigger_probe`)
	if err != nil {
		t.Fatalf("read probe: %v", err)
	}
	defer func() { _ = probe.Close() }()
	var got []int64
	for probe.Next() {
		var n int64
		if err := probe.Scan(&n); err != nil {
			t.Fatalf("scan probe: %v", err)
		}
		got = append(got, n)
	}
	// An INSERT ... ON CONFLICT DO UPDATE statement fires the statement-level
	// UPDATE trigger even when it only inserts, so the seed fires once with an
	// empty transition table; the unchanged replay fires once with an empty one;
	// the changed replay fires once with one changed row.
	if len(got) != 3 || got[0] != 0 || got[1] != 0 || got[2] != 1 {
		t.Fatalf("statement UPDATE trigger firings saw changed rows %v, want [0 0 1] (seed insert, skipped statement, changed statement)", got)
	}
}
