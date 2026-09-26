// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/rebuild/reset"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// replayFailedWorkItemsTemplate resets matching terminal rows to pending. The
// %s is replaced by the dynamic predicate built from the replay filter (scope,
// failure class, and the manual-review exclusion), so every replay variant
// shares one UPDATE body and the exclusion can never be dropped by a missing
// hand-written variant. $1 is the replay timestamp; the predicate placeholders
// start at $2.
const replayFailedWorkItemsTemplate = `
WITH replayed AS (
    UPDATE fact_work_items
    SET status = 'pending',
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
        updated_at = $1
    WHERE status IN ('dead_letter', 'failed')
      %s
    RETURNING work_item_id
)
SELECT work_item_id FROM replayed ORDER BY work_item_id
`

// replayFailedWorkItemsBoundedTemplate is the limited replay variant for the
// dead-letter backlog drain (#3560, #3652 P3). It mutates at most $2 terminal
// rows by selecting their primary keys in a bounded subquery first, so a
// Limit=100 drain against thousands of retry_exhausted rows replays exactly 100
// rows instead of resetting every matching row to pending and recreating the
// write surge the drain exists to avoid.
//
// FOR UPDATE SKIP LOCKED locks only the chosen rows and skips rows another
// concurrent drain already holds, so two drains never fight over the same rows
// and the bound stays a true cap under concurrent execution rather than a
// serialization point. ORDER BY work_item_id makes the selected set
// deterministic across calls so repeated bounded drains make forward progress.
//
// The %s is the shared replay predicate. $1 is the replay timestamp; $2 is the
// row limit; the predicate placeholders start at $3.
const replayFailedWorkItemsBoundedTemplate = `
WITH replayed AS (
    UPDATE fact_work_items
    SET status = 'pending',
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
        updated_at = $1
    WHERE work_item_id IN (
        SELECT work_item_id FROM fact_work_items
        WHERE status IN ('dead_letter', 'failed')
          %s
        ORDER BY work_item_id
        LIMIT $2
        FOR UPDATE SKIP LOCKED
    )
    RETURNING work_item_id
)
SELECT work_item_id FROM replayed ORDER BY work_item_id
`

// countDeadLetterBacklogTemplate counts the terminal rows a replay with the same
// filter would touch, before any mutation. It shares the predicate builder with
// the replay so the count reflects exactly the rows the drain is allowed to
// move; the predicate placeholders start at $1 because there is no timestamp.
const countDeadLetterBacklogTemplate = `
SELECT COUNT(*) FROM fact_work_items
WHERE status IN ('dead_letter', 'failed')
  %s
`

// RecoveryStore implements recovery.ReplayStore over Postgres.
type RecoveryStore struct {
	database db.ExecQueryer

	// refinalizeDrainTimeout bounds the in-flight reducer drain wait in
	// RefinalizeScopeProjections; refinalizeDrainPoll is the poll interval.
	// Zero means the reset defaults apply. They are set with
	// RecoveryStoreOption so existing constructors keep working.
	refinalizeDrainTimeout time.Duration
	refinalizeDrainPoll    time.Duration

	// instruments records the superseded-generation replay fence counter.
	// Nil is a no-op.
	instruments *telemetry.Instruments
}

// resetQueryer adapts Transaction to reset.Queryer. The row
// interfaces already match, so only the entrypoint needs adapting.
type resetQueryer struct {
	db.Transaction
}

// QueryContext implements reset.Queryer.
func (q resetQueryer) QueryContext(ctx context.Context, query string, args ...any) (reset.Rows, error) {
	return q.Transaction.QueryContext(ctx, query, args...)
}

// RecoveryStoreOption tunes a RecoveryStore. The zero store (no options) is
// valid and uses the documented defaults.
type RecoveryStoreOption func(*RecoveryStore)

// WithRefinalizeDrainTimeout bounds how long RefinalizeScopeProjections waits
// for in-flight reducer work to drain before it aborts the refinalize instead
// of retiring generations under running resolvers. Non-positive keeps the
// default. Tests use a short timeout to prove the abort without sleeping.
func WithRefinalizeDrainTimeout(d time.Duration) RecoveryStoreOption {
	return func(s *RecoveryStore) {
		s.refinalizeDrainTimeout = d
	}
}

// WithRefinalizeDrainPollInterval sets the poll interval of the in-flight
// reducer drain wait. Non-positive keeps the default.
func WithRefinalizeDrainPollInterval(d time.Duration) RecoveryStoreOption {
	return func(s *RecoveryStore) {
		s.refinalizeDrainPoll = d
	}
}

