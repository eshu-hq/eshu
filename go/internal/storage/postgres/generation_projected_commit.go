// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// lastProjectedCommitSHAQuery returns the source commit of the scope's active
// generation: the one generation whose content the graph currently holds.
// Projector Ack supersedes the previous active generation and activates the
// new one in a single transaction, so a reader sees exactly one of them.
//
// A superseded generation is not a safe baseline. A generation superseded
// while still pending never activated and never reached the graph (#7317);
// diffing the next delta against its commit would skip every change between
// the active commit and it. When no generation is active (a failed projection
// can clear the active pointer), the graph content is unknown, so the read
// returns no row and the caller takes the full-snapshot path.
//
// The partial unique index scope_generations_active_scope_idx
// (scope_id) WHERE status = 'active' serves the read as a single-row lookup.
const lastProjectedCommitSHAQuery = `
SELECT source_commit_sha
FROM scope_generations
WHERE scope_id = $1
  AND ` + activeGenerationPredicate + `
LIMIT 1
`

// activeGenerationPredicate selects a scope's active generation. The delta
// baseline read above and the projector's delta-baseline fence
// (deltaBaselineFenceQuery, #7319) share it, so the commit a delta is diffed
// from and the commit the fence compares it with come from the same rule. The
// destructive-successor guard's active read (#8750) uses it too.
const activeGenerationPredicate = `status = 'active'
  AND activated_at IS NOT NULL`

// successorGuardActiveGenerationQuery reads the scope's active generation id
// for the destructive-successor guard (#8750 review F1): the generation whose
// content the graph holds, sized next to a pending duplicate-check candidate.
// It never falls back to a pending row (backfill.ActiveScopeGenerationQuery
// does, through COALESCE over the newest row): sizing the guard on a pending
// generation is the defect this read exists to close. A single-row lookup on
// the partial unique index scope_generations_active_scope_idx.
const successorGuardActiveGenerationQuery = `
SELECT generation_id
FROM scope_generations
WHERE scope_id = $1
  AND ` + activeGenerationPredicate + `
LIMIT 1
`

// activeGenerationForGuard returns the scope's active generation id, or false
// when no generation is active. The guard calls it only when its candidate is
// not itself active, on the base connection before the commit transaction
// opens; an error fails the commit closed.
func (s IngestionStore) activeGenerationForGuard(ctx context.Context, scopeID string) (string, bool, error) {
	rows, err := s.database.QueryContext(ctx, successorGuardActiveGenerationQuery, scopeID)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return "", false, rows.Err()
	}
	var generationID string
	if err := rows.Scan(&generationID); err != nil {
		return "", false, err
	}
	generationID = strings.TrimSpace(generationID)
	return generationID, generationID != "", rows.Err()
}

// LastProjectedCommitSHA returns the source commit SHA of the active generation
// for scopeID, or an empty string when the scope has no active generation or
// the active generation carries no commit. It is the durable delta-sync
// baseline: diffing the next snapshot against this commit rather than the local
// working-copy HEAD prevents a projection that failed after a checkout advanced
// HEAD from silently skipping its changes. An empty result drives a full
// snapshot.
//
// A blank scopeID returns an empty SHA without querying so callers can probe
// optimistically. Reachability of the returned SHA in the local checkout is
// the caller's concern: a SHA pruned by a shallow fetch is unreachable and the
// caller must fall back to a full snapshot rather than a broken delta.
func (s IngestionStore) LastProjectedCommitSHA(ctx context.Context, scopeID string) (string, error) {
	if s.database == nil || strings.TrimSpace(scopeID) == "" {
		return "", nil
	}

	rows, err := s.database.QueryContext(ctx, lastProjectedCommitSHAQuery, scopeID)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return "", rows.Err()
	}

	var sha sql.NullString
	if err := rows.Scan(&sha); err != nil {
		return "", err
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return strings.TrimSpace(sha.String), nil
}

