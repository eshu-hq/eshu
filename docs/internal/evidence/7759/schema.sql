-- 7759 shim: scratch schema mirroring production content_entities index set.
-- Tables from migrations/004_content_store.sql; indexes from 004/035/062/077/104/107/132/164.
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

-- 004 set
CREATE INDEX content_entities_repo_idx ON content_entities (repo_id);
CREATE INDEX content_entities_type_idx ON content_entities (entity_type);
CREATE INDEX content_entities_path_idx ON content_entities (relative_path);
CREATE INDEX content_entities_artifact_type_idx ON content_entities (artifact_type);
CREATE INDEX content_entities_template_dialect_idx ON content_entities (template_dialect);
CREATE INDEX content_entities_iac_relevant_idx ON content_entities (iac_relevant);
CREATE INDEX content_entities_source_trgm_idx ON content_entities USING gin (source_cache gin_trgm_ops);
-- 035
CREATE INDEX content_entities_repo_entity_idx ON content_entities (repo_id, entity_id);
-- 062
CREATE INDEX content_entities_name_trgm_idx ON content_entities USING gin (entity_name gin_trgm_ops);
-- 077
CREATE INDEX content_entities_k8s_select_partial_idx
    ON content_entities (repo_id, relative_path, start_line, entity_id)
    INCLUDE (entity_name)
    WHERE entity_type = 'K8sResource';
-- 104
CREATE INDEX content_entities_language_type_idx ON content_entities (language, entity_type);
-- 107
CREATE INDEX content_entities_language_type_path_idx
    ON content_entities (language, entity_type, relative_path, start_line, entity_name);
-- 132
CREATE INDEX content_entities_repo_path_start_idx
    ON content_entities (repo_id, relative_path, start_line, entity_id);
-- 164
CREATE INDEX content_entities_repo_language_type_idx
    ON content_entities (repo_id, language, entity_type);
