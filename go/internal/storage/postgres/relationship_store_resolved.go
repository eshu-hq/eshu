// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// GetResolvedRelationshipsForRepos returns active resolved relationships that
// touch any candidate repository as either source or target.
func (s *RelationshipStore) GetResolvedRelationshipsForRepos(
	ctx context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, error) {
	repoIDs = uniqueNonEmptyStrings(repoIDs)
	if len(repoIDs) == 0 {
		return nil, nil
	}

	placeholderList, args := repoIDPlaceholders(repoIDs)
	sqlRows, err := s.database.QueryContext(ctx, fmt.Sprintf(listResolvedByReposSQL, placeholderList, placeholderList), args...)
	if err != nil {
		return nil, fmt.Errorf("list resolved by repos: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	return scanResolvedRelationshipRows(sqlRows, "by repos")
}

// errCorpusFencedResolvedNoRows reports a fused fence-and-read result with no
// row at all. The LEFT JOIN from the single-row fence CTE always yields one,
// so this means the backend violated the query shape; it fails the read
// rather than inventing a verdict.
var errCorpusFencedResolvedNoRows = errors.New(
	"list resolved by repos with corpus fence: no rows returned",
)

// GetResolvedRelationshipsForReposWithCorpusFence returns the active resolved
// relationships touching any candidate repository together with the
// corpus-completeness fence verdict, both from ONE statement snapshot
// (#6740), so a foreign scope that retires and re-activates its generation
// mid-pass can never pair a passing verdict with a partial row set. When
// complete is false the rows are nil and must not be consumed. An empty
// repository set still evaluates the fence (there is no read to couple it
// to). A query error fails safe: the caller receives the error, never a
// verdict.
func (s *RelationshipStore) GetResolvedRelationshipsForReposWithCorpusFence(
	ctx context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, bool, error) {
	repoIDs = uniqueNonEmptyStrings(repoIDs)
	if len(repoIDs) == 0 {
		complete, err := s.AreActiveScopeRelationshipGenerationsComplete(ctx)
		if err != nil {
			return nil, false, err
		}
		return nil, complete, nil
	}

	placeholderList, args := repoIDPlaceholders(repoIDs)
	sqlRows, err := s.database.QueryContext(
		ctx, fmt.Sprintf(listResolvedByReposWithCorpusFenceSQL, placeholderList, placeholderList), args...,
	)
	if err != nil {
		return nil, false, fmt.Errorf("list resolved by repos with corpus fence: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	return scanCorpusFencedResolvedRows(sqlRows)
}

// scanCorpusFencedResolvedRows reads the fused fence-and-read result: every
// row carries the same fence verdict (one materialized CTE row), and rows with
// a NULL relationship_type are the LEFT JOIN filler for an incomplete corpus
// or an empty match. No row at all is impossible from the LEFT JOIN shape and
// is reported as an error rather than read as a verdict.
func scanCorpusFencedResolvedRows(rows db.Rows) ([]relationships.ResolvedRelationship, bool, error) {
	return scanCorpusFencedResolvedRowsInto(rows, nil)
}

// scanCorpusFencedResolvedRowsInto is scanCorpusFencedResolvedRows for a
// result that may carry one more column after the verdict: the #8283 input
// fingerprint, scanned into fingerprint when it is non-nil.
func scanCorpusFencedResolvedRowsInto(
	rows db.Rows, fingerprint *string,
) ([]relationships.ResolvedRelationship, bool, error) {
	var result []relationships.ResolvedRelationship
	sawRow := false
	complete := false
	for rows.Next() {
		var rowComplete bool
		var sourceRepoID, targetRepoID, sourceEntityID, targetEntityID, relType sql.NullString
		var r relationships.ResolvedRelationship
		var resSrc string
		var detailsBytes []byte
		dest := []any{&rowComplete}
		if fingerprint != nil {
			dest = append(dest, fingerprint)
		}
		if err := rows.Scan(append(dest,
			&sourceRepoID,
			&targetRepoID,
			&sourceEntityID,
			&targetEntityID,
			&relType,
			&r.Confidence,
			&r.EvidenceCount,
			&r.Rationale,
			&resSrc,
			&detailsBytes,
		)...); err != nil {
			return nil, false, fmt.Errorf("scan resolved by repos with corpus fence: %w", err)
		}
		sawRow = true
		complete = rowComplete
		if !rowComplete || !relType.Valid {
			continue
		}
		r.SourceRepoID = nullableString(sourceRepoID)
		r.TargetRepoID = nullableString(targetRepoID)
		r.SourceEntityID = nullableString(sourceEntityID)
		r.TargetEntityID = nullableString(targetEntityID)
		r.RelationshipType = relationships.RelationshipType(relType.String)
		r.ResolutionSource = relationships.ResolutionSource(resSrc)
		if len(detailsBytes) > 0 {
			if err := json.Unmarshal(detailsBytes, &r.Details); err != nil {
				return nil, false, fmt.Errorf("unmarshal resolved by repos with corpus fence details: %w", err)
			}
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate resolved by repos with corpus fence: %w", err)
	}
	if !sawRow {
		return nil, false, errCorpusFencedResolvedNoRows
	}
	if !complete {
		return nil, false, nil
	}
	return result, true, nil
}

// repoIDPlaceholders renders the positional placeholder list and bind
// arguments shared by the by-repos reads, which repeat the list for the
// source and target predicates.
func repoIDPlaceholders(repoIDs []string) (string, []any) {
	placeholders := make([]string, len(repoIDs))
	args := make([]any, len(repoIDs))
	for i, repoID := range repoIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = repoID
	}
	return strings.Join(placeholders, ", "), args
}

func scanResolvedRelationshipRows(rows db.Rows, label string) ([]relationships.ResolvedRelationship, error) {
	var result []relationships.ResolvedRelationship
	for rows.Next() {
		var r relationships.ResolvedRelationship
		var sourceRepoID sql.NullString
		var targetRepoID sql.NullString
		var sourceEntityID sql.NullString
		var targetEntityID sql.NullString
		var relType, resSrc string
		var detailsBytes []byte
		if err := rows.Scan(
			&sourceRepoID,
			&targetRepoID,
			&sourceEntityID,
			&targetEntityID,
			&relType,
			&r.Confidence,
			&r.EvidenceCount,
			&r.Rationale,
			&resSrc,
			&detailsBytes,
		); err != nil {
			return nil, fmt.Errorf("scan resolved %s: %w", label, err)
		}
		r.SourceRepoID = nullableString(sourceRepoID)
		r.TargetRepoID = nullableString(targetRepoID)
		r.SourceEntityID = nullableString(sourceEntityID)
		r.TargetEntityID = nullableString(targetEntityID)
		r.RelationshipType = relationships.RelationshipType(relType)
		r.ResolutionSource = relationships.ResolutionSource(resSrc)
		if len(detailsBytes) > 0 {
			if err := json.Unmarshal(detailsBytes, &r.Details); err != nil {
				return nil, fmt.Errorf("unmarshal resolved %s details: %w", label, err)
			}
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// ActivateResolutionGenerationForClaim activates a relationship generation
// only while the exact reducer claim that produced it remains live. Locking the
// queue row in the same statement serializes publication against recovery's
// deletion; matching last_attempt_at rejects an older execution after reclaim.
func (s *RelationshipStore) ActivateResolutionGenerationForClaim(
	ctx context.Context,
	generationID string,
	scopeID string,
	workItemID string,
	claimedAt time.Time,
) error {
	now := time.Now().UTC()
	affected, err := s.activateWithConsumerWake(ctx, activateResolutionGenerationForClaimSQL,
		[]any{generationID, scopeID, now, now, workItemID, claimedAt.UTC()},
		scopeID, generationID, now)
	if err != nil {
		return fmt.Errorf("activate resolution generation for reducer claim: %w", err)
	}
	if affected == 0 {
		return reducercontract.ErrExecutionClaimRejected
	}
	return nil
}

// defaultActivationWakeLockTimeout bounds how long the activation transaction
// waits on an in-flight consumer row lock (#7597), like the projector Ack's
// lock_timeout (#6738). A 55P03 fails the activation retryably; the
// deployment_mapping item re-runs and activation is idempotent.
const defaultActivationWakeLockTimeout = 2 * time.Second

// activationWakeSetClause is the SET list of the #7597 consumer wake: the
// recovery replay reset list (replayFailedWorkItemsTemplate, kept in lockstep
// by TestActivationWakeSetClauseMatchesReplayReset) plus reopened_at, so a
// woken consumer keeps its retry evidence and its elapsed-time readiness
// bounds start a fresh window. $1 is the wake timestamp.
const activationWakeSetClause = `status = 'pending',
        attempt_count = GREATEST(attempt_count, 1),
        container_image_identity_v2_authorized_status = CASE
            WHEN container_image_identity_v2_required THEN 'pending'
            ELSE ''
        END,
        container_image_identity_v3_authorized_status = CASE
            WHEN container_image_identity_v3_required THEN 'pending'
            ELSE ''
        END,
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = $1,
        next_attempt_at = NULL,
        failure_class = NULL,
        failure_message = NULL,
        failure_details = NULL,
        updated_at = $1,
        reopened_at = $1`

// lockInflightOwnGateConsumersSQL waits for, then holds, every in-flight
// own-gate consumer row of the activated scope generation (#7597), in the
// byte order (COLLATE "C") every multi-row reducer locker uses, the batch ack
// included, so the two cannot form a lock cycle. A consumer
// whose final counted Fail is committing finishes first, so the wake
// statement that follows (a fresh snapshot) sees it dead-lettered; a Fail that
// starts later waits for this transaction and then sees the active generation
// through its own post-dead-letter wake (wakeAfterProducerTerminalDeadLetter).
// It is the only statement here that waits on a consumer row. Zero rows in
// the common case: no own-gate consumer is claimed or running.
var lockInflightOwnGateConsumersSQL = `
SELECT work_item_id
FROM fact_work_items
WHERE scope_id = $1
  AND generation_id = $2
  AND stage = 'reducer'
  AND status IN ('claimed', 'running')
  AND domain IN (` + sqlStringList(ownGateConsumerDomainStrings()) + `)
ORDER BY work_item_id COLLATE "C"
FOR UPDATE
`

// wakeProducerTerminalConsumersSQL reopens the own-gate consumers of one scope
// generation that dead-lettered in an own-gate counting class
// (ownGateCountingClasses) once its relationship generation is active
// (#7597). It runs inside every activation transaction (after the
// generation write, which the EXISTS then sees) and after a consumer's own
// producer-terminal dead letter. The scan uses fact_work_items_scope_generation_idx
// (scope_id, generation_id, status) and touches zero rows in the common case.
// Dead-lettered rows are locked SKIP LOCKED: their only other writers reopen
// or retire them (recovery and admin replay, the claim-time supersede sweep, a
// concurrent wake), so the wake never waits on them. $1 timestamp, $2 scope,
// $3 generation.
var wakeProducerTerminalConsumersSQL = `
UPDATE fact_work_items
SET ` + activationWakeSetClause + `
WHERE status = 'dead_letter'
  AND work_item_id IN (
    SELECT consumer.work_item_id
    FROM fact_work_items AS consumer
    WHERE consumer.scope_id = $2
      AND consumer.generation_id = $3
      AND consumer.stage = 'reducer'
      AND consumer.status = 'dead_letter'
      AND consumer.failure_class IN (` + sqlStringList(ownGateCountingClasses()) + `)
      AND EXISTS (
        SELECT 1 FROM relationship_generations AS rg
        WHERE rg.generation_id = $3 AND rg.scope = $2 AND rg.status = 'active'
      )
    ORDER BY consumer.work_item_id COLLATE "C"
    FOR UPDATE OF consumer SKIP LOCKED
  )
`

// ActivationWakeContentionError reports an activation transaction that hit
// lock_timeout (55P03) or a deadlock (40P01) and rolled back (#7597). It is
// Retryable with no class, so the deployment_mapping item re-runs on its
// ordinary counted budget; activation is idempotent.
type ActivationWakeContentionError struct {
	err error
}

// Error names the contention.
func (e ActivationWakeContentionError) Error() string {
	return fmt.Sprintf("activate relationship generation: lock contention, retrying: %v", e.err)
}

// Unwrap exposes the driver error.
func (e ActivationWakeContentionError) Unwrap() error { return e.err }

// Retryable opts the rolled-back activation into the counted retry path.
func (ActivationWakeContentionError) Retryable() bool { return true }

// activateWithConsumerWake runs one relationship_generations activation
// statement and the #7597 consumer wake in one transaction: activation, then
// SET LOCAL lock_timeout, then lockInflightOwnGateConsumersSQL, then
// wakeProducerTerminalConsumersSQL. Lock order: claimant work row (claim
// variant) -> relationship_generations -> in-flight consumer rows (ordered by
// work_item_id) -> dead-lettered consumer rows (SKIP LOCKED). It returns the
// activation statement's affected rows; zero rolls back with no wake.
// TestEveryRelationshipActivationWakesConsumers enumerates the module source
// so no activation statement can run outside this function.
func (s *RelationshipStore) activateWithConsumerWake(
	ctx context.Context,
	activationSQL string,
	args []any,
	scopeID, generationID string,
	now time.Time,
) (int64, error) {
	beginner, ok := s.database.(db.Beginner)
	if !ok {
		return 0, errors.New("relationship store database must support Begin to activate a generation")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin activation: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	result, err := tx.ExecContext(ctx, activationSQL, args...)
	if err != nil {
		return 0, classifyActivationError(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("activation rows affected: %w", err)
	}
	if affected == 0 {
		return 0, nil
	}
	timeout := s.activationLockTimeout
	if timeout <= 0 {
		timeout = defaultActivationWakeLockTimeout
	}
	if _, err := tx.ExecContext(ctx, "SELECT set_config('lock_timeout', $1, true)",
		fmt.Sprintf("%dms", timeout.Milliseconds())); err != nil {
		return 0, fmt.Errorf("activation wake: set lock timeout: %w", err)
	}
	woken, err := s.lockAndWake(ctx, tx, scopeID, generationID, now)
	if err != nil {
		recordOwnGateWake(ctx, s.instruments, telemetry.OwnGateWakeSiteActivation, telemetry.OwnGateWakeOutcomeError)
		return 0, classifyActivationError(err)
	}
	if err := tx.Commit(); err != nil {
		recordOwnGateWake(ctx, s.instruments, telemetry.OwnGateWakeSiteActivation, telemetry.OwnGateWakeOutcomeError)
		return 0, classifyActivationError(err)
	}
	committed = true
	recordOwnGateWake(ctx, s.instruments, telemetry.OwnGateWakeSiteActivation, wakeOutcome(woken))
	if n := woken; n > 0 {
		slog.Default().InfoContext(ctx, "relationship generation activation reopened own-gate consumers",
			"scope_id", scopeID, "generation_id", generationID, "woken_consumers", n)
	}
	return affected, nil
}

// lockAndWake runs the activation wake's in-flight lock and wake statements
// inside tx and returns how many consumers the wake reopened.
func (s *RelationshipStore) lockAndWake(
	ctx context.Context, tx db.Transaction, scopeID, generationID string, now time.Time,
) (int64, error) {
	rows, err := tx.QueryContext(ctx, lockInflightOwnGateConsumersSQL, scopeID, generationID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	if rowsErr != nil {
		return 0, rowsErr
	}
	result, err := tx.ExecContext(ctx, wakeProducerTerminalConsumersSQL, now, scopeID, generationID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// classifyActivationError makes lock_timeout and deadlock retryable and
// returns every other error unchanged.
func classifyActivationError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "55P03" || pgErr.Code == "40P01") {
		return ActivationWakeContentionError{err: err}
	}
	return err
}
