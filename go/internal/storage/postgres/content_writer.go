package postgres

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
)

const (
	contentFileBatchSize    = 500
	columnsPerContentFile   = 11
	contentEntityBatchSize  = 300 // 16 columns × 300 = 4800 params, under 65535
	columnsPerContentEntity = 16
)

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

const deleteContentFileQuery = `
DELETE FROM content_files
WHERE repo_id = $1
  AND relative_path = $2
`

const deleteContentEntityQuery = `
DELETE FROM content_entities
WHERE repo_id = $1
  AND relative_path = $2
`

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

// ContentWriter persists repo-local content rows into the canonical content store.
type ContentWriter struct {
	db              ExecQueryer
	entityBatchSize int
	Now             func() time.Time
	Logger          *slog.Logger
}

// NewContentWriter constructs a Postgres-backed canonical content writer.
func NewContentWriter(db ExecQueryer) ContentWriter {
	return ContentWriter{db: db}
}

// WithLogger returns a copy that emits per-stage write timings to logger.
func (w ContentWriter) WithLogger(logger *slog.Logger) ContentWriter {
	w.Logger = logger
	return w
}

// WithEntityBatchSize returns a copy that uses size for content-entity upsert batches.
func (w ContentWriter) WithEntityBatchSize(size int) ContentWriter {
	if size > 0 {
		w.entityBatchSize = size
	}
	return w
}

// preparedFileRow holds prepared file record values for batched insertion.
type preparedFileRow struct {
	repoID          string
	path            string
	commitSHA       any
	body            string
	contentHash     string
	lineCount       int
	language        any
	artifactType    any
	templateDialect any
	iacRelevant     any
}

// preparedEntityRow holds prepared entity record values for batched insertion.
type preparedEntityRow struct {
	entityID        string
	repoID          string
	path            string
	entityType      string
	entityName      string
	startLine       int
	endLine         int
	startByte       any
	endByte         any
	language        any
	artifactType    any
	templateDialect any
	iacRelevant     any
	sourceCache     string
	metadataJSON    []byte
}

