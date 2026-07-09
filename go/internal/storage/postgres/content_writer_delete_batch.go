// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
)

// deleteContentEntityPathsBatch removes content_entities rows keyed by
// (repo_id, relative_path) in batches of contentFileBatchSize. Used for
// tombstoned record deletes and PurgeEntities path-scoped entity purges.
func (w ContentWriter) deleteContentEntityPathsBatch(ctx context.Context, repoID string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	for i := 0; i < len(paths); i += contentFileBatchSize {
		end := i + contentFileBatchSize
		if end > len(paths) {
			end = len(paths)
		}
		chunk := paths[i:end]
		if err := w.deleteContentEntityPathsChunk(ctx, repoID, chunk); err != nil {
			return err
		}
	}
	return nil
}

// deleteContentEntityPathsChunk issues one DELETE ... IN (...) for a chunk
// of paths so the parameter count stays bounded.
func (w ContentWriter) deleteContentEntityPathsChunk(ctx context.Context, repoID string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := make([]any, 0, len(paths)*2)
	var values strings.Builder
	for i, path := range paths {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * 2
		fmt.Fprintf(&values, "($%d, $%d)", offset+1, offset+2)
		args = append(args, repoID, path)
	}
	query := "DELETE FROM content_entities WHERE (repo_id, relative_path) IN (" + values.String() + ")"
	if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete content_entities batch by path (%d rows): %w", len(paths), err)
	}
	return nil
}

// deleteContentEntityIDsBatch removes content_entities rows keyed by
// (repo_id, entity_id) in batches of contentFileBatchSize. Used for
// tombstoned entity-record deletes.
func (w ContentWriter) deleteContentEntityIDsBatch(ctx context.Context, repoID string, entityIDs []string) error {
	if len(entityIDs) == 0 {
		return nil
	}
	for i := 0; i < len(entityIDs); i += contentFileBatchSize {
		end := i + contentFileBatchSize
		if end > len(entityIDs) {
			end = len(entityIDs)
		}
		chunk := entityIDs[i:end]
		if err := w.deleteContentEntityIDsChunk(ctx, repoID, chunk); err != nil {
			return err
		}
	}
	return nil
}

// deleteContentEntityIDsChunk issues one DELETE ... IN (...) for a chunk
// of entity_ids so the parameter count stays bounded.
func (w ContentWriter) deleteContentEntityIDsChunk(ctx context.Context, repoID string, entityIDs []string) error {
	if len(entityIDs) == 0 {
		return nil
	}
	args := make([]any, 0, len(entityIDs)*2)
	var values strings.Builder
	for i, eid := range entityIDs {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * 2
		fmt.Fprintf(&values, "($%d, $%d)", offset+1, offset+2)
		args = append(args, repoID, eid)
	}
	query := "DELETE FROM content_entities WHERE (repo_id, entity_id) IN (" + values.String() + ")"
	if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete content_entities batch by entity_id (%d rows): %w", len(entityIDs), err)
	}
	return nil
}

// deleteContentFilesBatch removes content_files rows keyed by
// (repo_id, relative_path) in batches of contentFileBatchSize. Used for
// tombstoned record deletes.
func (w ContentWriter) deleteContentFilesBatch(ctx context.Context, repoID string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	for i := 0; i < len(paths); i += contentFileBatchSize {
		end := i + contentFileBatchSize
		if end > len(paths) {
			end = len(paths)
		}
		chunk := paths[i:end]
		if err := w.deleteContentFilesChunk(ctx, repoID, chunk); err != nil {
			return err
		}
	}
	return nil
}

// deleteContentFilesChunk issues one DELETE ... IN (...) for a chunk of
// paths so the parameter count stays bounded.
func (w ContentWriter) deleteContentFilesChunk(ctx context.Context, repoID string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := make([]any, 0, len(paths)*2)
	var values strings.Builder
	for i, path := range paths {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * 2
		fmt.Fprintf(&values, "($%d, $%d)", offset+1, offset+2)
		args = append(args, repoID, path)
	}
	query := "DELETE FROM content_files WHERE (repo_id, relative_path) IN (" + values.String() + ")"
	if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete content_files batch (%d rows): %w", len(paths), err)
	}
	return nil
}

// deleteContentReferencePathsBatch removes content_file_references rows keyed by
// (repo_id, relative_path) in batches of contentFileBatchSize. Used for
// tombstoned record deletes. This is the batch equivalent of the per-row
// deleteContentReferenceQuery.
func (w ContentWriter) deleteContentReferencePathsBatch(ctx context.Context, repoID string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	for i := 0; i < len(paths); i += contentFileBatchSize {
		end := i + contentFileBatchSize
		if end > len(paths) {
			end = len(paths)
		}
		chunk := paths[i:end]
		args := make([]any, 0, len(chunk)*2)
		var values strings.Builder
		for j, path := range chunk {
			if j > 0 {
				values.WriteString(", ")
			}
			offset := j * 2
			fmt.Fprintf(&values, "($%d, $%d)", offset+1, offset+2)
			args = append(args, repoID, path)
		}
		query := "DELETE FROM content_file_references WHERE (repo_id, relative_path) IN (" + values.String() + ")"
		if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("delete content_file_references batch by path (%d rows): %w", len(chunk), err)
		}
	}
	return nil
}
