// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8915 disposable PostgreSQL 18 proof, per statement shape and per guarded column, that the content_files upsert guard fires exactly when a stored column differs (NULL-safe, content alone included) and skips when only indexed_at differs, plus the per-session WAL bound of an unchanged replay; enrolled in the live-postgres-readiness runner; bridges the family ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN as the retention proofs do; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// guardFileArgs is one content_files upsert in the column order both
// statements bind: nullable columns are any so a case can bind SQL NULL.
type guardFileArgs struct {
	path            string
	commitSHA       any
	content         string
	contentHash     string
	lineCount       int
	language        any
	artifactType    any
	templateDialect any
	iacRelevant     bool
	indexedAt       time.Time
	generationID    any
}

func newGuardFileArgs(path string) guardFileArgs {
	return guardFileArgs{
		path:            path,
		commitSHA:       "sha-1",
		content:         "package p\n",
		contentHash:     "hash-1",
		lineCount:       1,
		language:        "go",
		artifactType:    "source",
		templateDialect: "none",
		iacRelevant:     false,
		indexedAt:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		generationID:    "gen-1",
	}
}

func (a guardFileArgs) args() []any {
	return []any{
		"repo-8915-columns", a.path, a.commitSHA, a.content, a.contentHash,
		a.lineCount, a.language, a.artifactType, a.templateDialect,
		a.iacRelevant, a.indexedAt, a.generationID,
	}
}

// upsertShape is one production statement: the single-row ContentStore query
// and the one-row form of the ContentWriter batch statement, built from the
// same prefix and suffix constants the writer uses.
type upsertShape struct {
	name  string
	query string
}

func upsertShapes() []upsertShape {
	return []upsertShape{
		{name: "single_row", query: upsertContentFileQuery},
		{name: "batch", query: upsertContentFileBatchPrefix + "($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)" + upsertContentFileBatchSuffix},
	}
}

// guardColumnCase mutates one guarded column between the base and the replay.
type guardColumnCase struct {
	name       string
	base       func(*guardFileArgs)
	next       func(*guardFileArgs)
	wantUpdate bool
}

// guardColumnCases covers every guarded column, NULL-safely for the nullable
// ones (NULL to NULL is no change; NULL to a value and a value to NULL are
// changes), a content-only change that keeps the hash, and indexed_at alone.
func guardColumnCases() []guardColumnCase {
	cases := []guardColumnCase{
		{name: "indexed_at_only", next: func(a *guardFileArgs) {}, wantUpdate: false},
		{name: "content_hash", next: func(a *guardFileArgs) { a.contentHash = "hash-2" }, wantUpdate: true},
		{name: "line_count", next: func(a *guardFileArgs) { a.lineCount = 2 }, wantUpdate: true},
		{name: "iac_relevant", next: func(a *guardFileArgs) { a.iacRelevant = true }, wantUpdate: true},
		{name: "content_only_same_hash", next: func(a *guardFileArgs) { a.content = "package q\n" }, wantUpdate: true},
	}
	nullable := []struct {
		name string
		set  func(*guardFileArgs, any)
	}{
		{"commit_sha", func(a *guardFileArgs, v any) { a.commitSHA = v }},
		{"language", func(a *guardFileArgs, v any) { a.language = v }},
		{"artifact_type", func(a *guardFileArgs, v any) { a.artifactType = v }},
		{"template_dialect", func(a *guardFileArgs, v any) { a.templateDialect = v }},
		{"generation_id", func(a *guardFileArgs, v any) { a.generationID = v }},
	}
	for _, column := range nullable {
		set := column.set
		cases = append(cases,
			guardColumnCase{name: column.name + "_value_to_other", next: func(a *guardFileArgs) { set(a, "other") }, wantUpdate: true},
			guardColumnCase{name: column.name + "_value_to_null", next: func(a *guardFileArgs) { set(a, nil) }, wantUpdate: true},
			guardColumnCase{name: column.name + "_null_to_value", base: func(a *guardFileArgs) { set(a, nil) }, next: func(a *guardFileArgs) { set(a, "other") }, wantUpdate: true},
			guardColumnCase{name: column.name + "_null_to_null", base: func(a *guardFileArgs) { set(a, nil) }, next: func(a *guardFileArgs) { set(a, nil) }, wantUpdate: false},
		)
	}
	return cases
}

