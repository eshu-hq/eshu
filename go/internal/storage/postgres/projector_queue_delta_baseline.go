// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// ProjectorQueue implements the delta-baseline fence (#7319) for every
// projection loop that claims from it.
var _ projector.DeltaBaselineFence = ProjectorQueue{}

// ReadDeltaBaseline reads the claimed generation and the scope's active
// generation without a lock. It is the preflight read; Ack issues the same
// statement inside its transaction. A missing generation row returns a state
// with TargetFound false.
func (q ProjectorQueue) ReadDeltaBaseline(
	ctx context.Context,
	work projector.ScopeGenerationWork,
) (projector.DeltaBaselineState, error) {
	if q.database == nil {
		return projector.DeltaBaselineState{}, errors.New("projector queue database is required")
	}
	return readDeltaBaselineState(ctx, q.database, work)
}

// readDeltaBaselineState runs deltaBaselineFenceQuery on queryer, which is the
// pool for the preflight read and the Ack transaction for the Ack read.
func readDeltaBaselineState(
	ctx context.Context,
	queryer db.Queryer,
	work projector.ScopeGenerationWork,
) (projector.DeltaBaselineState, error) {
	rows, err := queryer.QueryContext(ctx, deltaBaselineFenceQuery, work.Scope.ScopeID, work.Generation.GenerationID)
	if err != nil {
		return projector.DeltaBaselineState{}, fmt.Errorf("read delta baseline: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return projector.DeltaBaselineState{}, fmt.Errorf("read delta baseline: %w", err)
		}
		return projector.DeltaBaselineState{}, nil
	}
	var (
		status                            string
		isDelta                           bool
		baseline, activeGen, activeCommit sql.NullString
	)
	if err := rows.Scan(&status, &isDelta, &baseline, &activeGen, &activeCommit); err != nil {
		return projector.DeltaBaselineState{}, fmt.Errorf("read delta baseline: scan: %w", err)
	}
	if err := rows.Err(); err != nil {
		return projector.DeltaBaselineState{}, fmt.Errorf("read delta baseline: %w", err)
	}
	return projector.DeltaBaselineState{
		TargetFound:        true,
		TargetStatus:       scope.GenerationStatus(status),
		IsDelta:            isDelta,
		BaselineCommitSHA:  strings.TrimSpace(baseline.String),
		ActiveGenerationID: strings.TrimSpace(activeGen.String),
		ActiveCommitSHA:    strings.TrimSpace(activeCommit.String),
	}, nil
}

// RefuseDeltaBaseline marks a refused delta's work row and generation
// superseded in one statement with the refusal's failure class and details.
// It returns an error wrapping failure.ErrWorkSuperseded that names the class,
// or ErrProjectorClaimRejected when this attempt no longer owns the work.
func (q ProjectorQueue) RefuseDeltaBaseline(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	refusal projector.DeltaBaselineRefusal,
) error {
	if err := q.validate(); err != nil {
		return err
	}
	return q.markDeltaBaselineRefused(ctx, work, refusal, q.now())
}

