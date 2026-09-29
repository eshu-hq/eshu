// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
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
// from and the commit the fence compares it with come from the same rule.
const activeGenerationPredicate = `status = 'active'
  AND activated_at IS NOT NULL`

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
    (SELECT projected.ingested_at
     FROM scope_generations AS projected
     WHERE projected.scope_id = $1
       AND projected.is_delta = false
       AND projected.activated_at IS NOT NULL
       AND projected.status IN ('active', 'completed', 'superseded')
     ORDER BY projected.ingested_at DESC, projected.generation_id DESC
     LIMIT 1)                        AS last_projected_full_at,
    latest.ingested_at               AS latest_full_at,
    latest.status                    AS latest_full_status,
    latest.activated_at IS NOT NULL  AS latest_full_projected
FROM (SELECT 1) AS one
LEFT JOIN LATERAL (
    SELECT attempt.ingested_at, attempt.status, attempt.activated_at
    FROM scope_generations AS attempt
    WHERE attempt.scope_id = $1
      AND attempt.is_delta = false
    ORDER BY attempt.ingested_at DESC, attempt.generation_id DESC
    LIMIT 1
) AS latest ON true
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
		lastProjectedFullAt sql.NullTime
		latestFullAt        sql.NullTime
		latestFullStatus    sql.NullString
		latestFullProjected bool
	)
	if err := rows.Scan(&lastProjectedFullAt, &latestFullAt, &latestFullStatus, &latestFullProjected); err != nil {
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
	if latestFullAt.Valid {
		state.LatestFullAt = latestFullAt.Time.UTC()
		state.LatestFullStatus = scope.GenerationStatus(latestFullStatus.String)
		state.LatestFullProjected = latestFullProjected
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
