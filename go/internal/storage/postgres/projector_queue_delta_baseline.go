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
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// ProjectorQueue implements the delta-baseline fence (#7319) and the
// projection write-start marker (#7389) for every projection loop that claims
// from it.
var (
	_ projector.DeltaBaselineFence    = ProjectorQueue{}
	_ projector.ProjectionWriteMarker = ProjectorQueue{}
)

// projectorWriteMarkerGenerationRetiredClass is the failure class the marker
// reports when the claimed generation is no longer pending or active and no
// other writer recorded a class on the work row. It is logged, never written.
const projectorWriteMarkerGenerationRetiredClass = "projector_write_marker_generation_retired"

// MarkProjectionWriteStarted records that this attempt is about to write the
// canonical graph and content store for its generation (#7389), keeping the
// latest write start.
// It runs markProjectionWriteStartedQuery in its own transaction under Ack's
// lock_timeout. After it commits, the heartbeat supersede no longer retires
// the generation for a newer one, so the attempt runs to Ack; a generation
// that wrote and still never activated is found by UncoveredProjectionWriters.
// #7819: the transaction first locks the scope's claim fence row non-blocking
// and bumps it when the marker sets, so the marker commit and a claim's fence
// lock serialize and a claim whose snapshot predates the marker drops its
// candidate through the #7115 fence recheck.
//
// It returns nil when the marker is set. A lock timeout, or a busy fence row,
// returns an error wrapping failure.ErrWorkWriteMarkerDeferred (a busy fence
// wraps failure.ErrWorkWriteMarkerFenceBusy): nothing changed and the caller
// re-runs it. A missing fence row means the scope is gone, so it refuses at
// once through the classifier below instead of deferring. When no row is
// marked, writeMarkerRefusal decides: an error wrapping
// failure.ErrWorkSuperseded when the generation is retired (superseded,
// completed, or gone) or the work row was superseded, and
// ErrProjectorClaimRejected when this attempt lost its claim.
func (q ProjectorQueue) MarkProjectionWriteStarted(
	ctx context.Context,
	work projector.ScopeGenerationWork,
) (err error) {
	if err := q.validate(); err != nil {
		return err
	}
	defer func() {
		if isPostgresLockNotAvailable(err) {
			err = fmt.Errorf("%w: %w", failure.ErrWorkWriteMarkerDeferred, err)
		}
	}()
	beginner, ok := q.database.(db.Beginner)
	if !ok {
		return errors.New("projector queue database must support Begin for the write marker")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("mark projection write started: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, "SELECT set_config('lock_timeout', $1, true)", q.ackLockTimeoutSetting()); err != nil {
		return fmt.Errorf("mark projection write started: set lock timeout: %w", err)
	}
	fenceLocked, fenceExists, err := checkMarkerClaimFence(ctx, tx, work.Scope.ScopeID)
	if err != nil {
		return err
	}
	if !fenceLocked {
		committed = true
		_ = tx.Rollback()
		if !fenceExists {
			return q.classifyWriteMarkerRefusal(ctx, work)
		}
		return fmt.Errorf("mark projection write started: scope %s claim fence busy: %w",
			work.Scope.ScopeID, failure.ErrWorkWriteMarkerFenceBusy)
	}
	rows, err := tx.QueryContext(ctx, markProjectionWriteStartedQuery,
		work.Scope.ScopeID, work.Generation.GenerationID, q.LeaseOwner, work.AttemptCount, q.now())
	if err != nil {
		return fmt.Errorf("mark projection write started: %w", err)
	}
	marked := rows.Next()
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("mark projection write started: %w", err)
	}
	_ = rows.Close()
	if !marked {
		// Release the connection before the classification read, which runs on
		// the pool and would otherwise wait on a one-connection pool.
		committed = true
		_ = tx.Rollback()
		return q.classifyWriteMarkerRefusal(ctx, work)
	}
	// The marker set: bump the fence row this transaction already holds so a
	// claim whose snapshot predates the marker drops its candidate.
	if _, err := tx.ExecContext(ctx, bumpProjectorMarkerFenceQuery, work.Scope.ScopeID); err != nil {
		return fmt.Errorf("mark projection write started: bump claim fence: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mark projection write started: commit: %w", err)
	}
	committed = true
	return nil
}

