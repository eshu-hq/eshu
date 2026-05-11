package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// upsertContentFileBatches persists file records using batched multi-row
// INSERT statements. File batches stay serial: each batch deletes
// content_reference rows for its paths immediately before the file INSERT,
// and the existing exec-order test (TestContentWriterBatchesLargeFileSet)
// asserts the strict [delete, insert] interleaving per batch. The serial
// loop costs little at repo scale (Kubernetes had ~40 file batches), so
// the parallel-batch optimization is reserved for the entity path where
// the K8s gate miss actually lives.
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

// upsertContentEntityBatches persists entity records using batched multi-row
// INSERT statements. Batches are dispatched to a bounded worker pool so a
// repo-scale projection (e.g. 463k Kubernetes content_entity rows produced
// 1,544 batches of 300 rows; serial wall ~9.3 min before #161 follow-up)
// is not serialized behind a single Postgres connection. Each batch is one
// INSERT ... ON CONFLICT and entity_id is unique per
// repo_id+path+kind+identifier within a Materialization, so concurrent
// batches do not contend on the same row. See runConcurrentBatches in
// content_writer_batch.go for the worker-count and safety contract.
func (w ContentWriter) upsertContentEntityBatches(ctx context.Context, rows []preparedEntityRow, indexedAt time.Time) error {
	batchSize := w.effectiveEntityBatchSize()
	return runConcurrentBatches(ctx, len(rows), batchSize, w.effectiveBatchConcurrency(), func(c context.Context, start, end int) error {
		return w.upsertContentEntityBatch(c, rows[start:end], indexedAt)
	})
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
