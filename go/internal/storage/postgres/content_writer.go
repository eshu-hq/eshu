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

// Content writer SQL constants and column counts live in
// content_writer_sql.go so this file stays focused on the ContentWriter
// behavior (struct, setters, Write, batch helpers).

// ContentWriter persists repo-local content rows into the canonical content store.
type ContentWriter struct {
	db               ExecQueryer
	entityBatchSize  int
	batchConcurrency int
	Now              func() time.Time
	Logger           *slog.Logger
}

// NewContentWriter constructs a Postgres-backed canonical content writer.
// Batch concurrency is resolved once here so a long-running ingester does not
// pick up live env changes mid-run; callers that want to override pass
// WithBatchConcurrency after construction.
func NewContentWriter(db ExecQueryer) ContentWriter {
	return ContentWriter{
		db:               db,
		batchConcurrency: contentWriterBatchConcurrencyFromEnv(),
	}
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

// WithBatchConcurrency returns a copy that runs content-entity upsert batches
// with the given worker fan-out. Zero or negative falls back to the
// env/runtime default resolved at construction time. The package-level cap
// in contentWriterBatchConcurrencyCap applies regardless of the value passed.
func (w ContentWriter) WithBatchConcurrency(workers int) ContentWriter {
	if workers > 0 {
		if workers > contentWriterBatchConcurrencyCap {
			workers = contentWriterBatchConcurrencyCap
		}
		w.batchConcurrency = workers
	}
	return w
}

// effectiveBatchConcurrency returns the worker count to use for content-entity
// upsert fan-out, falling back to the env/runtime default if no value was set
// at construction.
func (w ContentWriter) effectiveBatchConcurrency() int {
	if w.batchConcurrency > 0 {
		return w.batchConcurrency
	}
	return contentWriterDefaultBatchConcurrency()
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
	// Deduplicate by entity_id before fan-out. content_entities has
	// entity_id as its PRIMARY KEY (see upsertContentEntityQuery's
	// ON CONFLICT (entity_id) clause in content_writer_sql.go), so
	// duplicate entity_id rows in entityUpserts would either trip
	// SQLSTATE 21000 inside one batch ("ON CONFLICT DO UPDATE command
	// cannot affect row a second time") or produce race-determined
	// last-writer-wins across parallel batches. Mirror deduplicateEnvelopes
	// (facts.go) by keeping the last occurrence so callers see the same
	// "later in input wins" outcome the prior serial path achieved via
	// row-level lock contention.
	entityUpserts = deduplicateEntityRows(entityUpserts)
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
		"batch_concurrency", w.effectiveBatchConcurrency(),
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

// upsertContentFileBatches and upsertContentEntityBatches live in
// content_writer_upserts.go so this file stays focused on the
// ContentWriter type, Write, and small helpers.

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
