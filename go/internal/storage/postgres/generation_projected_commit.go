// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// deltaSyncBaselineQuery reads what a git delta sync diffs from (#8780), in
// one statement and so one snapshot, always as exactly one row:
//
//   - active_*: the scope's active generation, the one whose content the
//     graph holds. Its commit is the #7319 fence commit a delta records as
//     delta_baseline_commit_sha. Projector Ack supersedes the previous active
//     generation and activates the new one in one transaction, so a reader
//     sees exactly one of them. A generation superseded while still pending
//     never activated (#7317), so it is never the active row. When no
//     generation is active the columns are empty and the caller takes the
//     full-snapshot path. Served by the partial unique index
//     scope_generations_active_scope_idx as a single-row lookup.
//   - newest_full_commit_sha: the newest activated full generation's commit,
//     the union base the delta's changed set is diffed from. It uses the
//     projected_full rule of fullReconcileStateQuery. Generation retention's
//     delta_base CTE keeps the same row for a delta-active scope (#7802)
//     provided no activated full carries a status outside
//     (active,completed,superseded); a mismatch refuses fail-closed
//     to a forced full.
//     An ordered LIMIT 1 read on scope_generations_scope_latest_lookup_idx.
const deltaSyncBaselineQuery = `
SELECT COALESCE(active.source_commit_sha, ''),
       COALESCE(active.is_delta, false),
       COALESCE(projected.source_commit_sha, '')
FROM (SELECT 1) AS one
LEFT JOIN LATERAL (
    SELECT source_commit_sha, is_delta
    FROM scope_generations
    WHERE scope_id = $1
      AND ` + activeGenerationPredicate + `
    LIMIT 1
) AS active ON true
LEFT JOIN LATERAL (
    SELECT projected.source_commit_sha
    FROM scope_generations AS projected
    WHERE projected.scope_id = $1
      AND projected.is_delta = false
      AND projected.activated_at IS NOT NULL
      AND projected.status IN ('active', 'completed', 'superseded')
    ORDER BY projected.ingested_at DESC, projected.generation_id DESC
    LIMIT 1
) AS projected ON true
`