// fullReconcileStateQuery reads the two facts the reconciliation sweep decides
// on for one scope (#7288), always as exactly one row:
//
//   - last_projected_full_at: the ingest time of the newest full (non-delta)
//     generation that was activated. A generation superseded while pending has
//     activated_at NULL and does not count, so it cannot satisfy the sweep.
//     While that generation is still active, the time its last #7978
//     re-projection was acknowledged (payload reconcile_projected_at) counts
//     when it is later.
//   - last_stored_full_at: that generation's own ingest time, without any
//     re-projection Ack. Only it can satisfy a reindex watermark (a9-125).
//   - active_* (ruling a9-143): the generation ingestion_scopes points at, its
//     delta flag, commit, stamped pre-parse identity, and skip count, read by
//     primary key so the skipped-parse decision sees a delta-active scope.
//   - latest_full_source_commit_sha / latest_full_work_live (ruling a9-126):
//     the newest full's commit and whether its projector work row is pending,
//     retrying, claimed, or running, so the sweep holds a live pending full at
//     the remote head instead of forcing a snapshot the store would skip. The
//     probe uses the canonical projector_ work_item_id, as the candidate read
//     does; refinalize_ rebuild rows never target a pending generation.
//   - reconcile_*: that generation's #7978 re-projection request, Ack, and
//     dead-letter stamps, read only while it is still active, so a request a
//     newer generation fenced off never holds the sweep off.
//   - latest_full_*: the newest full generation of any status, so an in-flight
//     or recently failed full generation can hold the sweep off. With no full
//     generation the LEFT JOIN yields NULLs and latest_full_projected is false
//     (NULL IS NOT NULL), never NULL.
//
// Both probes are ordered LIMIT 1 reads on
// scope_generations_scope_latest_lookup_idx (scope_id, ingested_at DESC,
// generation_id DESC). completed stays in the projected status list for enum
// compatibility; it has no scope_generations writer today.
const fullReconcileStateQuery = `
SELECT
    projected_full.last_projected_full_at,
    projected_full.last_stored_full_at,
    projected_full.reconcile_requested_at,
    projected_full.reconcile_projected_at,
    projected_full.reconcile_dead_lettered_at,
    latest.ingested_at               AS latest_full_at,
    latest.status                    AS latest_full_status,
    latest.activated_at IS NOT NULL  AS latest_full_projected,
    COALESCE(latest.source_commit_sha, '') AS latest_full_source_commit_sha,
    COALESCE(latest.work_live, false) AS latest_full_work_live,
    COALESCE(active.generation_id, '') AS active_generation_id,
    COALESCE(active.is_delta, false) AS active_is_delta,
    COALESCE(active.source_commit_sha, '') AS active_commit_sha,
    COALESCE(active.payload ->> 'preparse_identity', '') AS active_preparse_identity,
    COALESCE((active.payload ->> 'preparse_skips')::int, 0) AS active_preparse_skips,
    COALESCE(active.payload ->> 'stream_dropped_paths', '[]') AS active_stream_dropped_paths,
    COALESCE((active.payload ->> 'stream_dropped_truncated')::boolean, false) AS active_stream_dropped_truncated
FROM (SELECT 1) AS one
LEFT JOIN LATERAL (
    SELECT GREATEST(projected.ingested_at, reconcile.projected_at) AS last_projected_full_at,
           projected.ingested_at     AS last_stored_full_at,
           reconcile.requested_at    AS reconcile_requested_at,
           reconcile.projected_at    AS reconcile_projected_at,
           reconcile.dead_lettered_at AS reconcile_dead_lettered_at
    FROM scope_generations AS projected
    CROSS JOIN LATERAL (
        SELECT
            CASE WHEN projected.status = 'active'
                 THEN (projected.payload ->> 'reconcile_requested_at')::timestamptz END AS requested_at,
            CASE WHEN projected.status = 'active'
                 THEN (projected.payload ->> 'reconcile_projected_at')::timestamptz END AS projected_at,
            CASE WHEN projected.status = 'active'
                 THEN (projected.payload ->> 'reconcile_dead_lettered_at')::timestamptz END AS dead_lettered_at
    ) AS reconcile
    WHERE projected.scope_id = $1
      AND projected.is_delta = false
      AND projected.activated_at IS NOT NULL
      AND projected.status IN ('active', 'completed', 'superseded')
    ORDER BY projected.ingested_at DESC, projected.generation_id DESC
    LIMIT 1
) AS projected_full ON true
LEFT JOIN LATERAL (
    SELECT newest.ingested_at, newest.status, newest.activated_at, newest.source_commit_sha,
           EXISTS (
               SELECT 1
               FROM fact_work_items AS work
               WHERE work.work_item_id = 'projector_' || $1 || '_' || newest.generation_id
                 AND work.status IN ('pending', 'retrying', 'claimed', 'running')
           ) AS work_live
    FROM (
        SELECT attempt.generation_id, attempt.ingested_at, attempt.status,
               attempt.activated_at, attempt.source_commit_sha
        FROM scope_generations AS attempt
        WHERE attempt.scope_id = $1
          AND attempt.is_delta = false
        ORDER BY attempt.ingested_at DESC, attempt.generation_id DESC
        LIMIT 1
    ) AS newest
) AS latest ON true
LEFT JOIN LATERAL (
    SELECT pointed.generation_id, pointed.is_delta, pointed.source_commit_sha, pointed.payload
    FROM ingestion_scopes AS pointer
    JOIN scope_generations AS pointed
      ON pointed.generation_id = pointer.active_generation_id
     AND pointed.scope_id = pointer.scope_id
    WHERE pointer.scope_id = $1
      AND pointed.status = 'active'
) AS active ON true
`