// TestContentFileUpsertGuardColumnsLive proves, per statement shape and per
// guarded column, that the DO UPDATE guard fires exactly when a stored column
// differs (NULL-safely) and skips when only indexed_at differs.
func TestContentFileUpsertGuardColumnsLive(t *testing.T) {
	ctx, db := openContentUpsertGuardDatabase(t)
	for _, shape := range upsertShapes() {
		for _, tc := range guardColumnCases() {
			t.Run(shape.name+"/"+tc.name, func(t *testing.T) {
				base := newGuardFileArgs(shape.name + "/" + tc.name)
				if tc.base != nil {
					tc.base(&base)
				}
				if _, err := db.ExecContext(ctx, shape.query, base.args()...); err != nil {
					t.Fatalf("seed: %v", err)
				}
				was := readGuardRow(t, ctx, db, base.path)

				next := base
				tc.next(&next)
				next.indexedAt = base.indexedAt.Add(time.Hour)
				if _, err := db.ExecContext(ctx, shape.query, next.args()...); err != nil {
					t.Fatalf("replay: %v", err)
				}
				now := readGuardRow(t, ctx, db, base.path)

				if tc.wantUpdate {
					if now.xmin == was.xmin || !now.indexedAt.Equal(next.indexedAt) {
						t.Fatalf("changed column left the row stale: xmin %s->%s, indexed_at %s (want %s)", was.xmin, now.xmin, now.indexedAt, next.indexedAt)
					}
					if now.content != next.content || now.contentHash != next.contentHash || now.lineCount != next.lineCount ||
						now.iacRelevant != next.iacRelevant || !sameNullable(now.commitSHA, next.commitSHA) ||
						!sameNullable(now.language, next.language) || !sameNullable(now.artifactType, next.artifactType) ||
						!sameNullable(now.templateDialect, next.templateDialect) || !sameNullable(now.generationID, next.generationID) {
						t.Fatalf("row after replay = %+v, want the replayed values %+v", now, next)
					}
					return
				}
				if now.xmin != was.xmin || now.ctid != was.ctid || !now.indexedAt.Equal(base.indexedAt) {
					t.Fatalf("unchanged replay rewrote the row: xmin %s->%s, ctid %s->%s, indexed_at %s (want %s)", was.xmin, now.xmin, was.ctid, now.ctid, now.indexedAt, base.indexedAt)
				}
			})
		}
	}
}

type guardRow struct {
	xmin, ctid                                                       string
	indexedAt                                                        time.Time
	content, contentHash                                             string
	lineCount                                                        int
	iacRelevant                                                      bool
	commitSHA, language, artifactType, templateDialect, generationID sql.NullString
}

func readGuardRow(t *testing.T, ctx context.Context, db *sql.DB, path string) guardRow {
	t.Helper()
	var r guardRow
	err := db.QueryRowContext(ctx, `
SELECT xmin::text, ctid::text, indexed_at, content, content_hash, line_count, iac_relevant,
       commit_sha, language, artifact_type, template_dialect, generation_id
FROM content_files WHERE repo_id = 'repo-8915-columns' AND relative_path = $1`, path).Scan(
		&r.xmin, &r.ctid, &r.indexedAt, &r.content, &r.contentHash, &r.lineCount, &r.iacRelevant,
		&r.commitSHA, &r.language, &r.artifactType, &r.templateDialect, &r.generationID)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	r.indexedAt = r.indexedAt.UTC()
	return r
}

func sameNullable(got sql.NullString, want any) bool {
	if want == nil {
		return !got.Valid
	}
	return got.Valid && got.String == want.(string)
}

// explainWALBytes runs the statement under EXPLAIN (ANALYZE, WAL) and returns
// the WAL bytes this one session wrote: unlike a pg_current_wal_lsn delta it
// is not polluted by other sessions on a shared cluster.
func explainWALBytes(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var raw string
	if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, WAL, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatalf("explain analyze: %v", err)
	}
	var plan []struct {
		Plan struct {
			WALBytes int64 `json:"WAL Bytes"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(raw), &plan); err != nil || len(plan) != 1 {
		t.Fatalf("parse explain json: %v (%d entries)", err, len(plan))
	}
	return plan[0].Plan.WALBytes
}

// TestContentFileUpsertGuardUnchangedReplayWritesNoRowWALLive measures the
// cost the guard removes: an unchanged replay of a large findings-heavy file
// writes almost no WAL, while a changed replay still writes the row.
func TestContentFileUpsertGuardUnchangedReplayWritesNoRowWALLive(t *testing.T) {
	ctx, db := openContentUpsertGuardDatabase(t)
	for _, shape := range upsertShapes() {
		t.Run(shape.name, func(t *testing.T) {
			a := newGuardFileArgs("wal/" + shape.name)
			a.content = guardedSecretBody("wal", 200)
			if _, err := db.ExecContext(ctx, shape.query, a.args()...); err != nil {
				t.Fatalf("seed: %v", err)
			}
			replay := a
			replay.indexedAt = a.indexedAt.Add(time.Hour)
			unchanged := explainWALBytes(t, ctx, db, shape.query, replay.args()...)
			if limit := int64(8 * 1024); unchanged > limit {
				t.Errorf("unchanged replay wrote %d WAL bytes, want <= %d for a %d byte body", unchanged, limit, len(a.content))
			}
			edited := replay
			edited.content = strings.ReplaceAll(a.content, "wal-pass", "edit-pass")
			edited.contentHash = "hash-wal-edit"
			changed := explainWALBytes(t, ctx, db, shape.query, edited.args()...)
			// TOAST compresses the synthetic body, so the changed write is
			// well under the body size; it must still dwarf the skipped one.
			if floor := int64(32 * 1024); changed < floor {
				t.Errorf("changed replay wrote %d WAL bytes, want at least %d: the guard must not skip a real change", changed, floor)
			}
			t.Logf("%s: unchanged replay %d WAL bytes, changed replay %d WAL bytes", shape.name, unchanged, changed)
		})
	}
}
