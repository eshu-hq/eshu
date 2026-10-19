// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// defaultCompletionWakeLockTimeout bounds how long one completion-wake
// transaction waits on a contended work-item row before giving up with
// 55P03 and letting the resolution item retry. It matches the #7597
// activation wake's bound.
const defaultCompletionWakeLockTimeout = 2 * time.Second

// completionWakeInputChanged is the #8283 changed-evidence verdict over one
// candidate's LEFT JOINed fingerprint row: missing row, moved logic
// version, or a fingerprint that differs from the recompute over the
// recorded repository set. It renders the same ResolvedInputFingerprintSQL
// the maintenance gate renders, so the two wakes can never disagree about
// whether an item's input changed.
var completionWakeInputChanged = `(fingerprint.work_item_id IS NULL
        OR fingerprint.logic_version IS DISTINCT FROM ` +
	strconv.Itoa(reducercontract.DeployableUnitCorrelationLogicVersion) + `
        OR fingerprint.input_fingerprint IS DISTINCT FROM ` +
	backfill.ResolvedInputFingerprintSQL("fingerprint.repo_ids") + `)`

// completionWakeCandidateIDsSQL is the candidate selection both wake arms
// share ($1 wake timestamp, unused here; $2 the wake's repository scope). It
// composes the single scope replay floor (backfill.ScopeReplayFloorCTE and
// ScopeReplayFloorJoins, the bound every floored reopen listing uses), the
// failed-generation exclusion, and the floor tuple predicate, so the write
// set is the at-or-above-floor items only — never the accumulated succeeded
// history below each scope's floor. A fingerprinted item must overlap the
// wake's repositories; an item with no fingerprint row has no recorded set
// to overlap, and the succeeded arm keeps maintenance's missing-as-always-
// reopen treatment for it, so the floor alone bounds that arm. The in-flight
// arm additionally requires a recorded fingerprint row (see the flag query):
// without recorded input there is nothing to prove stale, and flagging a
// mid-first-execution claim forces a redundant replay whose ACK the
// migration 093 trigger diverts to pending with attempt_count reset —
// erasing a killed claim's reclaim evidence. Both arms additionally require
// the changed-evidence verdict, which is what makes a re-run wake a no-op.
// Rows lock FOR UPDATE in work_item_id C order, the order every multi-row
// reducer locker uses, so two ordered lockers cannot form a lock cycle;
// under Read Committed the lock rechecks this level's quals
// (EvalPlanQual), so a row another transaction reopened, claimed, or
// superseded between the snapshot and the lock drops out instead of being
// rewritten.
func completionWakeCandidateIDsSQL(statusPredicate string) string {
	return `SELECT work.work_item_id
` + backfill.ScopeReplayFloorJoins + `LEFT JOIN deployable_unit_reopen_fingerprints AS fingerprint
  ON fingerprint.work_item_id = work.work_item_id
WHERE work.stage = 'reducer'
  AND work.domain = 'deployable_unit_correlation'
  AND ` + statusPredicate + `
  AND work_generation.status <> 'failed'
  AND (work_generation.ingested_at, work_generation.generation_id)
      >= (floor.floor_ingested_at, floor.floor_generation_id)
  AND (fingerprint.work_item_id IS NULL OR fingerprint.repo_ids && $2::text[])
  AND ` + completionWakeInputChanged + `
ORDER BY work.work_item_id COLLATE "C"
FOR UPDATE OF work`
}

