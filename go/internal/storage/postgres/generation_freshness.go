// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// generationFreshnessSQL reads, in one statement (one snapshot), the scope's
// active generation plus the intent generation's status and whether that
// generation sorts after the active one. The order is (ingested_at,
// generation_id): the same order the projector supersession SQL
// (supersedeProjectorObsoleteGenerationsQuery and the running-work heartbeat
// check) and the reducer claim's superseded_stale_reducer_generations CTE use
// to decide which generation wins, so "newer" here means "the projector will
// let this generation activate over the current one". Every join is a primary
// key lookup (ingestion_scopes.scope_id, scope_generations.generation_id). An
// unknown scope returns no rows.
const generationFreshnessSQL = `
SELECT scope.active_generation_id,
       intent_generation.status,
       COALESCE(
           (intent_generation.ingested_at, intent_generation.generation_id)
               > (active_generation.ingested_at, active_generation.generation_id),
           false
       ) AS intent_is_newer
FROM ingestion_scopes AS scope
LEFT JOIN scope_generations AS intent_generation
       ON intent_generation.generation_id = $2
      AND intent_generation.scope_id = scope.scope_id
LEFT JOIN scope_generations AS active_generation
       ON active_generation.generation_id = scope.active_generation_id
      AND active_generation.scope_id = scope.scope_id
WHERE scope.scope_id = $1
`

const priorGenerationExistsSQL = `
SELECT EXISTS (
    SELECT 1
    FROM scope_generations
    WHERE scope_id = $1
      AND generation_id <> $2
)
`

// priorGenerationIDSQL resolves the strictly-older generation: the newest
// generation whose (observed_at, generation_id) sorts before the intent's
// own row. A newest-other lookup would return a NEWER generation whenever a
// non-newest intent executes (retry crossing a scan boundary, backfill,
// queue lag), and the diff would then yield newly-created uids as retract
// candidates. A missing intent row (or no older generation) yields no rows,
// which NewPriorGenerationID reports as found=false: the handler skips the
// retract, which is the safe direction.
const priorGenerationIDSQL = `
SELECT generation_id
FROM scope_generations
WHERE scope_id = $1
  AND (observed_at, generation_id) < (
    SELECT observed_at, generation_id
    FROM scope_generations
    WHERE scope_id = $1 AND generation_id = $2
  )
ORDER BY observed_at DESC, generation_id DESC
LIMIT 1
`

// CurrentScopeGeneration reports the newest pending or active generation with
// a non-empty freshness hint for one scope.
type CurrentScopeGeneration struct {
	GenerationID  string
	FreshnessHint string
}

// CurrentScopeGeneration returns the newest pending or active generation with
// a non-empty freshness hint for one scope.
func (s IngestionStore) CurrentScopeGeneration(
	ctx context.Context,
	scopeID string,
) (CurrentScopeGeneration, bool, error) {
	if s.database == nil {
		return CurrentScopeGeneration{}, false, fmt.Errorf("ingestion store db is required")
	}
	rows, err := s.database.QueryContext(ctx, activeGenerationFreshnessQuery, scopeID)
	if err != nil {
		return CurrentScopeGeneration{}, false, fmt.Errorf("query current generation for scope %s: %w", scopeID, err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return CurrentScopeGeneration{}, false, fmt.Errorf("query current generation for scope %s: %w", scopeID, err)
		}
		return CurrentScopeGeneration{}, false, nil
	}

	var current CurrentScopeGeneration
	if err := rows.Scan(&current.GenerationID, &current.FreshnessHint); err != nil {
		return CurrentScopeGeneration{}, false, fmt.Errorf("scan current generation for scope %s: %w", scopeID, err)
	}
	current.GenerationID = strings.TrimSpace(current.GenerationID)
	current.FreshnessHint = strings.TrimSpace(current.FreshnessHint)
	return current, current.GenerationID != "", rows.Err()
}

