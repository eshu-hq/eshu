// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// This file holds the content-store schema DDL: the base tables and indexes for
// indexed repository content (files, entities, file references, refs) plus the
// search and filter index variants. It is split out of schema.go to keep each
// schema file under the package size limit; the bootstrap assembly and apply
// helpers stay in schema.go and reference these constants.

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

// contentStoreSearchIndexSchemaSQL defines two pg_trgm GIN indexes that are load-bearing
// for the all-repo / code-topic ILIKE '%term%' search read path (investigateCodeTopic
// and the *AnyRepo content readers). The live-audit on an 838-repo drained stack confirmed
// the planner selects both via Bitmap Index Scan. Forcing a sequential scan is ~4.9× slower
// on content_entities, and the gap widens on larger corpora. Issue #4980 disproved replacing
// these exact full-content reads with the curated/tokenized eshu_search_index_* surface.
// Cold bootstrap may defer creation, but steady-state schema must keep both exact indexes.
// See #4862 and evidence-4980-deferred-content-gin.md.
const contentFilesSearchIndexSchemaSQL = `CREATE INDEX IF NOT EXISTS content_files_content_trgm_idx
    ON content_files USING gin (content gin_trgm_ops);
`

const contentEntitiesSearchIndexSchemaSQL = `CREATE INDEX IF NOT EXISTS content_entities_source_trgm_idx
    ON content_entities USING gin (source_cache gin_trgm_ops);
`

const contentStoreSearchIndexSchemaSQL = contentFilesSearchIndexSchemaSQL + contentEntitiesSearchIndexSchemaSQL

const contentStoreFilterIndexSchemaSQL = `CREATE INDEX IF NOT EXISTS content_files_artifact_type_idx
    ON content_files (artifact_type);
CREATE INDEX IF NOT EXISTS content_files_template_dialect_idx
    ON content_files (template_dialect);
CREATE INDEX IF NOT EXISTS content_files_iac_relevant_idx
    ON content_files (iac_relevant);
CREATE INDEX IF NOT EXISTS content_files_language_repo_idx
    ON content_files (language, repo_id);
CREATE INDEX IF NOT EXISTS content_entities_artifact_type_idx
    ON content_entities (artifact_type);
CREATE INDEX IF NOT EXISTS content_entities_template_dialect_idx
    ON content_entities (template_dialect);
CREATE INDEX IF NOT EXISTS content_entities_iac_relevant_idx
    ON content_entities (iac_relevant);
`

const contentStoreSchemaWithoutSearchIndexesSQL = contentStoreBaseSchemaSQL + contentStoreFilterIndexSchemaSQL