// Write persists canonical file and entity rows and removes tombstoned rows.
func (w ContentWriter) Write(ctx context.Context, materialization content.Materialization) (content.Result, error) {
	if w.db == nil {
		return content.Result{}, fmt.Errorf("content writer database is required")
	}

	cloned := materialization.Clone()
	if strings.TrimSpace(cloned.RepoID) == "" {
		return content.Result{}, fmt.Errorf("content materialization repo_id is required")
	}

	indexedAt := w.now()
	filePrepareStart := time.Now()
	result := content.Result{
		ScopeID:      cloned.ScopeID,
		GenerationID: cloned.GenerationID,
		RecordCount:  len(cloned.Records),
		EntityCount:  len(cloned.Entities),
	}

	// Process file records: handle deletes first, then batch upserts
	var fileUpserts []preparedFileRow
	for _, record := range cloned.Records {
		if strings.TrimSpace(record.Path) == "" {
			return content.Result{}, fmt.Errorf("content record path is required")
		}

		if record.Deleted {
			if _, err := w.db.ExecContext(ctx, deleteContentEntityQuery, cloned.RepoID, record.Path); err != nil {
				return content.Result{}, fmt.Errorf("delete content_entities for %q: %w", record.Path, err)
			}
			if err := w.deleteContentReferences(ctx, cloned.RepoID, record.Path); err != nil {
				return content.Result{}, fmt.Errorf("delete content_file_references for %q: %w", record.Path, err)
			}
			if _, err := w.db.ExecContext(ctx, deleteContentFileQuery, cloned.RepoID, record.Path); err != nil {
				return content.Result{}, fmt.Errorf("delete content_files for %q: %w", record.Path, err)
			}
			result.DeletedCount++
			continue
		}

		// Validate and prepare row for batching
		contentHash, err := fileContentHash(record)
		if err != nil {
			return content.Result{}, fmt.Errorf("derive content hash for %q: %w", record.Path, err)
		}

		commitSHA, err := optionalMetadataText(record.Metadata, "commit_sha")
		if err != nil {
			return content.Result{}, fmt.Errorf("commit_sha metadata for %q: %w", record.Path, err)
		}
		language, err := optionalMetadataText(record.Metadata, "language")
		if err != nil {
			return content.Result{}, fmt.Errorf("language metadata for %q: %w", record.Path, err)
		}
		artifactType, err := optionalMetadataText(record.Metadata, "artifact_type")
		if err != nil {
			return content.Result{}, fmt.Errorf("artifact_type metadata for %q: %w", record.Path, err)
		}
		templateDialect, err := optionalMetadataText(record.Metadata, "template_dialect")
		if err != nil {
			return content.Result{}, fmt.Errorf("template_dialect metadata for %q: %w", record.Path, err)
		}
		iacRelevant, err := optionalMetadataBool(record.Metadata, "iac_relevant")
		if err != nil {
			return content.Result{}, fmt.Errorf("iac_relevant metadata for %q: %w", record.Path, err)
		}

		fileUpserts = append(fileUpserts, preparedFileRow{
			repoID:          cloned.RepoID,
			path:            record.Path,
			commitSHA:       commitSHA,
			body:            record.Body,
			contentHash:     contentHash,
			lineCount:       lineCount(record.Body),
			language:        language,
			artifactType:    artifactType,
			templateDialect: templateDialect,
			iacRelevant:     iacRelevant,
		})
	}
	w.logStage(ctx, cloned, "prepare_files", filePrepareStart,
		"row_count", len(fileUpserts),
		"deleted_count", result.DeletedCount,
	)

	// Batch upsert file records
	fileUpsertStart := time.Now()
	if err := w.upsertContentFileBatches(ctx, fileUpserts, indexedAt); err != nil {
		return content.Result{}, err
	}
	w.logStage(ctx, cloned, "upsert_files", fileUpsertStart,
		"row_count", len(fileUpserts),
		"batch_count", contentBatchCount(len(fileUpserts), contentFileBatchSize),
	)

	// Process entity records: handle deletes first, then batch upserts
	entityPrepareStart := time.Now()
	var entityUpserts []preparedEntityRow
	for _, entity := range cloned.Entities {
		if strings.TrimSpace(entity.EntityID) == "" {
			return content.Result{}, fmt.Errorf("content entity id is required")
		}
		if strings.TrimSpace(entity.Path) == "" {
			return content.Result{}, fmt.Errorf("content entity path is required")
		}
		if strings.TrimSpace(entity.EntityType) == "" {
			return content.Result{}, fmt.Errorf("content entity type is required for %q", entity.EntityID)
		}
		if strings.TrimSpace(entity.EntityName) == "" {
			return content.Result{}, fmt.Errorf("content entity name is required for %q", entity.EntityID)
		}
		if entity.StartLine <= 0 {
			return content.Result{}, fmt.Errorf("content entity start line is required for %q", entity.EntityID)
		}

		endLine := entity.EndLine
		if endLine < entity.StartLine {
			endLine = entity.StartLine
		}
		sourceCache := strings.TrimSpace(entity.SourceCache)

		if entity.Deleted {
			if _, err := w.db.ExecContext(
				ctx,
				deleteContentEntityByIDQuery,
				cloned.RepoID,
				entity.EntityID,
			); err != nil {
				return content.Result{}, fmt.Errorf("delete content_entities by entity_id for %q: %w", entity.EntityID, err)
			}
			result.DeletedCount++
			continue
		}

		// Validate and prepare row for batching
		metadataJSON, err := metadataJSON(entity.Metadata)
		if err != nil {
			return content.Result{}, fmt.Errorf("marshal content entity metadata for %q: %w", entity.EntityID, err)
		}

		entityUpserts = append(entityUpserts, preparedEntityRow{
			entityID:        entity.EntityID,
			repoID:          cloned.RepoID,
			path:            entity.Path,
			entityType:      entity.EntityType,
			entityName:      entity.EntityName,
			startLine:       entity.StartLine,
			endLine:         endLine,
			startByte:       optionalInt(entity.StartByte),
			endByte:         optionalInt(entity.EndByte),
			language:        optionalString(entity.Language),
			artifactType:    optionalString(entity.ArtifactType),
			templateDialect: optionalString(entity.TemplateDialect),
			iacRelevant:     optionalBool(entity.IACRelevant),
			sourceCache:     sourceCache,
			metadataJSON:    metadataJSON,
		})
	}
	w.logStage(ctx, cloned, "prepare_entities", entityPrepareStart,
		"row_count", len(entityUpserts),
		"deleted_count", result.DeletedCount,
	)

	// Batch upsert entity records
	entityUpsertStart := time.Now()
	if err := w.upsertContentEntityBatches(ctx, entityUpserts, indexedAt); err != nil {
		return content.Result{}, err
	}
	w.logStage(ctx, cloned, "upsert_entities", entityUpsertStart,
		"row_count", len(entityUpserts),
		"batch_count", contentBatchCount(len(entityUpserts), w.effectiveEntityBatchSize()),
	)

	return result, nil
}

func (w ContentWriter) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

// effectiveEntityBatchSize returns the configured entity batch size or the safe default.
func (w ContentWriter) effectiveEntityBatchSize() int {
	if w.entityBatchSize > 0 {
		return w.entityBatchSize
	}
	return contentEntityBatchSize
}

// logStage records the coarse write-stage ledger for repo-scale content-store diagnosis.
func (w ContentWriter) logStage(
	ctx context.Context,
	materialization content.Materialization,
	stage string,
	start time.Time,
	attrs ...any,
) {
	if w.Logger == nil {
		return
	}
	logAttrs := []any{
		"stage", stage,
		"scope_id", materialization.ScopeID,
		"generation_id", materialization.GenerationID,
		"repo_id", materialization.RepoID,
		"duration_seconds", time.Since(start).Seconds(),
	}
	logAttrs = append(logAttrs, attrs...)
	w.Logger.InfoContext(ctx, "content writer stage completed", logAttrs...)
}