// activeGenerationPredicate selects a scope's active generation. The delta
// sync baseline read above and the projector's delta-baseline fence
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
// not itself active: on the base connection before the commit transaction
// opens, and on the commit transaction for the in-tx recheck (#8750 window).
// An error fails the commit closed.
func activeGenerationForGuard(ctx context.Context, querier db.Queryer, scopeID string) (string, bool, error) {
	rows, err := querier.QueryContext(ctx, successorGuardActiveGenerationQuery, scopeID)
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

// recheckSuccessorPrior closes the #8750 window. The guard's prior is sized
// on the base connection before the commit transaction opens, so a
// same-scope commit or Ack that lands before the scope upsert locks the
// scope row can leave it stale and smaller than the truth: a fail-open. Once
// the row is locked no same-scope generation can be stored or activated and
// the active pointer cannot move until this transaction ends, so the caller
// runs this after upsertIngestionScope and before upsertScopeGeneration
// (after which the successor's own row would be the newest candidate). It
// re-reads on tx what the pre-tx sizing read: the candidate tuple, and the
// active id only when the in-tx candidate is pending. Equal: the pre-tx prior
// stands, at one or two indexed single-row reads. Different: the prior is
// re-sized on tx with the same helper and the in-tx result is used alone,
// never max(pre-tx, in-tx), so the decision is a function of the committed
// state at the lock. Any read or count error fails the commit closed.
func (s IngestionStore) recheckSuccessorPrior(
	ctx context.Context,
	tx db.Transaction,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	prior successorPrior,
) (successorPrior, error) {
	if !backfill.GenerationNeedsDuplicateCheck(generation) || generation.TrustedDeletion {
		return prior, nil
	}
	start := time.Now()
	candidate, found, err := unchangedGenerationOn(ctx, tx, scopeValue.ScopeID)
	if err != nil {
		return successorPrior{}, fmt.Errorf("recheck successor guard candidate on scope %s: %w", scopeValue.ScopeID, err)
	}
	reads := 1
	changed := found != prior.found || (found && !sameGuardCandidate(candidate, prior.candidate))
	var activeID string
	if found && candidate.Status != scope.GenerationStatusActive {
		reads = 2
		var hasActive bool
		activeID, hasActive, err = activeGenerationForGuard(ctx, tx, scopeValue.ScopeID)
		if err != nil {
			return successorPrior{}, fmt.Errorf("recheck successor guard active generation on scope %s: %w", scopeValue.ScopeID, err)
		}
		changed = changed || !prior.activeRead || hasActive != prior.hasActive || activeID != prior.activeID
	}
	attrs := []any{
		slog.Bool("changed", changed),
		slog.Int("recheck_reads", reads),
		slog.String("previous_candidate_generation_id", prior.candidate.GenerationID),
		slog.String("candidate_generation_id", candidate.GenerationID),
		slog.String("previous_active_generation_id", prior.activeID),
		slog.String("active_generation_id", activeID),
	}
	if changed {
		resizeStart := time.Now()
		resized, err := s.successorPriorForGuard(ctx, tx, scopeValue, candidate, found, generation)
		if err != nil {
			return successorPrior{}, fmt.Errorf("re-size successor guard prior on the commit transaction: %w", err)
		}
		prior = resized
		attrs = append(attrs, slog.Float64("resize_duration_seconds", time.Since(resizeStart).Seconds()))
	}
	s.logCommitStage(ctx, scopeValue, generation, "successor_prior_rechecked", start, attrs...)
	return prior, nil
}

// sameGuardCandidate compares the candidate fields the guard's sizing
// depends on: which generation, whether it is active, and whether its
// projector work is live (a wedged pending is not sized next to an active).
func sameGuardCandidate(a, b backfill.UnchangedGenerationCandidate) bool {
	return a.GenerationID == b.GenerationID && a.Status == b.Status && a.WorkLive == b.WorkLive
}

// DeltaSyncBaseline returns scopeID's delta-sync baseline (#8780): the
// active generation's commit and delta flag, and the newest activated full
// generation's commit. A git sync diffs the next delta's changed set from the
// full's commit (the union base the projector overlays), adds every path
// deleted since the active commit, and records the active commit for the
// #7319 fence. Diffing from the generation lifecycle rather than the local
// working-copy HEAD keeps a projection that failed after a checkout advanced
// HEAD from silently skipping its changes. Empty fields drive a full
// snapshot.
//
// A blank scopeID returns a zero baseline without querying so callers can
// probe optimistically. Reachability of either commit in the local checkout
// is the caller's concern: a commit pruned by a shallow fetch is unreachable
// and the caller must fall back to a full snapshot rather than a broken delta.
func (s IngestionStore) DeltaSyncBaseline(ctx context.Context, scopeID string) (scope.DeltaSyncBaseline, error) {
	if s.database == nil || strings.TrimSpace(scopeID) == "" {
		return scope.DeltaSyncBaseline{}, nil
	}
	rows, err := s.database.QueryContext(ctx, deltaSyncBaselineQuery, scopeID)
	if err != nil {
		return scope.DeltaSyncBaseline{}, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return scope.DeltaSyncBaseline{}, rows.Err()
	}
	var baseline scope.DeltaSyncBaseline
	if err := rows.Scan(&baseline.ActiveCommitSHA, &baseline.ActiveIsDelta, &baseline.NewestActivatedFullCommitSHA); err != nil {
		return scope.DeltaSyncBaseline{}, err
	}
	if err := rows.Err(); err != nil {
		return scope.DeltaSyncBaseline{}, err
	}
	baseline.ActiveCommitSHA = strings.TrimSpace(baseline.ActiveCommitSHA)
	baseline.NewestActivatedFullCommitSHA = strings.TrimSpace(baseline.NewestActivatedFullCommitSHA)
	return baseline, nil
}

// fullReconcileStateQuery reads the two facts the reconciliation sweep decides
// on for one scope (#7288), always as exactly one row:
//
//   - last_projected_full_at: the ingest time of the newest full (non-delta)
//     generation that was activated. A generation superseded while pending has
//     activated_at NULL and does not count, so it cannot satisfy the sweep.
//     While that generation is still active, the time its last #7978
//     re-projection was acknowledged (payload reconcile_projected_at) counts
//     when it is later. Its commit (last_projected_full_commit_sha) is the
//     #8780 union base a delta sync diffs from when it reuses this read.
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
    COALESCE(projected_full.source_commit_sha, '') AS last_projected_full_commit_sha,
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
           projected.source_commit_sha,
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
		lastProjectedFullCommit string
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
		&lastProjectedFullAt, &lastProjectedFullCommit, &lastStoredFullAt, &reconcileRequestedAt, &reconcileProjectedAt, &reconcileDeadLetteredAt,
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
		state.LastProjectedFullCommitSHA = strings.TrimSpace(lastProjectedFullCommit)
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