// flagInflightDeployableUnitCorrelationsQuery is the wake's in-flight arm:
// a claimed/running item whose recorded input no longer holds keeps its
// exact lease and gains cross_scope_replay_required, so the migration 093
// ACK trigger returns it to pending instead of landing it succeeded on stale
// input. The SET list names no status column: the trigger fires BEFORE
// UPDATE OF status, and this statement must not trip it early. Items
// already flagged are excluded so a repeat wake neither locks nor rewrites
// them. Items with no fingerprint row are excluded too: with no recorded
// input there is nothing to prove stale — the running execution is the
// first derivation and records its fingerprint on success — and flagging it
// would force a redundant replay whose diverted ACK resets attempt_count,
// erasing kill-recovery reclaim evidence (fault-injection shard 3/4
// killworkerdeployableunit: a pass that inserted nothing flagged the
// admission-blocked victim, and the replacement's reclaimed attempt read
// back as the reset instead of above the kill-time value).
var flagInflightDeployableUnitCorrelationsQuery = backfill.ScopeReplayFloorCTE + `UPDATE fact_work_items
SET cross_scope_replay_required = TRUE,
    updated_at = $1
WHERE work_item_id IN (` +
	completionWakeCandidateIDsSQL(`work.status IN ('claimed', 'running')
  AND NOT work.cross_scope_replay_required
  AND fingerprint.work_item_id IS NOT NULL`) + `
)
`

// reopenDeployableUnitCorrelationsForReposQuery is the wake's succeeded
// arm. It composes the shared succeeded-to-pending SET clause (#7731) so
// the wake resets exactly the columns the reducer claim path reads.
var reopenDeployableUnitCorrelationsForReposQuery = backfill.ScopeReplayFloorCTE + `UPDATE fact_work_items
SET ` + ReopenSucceededReducerSetClause + `
WHERE work_item_id IN (` +
	completionWakeCandidateIDsSQL(`work.status = 'succeeded'`) + `
)
`

// CompletionWakeContentionError reports a completion-wake transaction that
// hit lock_timeout (55P03) or a deadlock (40P01) and rolled back (#8294).
// It is Retryable with no class, so the resolution item re-runs on its
// ordinary counted budget; the wake is fingerprint-gated, so the retry's
// re-run converges.
type CompletionWakeContentionError struct {
	err error
}

// Error names the contention.
func (e CompletionWakeContentionError) Error() string {
	return fmt.Sprintf("reopen deployable_unit_correlation items for appended resolved rows: lock contention, retrying: %v", e.err)
}

// Unwrap exposes the driver error.
func (e CompletionWakeContentionError) Unwrap() error { return e.err }

// Retryable opts the rolled-back wake into the counted retry path.
func (CompletionWakeContentionError) Retryable() bool { return true }

// classifyCompletionWakeError makes lock_timeout and deadlock retryable and
// returns every other error unchanged.
func classifyCompletionWakeError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "55P03" || pgErr.Code == "40P01") {
		return CompletionWakeContentionError{err: err}
	}
	return err
}