// NewGenerationFreshnessCheck returns a GenerationFreshnessCheck backed by
// the ingestion_scopes.active_generation_id denormalized column and the
// scope_generations lifecycle row of the intent's generation.
//
// Outcomes:
//   - the intent generation is active, the scope is unknown, or the scope has
//     no active generation and the intent is neither pending nor terminal:
//     (true, nil), and the handler runs;
//   - the scope has no active generation and the intent is pending:
//     (false, reducercontract.GenerationNotYetActiveError) until first Ack;
//   - the scope has no active generation and the intent is failed or superseded:
//     (false, nil), and the intent is terminally superseded;
//   - the intent generation is still pending and sorts after the active
//     generation: (false, reducercontract.GenerationNotYetActiveError). Its
//     projector has enqueued reducer work but not yet acknowledged, so the
//     intent is retried (non-counting) until the generation activates, rather
//     than acked succeeded as superseded and never re-driven (#6686);
//   - anything else (an older, superseded, or failed generation, or a missing
//     generation row): (false, nil), and the intent is terminally superseded.
func NewGenerationFreshnessCheck(database db.ExecQueryer) reducer.GenerationFreshnessCheck {
	return func(ctx context.Context, scopeID, generationID string) (bool, error) {
		rows, err := database.QueryContext(ctx, generationFreshnessSQL, scopeID, generationID)
		if err != nil {
			return false, fmt.Errorf("query active generation for scope %s: %w", scopeID, err)
		}
		defer func() { _ = rows.Close() }()

		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return false, fmt.Errorf("query active generation for scope %s: %w", scopeID, err)
			}
			// Unknown scope — assume current (defensive, let handler decide).
			return true, nil
		}

		var (
			activeGenID   sql.NullString
			intentStatus  sql.NullString
			intentIsNewer bool
		)
		if err := rows.Scan(&activeGenID, &intentStatus, &intentIsNewer); err != nil {
			return false, fmt.Errorf("scan active generation for scope %s: %w", scopeID, err)
		}

		if !activeGenID.Valid {
			if intentStatus.Valid && intentStatus.String == string(scope.GenerationStatusPending) {
				return false, reducercontract.GenerationNotYetActiveError{
					ScopeID:      scopeID,
					GenerationID: generationID,
				}
			}
			if intentStatus.Valid && (intentStatus.String == string(scope.GenerationStatusFailed) ||
				intentStatus.String == string(scope.GenerationStatusSuperseded)) {
				return false, nil
			}
			// Preserve the legacy result for every other no-active state.
			return true, nil
		}
		if activeGenID.String == generationID {
			return true, nil
		}
		if intentStatus.Valid && intentStatus.String == string(scope.GenerationStatusPending) && intentIsNewer {
			return false, reducercontract.GenerationNotYetActiveError{
				ScopeID:            scopeID,
				GenerationID:       generationID,
				ActiveGenerationID: activeGenID.String,
			}
		}
		return false, nil
	}
}

// NewPriorGenerationID returns the predecessor-generation lookup backed by
// scope_generations. The ordering is observation time with the generation id
// as the deterministic tie-break, so concurrent same-timestamp generations
// still resolve one predecessor. A scope with no earlier generation reports
// found=false (first write: no diff source, no retract).
func NewPriorGenerationID(database db.ExecQueryer) reducer.PriorGenerationID {
	return func(ctx context.Context, scopeID, generationID string) (string, bool, error) {
		rows, err := database.QueryContext(ctx, priorGenerationIDSQL, scopeID, generationID)
		if err != nil {
			return "", false, fmt.Errorf("query prior generation id for scope %s: %w", scopeID, err)
		}
		defer func() { _ = rows.Close() }()

		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return "", false, fmt.Errorf("iterate prior generation id for scope %s: %w", scopeID, err)
			}
			return "", false, nil
		}

		var prior string
		if err := rows.Scan(&prior); err != nil {
			return "", false, fmt.Errorf("scan prior generation id for scope %s: %w", scopeID, err)
		}
		if err := rows.Err(); err != nil {
			return "", false, fmt.Errorf("iterate prior generation id for scope %s: %w", scopeID, err)
		}
		return strings.TrimSpace(prior), strings.TrimSpace(prior) != "", nil
	}
}

// NewPriorGenerationCheck returns a check backed by scope_generations for
// identifying first-generation writes.
func NewPriorGenerationCheck(database db.ExecQueryer) reducer.PriorGenerationCheck {
	return func(ctx context.Context, scopeID, generationID string) (bool, error) {
		rows, err := database.QueryContext(ctx, priorGenerationExistsSQL, scopeID, generationID)
		if err != nil {
			return false, fmt.Errorf("query prior generation for scope %s: %w", scopeID, err)
		}
		defer func() { _ = rows.Close() }()

		if !rows.Next() {
			return false, nil
		}

		var exists bool
		if err := rows.Scan(&exists); err != nil {
			return false, fmt.Errorf("scan prior generation for scope %s: %w", scopeID, err)
		}
		return exists, nil
	}
}

