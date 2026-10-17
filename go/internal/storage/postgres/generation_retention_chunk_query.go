// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// This file holds the over-limit chunk query runners (#7981). They lived in
// generation_retention_row_counts_sql.go until #8300 split that file over the
// 500-line cap: the page reader sizes the batch's deletes, and the prune
// counter scans one chunk prune into its destinations. The deletes run under
// the narrow lock in generation_retention_relock.go.

// queryChunkPage reads one oldest-first #7981 over-limit fact page, a bounded
// read sizing the batch's deletes like the counts above. The deletes run
// under the narrow lock in generation_retention_relock.go.
func queryChunkPage(
	ctx context.Context,
	tx db.Transaction,
	candidate generationRetentionCandidate,
	chunkRows int,
) (retentionChunkPage, error) {
	rows, err := tx.QueryContext(ctx, generationRetentionChunkPageQuery,
		candidate.scopeID, candidate.generationID, chunkRows)
	if err != nil {
		return retentionChunkPage{}, fmt.Errorf("generation retention: page chunk facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var page retentionChunkPage
	for rows.Next() {
		var id, kind, repoID, path, entityID string
		var tombstone bool
		if err := rows.Scan(&id, &kind, &tombstone, &repoID, &path, &entityID); err != nil {
			return retentionChunkPage{}, fmt.Errorf("generation retention: scan chunk page: %w", err)
		}
		page.ids = append(page.ids, id)
		switch {
		case !tombstone && kind == "file" && repoID != "" && path != "":
			page.fileRepos = append(page.fileRepos, repoID)
			page.filePaths = append(page.filePaths, path)
		case !tombstone && kind == "content_entity" && repoID != "" && entityID != "":
			page.entityRepos = append(page.entityRepos, repoID)
			page.entityIDs = append(page.entityIDs, entityID)
		}
	}
	if err := rows.Err(); err != nil {
		return retentionChunkPage{}, fmt.Errorf("generation retention: page chunk facts: %w", err)
	}
	return page, nil
}

// queryChunkPruneCounts runs one chunk content prune and scans its one
// count row into the given destinations.
func queryChunkPruneCounts(
	ctx context.Context,
	tx db.Transaction,
	statement string,
	keysA, keysB []string,
	counts ...*int64,
) error {
	rows, err := tx.QueryContext(ctx, statement, keysA, keysB)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	values := make([]any, len(counts))
	for i, count := range counts {
		values[i] = count
	}
	if !rows.Next() {
		return fmt.Errorf("generation retention: chunk prune returned no count row")
	}
	if err := rows.Scan(values...); err != nil {
		return fmt.Errorf("generation retention: scan chunk prune counts: %w", err)
	}
	return rows.Err()
}