// classifyWriteMarkerRefusal reads why the marker matched no row. It runs on
// the pool after the marker's transaction rolled back and takes no lock.
// Only logging depends on the split: either way nothing was written.
func (q ProjectorQueue) classifyWriteMarkerRefusal(ctx context.Context, work projector.ScopeGenerationWork) error {
	var generationStatus, workStatus, workClass sql.NullString
	rows, err := q.database.QueryContext(ctx, classifyWriteMarkerRefusalQuery,
		work.Scope.ScopeID, work.Generation.GenerationID)
	if err != nil {
		return fmt.Errorf("mark projection write started: classify refusal: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		if err := rows.Scan(&generationStatus, &workStatus, &workClass); err != nil {
			return fmt.Errorf("mark projection write started: classify refusal: scan: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("mark projection write started: classify refusal: %w", err)
	}
	return writeMarkerRefusal(work.Generation.GenerationID, generationStatus, workStatus, workClass)
}

// writeMarkerRetiredGenerationStatuses are the generation statuses the marker
// treats as retired: the generation can never be written or activated again.
// A missing row is retired too. failed is not: a dead-lettered generation can
// be replayed and then activate through Ack (status <> 'superseded').
var writeMarkerRetiredGenerationStatuses = map[string]bool{
	string(scope.GenerationStatusSuperseded): true,
	string(scope.GenerationStatusCompleted):  true,
}

// writeMarkerRefusal classifies a marker that matched no row. A superseded
// work row, or a retired generation (superseded, completed, or missing), is
// ErrWorkSuperseded, carrying the work row's failure class when it has one and
// projectorWriteMarkerGenerationRetiredClass otherwise. Any other state
// (pending, active or failed generation whose work row this attempt no longer
// owns, or one that changed after the marker's read) is a lost claim: the
// current owner, or the next claim, re-runs the marker.
func writeMarkerRefusal(generationID string, generationStatus, workStatus, workClass sql.NullString) error {
	retired := !generationStatus.Valid || writeMarkerRetiredGenerationStatuses[generationStatus.String]
	if workStatus.String == "superseded" || retired {
		class := strings.TrimSpace(workClass.String)
		if class == "" {
			class = projectorWriteMarkerGenerationRetiredClass
		}
		return fmt.Errorf("mark projection write started: generation %s: %w",
			generationID, projectorWorkSupersededError{failureClass: class})
	}
	return fmt.Errorf("mark projection write started: %w", ErrProjectorClaimRejected)
}

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
        ) || ` + priorFailureWorkSQL + `
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

// markProjectionWriteStartedQuery is the #7389 write-start marker. It locks
// only the claimed generation row and sets projection_write_started_at to the
// latest write start: it never moves backwards, and a replayed generation
// that writes again after a later full generation records the new write, so
// UncoveredProjectionWriters still sees it. Markers come from q.now() on the
// writing host, so comparing them across generations assumes the clock skew
// between projector hosts is smaller than one projection (not measured); do
// not switch to clock_timestamp(), which would make the value depend on lock
// waits instead of the attempt. A failed generation is markable because a
// dead-lettered generation can be replayed and then activate. The EXISTS is
// the lease fence: it reads the work row without locking it, so the marker
// never joins the work-row order. Because status sits on the locked row,
// EvalPlanQual rechecks it against a heartbeat or Ack supersede that committed
// while the marker waited, and the marker then matches no row.
//
// A pre-migration active generation with a NULL marker has one residual: a
// supersede that commits between the marker's snapshot and its row lock still
// passes the snapshot-read EXISTS. The worker then rewrites the active
// generation's own tree and its Ack returns ErrProjectorClaimRejected. That
// wastes the attempt but leaves the graph at the active generation.
//
// $1 scope, $2 generation, $3 lease owner, $4 attempt, $5 now.
const markProjectionWriteStartedQuery = `
UPDATE scope_generations
SET projection_write_started_at = GREATEST(COALESCE(projection_write_started_at, $5), $5)
WHERE scope_id = $1
  AND generation_id = $2
  AND status IN ('pending', 'active', 'failed')
  AND EXISTS (
      SELECT 1
      FROM fact_work_items AS work
      WHERE work.stage = 'projector'
        AND work.scope_id = $1
        AND work.generation_id = $2
        AND work.lease_owner = $3
        AND work.attempt_count = $4
        AND work.status IN ('claimed', 'running')
  )
RETURNING generation_id
`

// classifyWriteMarkerRefusalQuery explains a marker that matched no row: the
// generation status and the projector work row's status and failure class.
const classifyWriteMarkerRefusalQuery = `
SELECT generation.status,
       work.status,
       work.failure_class
FROM scope_generations AS generation
LEFT JOIN fact_work_items AS work
  ON work.stage = 'projector'
 AND work.scope_id = generation.scope_id
 AND work.generation_id = generation.generation_id
WHERE generation.scope_id = $1
  AND generation.generation_id = $2
`