// unchangedGenerationCandidateQuery reads the generation an incoming snapshot
// is compared with (#7978): the scope's newest pending or active generation
// that carries a freshness hint, with its commit and status. It is the same
// row activeGenerationFreshnessQuery selects, so a newer pending generation
// still wins over the active one: a snapshot equal to the active generation
// but not to a newer pending one is stored, because the pending one will
// replace the active one. Served by scope_generations_scope_latest_lookup_idx.
// work_live (ruling a9-126) reports whether that generation's projector work
// row is pending, retrying, claimed, or running: one primary-key probe on the
// canonical projector work_item_id for the single candidate row. Rebuild work
// (refinalize_ prefix) is invisible to the probe; that is harmless because a
// refinalize targets only the active or newest failed generation, never a
// pending one. A probe on stage, scope_id, and generation_id would not depend
// on the key format but used fact_work_items_scope_generation_idx and took
// 1.27-3.58 ms on a 3,040-generation scope, against 0.03-0.54 ms here.
const unchangedGenerationCandidateQuery = `
SELECT candidate.generation_id,
       candidate.freshness_hint,
       candidate.source_commit_sha,
       candidate.status,
       candidate.stream_dropped,
       EXISTS (
           SELECT 1
           FROM fact_work_items AS work
           WHERE work.work_item_id = 'projector_' || $1 || '_' || candidate.generation_id
             AND work.status IN ('pending', 'retrying', 'claimed', 'running')
       ) AS work_live
FROM (
    SELECT generation.generation_id,
           COALESCE(generation.freshness_hint, '') AS freshness_hint,
           COALESCE(generation.source_commit_sha, '') AS source_commit_sha,
           generation.status,
           COALESCE((generation.payload ->> 'stream_dropped')::boolean, false) AS stream_dropped
    FROM scope_generations AS generation
    WHERE generation.scope_id = $1
      AND generation.status IN ('pending', 'active')
      AND COALESCE(generation.freshness_hint, '') <> ''
    ORDER BY generation.ingested_at DESC, generation.generation_id DESC
    LIMIT 1
) AS candidate
`

// unchangedGenerationCandidate is the generation a snapshot is compared with.
type unchangedGenerationCandidate struct {
	generationID  string
	freshnessHint string
	sourceCommit  string
	status        scope.GenerationStatus
	// workLive reports whether the candidate has a projector work row a
	// worker can still run (pending, retrying, claimed, running); ruling
	// a9-126 skips a pending match only then.
	workLive bool
	// streamDropped reports that the candidate's own fact stream dropped a
	// file (#7978 review F7-4, ruling a9-143.20): its facts are incomplete, so
	// an equal snapshot stores a new generation instead of matching it.
	streamDropped bool
}

// matches reports whether generation duplicates the candidate (#7978): the
// same non-empty freshness hint, which folds the collector, parser, and
// fact-schema version, and the same source commit.
func (c unchangedGenerationCandidate) matches(generation scope.ScopeGeneration) bool {
	hint := strings.TrimSpace(generation.FreshnessHint)
	return hint != "" && hint == c.freshnessHint &&
		strings.TrimSpace(generation.SourceCommitSHA) == c.sourceCommit
}

// unchangedGeneration returns the generation an incoming snapshot is compared
// with, or false when the scope has none with a freshness hint.
func (s IngestionStore) unchangedGeneration(ctx context.Context, scopeID string) (unchangedGenerationCandidate, bool, error) {
	if s.database == nil || strings.TrimSpace(scopeID) == "" {
		return unchangedGenerationCandidate{}, false, nil
	}
	rows, err := s.database.QueryContext(ctx, unchangedGenerationCandidateQuery, scopeID)
	if err != nil {
		return unchangedGenerationCandidate{}, false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return unchangedGenerationCandidate{}, false, rows.Err()
	}
	var candidate unchangedGenerationCandidate
	var status string
	if err := rows.Scan(&candidate.generationID, &candidate.freshnessHint, &candidate.sourceCommit, &status,
		&candidate.streamDropped, &candidate.workLive); err != nil {
		return unchangedGenerationCandidate{}, false, err
	}
	candidate.freshnessHint = strings.TrimSpace(candidate.freshnessHint)
	candidate.sourceCommit = strings.TrimSpace(candidate.sourceCommit)
	candidate.status = scope.GenerationStatus(status)
	return candidate, true, rows.Err()
}

// reprojectByIdentityQuery is the skipped-parse re-open (#7978 follow-up,
// ruling a9-143): the collector found the active generation's pre-parse
// identity equal at the same commit and asks for a re-projection without a
// snapshot. The target is fenced on the scope's active pointer, a full
// (is_delta false) active generation, its commit, and its stamped identity, so
// a newer activation, a delta, or a changed identity is a no-op. The re-open
// itself is the store re-open's (reprojectReopenedCTE): the same in-flight
// guard and attempt reset. A real re-open stamps the request like the store
// re-open and increments preparse_skips, the valve count.
//
// $1 now, $2 scope_id, $3 generation_id, $4 commit, $5 identity, $6 reason.
const reprojectByIdentityQuery = `
WITH target AS (
    SELECT generation.generation_id
    FROM scope_generations AS generation
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
     AND scope.active_generation_id = generation.generation_id
    WHERE generation.scope_id = $2
      AND generation.generation_id = $3
      AND generation.status = 'active'
      AND generation.activated_at IS NOT NULL
      AND generation.is_delta = false
      AND COALESCE(generation.source_commit_sha, '') = $4
      AND $5::text <> ''
      AND COALESCE(generation.payload ->> 'preparse_identity', '') = $5
),
` + reprojectReopenedCTE + `,
stamped AS (
    UPDATE scope_generations AS generation
    SET payload = COALESCE(generation.payload, '{}'::jsonb) || jsonb_build_object(
            'reconcile_reason', $6::text,
            'reconcile_requested_at', $1::timestamptz,
            'preparse_skips', COALESCE((generation.payload ->> 'preparse_skips')::int, 0) + 1
        )
    FROM reopened
    WHERE generation.generation_id = reopened.generation_id
      AND generation.status = 'active'
    RETURNING generation.generation_id
)
SELECT EXISTS (SELECT 1 FROM target), EXISTS (SELECT 1 FROM stamped)
`

