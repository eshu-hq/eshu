// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	queuestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// This file holds the active-generation re-projection path: the re-open SQL,
// the store re-open worker, and the skipped-parse entry point. It is the
// commit-path half of the #7978 duplicate-generation guard; the duplicate
// decision itself lives in backfill.
const lockReprojectScopeQuery = `
SELECT scope_id FROM ingestion_scopes WHERE scope_id = $1 FOR NO KEY UPDATE
`

// reprojectActiveGenerationQuery re-opens the active generation's canonical
// projector work row for a reconciliation snapshot that matched it (#7978),
// instead of storing a duplicate generation. It mirrors the #7209 zombie heal
// (healProjectorActiveGenerationQuery): same canonical work_item_id, same
// reopened columns, and the same write-time in-flight guard, so repeated
// reconciliations converge on one open row. Ruling a9-117 adds two rules:
//   - attempt_count resets to 0 when the row belongs to a run that ended
//     (succeeded, failed, dead_letter): a carried count would dead-letter
//     the first retryable error of the re-projection. A dead_letter row on
//     the active generation comes from a #7209 heal or liveness re-drive that
//     no request covers (PR #7997 review). The reset is bounded: each re-open
//     still gets at most the projector's max attempts, and the re-open stamps
//     reconcile_requested_at, so if that run dead-letters the sweep decides
//     reconcile_request_stale and mints instead of re-opening the row again.
//     An expired lease keeps its count: that run never ended, so the cap
//     keeps counting a crash loop. payload.reconcile_reopen_count stays
//     monotonic for operators.
//   - reconcile_requested_at is stamped only when the row is really re-opened
//     (stamped follows reopened), never on a pending or in-flight outcome, so
//     a re-projection that never runs cannot keep the scope fresh.
//
// Fences, all in one statement after the scope lock: target requires the
// generation to still be the scope's active generation, status active and
// activated, with the same freshness hint and commit as the snapshot. The
// claim sweep retires a pending row whose generation is superseded later, and
// Heartbeat, the write marker, and Ack refuse a superseded generation, so a
// supersession after this commit makes the re-projection a no-op.
//
// $1 now, $2 scope_id, $3 active generation_id, $4 freshness hint,
// $5 source commit, $6 reconcile reason, $7 the snapshot's pre-parse identity:
// when non-empty, a real re-open stamps it and resets preparse_skips (the first
// sweep after deploy and every valve parse, ruling a9-143). It returns one row: whether the
// target matched (false: fenced) and whether the row was re-opened and stamped.
const reprojectActiveGenerationQuery = `
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
      AND COALESCE(generation.freshness_hint, '') = $4
      AND COALESCE(generation.source_commit_sha, '') = $5
),
` + reprojectReopenedCTE + `,
stamped AS (
    UPDATE scope_generations AS generation
    SET payload = COALESCE(generation.payload, '{}'::jsonb) || jsonb_build_object(
            'reconcile_reason', $6::text,
            'reconcile_requested_at', $1::timestamptz
        ) || CASE WHEN $7::text <> '' THEN jsonb_build_object('preparse_identity', $7::text, 'preparse_skips', 0)
                  ELSE '{}'::jsonb END
    FROM reopened
    WHERE generation.generation_id = reopened.generation_id
      AND generation.status = 'active'
    RETURNING generation.generation_id
)
SELECT EXISTS (SELECT 1 FROM target), EXISTS (SELECT 1 FROM stamped)
`

// reprojectReopenedCTE re-opens the target generation's canonical projector
// row. The store re-open (hint-fenced) and the skipped-parse re-open
// (identity-fenced, ReprojectActiveGeneration) share it, so both apply the same
// in-flight guard and attempt reset.
const reprojectReopenedCTE = `reopened AS (
    INSERT INTO fact_work_items (
        work_item_id, scope_id, generation_id, stage, domain, status,
        attempt_count, lease_owner, claim_until, visible_at, last_attempt_at,
        next_attempt_at, failure_class, failure_message, failure_details,
        payload, created_at, updated_at
    )
    SELECT
        'projector_' || $2 || '_' || target.generation_id, $2, target.generation_id,
        'projector', 'source_local', 'pending', 0, NULL, NULL, $1, NULL, NULL,
        NULL, NULL, NULL, jsonb_build_object('reconcile_reopen_count', 1), $1, $1
    FROM target
    ON CONFLICT (work_item_id) DO UPDATE
    SET status = 'pending',
        attempt_count = CASE
            WHEN fact_work_items.status IN ('succeeded', 'failed', 'dead_letter') THEN 0
            ELSE fact_work_items.attempt_count
        END,
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = EXCLUDED.visible_at,
        next_attempt_at = NULL,
        failure_class = NULL,
        failure_message = NULL,
        failure_details = NULL,
        payload = jsonb_set(
            COALESCE(fact_work_items.payload, '{}'::jsonb),
            '{reconcile_reopen_count}',
            to_jsonb(COALESCE((fact_work_items.payload ->> 'reconcile_reopen_count')::int, 0) + 1)
        ),
        updated_at = EXCLUDED.updated_at,
        reopened_at = ` + queuestore.ReopenedAtUpsertValue + `
    WHERE NOT (
        fact_work_items.status IN ('pending', 'retrying')
        OR (
            fact_work_items.status IN ('claimed', 'running')
            AND fact_work_items.claim_until > $1
        )
    )
    RETURNING generation_id
)
`