// contentBatchCount returns how many bounded INSERT statements a row set needs.
func contentBatchCount(rowCount, batchSize int) int {
	if rowCount <= 0 || batchSize <= 0 {
		return 0
	}
	return (rowCount + batchSize - 1) / batchSize
}

// upsertContentFileBatches persists file records using batched multi-row INSERT statements.
func (w ContentWriter) upsertContentFileBatches(ctx context.Context, rows []preparedFileRow, indexedAt time.Time) error {
	for i := 0; i < len(rows); i += contentFileBatchSize {
		end := i + contentFileBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := w.upsertContentFileBatch(ctx, rows[i:end], indexedAt); err != nil {
			return err
		}
	}
	return nil
}

// upsertContentFileBatch inserts one batch of file records using a multi-row INSERT query.
func (w ContentWriter) upsertContentFileBatch(ctx context.Context, batch []preparedFileRow, indexedAt time.Time) error {
	if len(batch) == 0 {
		return nil
	}

	if err := w.deleteContentReferenceBatch(ctx, batch); err != nil {
		return err
	}

	args := make([]any, 0, len(batch)*columnsPerContentFile)
	var values strings.Builder

	for i, row := range batch {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * columnsPerContentFile
		fmt.Fprintf(&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5,
			offset+6, offset+7, offset+8, offset+9, offset+10, offset+11,
		)

		args = append(args,
			row.repoID,
			row.path,
			row.commitSHA,
			row.body,
			row.contentHash,
			row.lineCount,
			row.language,
			row.artifactType,
			row.templateDialect,
			row.iacRelevant,
			indexedAt,
		)
	}

	query := upsertContentFileBatchPrefix + values.String() + upsertContentFileBatchSuffix

	if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert content_files batch (%d files): %w", len(batch), err)
	}

	if err := w.upsertContentReferenceBatch(ctx, batch, indexedAt); err != nil {
		return err
	}

	return nil
}

// upsertContentEntityBatches persists entity records using batched multi-row INSERT statements.
func (w ContentWriter) upsertContentEntityBatches(ctx context.Context, rows []preparedEntityRow, indexedAt time.Time) error {
	batchSize := w.effectiveEntityBatchSize()
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := w.upsertContentEntityBatch(ctx, rows[i:end], indexedAt); err != nil {
			return err
		}
	}
	return nil
}

// upsertContentEntityBatch inserts one batch of entity records using a multi-row INSERT query.
func (w ContentWriter) upsertContentEntityBatch(ctx context.Context, batch []preparedEntityRow, indexedAt time.Time) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*columnsPerContentEntity)
	var values strings.Builder

	for i, row := range batch {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * columnsPerContentEntity
		fmt.Fprintf(&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d::jsonb, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5,
			offset+6, offset+7, offset+8, offset+9, offset+10,
			offset+11, offset+12, offset+13, offset+14, offset+15, offset+16,
		)

		args = append(args,
			row.entityID,
			row.repoID,
			row.path,
			row.entityType,
			row.entityName,
			row.startLine,
			row.endLine,
			row.startByte,
			row.endByte,
			row.language,
			row.artifactType,
			row.templateDialect,
			row.iacRelevant,
			row.sourceCache,
			row.metadataJSON,
			indexedAt,
		)
	}

	query := upsertContentEntityBatchPrefix + values.String() + upsertContentEntityBatchSuffix

	if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert content_entities batch (%d entities): %w", len(batch), err)
	}

	return nil
}

func fileContentHash(record content.Record) (string, error) {
	if strings.TrimSpace(record.Digest) != "" {
		return record.Digest, nil
	}

	sum := sha1.Sum([]byte(record.Body))
	return hex.EncodeToString(sum[:]), nil
}

func lineCount(contentText string) int {
	if contentText == "" {
		return 0
	}

	count := strings.Count(contentText, "\n")
	if strings.HasSuffix(contentText, "\n") {
		return count
	}

	return count + 1
}

func optionalMetadataText(metadata map[string]string, key string) (any, error) {
	if len(metadata) == 0 {
		return nil, nil
	}

	value, ok := metadata[key]
	if !ok {
		return nil, nil
	}

	text := strings.TrimSpace(value)
	if text == "" {
		return nil, nil
	}

	return text, nil
}

func optionalMetadataBool(metadata map[string]string, key string) (any, error) {
	if len(metadata) == 0 {
		return nil, nil
	}

	value, ok := metadata[key]
	if !ok {
		return nil, nil
	}

	text := strings.TrimSpace(value)
	if text == "" {
		return nil, nil
	}

	parsed, err := strconv.ParseBool(text)
	if err != nil {
		return nil, fmt.Errorf("parse %s %q as bool: %w", key, value, err)
	}

	return parsed, nil
}

func metadataJSON(metadata map[string]any) ([]byte, error) {
	if len(metadata) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(metadata)
}

func optionalString(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}

	return trimmed
}

func optionalInt(value *int) any {
	if value == nil {
		return nil
	}

	return *value
}

func optionalBool(value *bool) any {
	if value == nil {
		return nil
	}

	return *value
}
