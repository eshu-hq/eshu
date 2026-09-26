// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"

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
) (bool, error) {
	entityUpsertStart := time.Now()
	if err := w.upsertContentEntityBatches(ctx, entityUpserts, indexedAt); err != nil {
		return false, err
	}
	w.logStage(
		ctx, materialization, "upsert_entities", entityUpsertStart,
		"row_count", len(entityUpserts),
		"batch_count", contentBatchCount(len(entityUpserts), w.effectiveEntityBatchSize()),
		"batch_concurrency", w.effectiveBatchConcurrency(),
	)

	// All rows flow to the fingerprint writer, fingerprinted or not: it
	// upserts the former and deletes stale side rows for the latter
	// (withdrawn) in the same call.
	fingerprintRows := make([]preparedFingerprintRow, 0, len(entityUpserts))
	for _, row := range entityUpserts {
		fingerprintRows = append(fingerprintRows, row.fingerprint)
	}
	fingerprintUpsertStart := time.Now()
	fingerprintsChanged, err := w.upsertFingerprintBatches(ctx, fingerprintRows, indexedAt)
	if err != nil {
		return false, err
	}
	w.logStage(
		ctx, materialization, "upsert_fingerprints", fingerprintUpsertStart,
		"row_count", len(fingerprintRows),
	)

	entityReapStart := time.Now()
	if err := w.reapStaleContentEntities(ctx, materialization.RepoID, entityUpserts); err != nil {
		return false, err
	}
	w.logStage(
		ctx, materialization, "reap_stale_entities", entityReapStart,
		"fresh_row_count", len(entityUpserts),
	)

	fingerprintReapStart := time.Now()
	reap, err := w.reapStaleFingerprints(ctx, materialization.RepoID)
	if err != nil {
		return false, err
	}
	w.logStage(
		ctx, materialization, "reap_stale_fingerprints", fingerprintReapStart,
		"stale_fingerprint_entities", reap.staleFingerprintEntities,
		"stale_band_entities", reap.staleBandEntities,
		"fingerprint_rows_deleted", reap.fingerprintRowsDeleted,
		"band_rows_deleted", reap.bandRowsDeleted,
	)

	return fingerprintsChanged || reap.changed(), nil
}

// staleFingerprintReapChunkSize bounds the entity ids one stale side-row
// delete carries. The band delete scans the repository's band rows once per
// chunk and probes a hash of the chunk, so a larger chunk means fewer scans;
// 5,000 of the 29-byte content-entity ids is about 150 KB of parameter, and
// the chunk's hash stays far under hash_mem at any statistics estimate.
const staleFingerprintReapChunkSize = 5000

// fingerprintReap reports what reapStaleFingerprints found and removed, for
// the reap_stale_fingerprints stage log and the #6837 trigger signal.
type fingerprintReap struct {
	staleFingerprintEntities int
	staleBandEntities        int
	fingerprintRowsDeleted   int64
	bandRowsDeleted          int64
}

// changed reports whether the reap removed any side-table row.
func (r fingerprintReap) changed() bool {
	return r.fingerprintRowsDeleted > 0 || r.bandRowsDeleted > 0
}

// reapStaleFingerprints deletes code_function_fingerprint and
// code_fingerprint_band rows whose entity no longer exists in
// content_entities for the repo. It runs after the entity upsert+reap in the
// same Write call, so tombstoned, churned, and path-reaped entities all
// converge here.
//
// Each table is reaped in two steps: a join-free set-difference read of the
// stale entity ids (see staleFingerprintEntityIDsSQL for why it is not an
// anti-join), then chunked deletes of exactly those ids. The common case,
// including every first generation, reads nothing and issues no delete. The
// deleted rows are the rows the former single-statement NOT EXISTS reaps
// deleted: a row goes iff its entity_id has no content_entities row in the
// repo. The read and the delete are separate statements, which is safe for
// the reason the entity reap above is: no two Write calls for one repository
// run at once (claimProjectorWorkQuery's scope_id NOT EXISTS guard), so
// nothing can re-create a stale entity between them.
func (w ContentWriter) reapStaleFingerprints(ctx context.Context, repoID string) (fingerprintReap, error) {
	var reap fingerprintReap

	staleIDs, err := w.queryStaleFingerprintEntityIDs(ctx, staleFingerprintEntityIDsSQL, repoID)
	if err != nil {
		return fingerprintReap{}, fmt.Errorf("read stale code_function_fingerprint entities: %w", err)
	}
	reap.staleFingerprintEntities = len(staleIDs)
	for _, chunk := range chunkStaleFingerprintIDs(staleIDs) {
		res, err := w.database.ExecContext(ctx, deleteWithdrawnFingerprintSQL, repoID, array.StringArray(chunk))
		if err != nil {
			return fingerprintReap{}, fmt.Errorf("reap %d stale code_function_fingerprint entities: %w", len(chunk), err)
		}
		reap.fingerprintRowsDeleted += rowsAffected(res)
	}

	staleIDs, err = w.queryStaleFingerprintEntityIDs(ctx, staleFingerprintBandEntityIDsSQL, repoID)
	if err != nil {
		return fingerprintReap{}, fmt.Errorf("read stale code_fingerprint_band entities: %w", err)
	}
	reap.staleBandEntities = len(staleIDs)
	for _, chunk := range chunkStaleFingerprintIDs(staleIDs) {
		res, err := w.database.ExecContext(ctx, deleteStaleFingerprintBandsSQL, repoID, array.StringArray(chunk))
		if err != nil {
			return fingerprintReap{}, fmt.Errorf("reap %d stale code_fingerprint_band entities: %w", len(chunk), err)
		}
		reap.bandRowsDeleted += rowsAffected(res)
	}
	return reap, nil
}

// queryStaleFingerprintEntityIDs runs one stale-id read and returns the ids
// sorted, so delete chunks and their row-lock order are reproducible.
func (w ContentWriter) queryStaleFingerprintEntityIDs(ctx context.Context, query, repoID string) ([]string, error) {
	rows, err := w.database.QueryContext(ctx, query, repoID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(ids)
	return ids, nil
}

// chunkStaleFingerprintIDs splits stale ids into delete chunks. An empty set
// yields no chunk, so a repository with nothing stale issues no delete.
func chunkStaleFingerprintIDs(ids []string) [][]string {
	if len(ids) == 0 {
		return nil
	}
	return chunkEntityIDs(ids, staleFingerprintReapChunkSize)
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
// Scope invariant (load-bearing — see content_writer.go's Write() doc for the
// completeness contract this relies on, and the "content_entities stale-row
// reap (#5329)" section of go/internal/storage/postgres/README.md for the
// full proof): Write()'s doc requires every caller to pass the COMPLETE,
// all-label entity set for a touched file in one call, so grouping freshRows
// by path and reaping only those paths is safe here: a path absent from
// freshRows was not touched by this generation, so its rows are correctly
// left alone, and a path present in freshRows has its COMPLETE fresh identity
// set, not a label-filtered subset. If a future caller ever violates that
// contract (a label-filtered batch, a partial-generation retry that only
// replays a subset of facts, etc.), this reap would over-delete under the
// #5147/#5327 defect class and MUST NOT be enabled for that caller without
// re-deriving the anti-join from a durable per-path fresh-set marker instead
// of this call's in-memory freshRows.
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

		if _, err := w.database.ExecContext(
			ctx, reapStaleContentEntitiesSQL,
			repoID, array.StringArray(chunkPaths), array.StringArray(freshIDs),
		); err != nil {
			return fmt.Errorf("reap stale content_entities batch (%d paths): %w", len(chunkPaths), err)
		}
	}

	return nil
}
