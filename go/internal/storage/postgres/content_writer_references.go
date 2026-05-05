package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/contentrefs"
)

const (
	contentReferenceBatchSize    = 500
	columnsPerContentReference   = 5
	deleteContentReferenceQuery  = `DELETE FROM content_file_references WHERE repo_id = $1 AND relative_path = $2`
	upsertContentReferencePrefix = `INSERT INTO content_file_references (
    repo_id, relative_path, reference_kind, reference_value, indexed_at
) VALUES `
	upsertContentReferenceSuffix = `
ON CONFLICT (repo_id, relative_path, reference_kind, reference_value) DO UPDATE
SET indexed_at = EXCLUDED.indexed_at
`
)

// preparedContentReferenceRow is a normalized file-level lookup token derived
// from content during projection, not user-visible graph truth.
type preparedContentReferenceRow struct {
	repoID string
	path   string
	kind   string
	value  string
}

func (w ContentWriter) deleteContentReferences(ctx context.Context, repoID, path string) error {
	if _, err := w.db.ExecContext(ctx, deleteContentReferenceQuery, repoID, path); err != nil {
		return fmt.Errorf("delete content_file_references: %w", err)
	}
	return nil
}

func (w ContentWriter) deleteContentReferenceBatch(ctx context.Context, batch []preparedFileRow) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*2)
	var values strings.Builder
	for i, row := range batch {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * 2
		fmt.Fprintf(&values, "($%d, $%d)", offset+1, offset+2)
		args = append(args, row.repoID, row.path)
	}

	query := "DELETE FROM content_file_references WHERE (repo_id, relative_path) IN (" +
		values.String() + ")"
	if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete stale content_file_references batch (%d files): %w", len(batch), err)
	}
	return nil
}

func (w ContentWriter) upsertContentReferenceBatch(
	ctx context.Context,
	batch []preparedFileRow,
	indexedAt time.Time,
) error {
	references := contentReferencesForFiles(batch)
	for i := 0; i < len(references); i += contentReferenceBatchSize {
		end := i + contentReferenceBatchSize
		if end > len(references) {
			end = len(references)
		}
		if err := w.upsertPreparedContentReferenceBatch(ctx, references[i:end], indexedAt); err != nil {
			return err
		}
	}
	return nil
}

func contentReferencesForFiles(rows []preparedFileRow) []preparedContentReferenceRow {
	references := make([]preparedContentReferenceRow, 0)
	for _, row := range rows {
		for _, hostname := range contentrefs.Hostnames(row.body) {
			references = append(references, preparedContentReferenceRow{
				repoID: row.repoID,
				path:   row.path,
				kind:   "hostname",
				value:  hostname,
			})
		}
		for _, serviceName := range contentrefs.ServiceNames(row.body) {
			references = append(references, preparedContentReferenceRow{
				repoID: row.repoID,
				path:   row.path,
				kind:   "service_name",
				value:  serviceName,
			})
		}
	}
	return references
}

func (w ContentWriter) upsertPreparedContentReferenceBatch(
	ctx context.Context,
	batch []preparedContentReferenceRow,
	indexedAt time.Time,
) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*columnsPerContentReference)
	var values strings.Builder
	for i, row := range batch {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * columnsPerContentReference
		fmt.Fprintf(&values,
			"($%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5,
		)
		args = append(args, row.repoID, row.path, row.kind, row.value, indexedAt)
	}

	query := upsertContentReferencePrefix + values.String() + upsertContentReferenceSuffix
	if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert content_file_references batch (%d references): %w", len(batch), err)
	}
	return nil
}