// ReopenDeployableUnitCorrelationsForRepos is the completion-driven wake
// (#8294): one lock_timeout-bounded transaction that flags in-flight
// deployable_unit_correlation items with recorded input for replay after
// their ACK, then reopens the succeeded ones, over the resolution pass's
// repository scope. Both arms are bounded by the scope replay floor and the
// #8283 changed-evidence verdict, so the wake is runnable on any resolution
// pass — including a retry that inserted nothing — and converges: an item
// that already re-read the appended rows has a matching fingerprint and is
// left alone, as is an in-flight item with no recorded fingerprint (its
// running execution is the first derivation). The in-flight arm runs first
// and locks its rows, so an ACK that would land between the two arms waits
// on the lock and then sees the replay flag; an item that ACKed before the
// wake is succeeded and the second arm reopens it. Empty input reopens
// nothing without touching the database. No worker count, batch size, or
// claim shape changes: this is one extra transaction per resolution pass
// that resolved rows.
//
// Telemetry: one eshu_dp_reducer_completion_wake_total increment per
// transaction by outcome, plus the reopened row count on
// eshu_dp_correlation_reopened_total{domain="deployable_unit_correlation"},
// the counter the maintenance reopen already feeds.
func (s *RelationshipStore) ReopenDeployableUnitCorrelationsForRepos(
	ctx context.Context,
	repoIDs []string,
) (crossrepo.CompletionWakeResult, error) {
	repos := uniqueNonEmptyStrings(repoIDs)
	if len(repos) == 0 {
		return crossrepo.CompletionWakeResult{}, nil
	}
	beginner, ok := s.database.(db.Beginner)
	if !ok {
		return crossrepo.CompletionWakeResult{}, errors.New("relationship store database must support Begin to run the completion wake")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return crossrepo.CompletionWakeResult{}, fmt.Errorf("begin completion wake: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	timeout := s.completionWakeLockTimeout
	if timeout <= 0 {
		timeout = defaultCompletionWakeLockTimeout
	}
	if _, err := tx.ExecContext(ctx, "SELECT set_config('lock_timeout', $1, true)",
		fmt.Sprintf("%dms", timeout.Milliseconds())); err != nil {
		s.recordCompletionWake(ctx, telemetry.CompletionWakeOutcomeError, 0)
		return crossrepo.CompletionWakeResult{}, fmt.Errorf("completion wake: set lock timeout: %w", err)
	}
	now := time.Now().UTC()
	reposArg := array.StringArray(repos)
	var woken crossrepo.CompletionWakeResult
	flagResult, err := tx.ExecContext(ctx, flagInflightDeployableUnitCorrelationsQuery, now, reposArg)
	if err != nil {
		s.recordCompletionWake(ctx, telemetry.CompletionWakeOutcomeError, 0)
		return crossrepo.CompletionWakeResult{}, classifyCompletionWakeError(
			fmt.Errorf("flag in-flight deployable_unit_correlation items for replay: %w", err))
	}
	if woken.ReplayFlagged, err = flagResult.RowsAffected(); err != nil {
		s.recordCompletionWake(ctx, telemetry.CompletionWakeOutcomeError, 0)
		return crossrepo.CompletionWakeResult{}, fmt.Errorf("flag in-flight deployable_unit_correlation items for replay: rows affected: %w", err)
	}
	reopenResult, err := tx.ExecContext(ctx, reopenDeployableUnitCorrelationsForReposQuery, now, reposArg)
	if err != nil {
		s.recordCompletionWake(ctx, telemetry.CompletionWakeOutcomeError, 0)
		return crossrepo.CompletionWakeResult{}, classifyCompletionWakeError(
			fmt.Errorf("reopen deployable_unit_correlation items for appended resolved rows: %w", err))
	}
	if woken.Reopened, err = reopenResult.RowsAffected(); err != nil {
		s.recordCompletionWake(ctx, telemetry.CompletionWakeOutcomeError, 0)
		return crossrepo.CompletionWakeResult{}, fmt.Errorf("reopen deployable_unit_correlation items for appended resolved rows: rows affected: %w", err)
	}
	if err := tx.Commit(); err != nil {
		s.recordCompletionWake(ctx, telemetry.CompletionWakeOutcomeError, 0)
		return crossrepo.CompletionWakeResult{}, classifyCompletionWakeError(
			fmt.Errorf("commit completion wake: %w", err))
	}
	committed = true
	s.recordCompletionWake(ctx, wakeOutcomeForCompletion(woken), woken.Reopened)
	return woken, nil
}

// wakeOutcomeForCompletion maps a committed wake to its outcome label.
func wakeOutcomeForCompletion(woken crossrepo.CompletionWakeResult) string {
	if woken.Reopened > 0 || woken.ReplayFlagged > 0 {
		return telemetry.CompletionWakeOutcomeReopened
	}
	return telemetry.CompletionWakeOutcomeNoop
}

// recordCompletionWake counts one wake transaction on
// eshu_dp_reducer_completion_wake_total and, for a committed wake, its
// reopened rows on eshu_dp_correlation_reopened_total (#8294). Nil
// instruments are a no-op.
func (s *RelationshipStore) recordCompletionWake(ctx context.Context, outcome string, reopened int64) {
	if s.instruments == nil {
		return
	}
	if s.instruments.ReducerCompletionWake != nil {
		s.instruments.ReducerCompletionWake.Add(ctx, 1,
			metric.WithAttributes(telemetry.AttrOutcome(outcome)))
	}
	if reopened > 0 && s.instruments.CorrelationReopened != nil {
		s.instruments.CorrelationReopened.Add(ctx, reopened,
			metric.WithAttributes(attribute.String("domain", "deployable_unit_correlation")))
	}
}
