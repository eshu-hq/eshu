// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// retentionRepositoriesSQL names every repository with content_entity facts in
// the generations being pruned: the only repositories whose content_entities
// rows the retention prune can delete.
const retentionRepositoriesSQL = `-- retention: orphan repository list
SELECT DISTINCT fact.payload->>'repo_id' AS repo_id
FROM fact_records AS fact
WHERE fact.generation_id = ANY($1::text[])
  AND fact.fact_kind = 'content_entity'
  AND fact.is_tombstone = FALSE
  AND fact.payload->>'repo_id' <> ''`

// retentionLockSQL takes the derive lock of each affected repository in
// repo_id order. A derive holds at most one repository lock at a time, so the
// ordered acquisition here cannot form a cycle with it, and two retention
// batches acquire overlapping sets in the same order.
const retentionLockSQL = `
SELECT count(pg_advisory_xact_lock(hashtextextended('infra_resource_entities:' || repos.repo_id, 0)))
FROM (` + retentionRepositoriesSQL + `
  ORDER BY 1
) AS repos`

// retentionOrphanDeleteByReposSQL keeps the invariant that every table row
// has a content_entities row: after the retention prune deleted content rows,
// drop the given repositories' table rows whose content row is gone. The
// repositories arrive as an array binding ($1) holding exactly the set
// retentionRepositoriesSQL returns for the batch, instead of as an
// IN-subquery: the subquery shape joined the mirror table by filter after a
// sequential scan (183,775 rows plus one content_entities_pkey probe per row,
// plan cost 284,710 on the #10020 fitted batch), while the array binding
// drives the existing (repo_id, relative_path) index, one range per batch
// repository. The delete semantics are unchanged: same repository set, same
// orphan predicate.
const retentionOrphanDeleteByReposSQL = `
DELETE FROM infra_resource_entities AS ire
WHERE ire.repo_id = ANY($1::text[])
  AND NOT EXISTS (
      SELECT 1 FROM content_entities AS ce
      WHERE ce.entity_id = ire.entity_id
  )`

// LockRepositoriesForGenerations takes the per-repository derive lock of every
// repository with content_entity facts in generationIDs. Generation retention
// calls it inside its transaction BEFORE pruning content_entities, so no
// derive can read the pre-prune content rows and write them back after
// DeleteOrphanedRows ran. The locks are released when the retention
// transaction ends.
func LockRepositoriesForGenerations(ctx context.Context, tx db.Executor, generationIDs []string) error {
	if len(generationIDs) == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, retentionLockSQL, array.StringArray(generationIDs)); err != nil {
		return fmt.Errorf("lock infra inventory repositories: %w", err)
	}
	return nil
}

// orphanReposForGenerations lists the repositories with content_entity facts
// in generationIDs: the exact set the orphan delete is scoped to. It runs in
// the caller's transaction, right before the delete, so both statements see
// the same generation facts.
func orphanReposForGenerations(ctx context.Context, q db.Queryer, generationIDs []string) ([]string, error) {
	rows, err := q.QueryContext(ctx, retentionRepositoriesSQL, array.StringArray(generationIDs))
	if err != nil {
		return nil, fmt.Errorf("list orphaned infra inventory repositories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var repos []string
	for rows.Next() {
		var repoID string
		if err := rows.Scan(&repoID); err != nil {
			return nil, fmt.Errorf("scan orphaned infra inventory repository: %w", err)
		}
		repos = append(repos, repoID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list orphaned infra inventory repositories: %w", err)
	}
	return repos, nil
}

// DeleteOrphanedRows removes the table rows of the repositories touched by
// generationIDs whose content_entities row no longer exists. Generation
// retention calls it inside its transaction AFTER pruning content_entities.
// It returns the number of rows deleted.
func DeleteOrphanedRows(ctx context.Context, tx db.ExecQueryer, generationIDs []string) (int64, error) {
	if len(generationIDs) == 0 {
		return 0, nil
	}
	repos, err := orphanReposForGenerations(ctx, tx, generationIDs)
	if err != nil {
		return 0, fmt.Errorf("delete orphaned infra inventory rows: %w", err)
	}
	if len(repos) == 0 {
		return 0, nil
	}
	result, err := tx.ExecContext(ctx, retentionOrphanDeleteByReposSQL, array.StringArray(repos))
	if err != nil {
		return 0, fmt.Errorf("delete orphaned infra inventory rows: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete orphaned infra inventory rows: rows affected: %w", err)
	}
	return deleted, nil
}
