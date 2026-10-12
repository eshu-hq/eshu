-- 7759 shim tables: tables-only half of the scratch schema.
-- Pair with indexes.sql for the full setup; schema.sql holds the same
-- tables plus the full 004/035/062/077/104/107/132/164 index set in one file.
-- Tables from migrations/004_content_store.sql.
-- Non-CONCURRENTLY builds: same definitions, faster on a scratch box.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE content_entities (
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

CREATE TABLE content_files (
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
