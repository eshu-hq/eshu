package postgres

// Batch-size and column-count constants for the content writer's multi-row
// INSERT statements. The product must stay under the Postgres 65535
// parameter limit: 500 file rows × 11 columns = 5500 params; 300 entity
// rows × 16 columns = 4800 params; both comfortable.
const (
	contentFileBatchSize    = 500
	columnsPerContentFile   = 11
	contentEntityBatchSize  = 300 // 16 columns × 300 = 4800 params, under 65535
	columnsPerContentEntity = 16
)

// upsertContentFileQuery is the single-row upsert used by callers that
// write one file at a time (not the batched path). Kept alongside the
// batch prefix/suffix so the conflict update list cannot drift between
// the two shapes.
const upsertContentFileQuery = `
INSERT INTO content_files (
    repo_id, relative_path, commit_sha, content, content_hash,
    line_count, language, artifact_type, template_dialect,
    iac_relevant, indexed_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
ON CONFLICT (repo_id, relative_path) DO UPDATE
SET commit_sha = EXCLUDED.commit_sha,
    content = EXCLUDED.content,
    content_hash = EXCLUDED.content_hash,
    line_count = EXCLUDED.line_count,
    language = EXCLUDED.language,
    artifact_type = EXCLUDED.artifact_type,
    template_dialect = EXCLUDED.template_dialect,
    iac_relevant = EXCLUDED.iac_relevant,
    indexed_at = EXCLUDED.indexed_at
`

// upsertContentFileBatchPrefix and upsertContentFileBatchSuffix bracket
// the per-row VALUES list assembled by upsertContentFileBatch; keeping
// the column list and ON CONFLICT clause in one place prevents drift
// between the single-row and multi-row write paths.
const upsertContentFileBatchPrefix = `INSERT INTO content_files (
    repo_id, relative_path, commit_sha, content, content_hash,
    line_count, language, artifact_type, template_dialect,
    iac_relevant, indexed_at
) VALUES `

const upsertContentFileBatchSuffix = `
ON CONFLICT (repo_id, relative_path) DO UPDATE
SET commit_sha = EXCLUDED.commit_sha,
    content = EXCLUDED.content,
    content_hash = EXCLUDED.content_hash,
    line_count = EXCLUDED.line_count,
    language = EXCLUDED.language,
    artifact_type = EXCLUDED.artifact_type,
    template_dialect = EXCLUDED.template_dialect,
    iac_relevant = EXCLUDED.iac_relevant,
    indexed_at = EXCLUDED.indexed_at
`

// deleteContentFileQuery removes one file row for a tombstoned record.
// The matching content_file_references rows are removed separately via
// deleteContentReferences before the file delete fires.
const deleteContentFileQuery = `
DELETE FROM content_files
WHERE repo_id = $1
  AND relative_path = $2
`

// deleteContentEntityQuery removes every entity row attached to one file
// path when the projector reports the file as deleted; deleteContentEntityByIDQuery
// removes a single entity_id when the projector marks one entity tombstoned.
const deleteContentEntityQuery = `
DELETE FROM content_entities
WHERE repo_id = $1
  AND relative_path = $2
`

// upsertContentEntityQuery and the matching batch prefix/suffix below
// share one ON CONFLICT clause keyed on entity_id. Callers must
// deduplicate by entity_id before invoking the batched path; see
// deduplicateEntityRows in content_writer_batch.go.
const upsertContentEntityQuery = `
INSERT INTO content_entities (
    entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, start_byte, end_byte, language,
    artifact_type, template_dialect, iac_relevant,
    source_cache, metadata, indexed_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10,
    $11, $12, $13,
    $14, $15::jsonb, $16
)
ON CONFLICT (entity_id) DO UPDATE
SET repo_id = EXCLUDED.repo_id,
    relative_path = EXCLUDED.relative_path,
    entity_type = EXCLUDED.entity_type,
    entity_name = EXCLUDED.entity_name,
    start_line = EXCLUDED.start_line,
    end_line = EXCLUDED.end_line,
    start_byte = EXCLUDED.start_byte,
    end_byte = EXCLUDED.end_byte,
    language = EXCLUDED.language,
    artifact_type = EXCLUDED.artifact_type,
    template_dialect = EXCLUDED.template_dialect,
    iac_relevant = EXCLUDED.iac_relevant,
    source_cache = EXCLUDED.source_cache,
    metadata = EXCLUDED.metadata,
    indexed_at = EXCLUDED.indexed_at
`

const upsertContentEntityBatchPrefix = `INSERT INTO content_entities (
    entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, start_byte, end_byte, language,
    artifact_type, template_dialect, iac_relevant,
    source_cache, metadata, indexed_at
) VALUES `

const upsertContentEntityBatchSuffix = `
ON CONFLICT (entity_id) DO UPDATE
SET repo_id = EXCLUDED.repo_id,
    relative_path = EXCLUDED.relative_path,
    entity_type = EXCLUDED.entity_type,
    entity_name = EXCLUDED.entity_name,
    start_line = EXCLUDED.start_line,
    end_line = EXCLUDED.end_line,
    start_byte = EXCLUDED.start_byte,
    end_byte = EXCLUDED.end_byte,
    language = EXCLUDED.language,
    artifact_type = EXCLUDED.artifact_type,
    template_dialect = EXCLUDED.template_dialect,
    iac_relevant = EXCLUDED.iac_relevant,
    source_cache = EXCLUDED.source_cache,
    metadata = EXCLUDED.metadata,
    indexed_at = EXCLUDED.indexed_at
`

const deleteContentEntityByIDQuery = `
DELETE FROM content_entities
WHERE repo_id = $1
  AND entity_id = $2
`