// reprojectActiveGeneration runs reprojectActiveGenerationQuery under the
// scope lock and the workflow claim fence, and returns the closed skip reason.
func (s IngestionStore) reprojectActiveGeneration(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	requireClaimFence bool,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	candidate backfill.UnchangedGenerationCandidate,
) (string, error) {
	if s.beginner == nil {
		return "", errors.New("transaction beginner is required")
	}
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin re-projection transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if requireClaimFence {
		mutation.ObservedAt = s.now()
		if err := NewWorkflowControlStore(tx).HeartbeatClaim(ctx, mutation); err != nil {
			return "", fmt.Errorf("verify active workflow claim before re-projection: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, lockReprojectScopeQuery, scopeValue.ScopeID); err != nil {
		return "", fmt.Errorf("lock scope for re-projection: %w", err)
	}
	rows, err := tx.QueryContext(ctx, reprojectActiveGenerationQuery,
		s.now(), scopeValue.ScopeID, candidate.GenerationID, candidate.FreshnessHint,
		candidate.SourceCommit, strings.TrimSpace(generation.ReconcileReason), strings.TrimSpace(generation.PreparseIdentity))
	if err != nil {
		return "", fmt.Errorf("re-open active generation projector work: %w", err)
	}
	var matched, reopened bool
	if rows.Next() {
		err = rows.Scan(&matched, &reopened)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return "", fmt.Errorf("read re-open result: %w", err)
	}
	if !matched {
		return telemetry.RefreshSkipReasonReconcileSuperseded, nil
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit re-projection: %w", err)
	}
	committed = true
	if reopened {
		return telemetry.RefreshSkipReasonReconcileReprojected, nil
	}
	return telemetry.RefreshSkipReasonReconcileReprojectionPending, nil
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

// stampActiveGenerationQuery is the first-sweep stamp re-open (#8095): the
// collector proved the active generation equals what a full parse of the same
// commit would emit, but no identity was ever stored for it. The target is
// fenced on the scope's active pointer, a full (is_delta false) active
// generation, its commit, an EMPTY stored identity, and a non-empty requested
// one, so a second stamp (or any stamp after a parse stored an identity) is a
// no-op: the stamp is one-shot by construction. The re-open itself is the
// store re-open's (reprojectReopenedCTE): the same in-flight guard and attempt
// reset. A real re-open stamps the requested identity and RESETS preparse_skips
// to 0, like the parse path: a stamp counts as a parse for the valve, so a
// due valve still forces a real parse.
//
// $1 now, $2 scope_id, $3 generation_id, $4 commit, $5 identity, $6 reason.
const stampActiveGenerationQuery = `
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
      AND COALESCE(generation.payload ->> 'preparse_identity', '') = ''
),
` + reprojectReopenedCTE + `,
stamped AS (
    UPDATE scope_generations AS generation
    SET payload = COALESCE(generation.payload, '{}'::jsonb) || jsonb_build_object(
            'reconcile_reason', $6::text,
            'reconcile_requested_at', $1::timestamptz,
            'preparse_identity', $5::text,
            'preparse_skips', 0
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
	return s.execReprojectQuery(ctx, "skipped-parse re-projection", reprojectByIdentityQuery, request)
}

// StampActiveGeneration stamps the scope's active generation without a
// snapshot, for a proven first-sweep stamp (#8095): the stored identity is
// empty and the collector proved the running content version and
// configuration would emit the requested identity for the same commit. It
// returns the same closed reasons as ReprojectActiveGeneration:
// reconcile_reprojected (re-opened, identity stamped, skips reset), or
// reconcile_superseded (fenced: another generation is active, the generation
// is a delta, the commit moved, the request is empty, or an identity is
// already stored — the one-shot fence). An error leaves nothing changed; the
// collector then parses as before.
func (s IngestionStore) StampActiveGeneration(ctx context.Context, request scope.ActiveReprojection) (string, error) {
	return s.execReprojectQuery(ctx, "stamp re-projection", stampActiveGenerationQuery, request)
}

// execReprojectQuery runs one snapshot-less re-open (operation names the
// errors) under the scope row lock and returns the closed skip reason it
// recorded: reconcile_reprojected (re-opened and stamped),
// reconcile_reprojection_pending (the row is already open or leased; nothing
// stamped), or reconcile_superseded (fenced). A fenced target commits nothing
// and records nothing; an error leaves nothing changed.
func (s IngestionStore) execReprojectQuery(ctx context.Context, operation, query string, request scope.ActiveReprojection) (string, error) {
	if s.beginner == nil {
		return "", errors.New("transaction beginner is required")
	}
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin %s: %w", operation, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, lockReprojectScopeQuery, request.ScopeID); err != nil {
		return "", fmt.Errorf("lock scope for %s: %w", operation, err)
	}
	rows, err := tx.QueryContext(ctx, query, s.now(), request.ScopeID, request.GenerationID,
		strings.TrimSpace(request.CommitSHA), strings.TrimSpace(request.PreparseIdentity), strings.TrimSpace(request.Reason))
	if err != nil {
		return "", fmt.Errorf("re-open active generation without a parse: %w", err)
	}
	var matched, reopened bool
	if rows.Next() {
		err = rows.Scan(&matched, &reopened)
	}
	if err = errors.Join(err, rows.Err(), rows.Close()); err != nil {
		return "", fmt.Errorf("read %s re-open result: %w", operation, err)
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
			return "", fmt.Errorf("commit %s: %w", operation, err)
		}
	}
	s.backfillStore().RecordSkippedRefresh(ctx, scope.IngestionScope{ScopeID: request.ScopeID}, scope.ScopeGeneration{
		GenerationID: request.GenerationID, ScopeID: request.ScopeID, ReconcileReason: request.Reason,
	}, request.GenerationID, reason)
	return reason, nil
}