// WithRecoveryInstruments records eshu_dp_superseded_generation_fence_total
// when a replay leaves superseded-generation projector rows in place. Nil
// keeps the store uninstrumented.
func WithRecoveryInstruments(instruments *telemetry.Instruments) RecoveryStoreOption {
	return func(s *RecoveryStore) {
		s.instruments = instruments
	}
}

// NewRecoveryStore constructs a Postgres-backed recovery store.
func NewRecoveryStore(database db.ExecQueryer, opts ...RecoveryStoreOption) RecoveryStore {
	s := RecoveryStore{database: database}
	for _, opt := range opts {
		if opt != nil {
			opt(&s)
		}
	}
	return s
}

// replayPredicate is the dynamic WHERE tail (after the terminal-status clause)
// for one replay filter, plus the positional args that fill its placeholders.
// startPlaceholder is the next $N to use, so a query with a leading timestamp
// arg ($1) starts the predicate at $2 while a count query starts it at $1.
type replayPredicate struct {
	clause string
	args   []any
}

// buildReplayPredicate renders the stage, scope, failure-class,
// manual-review-exclusion, and superseded-generation predicates shared by the
// replay UPDATE and the backlog COUNT. Both call it so a count can never select
// a different row set than the replay it precedes, and the exclusions apply
// uniformly: an unscoped drain that selects a broad set still cannot move a
// manual-review (poison) row or a superseded-generation projector row.
func buildReplayPredicate(filter recovery.ReplayFilter, startPlaceholder int) replayPredicate {
	predicate := buildUnfencedReplayPredicate(filter, startPlaceholder)
	predicate.clause += "\n      AND NOT " + supersededProjectorGenerationFence
	return predicate
}

// buildUnfencedReplayPredicate renders the filter predicates without the
// superseded-generation fence; countSupersededReplaySkipsTemplate adds the
// fence in its positive form to count what the replay skipped.
func buildUnfencedReplayPredicate(filter recovery.ReplayFilter, startPlaceholder int) replayPredicate {
	var (
		clauses = []string{fmt.Sprintf("AND stage = $%d", startPlaceholder)}
		args    = []any{string(filter.Stage)}
		next    = startPlaceholder + 1
	)

	if len(filter.ScopeIDs) > 0 {
		clauses = append(clauses, fmt.Sprintf("AND scope_id = ANY($%d)", next))
		args = append(args, filter.ScopeIDs)
		next++
	}

	if strings.TrimSpace(filter.FailureClass) != "" {
		clauses = append(clauses, fmt.Sprintf("AND failure_class = $%d", next))
		args = append(args, filter.FailureClass)
		next++
	}

	if excluded := nonEmptyClasses(filter.ExcludeFailureClasses); len(excluded) > 0 {
		// failure_class <> ALL(...) excludes the poison buckets. NULL is impossible
		// here because terminal rows always carry a failure_class, but <> ALL also
		// safely keeps a NULL out, which is acceptable for a drain.
		clauses = append(clauses, fmt.Sprintf("AND failure_class <> ALL($%d)", next))
		args = append(args, excluded)
	}

	return replayPredicate{clause: strings.Join(clauses, "\n      "), args: args}
}

