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
CREATE INDEX IF NOT EXISTS content_files_content_trgm_idx
    ON content_files USING gin (content gin_trgm_ops);
CREATE INDEX IF NOT EXISTS content_entities_source_trgm_idx
    ON content_entities USING gin (source_cache gin_trgm_ops);
CREATE INDEX IF NOT EXISTS content_files_artifact_type_idx
    ON content_files (artifact_type);
CREATE INDEX IF NOT EXISTS content_files_template_dialect_idx
    ON content_files (template_dialect);
CREATE INDEX IF NOT EXISTS content_files_iac_relevant_idx
    ON content_files (iac_relevant);
CREATE INDEX IF NOT EXISTS content_entities_artifact_type_idx
    ON content_entities (artifact_type);
CREATE INDEX IF NOT EXISTS content_entities_template_dialect_idx
    ON content_entities (template_dialect);
CREATE INDEX IF NOT EXISTS content_entities_iac_relevant_idx
    ON content_entities (iac_relevant);
