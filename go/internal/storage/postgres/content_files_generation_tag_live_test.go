// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	producerstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/producers"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// openContentGenerationTagSchema provisions an isolated Postgres schema with
// the content store plus migration 169's generation tag. On a tree without
// migration 169 the MigrationSQL lookup fails loudly: that missing contract
// is the RED side of this file's tests.
func openContentGenerationTagSchema(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the real-Postgres generation-tag proof")
	}
	ctx := context.Background()
	database, schemaName := openFactCrossBatchFencingSchema(t, ctx, dsn)
	// content_store creates pg_trgm, which lives in public when another schema
	// installed it first, so keep public on the path for its operator classes.
	if _, err := database.ExecContext(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := database.ExecContext(ctx, MigrationSQL("content_store")); err != nil {
		t.Fatalf("apply content_store schema: %v", err)
	}
	if _, err := database.ExecContext(ctx, MigrationSQL("content_files_generation_id")); err != nil {
		t.Fatalf("apply content_files_generation_id migration: %v", err)
	}
	// The writer reaps fingerprint side rows on every Write, so the writer
	// stamp test needs the table to exist even with no function entities.
	if _, err := database.ExecContext(ctx, MigrationSQL("code_function_fingerprint")); err != nil {
		t.Fatalf("apply code_function_fingerprint schema: %v", err)
	}
	return ctx, database
}

// openContentPrefillSchema provisions the same isolated schema but stops
// before migration 169: rows seeded here are legacy untagged rows, and the
// test applies 169 itself to prove the B1 backfill attribution.
func openContentPrefillSchema(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the real-Postgres generation-tag proof")
	}
	ctx := context.Background()
	database, schemaName := openFactCrossBatchFencingSchema(t, ctx, dsn)
	if _, err := database.ExecContext(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := database.ExecContext(ctx, MigrationSQL("content_store")); err != nil {
		t.Fatalf("apply content_store schema: %v", err)
	}
	return ctx, database
}

// seedGenerationTagScope inserts one repository scope with a stamped active
// generation. Unactivated generations are added separately per test, so a
// scope seeded this way is clean under both the #7776 dirty predicate and
// the generation-tag rule.
func seedGenerationTagScope(t *testing.T, ctx context.Context, database db.Executor, scopeID, sourceKey, generationID string, observedAt time.Time) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ($1, 'repository', 'git', $2, 'git', $1, $4, $4, 'active', $3)`, scopeID, sourceKey, generationID, observedAt); err != nil {
		t.Fatalf("insert scope %q: %v", scopeID, err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES ($1, $2, 'snapshot', $3, $3, 'active', $3)`, generationID, scopeID, observedAt); err != nil {
		t.Fatalf("insert generation %q: %v", generationID, err)
	}
}

// seedGenerationTagGeneration inserts one non-active generation row. A nil
// activatedAt plants a never-activated generation (pending or refused).
func seedGenerationTagGeneration(t *testing.T, ctx context.Context, database db.Executor, scopeID, generationID, status string, activatedAt *time.Time, observedAt time.Time) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES ($1, $2, 'snapshot', $3, $3, $4, $5)`, generationID, scopeID, observedAt, status, activatedAt); err != nil {
		t.Fatalf("insert generation %q: %v", generationID, err)
	}
}

// seedGenerationTagManifest stores one package.json manifest with an explicit
// generation tag. A nil tag plants a legacy untagged row.
func seedGenerationTagManifest(t *testing.T, ctx context.Context, database db.Executor, repoID, relativePath, packageName string, tag any, indexedAt time.Time) {
	t.Helper()
	content := `{"name": "` + packageName + `"}`
	if _, err := database.ExecContext(ctx, `