// nonEmptyClasses returns classes with blank entries dropped so an accidental
// empty string never becomes a meaningless exclusion predicate.
func nonEmptyClasses(classes []string) []string {
	out := make([]string, 0, len(classes))
	for _, class := range classes {
		if trimmed := strings.TrimSpace(class); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// buildReplayFailedWorkItemsQuery renders the replay SQL and its positional
// args for one filter. When filter.Limit is positive it selects the bounded
// template so the UPDATE mutates at most Limit rows via a LIMIT/FOR UPDATE SKIP
// LOCKED subquery ($1 timestamp, $2 limit, predicate from $3). When Limit is
// zero it uses the unbounded template ($1 timestamp, predicate from $2). The
// bound lives in SQL, not the Go scan loop, so a drain never resets more rows to
// pending than it reports (#3652 P3).
func buildReplayFailedWorkItemsQuery(filter recovery.ReplayFilter, now time.Time) (string, []any) {
	if filter.Limit > 0 {
		predicate := buildReplayPredicate(filter, 3)
		query := fmt.Sprintf(replayFailedWorkItemsBoundedTemplate, predicate.clause)
		args := append([]any{now.UTC(), filter.Limit}, predicate.args...)
		return query, args
	}

	predicate := buildReplayPredicate(filter, 2)
	query := fmt.Sprintf(replayFailedWorkItemsTemplate, predicate.clause)
	args := append([]any{now.UTC()}, predicate.args...)
	return query, args
}

// ReplayFailedWorkItems resets terminal work items to pending for the given
// stage and filter criteria. New Go runtime rows use dead_letter; legacy failed
// rows remain replayable until they age out. When the filter carries
// ExcludeFailureClasses (the drain path), those classes are excluded store-side
// so a broad selector can never replay a manual-review row. A positive
// filter.Limit bounds the mutation in SQL so only that many rows are replayed.
func (s RecoveryStore) ReplayFailedWorkItems(
	ctx context.Context,
	filter recovery.ReplayFilter,
	now time.Time,
) (recovery.ReplayResult, error) {
	if s.database == nil {
		return recovery.ReplayResult{}, fmt.Errorf("recovery store database is required")
	}

	query, args := buildReplayFailedWorkItemsQuery(filter, now)

	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		return recovery.ReplayResult{}, fmt.Errorf("replay failed work items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var workItemIDs []string
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			return recovery.ReplayResult{}, fmt.Errorf("replay failed work items: %w", scanErr)
		}
		workItemIDs = append(workItemIDs, id)
	}
	if err := rows.Err(); err != nil {
		return recovery.ReplayResult{}, fmt.Errorf("replay failed work items: %w", err)
	}

	skipped, err := s.countSupersededReplaySkips(ctx, filter)
	if err != nil {
		return recovery.ReplayResult{}, err
	}
	recordSupersededGenerationFence(ctx, s.instruments, projectorReplayGenerationSupersededClass, skipped)

	return recovery.ReplayResult{
		Stage:                       filter.Stage,
		Replayed:                    len(workItemIDs),
		WorkItemIDs:                 workItemIDs,
		SkippedSupersededGeneration: skipped,
	}, nil
}

// countSupersededReplaySkips counts the terminal projector rows matching
// filter that the replay fence left in place. It runs after the replay, so
// the rows it counts are exactly those still terminal on superseded
// generations; a non-projector filter skips nothing and costs no query.
func (s RecoveryStore) countSupersededReplaySkips(ctx context.Context, filter recovery.ReplayFilter) (int, error) {
	if filter.Stage != recovery.StageProjector {
		return 0, nil
	}
	predicate := buildUnfencedReplayPredicate(filter, 1)
	var skipped int
	if err := queryCount(ctx, s.database, fmt.Sprintf(countSupersededReplaySkipsTemplate, predicate.clause),
		predicate.args, &skipped); err != nil {
		return 0, fmt.Errorf("count superseded replay skips: %w", err)
	}
	return skipped, nil
}

// CountDeadLetterBacklog reports how many terminal rows match the filter before
// any replay runs. It shares the predicate builder with ReplayFailedWorkItems so
// the depth reflects exactly the rows a drain with the same filter would move,
// including the manual-review exclusion.
func (s RecoveryStore) CountDeadLetterBacklog(
	ctx context.Context,
	filter recovery.ReplayFilter,
) (int, error) {
	if s.database == nil {
		return 0, fmt.Errorf("recovery store database is required")
	}

	predicate := buildReplayPredicate(filter, 1)
	var depth int
	if err := queryCount(ctx, s.database, fmt.Sprintf(countDeadLetterBacklogTemplate, predicate.clause),
		predicate.args, &depth); err != nil {
		return 0, fmt.Errorf("count dead letter backlog: %w", err)
	}
	return depth, nil
}

// queryCount runs a single-row COUNT query and scans it into out. An empty
// result leaves out at zero.
func queryCount(ctx context.Context, database db.ExecQueryer, query string, args []any, out *int) error {
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		if err := rows.Scan(out); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ReplayCollectorGenerations marks collector generation commit failures for
// source-level replay.
func (s RecoveryStore) ReplayCollectorGenerations(
	ctx context.Context,
	filter recovery.CollectorGenerationReplayFilter,
	now time.Time,
) (recovery.CollectorGenerationReplayResult, error) {
	if s.database == nil {
		return recovery.CollectorGenerationReplayResult{}, fmt.Errorf("recovery store database is required")
	}

	result, err := NewCollectorGenerationDeadLetterStore(s.database).ReplayGenerationDeadLetters(ctx, collector.GenerationDeadLetterReplayFilter{
		ScopeIDs:      filter.ScopeIDs,
		FailureClass:  filter.FailureClass,
		CollectorKind: scope.CollectorKind(filter.CollectorKind),
		Limit:         filter.Limit,
	}, now)
	if err != nil {
		return recovery.CollectorGenerationReplayResult{}, err
	}

	return recovery.CollectorGenerationReplayResult{
		Replayed:      result.Replayed,
		GenerationIDs: result.GenerationIDs,
	}, nil
}

// RefinalizeScopeProjections re-enqueues projector work by inserting new pending
// work items: for the given scope IDs, or for every recoverable scope when the
// filter sets AllScopes. An active scope is re-enqueued through its active
// generation; a failed scope with no active generation through its newest
// failed generation (#7116). Scopes it cannot re-enqueue are reported by reason
// in the result's Skipped field rather than dropped silently.
//
// It also clears the downstream dedup state that would otherwise stop the
// re-projection at source-local structure; the reset subpackage says
// which state and why each piece blocks a rebuild.
//
// All four statements run in one transaction so a refinalize cannot leave the
// queue re-enqueued while its downstream state still says the work is done; that
// half-applied state is invisible until the graph comes back short. They all
// bind one generation set, read once at the top -- see
// refinalizeAffectedGenerations for why re-deriving it per statement is unsafe.
func (s RecoveryStore) RefinalizeScopeProjections(
	ctx context.Context,
	filter recovery.RefinalizeFilter,
	now time.Time,
) (recovery.RefinalizeResult, error) {
	if s.database == nil {
		return recovery.RefinalizeResult{}, fmt.Errorf("recovery store database is required")
	}
	beginner, ok := s.database.(db.Beginner)
	if !ok {
		return recovery.RefinalizeResult{}, fmt.Errorf("refinalize scope projections: database must support Begin")
	}

	tx, err := beginner.Begin(ctx)
	if err != nil {
		return recovery.RefinalizeResult{}, fmt.Errorf("refinalize scope projections: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// Coordination (read once, drain-wait, enqueue, fence check) lives in
	// reset; the drain wait runs after the authoritative read and
	// before any write so a resolver that claimed first cannot get its
	// generation retired mid-flight. A resolver whose lease expires while it
	// is still executing is fenced at relationship-generation activation by
	// its exact queue claim (#6184 P1 review).
	rq := resetQueryer{Transaction: tx}

	generations, skipped, err := reset.ReadAffectedGenerations(ctx, rq, filter)
	if err != nil {
		return recovery.RefinalizeResult{}, err
	}

	if err := reset.WaitForReducerDrain(ctx, rq, generations, s.refinalizeDrainTimeout, s.refinalizeDrainPoll); err != nil {
		return recovery.RefinalizeResult{}, err
	}
	if err := reset.AcquireReducerClaimFence(ctx, rq, generations); err != nil {
		return recovery.RefinalizeResult{}, err
	}

	scopeIDs, err := reset.EnqueueProjectorWork(ctx, rq, generations, now)
	if err != nil {
		return recovery.RefinalizeResult{}, err
	}

	counts, err := reset.ApplyPreRetirement(ctx, tx, generations)
	if err != nil {
		return recovery.RefinalizeResult{}, err
	}
	counts.GenerationsRetired, err = reset.RetireResolutionGenerations(ctx, tx, generations)
	if err != nil {
		return recovery.RefinalizeResult{}, err
	}

	// Zero retired with live leases outstanding means the retirement guard
	// tripped; zero with none is the convergent re-run. See
	// reset.AssertRetirementFenced.
	if err := reset.AssertRetirementFenced(ctx, rq, generations, counts.GenerationsRetired); err != nil {
		return recovery.RefinalizeResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return recovery.RefinalizeResult{}, fmt.Errorf("refinalize scope projections: commit: %w", err)
	}
	committed = true

	return recovery.RefinalizeResult{
		Enqueued:               len(scopeIDs),
		ScopeIDs:               scopeIDs,
		ReducerWorkDeleted:     counts.ReducerWorkDeleted,
		SharedIntentsReopened:  counts.SharedIntentsReopened,
		ReadinessPhasesCleared: counts.ReadinessPhasesCleared,
		GenerationsRetired:     counts.GenerationsRetired,
		Skipped:                skipped,
	}, nil
}