// FullReconcileState returns the reconciliation-sweep view of scopeID's full
// generations: the newest activated full generation and the newest full
// generation of any status. A scope with no full generation returns a zero
// state (both Has flags false), which the sweep treats as never reconciled. A
// blank scopeID returns a zero state without querying.
func (s IngestionStore) FullReconcileState(ctx context.Context, scopeID string) (scope.FullReconcileState, error) {
	if s.database == nil || strings.TrimSpace(scopeID) == "" {
		return scope.FullReconcileState{}, nil
	}

	rows, err := s.database.QueryContext(ctx, fullReconcileStateQuery, scopeID)
	if err != nil {
		return scope.FullReconcileState{}, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return scope.FullReconcileState{}, rows.Err()
	}

	var (
		lastProjectedFullAt     sql.NullTime
		lastStoredFullAt        sql.NullTime
		reconcileRequestedAt    sql.NullTime
		reconcileProjectedAt    sql.NullTime
		reconcileDeadLetteredAt sql.NullTime
		latestFullAt            sql.NullTime
		latestFullStatus        sql.NullString
		latestFullProjected     bool
		latestFullCommit        string
		latestFullWorkLive      bool
		activeStreamDropped     string
		active                  scope.FullReconcileState
	)
	if err := rows.Scan(
		&lastProjectedFullAt, &lastStoredFullAt, &reconcileRequestedAt, &reconcileProjectedAt, &reconcileDeadLetteredAt,
		&latestFullAt, &latestFullStatus, &latestFullProjected, &latestFullCommit, &latestFullWorkLive,
		&active.ActiveGenerationID, &active.ActiveIsDelta, &active.ActiveCommitSHA,
		&active.ActivePreparseIdentity, &active.ActivePreparseSkips, &activeStreamDropped,
		&active.ActiveStreamDroppedTruncated,
	); err != nil {
		return scope.FullReconcileState{}, err
	}
	if err := rows.Err(); err != nil {
		return scope.FullReconcileState{}, err
	}

	state := scope.FullReconcileState{
		HasProjectedFull: lastProjectedFullAt.Valid,
		HasLatestFull:    latestFullAt.Valid,
	}
	if lastProjectedFullAt.Valid {
		state.LastProjectedFullAt = lastProjectedFullAt.Time.UTC()
	}
	if lastStoredFullAt.Valid {
		state.LastStoredFullAt = lastStoredFullAt.Time.UTC()
	}
	if reconcileRequestedAt.Valid {
		state.ReconcileRequestedAt = reconcileRequestedAt.Time.UTC()
	}
	if reconcileProjectedAt.Valid {
		state.ReconcileProjectedAt = reconcileProjectedAt.Time.UTC()
	}
	if reconcileDeadLetteredAt.Valid {
		state.ReconcileDeadLetteredAt = reconcileDeadLetteredAt.Time.UTC()
	}
	if latestFullAt.Valid {
		state.LatestFullAt = latestFullAt.Time.UTC()
		state.LatestFullStatus = scope.GenerationStatus(latestFullStatus.String)
		state.LatestFullProjected = latestFullProjected
		state.LatestFullSourceCommitSHA = strings.TrimSpace(latestFullCommit)
		state.LatestFullWorkLive = latestFullWorkLive
	}
	state.ActiveGenerationID = strings.TrimSpace(active.ActiveGenerationID)
	state.ActiveIsDelta = active.ActiveIsDelta
	state.ActiveCommitSHA = strings.TrimSpace(active.ActiveCommitSHA)
	state.ActivePreparseIdentity = strings.TrimSpace(active.ActivePreparseIdentity)
	state.ActivePreparseSkips = active.ActivePreparseSkips
	// A malformed stream_dropped_paths payload degrades to no recorded drops
	// rather than failing the sweep's state read; the snapshot then follows
	// the normal identity rules.
	var dropped []string
	if err := json.Unmarshal([]byte(activeStreamDropped), &dropped); err == nil {
		state.ActiveStreamDroppedPaths = scope.NormalizeStreamDroppedPaths(dropped)
	}
	return state, nil
}

