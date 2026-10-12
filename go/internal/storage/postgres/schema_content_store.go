// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// This file owns content-store schema DDL and the finalization lifecycle for
// its search indexes. The bootstrap assembly stays in schema.go.

const contentStoreBaseSchemaSQL = `
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS content_files (
    repo_id TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    commit_sha TEXT NULL,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    line_count INTEGER NOT NULL,
    language TEXT NULL,
    indexed_at TIMESTAMPTZ NOT NULL,
    artifact_type TEXT NULL,
    template_dialect TEXT NULL,
    iac_relevant BOOLEAN NULL,
    PRIMARY KEY (repo_id, relative_path)
);

CREATE TABLE IF NOT EXISTS content_entities (
    entity_id TEXT PRIMARY KEY,
    repo_id TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_name TEXT NOT NULL,
    start_line INTEGER NOT NULL,
    end_line INTEGER NOT NULL,
    start_byte INTEGER NULL,
    end_byte INTEGER NULL,
    language TEXT NULL,
    source_cache TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    indexed_at TIMESTAMPTZ NOT NULL,
    artifact_type TEXT NULL,
    template_dialect TEXT NULL,
    iac_relevant BOOLEAN NULL
);

CREATE TABLE IF NOT EXISTS content_file_references (
    repo_id TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    reference_kind TEXT NOT NULL,
    reference_value TEXT NOT NULL,
    indexed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (repo_id, relative_path, reference_kind, reference_value)
);

CREATE TABLE IF NOT EXISTS repository_refs (
    repo_id TEXT NOT NULL,
    ref_kind TEXT NOT NULL,
    name TEXT NOT NULL,
    head_sha TEXT NOT NULL,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    observed_at TIMESTAMPTZ NOT NULL,
    indexed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (repo_id, ref_kind, name)
);

CREATE INDEX IF NOT EXISTS content_files_repo_path_idx
    ON content_files (repo_id, relative_path);
CREATE INDEX IF NOT EXISTS content_entities_repo_idx
    ON content_entities (repo_id);
CREATE INDEX IF NOT EXISTS content_entities_type_idx
    ON content_entities (entity_type);
CREATE INDEX IF NOT EXISTS content_entities_path_idx
    ON content_entities (relative_path);
CREATE INDEX IF NOT EXISTS content_file_references_lookup_idx
    ON content_file_references (reference_kind, reference_value, repo_id);
CREATE INDEX IF NOT EXISTS content_file_references_repo_path_idx
    ON content_file_references (repo_id, relative_path);
CREATE INDEX IF NOT EXISTS repository_refs_repo_idx
    ON repository_refs (repo_id, ref_kind, name);
CREATE INDEX IF NOT EXISTS repository_refs_repo_default_idx
    ON repository_refs (repo_id, is_default, name);
`

// contentStoreSearchIndexSchemaSQL defines four load-bearing pg_trgm GIN
// indexes. The file-content, relative-path, and entity-source indexes bound
// all-repository source searches; the entity-name index bounds exact and
// substring global name searches. Cold bootstrap defers all four until the
// write-heavy drain ends.
// See #4862, #4980, #5318, evidence-4980-deferred-content-gin.md, and the #5318
// evidence note for the measured index roles.
const contentFilesSearchIndexSchemaSQL = `CREATE INDEX IF NOT EXISTS content_files_content_trgm_idx
    ON content_files USING gin (content gin_trgm_ops);
`

const contentEntitiesSearchIndexSchemaSQL = `CREATE INDEX IF NOT EXISTS content_entities_source_trgm_idx
    ON content_entities USING gin (source_cache gin_trgm_ops);
`

const contentEntityNamesSearchIndexSchemaSQL = `CREATE INDEX IF NOT EXISTS content_entities_name_trgm_idx
    ON content_entities USING gin (entity_name gin_trgm_ops);
`

const contentFilesRelativePathSearchIndexSchemaSQL = `CREATE INDEX IF NOT EXISTS content_files_relative_path_trgm_idx
    ON content_files USING gin (relative_path gin_trgm_ops);
`

const contentStoreSearchIndexSchemaSQL = contentFilesSearchIndexSchemaSQL + contentEntitiesSearchIndexSchemaSQL + contentEntityNamesSearchIndexSchemaSQL + contentFilesRelativePathSearchIndexSchemaSQL

const contentStoreFilterIndexSchemaSQL = `CREATE INDEX IF NOT EXISTS content_files_artifact_type_idx
    ON content_files (artifact_type);
CREATE INDEX IF NOT EXISTS content_files_template_dialect_idx
    ON content_files (template_dialect);
CREATE INDEX IF NOT EXISTS content_files_iac_relevant_idx
    ON content_files (iac_relevant);
CREATE INDEX IF NOT EXISTS content_files_language_repo_idx
    ON content_files (language, repo_id) INCLUDE (indexed_at);
CREATE INDEX IF NOT EXISTS content_entities_artifact_type_idx
    ON content_entities (artifact_type);
CREATE INDEX IF NOT EXISTS content_entities_template_dialect_idx
    ON content_entities (template_dialect);
CREATE INDEX IF NOT EXISTS content_entities_iac_relevant_idx
    ON content_entities (iac_relevant);
`