INSERT INTO content_files (
    repo_id, relative_path, content, content_hash, line_count, language, indexed_at, generation_id
) VALUES ($1, $2, $3, md5($3), 1, 'json', $4, $5)`, repoID, relativePath, content, indexedAt, tag); err != nil {
		t.Fatalf("insert manifest %s/%s: %v", repoID, relativePath, err)
	}
}

// generationTagProducerRow is one manifest-read row: the scope id, the content
// NULL-ness (NULL marks a dirty scope the anchored scan must always visit),
// and the tag outcome the SQL rule assigned.
type generationTagProducerRow struct {
	content sql.NullString
	outcome string
}

// seedGenerationTagLegacyManifest stores one manifest row without a generation
// tag, as legacy code wrote it before migration 169. Call only on a schema
// where 169 has not been applied yet.
func seedGenerationTagLegacyManifest(t *testing.T, ctx context.Context, database db.Executor, repoID, relativePath, packageName string, indexedAt time.Time) {
	t.Helper()
	content := `{"name": "` + packageName + `"}`
	if _, err := database.ExecContext(ctx, `
INSERT INTO content_files (
    repo_id, relative_path, content, content_hash, line_count, language, indexed_at
) VALUES ($1, $2, $3, md5($3), 1, 'json', $4)`, repoID, relativePath, content, indexedAt); err != nil {
		t.Fatalf("insert legacy manifest %s/%s: %v", repoID, relativePath, err)
	}
}

// queryGenerationTagProducerRows runs a producers manifest read and returns
// each row's scope id with its content and tag outcome.
func queryGenerationTagProducerRows(t *testing.T, ctx context.Context, database db.Queryer, query string) map[string]generationTagProducerRow {
	t.Helper()
	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		t.Fatalf("producer manifests query error = %v", err)
	}
	defer func() { _ = rows.Close() }()
	got := make(map[string]generationTagProducerRow)
	for rows.Next() {
		var scopeID string
		var content sql.NullString
		var outcome string
		if err := rows.Scan(&scopeID, &content, &outcome); err != nil {
			t.Fatalf("scan producer row: %v", err)
		}
		got[scopeID] = generationTagProducerRow{content: content, outcome: outcome}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("producer rows error = %v", err)
	}
	return got
}

// requireGenerationTagRow asserts one scope's row carries the expected
// NULL-ness and tag outcome.
func requireGenerationTagRow(t *testing.T, rows map[string]generationTagProducerRow, scopeID, wantOutcome string, wantNull bool) {
	t.Helper()
	row, ok := rows[scopeID]
	if !ok {
		t.Fatalf("%s missing from producer rows, got %v", scopeID, rows)
	}
	if row.content.Valid == wantNull {
		t.Fatalf("%s content valid = %v, want null = %v", scopeID, row.content.Valid, wantNull)
	}
	if row.outcome != wantOutcome {
		t.Fatalf("%s outcome = %q, want %q", scopeID, row.outcome, wantOutcome)
	}
}

// TestReducerContentionGateContentGenerationTagAheadWriteReadsDirty proves an
// ahead manifest — stored by a generation that never activated — is invisible
// to the active-generation read: the scope resolves as a dirty NULL row, so
// the anchored scan still visits it and the ambiguity rule still engages,
// instead of resolving the ahead content alone.
func TestReducerContentionGateContentGenerationTagAheadWriteReadsDirty(t *testing.T) {
	ctx, database := openContentGenerationTagSchema(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seedGenerationTagScope(t, ctx, database, "scope-ahead", "repo-ahead", "generation-ahead-active", now)
	seedGenerationTagGeneration(t, ctx, database, "scope-ahead", "generation-ahead-refused", "pending", nil, now)
	seedGenerationTagManifest(t, ctx, database, "repo-ahead", "package.json", "@acme/ahead", "generation-ahead-refused", now)

	rows := queryGenerationTagProducerRows(t, ctx, SQLDB{DB: database}, producerstore.PackageManifestsQuery)
	requireGenerationTagRow(t, rows, "scope-ahead", "unactivated_tag", true)

	store := producerstore.New(SQLDB{DB: database})
	scopes, err := store.PackageScopeIDs(ctx, []string{"package:@acme/ahead#main"})
	if err != nil {
		t.Fatalf("PackageScopeIDs() error = %v", err)
	}
	if len(scopes) != 1 || scopes[0] != "scope-ahead" {
		t.Fatalf("PackageScopeIDs() = %v, want [scope-ahead] via the dirty row", scopes)
	}
}

// TestReducerContentionGateContentGenerationTagActivatedTagReadsClean proves a
// manifest tagged with an activated generation resolves by content, including
// the carry-forward case where an older activated generation wrote it and the
// active generation never rewrote the path. It also proves the precision gain
// over the #7776 dirty union: an unactivated generation that never wrote the
// manifest no longer dirties the scope, so an unpublished package resolves
// nothing instead of scanning the scope.
func TestReducerContentionGateContentGenerationTagActivatedTagReadsClean(t *testing.T) {
	ctx, database := openContentGenerationTagSchema(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seedGenerationTagScope(t, ctx, database, "scope-clean", "repo-clean", "generation-clean-active", now)
	seedGenerationTagGeneration(t, ctx, database, "scope-clean", "generation-clean-unrelated", "pending", nil, now)
	seedGenerationTagManifest(t, ctx, database, "repo-clean", "package.json", "@acme/clean", "generation-clean-active", now)
	// Older activated tag: the active generation never rewrote the path, so
	// the stored bytes are still the active truth (carry-forward).
	seedGenerationTagScope(t, ctx, database, "scope-carry", "repo-carry", "generation-carry-active", now)
	seedGenerationTagGeneration(t, ctx, database, "scope-carry", "generation-carry-older", "superseded", &now, now.Add(-time.Hour))
	seedGenerationTagManifest(t, ctx, database, "repo-carry", "packages/lib/package.json", "@acme/carry", "generation-carry-older", now.Add(-time.Hour))

	rows := queryGenerationTagProducerRows(t, ctx, SQLDB{DB: database}, producerstore.PackageManifestsQuery)
	for _, scopeID := range []string{"scope-clean", "scope-carry"} {
		requireGenerationTagRow(t, rows, scopeID, "clean", false)
	}

	store := producerstore.New(SQLDB{DB: database})
	scopes, err := store.PackageScopeIDs(ctx, []string{"package:@acme/nowhere#main"})
	if err != nil {
		t.Fatalf("PackageScopeIDs() error = %v", err)
	}
	if len(scopes) != 0 {
		t.Fatalf("PackageScopeIDs(unpublished) = %v, want []: an unrelated unactivated generation must not dirty the scope", scopes)
	}
}

// TestReducerContentionGateContentGenerationTagManifestLessScopeWithSignalReadsDirty
// proves a scope with no stored manifest but a never-activated generation row
// still resolves dirty: per-row tags cannot see row-less scopes, so the
// manifest-less generation leg of the read must carry them (arbiter C1).
func TestReducerContentionGateContentGenerationTagManifestLessScopeWithSignalReadsDirty(t *testing.T) {
	ctx, database := openContentGenerationTagSchema(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seedGenerationTagScope(t, ctx, database, "scope-bare", "repo-bare", "generation-bare-active", now)
	seedGenerationTagGeneration(t, ctx, database, "scope-bare", "generation-bare-pending", "pending", nil, now)

	rows := queryGenerationTagProducerRows(t, ctx, SQLDB{DB: database}, producerstore.PackageManifestsQuery)
	requireGenerationTagRow(t, rows, "scope-bare", "manifest_less", true)
}

// TestReducerContentionGateContentGenerationTagDangledAndNullTagReadDirty
// proves the fail-safe comparison (F-R1-02, arbiter A2): a tag whose
// generation row is gone (retention-pruned), a legacy NULL tag, and an empty
// tag all resolve the scope dirty instead of dropping it.
func TestReducerContentionGateContentGenerationTagDangledAndNullTagReadDirty(t *testing.T) {
	ctx, database := openContentGenerationTagSchema(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seedGenerationTagScope(t, ctx, database, "scope-dangle", "repo-dangle", "generation-dangle-active", now)
	seedGenerationTagManifest(t, ctx, database, "repo-dangle", "package.json", "@acme/dangle", "generation-dangle-pruned", now)
	seedGenerationTagScope(t, ctx, database, "scope-legacy", "repo-legacy", "generation-legacy-active", now)
	seedGenerationTagManifest(t, ctx, database, "repo-legacy", "package.json", "@acme/legacy", nil, now)
	seedGenerationTagScope(t, ctx, database, "scope-empty", "repo-empty", "generation-empty-active", now)
	seedGenerationTagManifest(t, ctx, database, "repo-empty", "package.json", "@acme/empty", "", now)

	rows := queryGenerationTagProducerRows(t, ctx, SQLDB{DB: database}, producerstore.PackageManifestsQuery)
	// A pruned tag and an empty tag both miss the generation join, so both
	// read as dangling; a legacy NULL tag reads as null. All three fail safe
	// to a dirty NULL row.
	requireGenerationTagRow(t, rows, "scope-dangle", "dangling_tag", true)
	requireGenerationTagRow(t, rows, "scope-legacy", "null_tag", true)
	requireGenerationTagRow(t, rows, "scope-empty", "dangling_tag", true)
}

// TestReducerContentionGateContentWriterStampsGenerationTag proves the content
// writer tags every stored file row with the materialization's generation id,
// so later reads can pin content to the generation that wrote it.
func TestReducerContentionGateContentWriterStampsGenerationTag(t *testing.T) {
	ctx, database := openContentGenerationTagSchema(t)
	writer := NewContentWriter(SQLDB{DB: database})
	_, err := writer.Write(ctx, content.Materialization{
		RepoID:       "repo-stamp",
		ScopeID:      "scope-stamp",
		GenerationID: "generation-stamp-7",
		SourceSystem: "git",
		Records: []content.Record{
			{Path: "package.json", Body: `{"name": "@acme/stamp"}`},
			{Path: "src/index.js", Body: "export const x = 1;\n"},
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	var stamped, total int
	row := database.QueryRowContext(ctx,
		`SELECT count(*) FILTER (WHERE generation_id = 'generation-stamp-7'), count(*)
		 FROM content_files WHERE repo_id = 'repo-stamp'`)
	if err := row.Scan(&stamped, &total); err != nil {
		t.Fatalf("count tagged rows: %v", err)
	}
	if total != 2 || stamped != 2 {
		t.Fatalf("tagged/total = %d/%d, want 2/2: every written row carries the writing generation", stamped, total)
	}
}

// TestReducerContentionGateContentFilesBackfillAttributesOnlyCleanScopes
// proves migration 169's B1 backfill tags a legacy row with the scope's
// active generation only where every guard clause holds, and leaves every
// other shape NULL (dirty) instead of blessing ahead content as active truth.
func TestReducerContentionGateContentFilesBackfillAttributesOnlyCleanScopes(t *testing.T) {
	ctx, database := openContentPrefillSchema(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	before := now.Add(-time.Hour)
	after := now.Add(time.Hour)

	// Clean scope: one activated older generation, active generation
	// activated at now. The in-window row tags; the late row stays NULL by
	// the per-row form of clause (iii).
	seedGenerationTagScope(t, ctx, database, "scope-bf-clean", "repo-bf-clean", "generation-bf-clean", now)
	seedGenerationTagGeneration(t, ctx, database, "scope-bf-clean", "generation-bf-clean-older", "superseded", &before, before)
	seedGenerationTagLegacyManifest(t, ctx, database, "repo-bf-clean", "package.json", "@acme/bfclean", before)
	seedGenerationTagLegacyManifest(t, ctx, database, "repo-bf-clean", "src/late.js", "@acme/bfclean", after)
	// Clause (i): a refused-then-superseded writer still carries a NULL
	// activated_at, so the whole scope stays NULL.
	seedGenerationTagScope(t, ctx, database, "scope-bf-refused", "repo-bf-refused", "generation-bf-rf-active", now)
	seedGenerationTagGeneration(t, ctx, database, "scope-bf-refused", "generation-bf-rf-refused", "superseded", nil, before)
	seedGenerationTagLegacyManifest(t, ctx, database, "repo-bf-refused", "package.json", "@acme/bfrefused", before)
	// Clause (ii): the active pointer names a superseded (non-active) row.
	// Every generation activated, so only the status guard excludes it.
	seedGenerationTagScope(t, ctx, database, "scope-bf-inactive", "repo-bf-inactive", "generation-bf-in-older", now)
	if _, err := database.ExecContext(ctx,
		`UPDATE scope_generations SET status = 'superseded' WHERE generation_id = 'generation-bf-in-older'`); err != nil {
		t.Fatalf("supersede the active pointer row: %v", err)
	}
	seedGenerationTagLegacyManifest(t, ctx, database, "repo-bf-inactive", "package.json", "@acme/bfinactive", before.Add(-time.Hour))
	// Clause (iii): the row was indexed after the activation.
	seedGenerationTagScope(t, ctx, database, "scope-bf-future", "repo-bf-future", "generation-bf-fut", now)
	seedGenerationTagLegacyManifest(t, ctx, database, "repo-bf-future", "package.json", "@acme/bffuture", after)
	// Clause (iv): two repository scopes share one source key.
	seedGenerationTagScope(t, ctx, database, "scope-bf-dup1", "repo-bf-dup", "generation-bf-dup1", now)
	seedGenerationTagScope(t, ctx, database, "scope-bf-dup2", "repo-bf-dup", "generation-bf-dup2", now)
	seedGenerationTagLegacyManifest(t, ctx, database, "repo-bf-dup", "package.json", "@acme/bfdup", before)

	if _, err := database.ExecContext(ctx, MigrationSQL("content_files_generation_id")); err != nil {
		t.Fatalf("apply content_files_generation_id migration: %v", err)
	}

	rows, err := database.QueryContext(ctx, `SELECT repo_id, relative_path, generation_id FROM content_files`)
	if err != nil {
		t.Fatalf("read backfilled tags: %v", err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]sql.NullString{}
	for rows.Next() {
		var repo, path string
		var tag sql.NullString
		if err := rows.Scan(&repo, &path, &tag); err != nil {
			t.Fatalf("scan tag row: %v", err)
		}
		got[repo+"/"+path] = tag
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("tag rows error: %v", err)
	}
	want := map[string]string{
		"repo-bf-clean/package.json":    "generation-bf-clean",
		"repo-bf-clean/src/late.js":     "",
		"repo-bf-refused/package.json":  "",
		"repo-bf-inactive/package.json": "",
		"repo-bf-future/package.json":   "",
		"repo-bf-dup/package.json":      "",
	}
	if len(got) != len(want) {
		t.Fatalf("backfilled rows = %v, want %d rows", got, len(want))
	}
	for key, wantTag := range want {
		tag, ok := got[key]
		if !ok {
			t.Fatalf("missing backfill row %s, got %v", key, got)
		}
		if wantTag == "" && tag.Valid {
			t.Fatalf("%s tag = %q, want NULL (dirty)", key, tag.String)
		}
		if wantTag != "" && (!tag.Valid || tag.String != wantTag) {
			t.Fatalf("%s tag = %v, want %q", key, tag, wantTag)
		}
	}

	// The backfill is idempotent: a second run changes nothing, and the
	// WHERE generation_id IS NULL guard never clobbers an existing tag.
	if _, err := database.ExecContext(ctx, MigrationSQL("content_files_generation_id")); err != nil {
		t.Fatalf("re-apply content_files_generation_id migration: %v", err)
	}
	var tagged, untagged int
	row := database.QueryRowContext(ctx,
		`SELECT count(*) FILTER (WHERE generation_id IS NOT NULL), count(*) FILTER (WHERE generation_id IS NULL) FROM content_files`)
	if err := row.Scan(&tagged, &untagged); err != nil {
		t.Fatalf("count tags after re-run: %v", err)
	}
	if tagged != 1 || untagged != 5 {
		t.Fatalf("tagged/untagged after re-run = %d/%d, want 1/5", tagged, untagged)
	}
}
