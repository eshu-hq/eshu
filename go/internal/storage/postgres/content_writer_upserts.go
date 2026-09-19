// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha1" // #nosec G505 -- non-cryptographic content-addressing digest for body deduplication, not a security primitive
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
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
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5,
			offset+6, offset+7, offset+8, offset+9, offset+10, offset+11,
		)

		args = append(
			args,
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

	if _, err := w.database.ExecContext(ctx, query, args...); err != nil {
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
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d::jsonb, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5,
			offset+6, offset+7, offset+8, offset+9, offset+10,
			offset+11, offset+12, offset+13, offset+14, offset+15, offset+16,
		)

		args = append(
			args,
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

	if _, err := w.database.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert content_entities batch (%d entities): %w", len(batch), err)
	}

	return nil
}

// deriveInfraInventory re-derives the infra_resource_entities rows (#6793) of
// every path this Write touched: every file record, deleted or not, and every
// entity record, deleted or not. It runs last, after the file and entity
// upserts, the reap, and every tombstone delete, so it reads exactly the
// content_entities state this Write committed. A path whose infra entities
// were all removed therefore ends with no read-model rows, which is how file
// tombstones, PurgeEntities, entity tombstones, and the stale-entity reap all
// reach the table without separate mirror statements.
//
// A derive failure fails the Write. The projector retries the whole
// generation, and the derive is idempotent, so a retry converges.
func (w ContentWriter) deriveInfraInventory(ctx context.Context, materialization content.Materialization) error {
	paths := make([]string, 0, len(materialization.Records)+len(materialization.Entities))
	for _, record := range materialization.Records {
		paths = append(paths, record.Path)
	}
	for _, entity := range materialization.Entities {
		paths = append(paths, entity.Path)
	}
	start := time.Now()
	stats, err := inventory.MirrorPaths(ctx, w.database, inventory.Target{
		RepoID:       materialization.RepoID,
		ScopeID:      materialization.ScopeID,
		GenerationID: materialization.GenerationID,
	}, paths)
	if err != nil {
		return err
	}
	w.logStage(
		ctx, materialization, "derive_infra_inventory", start,
		"path_count", len(paths),
		"rows_deleted", stats.Deleted,
		"rows_inserted", stats.Inserted,
	)
	return nil
}

// Value-normalization helpers shared by ContentWriter.Write: content digests,
// line counts, and the optional-column coercions that turn empty metadata into
// SQL NULL. They live here rather than in content_writer.go to keep that file
// under the 500-line cap.

func fileContentHash(record content.Record) (string, error) {
	if strings.TrimSpace(record.Digest) != "" {
		return record.Digest, nil
	}

	sum := sha1.Sum([]byte(record.Body)) // #nosec G401 -- non-cryptographic body deduplication digest, not a security primitive
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