// uncoveredProjectionWritersQuery finds the generations of one scope that
// started writing the canonical graph and never activated (#7389): superseded
// or failed rows with projection_write_started_at set and activated_at NULL,
// whose write started after the write start of the newest activated full
// generation. That full generation rewrote the whole tree after the overlay
// landed, so it covers every earlier writer. The comparison is the exact write
// start on both sides, never ingested_at or activated_at. A newest activated
// full with no write start (written before migration 149), or no activated
// full at all, compares as -infinity, so every uncovered writer is returned
// and the scope fails closed to a full snapshot. Pending generations are not
// returned: they can still activate.
//
// No index: the rows are one scope's generations, read through the existing
// scope indexes; p99 0.587 ms on a 1,500-generation scope, and the failure
// class lookup runs only for returned rows
// (docs/internal/evidence/7389-superseded-writer-overlay.md).
const uncoveredProjectionWritersQuery = `
WITH last_full AS (
    SELECT projection_write_started_at AS pws
    FROM scope_generations
    WHERE scope_id = $1
      AND is_delta = false
      AND activated_at IS NOT NULL
    ORDER BY ingested_at DESC, generation_id DESC
    LIMIT 1
)
SELECT generation.generation_id,
       generation.status,
       generation.projection_write_started_at,
       COALESCE(work.failure_class, '')
FROM scope_generations AS generation
LEFT JOIN LATERAL (
    SELECT failure_class
    FROM fact_work_items
    WHERE stage = 'projector'
      AND scope_id = generation.scope_id
      AND generation_id = generation.generation_id
    ORDER BY updated_at DESC
    LIMIT 1
) AS work ON true
WHERE generation.scope_id = $1
  AND generation.projection_write_started_at IS NOT NULL
  AND generation.activated_at IS NULL
  AND generation.status IN ('superseded', 'failed')
  AND generation.projection_write_started_at >
      COALESCE((SELECT pws FROM last_full), '-infinity'::timestamptz)
ORDER BY generation.projection_write_started_at, generation.generation_id
`

// UncoveredProjectionWriters returns scopeID's generations that wrote the
// canonical graph, never activated, and are not covered by a later activated
// full generation (#7389), oldest write first. A non-empty result means the
// graph may hold an overlay the active generation does not describe, so the
// next git sync must take a full snapshot. A blank scopeID returns nil without
// querying; a read error is returned for the caller to fail closed on.
func (s IngestionStore) UncoveredProjectionWriters(ctx context.Context, scopeID string) ([]scope.UncoveredProjectionWriter, error) {
	if s.database == nil || strings.TrimSpace(scopeID) == "" {
		return nil, nil
	}
	rows, err := s.database.QueryContext(ctx, uncoveredProjectionWritersQuery, scopeID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var writers []scope.UncoveredProjectionWriter
	for rows.Next() {
		var writer scope.UncoveredProjectionWriter
		var status string
		if err := rows.Scan(&writer.GenerationID, &status, &writer.ProjectionWriteStartedAt, &writer.FailureClass); err != nil {
			return nil, err
		}
		writer.Status = scope.GenerationStatus(status)
		writer.ProjectionWriteStartedAt = writer.ProjectionWriteStartedAt.UTC()
		writers = append(writers, writer)
	}
	return writers, rows.Err()
}