// markDeltaBaselineRefused runs markProjectorDeltaBaselineRefusedQuery on the
// pool, outside any Ack transaction.
func (q ProjectorQueue) markDeltaBaselineRefused(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	refusal projector.DeltaBaselineRefusal,
	now time.Time,
) error {
	class := refusal.FailureClass()
	message := "projector delta refused before projection: baseline is not the active commit"
	if refusal.Phase == projector.DeltaBaselinePhaseAck {
		message = "projector delta refused at ack after projection: baseline is not the active commit"
	}
	rows, err := q.database.QueryContext(ctx, markProjectorDeltaBaselineRefusedQuery,
		now, work.Scope.ScopeID, work.Generation.GenerationID, q.LeaseOwner, work.AttemptCount,
		class, message, refusal.State.BaselineCommitSHA, refusal.ActiveCommitForLog(),
		refusal.State.ActiveGenerationID, refusal.Phase)
	if err != nil {
		return fmt.Errorf("mark delta baseline refusal: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var workRows, generationRows int64
	if rows.Next() {
		if err := rows.Scan(&workRows, &generationRows); err != nil {
			return fmt.Errorf("mark delta baseline refusal: scan: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("mark delta baseline refusal: %w", err)
	}
	if workRows != 1 {
		return fmt.Errorf("mark delta baseline refusal: %w", ErrProjectorClaimRejected)
	}
	return fmt.Errorf("delta generation %s refused: %w",
		work.Generation.GenerationID, projectorWorkSupersededError{failureClass: class})
}

// ackDeltaBaseline is Ack's authoritative fence decision.
type ackDeltaBaseline struct {
	outcome projector.DeltaBaselineOutcome
	state   projector.DeltaBaselineState
}

// checkAckDeltaBaseline runs the fence read inside Ack's transaction. Ack
// calls it after the scope-row lock statement and the work-row update have
// returned, and before the first statement that changes another generation.
// It adds no lock, so Ack's lock order is unchanged. A missing target row
// passes through: the activation statement then affects no row and Ack takes
// its existing refusal path.
func checkAckDeltaBaseline(
	ctx context.Context,
	tx db.Transaction,
	work projector.ScopeGenerationWork,
) (ackDeltaBaseline, error) {
	state, err := readDeltaBaselineState(ctx, tx, work)
	if err != nil {
		return ackDeltaBaseline{}, fmt.Errorf("ack projector work: %w", err)
	}
	return ackDeltaBaseline{outcome: projector.DecideDeltaBaseline(state), state: state}, nil
}

// refuseDeltaBaselineAck refuses a delta at Ack. Like refuseSupersededAck it
// rolls the transaction back first, which undoes the scope repoint and the
// work succeeded mark, and then marks the work and generation superseded in a
// separate statement; no savepoint. The generation already wrote its overlay,
// so this is an invariant breach and logs at ERROR. If the mark fails, the
// lease expires and the next attempt's preflight refuses again.
func (q ProjectorQueue) refuseDeltaBaselineAck(
	ctx context.Context,
	tx db.Transaction,
	work projector.ScopeGenerationWork,
	fence ackDeltaBaseline,
	now time.Time,
) error {
	if err := tx.Rollback(); err != nil {
		return fmt.Errorf("ack projector work: roll back refused delta: %w", err)
	}
	refusal := projector.DeltaBaselineRefusal{Phase: projector.DeltaBaselinePhaseAck, Outcome: fence.outcome, State: fence.state}
	err := q.markDeltaBaselineRefused(ctx, work, refusal, now)
	if errors.Is(err, ErrProjectorClaimRejected) {
		return fmt.Errorf("ack projector work: %w", err)
	}
	var superseded projectorWorkSupersededError
	if errors.As(err, &superseded) {
		projector.RecordDeltaBaselineFence(ctx, q.Instruments, refusal.Phase, refusal.Outcome)
		projector.LogDeltaBaselineRefusal(ctx, slog.Default(), work, refusal)
	}
	return fmt.Errorf("ack projector work: %w", err)
}

// deltaBaselineFenceQuery reads what the delta-baseline fence (#7319) decides
// on: the claimed generation and the scope's active generation. It takes no
// lock. Ack issues it as its own statement after the scope-row lock statement
// has returned: under Read Committed each statement takes a fresh snapshot, so
// it sees every Ack that committed while this Ack waited for the scope row. A
// statement that both locked the scope row and read scope_generations would
// read the generations from its pre-wait snapshot, so the read must never be
// folded into the lock statement or a lock CTE.
//
// Both sides are index scans: the primary key for the target and the partial
// unique index scope_generations_active_scope_idx for the active row
// (generic-plan measurement in docs/internal/evidence/7319-delta-baseline-fence.md).
const deltaBaselineFenceQuery = `
SELECT target.status,
       target.is_delta,
       target.delta_baseline_commit_sha,
       active.generation_id,
       active.source_commit_sha
FROM scope_generations AS target
LEFT JOIN LATERAL (
    SELECT generation_id, source_commit_sha
    FROM scope_generations
    WHERE scope_id = target.scope_id
      AND ` + activeGenerationPredicate + `
    LIMIT 1
) AS active ON true
WHERE target.scope_id = $1
  AND target.generation_id = $2
`

// markProjectorDeltaBaselineRefusedQuery ends a refused delta: the claimed work
// row first, then its generation, in one statement, matching Ack's work-then-
// generation lock order. It never takes the scope row. The work row keeps its
// claim fences (owner, attempt, claimed or running), so a lost claim marks
// nothing. The generation moves to superseded only from pending or failed, so
// an active generation is never demoted; superseded is terminal and replay
// leaves it alone, so a refused delta is never replayed. It returns the work
// and generation row counts.
//
// $1 now, $2 scope, $3 generation, $4 lease owner, $5 attempt, $6 failure
// class, $7 failure message, $8 baseline commit, $9 active commit (or none),
// $10 active generation id, $11 fence phase.
const markProjectorDeltaBaselineRefusedQuery = `
WITH refused_work AS (
    UPDATE fact_work_items AS work
    SET status = 'superseded',
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = NULL,
        next_attempt_at = NULL,
        updated_at = $1,
        failure_class = $6,
        failure_message = $7,
        failure_details = jsonb_build_object(
            'scope_id', work.scope_id,
            'work_item_id', work.work_item_id,
            'generation_id', work.generation_id,
            'delta_baseline_commit_sha', $8::text,
            'active_commit_sha', $9::text,
            'active_generation_id', $10::text,
            'phase', $11::text
        )
    WHERE work.stage = 'projector'
      AND work.scope_id = $2
      AND work.generation_id = $3
      AND work.lease_owner = $4
      AND work.attempt_count = $5
      AND work.status IN ('claimed', 'running')
    RETURNING work.scope_id, work.generation_id
), refused_generation AS (
    UPDATE scope_generations AS generation
    SET status = 'superseded',
        superseded_at = $1
    FROM refused_work
    WHERE generation.scope_id = refused_work.scope_id
      AND generation.generation_id = refused_work.generation_id
      AND generation.status IN ('pending', 'failed')
    RETURNING generation.generation_id
)
SELECT (SELECT count(*) FROM refused_work),
       (SELECT count(*) FROM refused_generation)
`
