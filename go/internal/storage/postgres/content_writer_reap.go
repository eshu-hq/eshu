// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/lib/pq"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// upsertAndReapEntities batch-upserts the fresh entity rows for this Write()
// call, then reaps any content_entities row an identity churn or removal
// left stale. The reap only runs after every fresh row has been written so
// the anti-join never races its own upsert; see reapStaleContentEntities for
// the completeness invariant that makes reaping safe here.
func (w ContentWriter) upsertAndReapEntities(
	ctx context.Context,
	materialization content.Materialization,
	entityUpserts []preparedEntityRow,
	indexedAt time.Time,
) error {
	entityUpsertStart := time.Now()
	if err := w.upsertContentEntityBatches(ctx, entityUpserts, indexedAt); err != nil {
		return err
	}
	w.logStage(
		ctx, materialization, "upsert_entities", entityUpsertStart,
		"row_count", len(entityUpserts),
		"batch_count", contentBatchCount(len(entityUpserts), w.effectiveEntityBatchSize()),
		"batch_concurrency", w.effectiveBatchConcurrency(),
	)

	entityReapStart := time.Now()
	if err := w.reapStaleContentEntities(ctx, materialization.RepoID, entityUpserts); err != nil {
		return err
	}
	w.logStage(
		ctx, materialization, "reap_stale_entities", entityReapStart,
		"fresh_row_count", len(entityUpserts),
	)

	return nil
}

// reapStaleContentEntitiesSQL deletes every content_entities row for a
// reprocessed path whose entity_id is not part of the entity_id set this
// Write() call just upserted for that path.
//
// This is the Postgres anti-join shape (mirroring the #5147/#5327 Cypher
// anti-join sweep, adapted to plain SQL: no relationship-existence predicate
// is involved here, only a NOT-IN-fresh-set comparison): "entity_id <>
// ALL($3)" is the standard SQL negation of membership and is vacuously true
// for every row when the fresh set is empty, so a path whose fresh entity
// count genuinely dropped to zero is fully reaped by the same statement.
//
// Scoped strictly to (repo_id, relative_path) for paths that received at
// least one fresh entity upsert this call — see reapStaleContentEntities for
// why that scope is safe.
const reapStaleContentEntitiesSQL = `
DELETE FROM content_entities
WHERE repo_id = $1
  AND relative_path = ANY($2::text[])
  AND entity_id <> ALL($3::text[])
`

// reapStaleContentEntitiesPathBatchSize bounds how many distinct paths one
// reap DELETE covers. Kept equal to contentFileBatchSize so a repo-scale
// Write() call (tens of thousands of touched files) issues a bounded number
// of reap statements instead of one per path or one unbounded statement.
const reapStaleContentEntitiesPathBatchSize = contentFileBatchSize

// reapStaleContentEntities deletes stale content_entities rows left behind
// when a reprocessed file's entity identity churns — most commonly a
// content.CanonicalEntityID whose hash changed because line_number moved
// (see the JSON dependency line_number fix this reap ships alongside), but
// the same anti-join also reaps a dependency that was removed outright while
// the file kept other entities.
//
// freshRows is the exact deduplicated set Write() is about to have upserted
// (or has just upserted) for this call, grouped here by path. The anti-join
// runs AFTER upsertContentEntityBatches has written every row in freshRows —
// never before — so the delete only ever removes rows this call's own fresh
// set has superseded, not rows a still-in-flight batch is about to write.
//
// Scope invariant (load-bearing — see content_writer.go's Write() doc and the
// "content_entities stale-row reap (#5329)" section of
// go/internal/storage/postgres/README.md for the completeness-invariant proof
// this relies on): every caller that reaches ContentWriter.Write
// gives it the COMPLETE, all-label entity set for a touched file in one call
// — go/internal/collector/git_snapshot_native.go parses a file once (all
// EntityBuckets together) and go/internal/projector's per-generation
// buildProjection loads a generation's full fact set via FactStore.LoadFacts
// before calling Project once. Because of that invariant, grouping freshRows
// by path and reaping only those paths is safe: a path absent from freshRows
// was not touched by this generation, so its rows are correctly left alone,
// and a path present in freshRows has its COMPLETE fresh identity set here,
// not a label-filtered subset. If a future caller ever splits one file's
// entities across two separate Write() calls (label-filtered batches, a
// partial-generation retry that only replays a subset of facts, etc.), this
// reap would over-delete under the #5147/#5327 defect class and MUST NOT be
// enabled for that caller without re-deriving the anti-join from a durable
// per-path fresh-set marker instead of this call's in-memory freshRows.
//
// Known residual gap: a path that legitimately drops to zero entities
// without any tombstone or PurgeEntities signal (entityUpserts carries no
// row for that path at all in this call, only unrelated paths) is not
// reaped here — reapStaleContentEntities only iterates paths that appear in
// freshRows. Scoping to freshRows keeps this reap free for the overwhelming
// majority of touched files that never had entities (docs, config, assets),
// matching entity.Deleted's existing narrower contract. Tracked as a
// follow-on, not fixed in this change.
func (w ContentWriter) reapStaleContentEntities(ctx context.Context, repoID string, freshRows []preparedEntityRow) error {
	if len(freshRows) == 0 {
		return nil
	}

	freshIDsByPath := make(map[string][]string, len(freshRows))
	for _, row := range freshRows {
		freshIDsByPath[row.path] = append(freshIDsByPath[row.path], row.entityID)
	}

	paths := make([]string, 0, len(freshIDsByPath))
	for path := range freshIDsByPath {
		paths = append(paths, path)
	}
	// Deterministic path ordering: production never has two concurrent
	// Write() calls touch the same (repo_id, path) — see the completeness
	// invariant above and the concurrency proof in
	// content_writer_reap_concurrency_test.go — but sorting keeps chunk
	// boundaries and row-lock acquisition order reproducible across runs
	// regardless of Go map iteration order, which is defense-in-depth
	// against a lock-ordering deadlock if that invariant is ever violated.
	sort.Strings(paths)

	for i := 0; i < len(paths); i += reapStaleContentEntitiesPathBatchSize {
		end := i + reapStaleContentEntitiesPathBatchSize
		if end > len(paths) {
			end = len(paths)
		}
		chunkPaths := paths[i:end]

		freshIDCount := 0
		for _, path := range chunkPaths {
			freshIDCount += len(freshIDsByPath[path])
		}
		freshIDs := make([]string, 0, freshIDCount)
		for _, path := range chunkPaths {
			freshIDs = append(freshIDs, freshIDsByPath[path]...)
		}

		if _, err := w.db.ExecContext(
			ctx, reapStaleContentEntitiesSQL,
			repoID, pq.StringArray(chunkPaths), pq.StringArray(freshIDs),
		); err != nil {
			return fmt.Errorf("reap stale content_entities batch (%d paths): %w", len(chunkPaths), err)
		}
	}

	return nil
}
