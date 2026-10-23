// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/scope"
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

// priorGenerationExistsSQL reports whether the scope has a generation
// strictly older than the intent's own row in activation order
// (ingested_at, generation_id): the order generationFreshnessSQL and the
// projector supersession SQL use, so a predecessor here is a generation that
// could have activated, and written edges, before the intent's. A generation
// committed after the intent's never counts, so the answer, and the
// first-generation retract skip built on it, does not depend on whether a
// newer generation committed before the intent ran (#8167). Failed and
// superseded predecessors count: their partial writes may still need cleanup.
// A missing intent row (retention pruned it) falls back to "any other
// generation exists", the conservative answer that keeps the retract.
// Both probes are index range scans on scope_id
// (scope_generations_scope_generation_idx for the intent row,
// scope_generations_scope_latest_lookup_idx for the row comparison), and
// the fallback runs only when the intent row is missing.
const priorGenerationExistsSQL = `
SELECT COALESCE(
    (
        SELECT EXISTS (
            SELECT 1
            FROM scope_generations AS prior
            WHERE prior.scope_id = $1
              AND (prior.ingested_at, prior.generation_id)
                  < (intent.ingested_at, intent.generation_id)
        )
        FROM scope_generations AS intent
        WHERE intent.scope_id = $1
          AND intent.generation_id = $2
    ),
    EXISTS (
        SELECT 1
        FROM scope_generations
        WHERE scope_id = $1
          AND generation_id <> $2
    )
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
//     no active generation and the intent's row exists but is neither pending
//     nor terminal: (true, nil), and the handler runs. An unknown scope keeps
//     this legacy result; it cannot leave search index orphans because
//     eshu_search_index_terms and eshu_search_index_documents keep their
//     scope_id foreign key, which rejects the row (#7427);
//   - the scope has no active generation and the intent is pending:
//     (false, reducercontract.GenerationNotYetActiveError) until first Ack;
//   - the scope has no active generation and the intent is failed or
//     superseded, or its generation row is missing (retention pruned it,
//     #7427): (false, nil), and the intent is terminally superseded;
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
			if !intentStatus.Valid || intentStatus.String == string(scope.GenerationStatusFailed) ||
				intentStatus.String == string(scope.GenerationStatusSuperseded) {
				// A missing row fails closed (#7427): retention pruned the
				// generation, and with the terms generation foreign key
				// gone nothing else would reject the writer's rows.
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
// identifying first-generation writes. It reports true only when the scope
// has a generation strictly older than generationID in activation order
// (ingested_at, generation_id), so a newer generation committed while the
// intent waits in the queue never changes the answer (#8167). An unknown
// generation row falls back to whether any other generation exists.
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

// unchangedGeneration returns the generation an incoming snapshot is compared
// with, or false when the scope has none with a freshness hint. The candidate
// type lives in backfill (backfill.UnchangedGenerationCandidate) because the
// duplicate decision (backfill.DecideUnchangedGeneration) consumes it; the
// freshness read itself stays root-owned.
func (s IngestionStore) unchangedGeneration(ctx context.Context, scopeID string) (backfill.UnchangedGenerationCandidate, bool, error) {
	if s.database == nil {
		return backfill.UnchangedGenerationCandidate{}, false, nil
	}
	return unchangedGenerationOn(ctx, s.database, scopeID)
}

// unchangedGenerationOn runs the candidate read on querier: the base
// connection before the commit transaction opens, or the commit transaction
// itself for the destructive-successor guard's in-tx recheck (#8750 window).
func unchangedGenerationOn(ctx context.Context, querier db.Queryer, scopeID string) (backfill.UnchangedGenerationCandidate, bool, error) {
	if strings.TrimSpace(scopeID) == "" {
		return backfill.UnchangedGenerationCandidate{}, false, nil
	}
	rows, err := querier.QueryContext(ctx, unchangedGenerationCandidateQuery, scopeID)
	if err != nil {
		return backfill.UnchangedGenerationCandidate{}, false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return backfill.UnchangedGenerationCandidate{}, false, rows.Err()
	}
	var candidate backfill.UnchangedGenerationCandidate
	var status string
	if err := rows.Scan(&candidate.GenerationID, &candidate.FreshnessHint, &candidate.SourceCommit, &status,
		&candidate.StreamDropped, &candidate.WorkLive); err != nil {
		return backfill.UnchangedGenerationCandidate{}, false, err
	}
	candidate.FreshnessHint = strings.TrimSpace(candidate.FreshnessHint)
	candidate.SourceCommit = strings.TrimSpace(candidate.SourceCommit)
	candidate.Status = scope.GenerationStatus(status)
	return candidate, true, rows.Err()
}

// streamDropped reports whether the generation's fact stream dropped a file
// it could not read (review F-7, rulings a9-143.14 and a9-143.23). It does
// not depend on the identity: an identity-less generation that dropped a file
// is marked stream_dropped too (review F7-5). Call it only after the stream
// has drained.
func streamDropped(generation scope.ScopeGeneration) bool {
	return generation.PreparseIdentityDropped != nil && generation.PreparseIdentityDropped.Load()
}

// recordStreamDroppedPathsQuery persists the recorded stream-drop paths next
// to the stream_dropped mark, keeping the pre-parse identity on the row
// (#8185). A persistent drop recurs identically, so the next equal snapshot
// may still skip once the skip probe re-checks the recorded paths; clearing
// the identity instead (ruling a9-143.20) re-parses in full forever without
// ever emitting different facts. The update still targets one row by primary
// key, so the plan is unchanged. Idempotent.
const recordStreamDroppedPathsQuery = `UPDATE scope_generations
SET payload = payload || '{"stream_dropped": true}'::jsonb || jsonb_build_object('stream_dropped_paths', $2::jsonb, 'stream_dropped_truncated', $3::boolean)
WHERE generation_id = $1`

// recordDroppedPreparseIdentity records a stored generation's fact-stream
// drop in its payload (review F-7, #8185): the row was written before the
// drain, so the mark, the normalized dropped paths, and the truncation flag
// land in the same transaction, before the commit. The pre-parse identity
// stays: the recorded paths let the next equal snapshot verify the drop still
// applies instead of re-parsing unconditionally. A truncated set forces the
// next parse, since unrecorded drops cannot be re-checked. It runs a
// statement only on a drop.
func recordDroppedPreparseIdentity(ctx context.Context, database db.ExecQueryer, generation scope.ScopeGeneration) error {
	if !streamDropped(generation) {
		return nil
	}
	paths := []string{}
	truncated := false
	if generation.PreparseIdentityDroppedPaths != nil {
		paths = generation.PreparseIdentityDroppedPaths.Sorted()
		truncated = generation.PreparseIdentityDroppedPaths.Truncated()
	}
	if paths == nil {
		paths = []string{}
	}
	pathsJSON, err := json.Marshal(paths)
	if err != nil {
		return fmt.Errorf("marshal dropped pre-parse paths: %w", err)
	}
	if _, err := database.ExecContext(ctx, recordStreamDroppedPathsQuery, generation.GenerationID, string(pathsJSON), truncated); err != nil {
		return fmt.Errorf("record dropped pre-parse identity: %w", err)
	}
	return nil
}