const contentStoreSchemaWithoutSearchIndexesSQL = contentStoreBaseSchemaSQL + contentStoreFilterIndexSchemaSQL

// EnsureContentSearchIndexes creates and validates the trigram indexes that
// accelerate content file and path plus entity source and name search. A
// transaction-scoped advisory lock serializes the complete finalization
// lifecycle, so concurrent finalizers wait and then recheck durable readiness
// instead of racing DDL.
func EnsureContentSearchIndexes(ctx context.Context, database db.Beginner) error {
	if database == nil {
		return fmt.Errorf("executor is required")
	}
	claimed, err := claimContentSearchIndexBuild(ctx, database)
	if err != nil {
		return errors.Join(err, markContentSearchIndexBuildFailed(database))
	}
	if !claimed {
		return nil
	}

	tx, err := database.Begin(ctx)
	if err != nil {
		return errors.Join(
			fmt.Errorf("begin content search index finalization: %w", err),
			markContentSearchIndexBuildFailed(database),
		)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, contentSearchIndexFinalizerLockSQL); err != nil {
		_ = tx.Rollback()
		return errors.Join(
			fmt.Errorf("lock content search index finalization: %w", err),
			markContentSearchIndexBuildFailed(database),
		)
	}
	if err := ensureContentSearchIndexesInTransaction(ctx, tx); err != nil {
		_ = tx.Rollback()
		return errors.Join(err, markContentSearchIndexBuildFailed(database))
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return errors.Join(
			fmt.Errorf("commit content search index finalization: %w", err),
			markContentSearchIndexBuildFailed(database),
		)
	}
	return nil
}

func claimContentSearchIndexBuild(ctx context.Context, database db.Beginner) (bool, error) {
	tx, err := database.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin content search index build claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, contentSearchIndexFinalizerLockSQL); err != nil {
		return false, fmt.Errorf("lock content search index build claim: %w", err)
	}
	claim, err := tx.ExecContext(ctx, contentSearchIndexClaimBuildSQL)
	if err != nil {
		return false, fmt.Errorf("claim content search index build: %w", err)
	}
	claimed, err := claim.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read content search index build claim: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit content search index build claim: %w", err)
	}
	return claimed == 1, nil
}

func ensureContentSearchIndexesInTransaction(ctx context.Context, exec db.Executor) error {
	claim, err := exec.ExecContext(ctx, contentSearchIndexClaimBuildSQL)
	if err != nil {
		return fmt.Errorf("claim content search index build: %w", err)
	}
	claimed, err := claim.RowsAffected()
	if err != nil {
		return fmt.Errorf("read content search index build claim: %w", err)
	}
	if claimed == 0 {
		return nil
	}

	buildSteps := []struct {
		name string
		sql  string
	}{
		{name: "content_files", sql: contentFilesSearchIndexSchemaSQL},
		{name: "content_entities", sql: contentEntitiesSearchIndexSchemaSQL},
		{name: "content_entity_names", sql: contentEntityNamesSearchIndexSchemaSQL},
		{name: "content_files_relative_path", sql: contentFilesRelativePathSearchIndexSchemaSQL},
		{name: "analyze", sql: "ANALYZE content_files; ANALYZE content_entities;"},
	}
	for _, step := range buildSteps {
		if _, err := exec.ExecContext(ctx, step.sql); err != nil {
			return fmt.Errorf("build content search index %s: %w", step.name, err)
		}
	}

	ready, err := exec.ExecContext(ctx, contentSearchIndexPublishReadySQL)
	if err != nil {
		return fmt.Errorf("publish content search indexes ready: %w", err)
	}
	published, err := ready.RowsAffected()
	if err != nil {
		return fmt.Errorf("read content search index ready publication: %w", err)
	}
	if published != 1 {
		return fmt.Errorf("publish content search indexes ready: exact valid indexes not found")
	}
	return nil
}

func markContentSearchIndexBuildFailed(database db.Beginner) error {
	failedCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := database.Begin(failedCtx)
	if err != nil {
		return fmt.Errorf("begin content search index failure publication: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(failedCtx, contentSearchIndexFinalizerLockSQL); err != nil {
		return fmt.Errorf("lock content search index failure publication: %w", err)
	}
	if _, err := tx.ExecContext(failedCtx, contentSearchIndexPublishFailedSQL); err != nil {
		return fmt.Errorf("publish content search indexes failed: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit content search index failure publication: %w", err)
	}
	return nil
}