// ReprojectActiveGeneration re-projects the scope's active generation without
// a snapshot, for a forced reconcile whose pre-parse identity equals the
// stored one (ruling a9-143). It takes the scope row lock first, as the store
// re-open does, and returns the refresh-skip reason it recorded:
// reconcile_reprojected (re-opened and stamped), reconcile_reprojection_pending
// (the row is already open or leased; nothing stamped), or
// reconcile_superseded (fenced: another generation is active, the generation is
// a delta, or the commit or identity differs). An error leaves nothing changed;
// the collector then parses as before.
func (s IngestionStore) ReprojectActiveGeneration(ctx context.Context, request scope.ActiveReprojection) (string, error) {
	if s.beginner == nil {
		return "", errors.New("transaction beginner is required")
	}
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin skipped-parse re-projection: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, lockReprojectScopeQuery, request.ScopeID); err != nil {
		return "", fmt.Errorf("lock scope for skipped-parse re-projection: %w", err)
	}
	rows, err := tx.QueryContext(ctx, reprojectByIdentityQuery, s.now(), request.ScopeID, request.GenerationID,
		strings.TrimSpace(request.CommitSHA), strings.TrimSpace(request.PreparseIdentity), strings.TrimSpace(request.Reason))
	if err != nil {
		return "", fmt.Errorf("re-open active generation without a parse: %w", err)
	}
	var matched, reopened bool
	if rows.Next() {
		err = rows.Scan(&matched, &reopened)
	}
	if err = errors.Join(err, rows.Err(), rows.Close()); err != nil {
		return "", fmt.Errorf("read skipped-parse re-open result: %w", err)
	}
	reason := telemetry.RefreshSkipReasonReconcileSuperseded
	switch {
	case matched && reopened:
		reason = telemetry.RefreshSkipReasonReconcileReprojected
	case matched:
		reason = telemetry.RefreshSkipReasonReconcileReprojectionPending
	}
	if matched {
		if err := tx.Commit(); err != nil {
			return "", fmt.Errorf("commit skipped-parse re-projection: %w", err)
		}
	}
	s.recordSkippedRefresh(ctx, scope.IngestionScope{ScopeID: request.ScopeID}, scope.ScopeGeneration{
		GenerationID: request.GenerationID, ScopeID: request.ScopeID, ReconcileReason: request.Reason,
	}, request.GenerationID, reason)
	return reason, nil
}

// preparseIdentityDropped reports whether the generation's fact stream
// dropped a file it could not read (review F-7, ruling a9-143.14). Call it
// only after the stream has drained.
func preparseIdentityDropped(generation scope.ScopeGeneration) bool {
	return generation.PreparseIdentity != "" && generation.PreparseIdentityDropped != nil &&
		generation.PreparseIdentityDropped.Load()
}

// clearPreparseIdentityQuery removes the pre-parse identity the stored path
// wrote with the generation row before it drained the fact stream, and marks
// the row stream_dropped (ruling a9-143.20), so the next equal snapshot stores
// a new generation instead of re-opening this incomplete one. Idempotent.
const clearPreparseIdentityQuery = `UPDATE scope_generations
SET payload = (payload - 'preparse_identity') || '{"stream_dropped": true}'::jsonb
WHERE generation_id = $1`

// clearDroppedPreparseIdentity keeps the identity off a stored generation
// whose fact stream dropped a file (review F-7): the row was written before
// the drain, so the identity is removed in the same transaction, before the
// commit. It runs a statement only on a drop.
func clearDroppedPreparseIdentity(ctx context.Context, database db.ExecQueryer, generation scope.ScopeGeneration) error {
	if !preparseIdentityDropped(generation) {
		return nil
	}
	if _, err := database.ExecContext(ctx, clearPreparseIdentityQuery, generation.GenerationID); err != nil {
		return fmt.Errorf("clear dropped pre-parse identity: %w", err)
	}
	return nil
}
