// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/rebuild/reset"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// countDeadLetterBacklogTemplate counts terminal rows matching a replay filter.
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
	refinalizeDrainTimeout          time.Duration
	refinalizeDrainPoll             time.Duration
	refinalizeClaimFenceRetryBudget time.Duration

	// instruments records the superseded-generation replay fence counter.
	// Nil is a no-op.
	instruments *telemetry.Instruments

	// logger receives the refinalize delta-active logs (#7797). Nil uses
	// slog.Default(). It is set with WithRecoveryLogger.
	logger *slog.Logger
}

// resetQueryer adapts a transaction to reset.Queryer.
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

// WithRecoveryLogger sets the logger for the refinalize delta-active logs
// (#7797). Nil keeps slog.Default().
func WithRecoveryLogger(logger *slog.Logger) RecoveryStoreOption {
	return func(s *RecoveryStore) {
		s.logger = logger
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

// buildUnfencedReplayPredicate renders replay filters without the generation fence.
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

// nonEmptyClasses drops blank failure-class entries.
func nonEmptyClasses(classes []string) []string {
	out := make([]string, 0, len(classes))
	for _, class := range classes {
		if trimmed := strings.TrimSpace(class); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// buildReplayFailedWorkItemsQuery renders bounded or unbounded replay SQL and args.
// Its single statement also counts skipped fenced rows, so reporting cannot fail
// after the replay commits; non-projector stages return a constant zero.
func buildReplayFailedWorkItemsQuery(filter recovery.ReplayFilter, now time.Time) (string, []any) {
	template, args, start := replayFailedWorkItemsTemplate, []any{now.UTC()}, 2
	if filter.Limit > 0 {
		template, args, start = replayFailedWorkItemsBoundedTemplate, []any{now.UTC(), filter.Limit}, 3
	}

	predicate := buildReplayPredicate(filter, start)
	skipped := replaySkippedNoneCTE
	if filter.Stage == recovery.StageProjector {
		skipped = fmt.Sprintf(replaySkippedProjectorCTE, buildUnfencedReplayPredicate(filter, start).clause)
	}
	return fmt.Sprintf(template, predicate.clause, skipped), append(args, predicate.args...)
}

// ReplayFailedWorkItems resets terminal work items to pending for the given
// stage and filter criteria. New Go runtime rows use dead_letter; legacy failed
// rows remain replayable until they age out. When the filter carries
// ExcludeFailureClasses (the drain path), those classes are excluded store-side
// so a broad selector can never replay a manual-review row. A positive
// filter.Limit bounds the mutation in SQL so only that many rows are replayed.
//
// One statement both replays and counts the superseded-generation projector
// rows it left terminal, so the two commit or fail together. Each result row
// carries the skip count; work_item_id is NULL on the single row a replay that
// moved nothing returns.
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

	var (
		workItemIDs []string
		skipped     int
	)
	for rows.Next() {
		var id sql.NullString
		if scanErr := rows.Scan(&id, &skipped); scanErr != nil {
			return recovery.ReplayResult{}, fmt.Errorf("replay failed work items: %w", scanErr)
		}
		if id.Valid {
			workItemIDs = append(workItemIDs, id.String)
		}
	}
	if err := rows.Err(); err != nil {
		return recovery.ReplayResult{}, fmt.Errorf("replay failed work items: %w", err)
	}

	recordSupersededGenerationFence(ctx, s.instruments, failure.ReplayGenerationSupersededClass, skipped)

	return recovery.ReplayResult{
		Stage:                       filter.Stage,
		Replayed:                    len(workItemIDs),
		WorkItemIDs:                 workItemIDs,
		SkippedSupersededGeneration: skipped,
	}, nil
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

// queryCount scans a single-row COUNT result into out.
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

// RefinalizeScopeProjections re-enqueues selected scopes and clears dedup state.
// Fence 55P03 retries the whole transaction; exhaustion returns a busy sentinel.
func (s RecoveryStore) RefinalizeScopeProjections(
	ctx context.Context,
	filter recovery.RefinalizeFilter,
	now time.Time,
) (recovery.RefinalizeResult, error) {
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	retryBudget := s.refinalizeClaimFenceRetryBudget
	if retryBudget <= 0 {
		retryBudget = reset.DefaultRefinalizeClaimFenceRetryBudget
	}
	admission := reset.NewFenceAdmissionBudget(ctx, retryBudget)
	defer admission.Close()
	budgetError := func(attempt int, cause string) error {
		elapsed := admission.Elapsed()
		logger.WarnContext(ctx, "refinalize claim fence admission budget exhausted",
			slog.String("outcome", "blocked/claim_fence_busy"), slog.Int("attempts", attempt),
			slog.Duration("retry_elapsed", elapsed), slog.String("phase", "fence_admission"),
			slog.String("cause", cause), slog.String("last_sqlstate", "55P03"))
		return fmt.Errorf("%w after %s and %d attempts", recovery.ErrRefinalizeClaimFenceBusy, elapsed.Round(time.Millisecond), attempt)
	}
	for attempt := 1; ; attempt++ {
		result, err := s.refinalizeScopeProjectionsAttempt(admission.Context(), admission, filter, now)
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return recovery.RefinalizeResult{}, err
		}
		if admission.Expired() {
			return recovery.RefinalizeResult{}, budgetError(attempt, "admission_deadline")
		}
		var lockTimeout *reset.ClaimFenceLockTimeoutError
		if !errors.As(err, &lockTimeout) || !coordination.IsLockNotAvailable(err) {
			return recovery.RefinalizeResult{}, err
		}
		elapsed := admission.Elapsed()
		if elapsed >= retryBudget {
			return recovery.RefinalizeResult{}, budgetError(attempt, "last_fence_timeout")
		}
		delay := 100*time.Millisecond + time.Duration(rand.Int64N(401))*time.Millisecond
		if remaining := retryBudget - elapsed; delay > remaining {
			delay = remaining
		}
		logger.InfoContext(ctx, "refinalize claim fence timed out; retrying transaction",
			slog.Int("attempt", attempt), slog.Duration("backoff", delay),
			slog.Duration("retry_elapsed", elapsed), slog.String("sqlstate", "55P03"))
		timer := time.NewTimer(delay)
		select {
		case <-admission.Context().Done():
			timer.Stop()
			if ctx.Err() != nil {
				return recovery.RefinalizeResult{}, fmt.Errorf("refinalize claim fence retry: %w", ctx.Err())
			}
			return recovery.RefinalizeResult{}, budgetError(attempt, "admission_deadline")
		case <-timer.C:
		}
	}
}

func (s RecoveryStore) refinalizeScopeProjectionsAttempt(
	ctx context.Context,
	admission *reset.FenceAdmissionBudget,
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

	// Read, drain, enqueue and fence coordination lives in reset. The drain
	// precedes writes; activation also fences a resolver whose lease expires
	// while it runs (#6184).
	rq := resetQueryer{Transaction: tx}

	generations, skipped, err := reset.ReadAffectedGenerations(ctx, rq, filter)
	if err != nil {
		return recovery.RefinalizeResult{}, err
	}

	if err := reset.WaitForReducerDrain(ctx, rq, generations, s.refinalizeDrainTimeout, s.refinalizeDrainPoll); err != nil {
		return recovery.RefinalizeResult{}, err
	}
	if err := reset.AcquireReducerClaimFenceWithAdmission(ctx, rq, generations, admission); err != nil {
		return recovery.RefinalizeResult{}, err
	}

	scopeIDs, err := reset.EnqueueProjectorWork(ctx, rq, generations, now)
	if err != nil {
		return recovery.RefinalizeResult{}, err
	}

	// A delta generation restores only its changed files; request the full
	// re-parse that restores the rest in this same transaction (#7797).
	deltaActive, deltaGenerations, err := reset.RequestDeltaActiveReindex(ctx, rq, generations)
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
	s.reportDeltaActive(ctx, deltaActive, deltaGenerations)

	return recovery.RefinalizeResult{
		Enqueued:                          len(scopeIDs),
		ScopeIDs:                          scopeIDs,
		ReducerWorkDeleted:                counts.ReducerWorkDeleted,
		SharedIntentsReopened:             counts.SharedIntentsReopened,
		ReadinessPhasesCleared:            counts.ReadinessPhasesCleared,
		GenerationsRetired:                counts.GenerationsRetired,
		SharedProjectionAcceptanceCleared: counts.SharedProjectionAcceptanceCleared,
		Skipped:                           skipped,
		DeltaActive:                       deltaActive,
	}, nil
}

// reportDeltaActive emits the operator signals for one committed refinalize's
// delta-active scopes (#7797): the count as a span attribute,
// eshu_dp_recovery_delta_active_scopes_total by outcome, and the logs, whose
// WARN budget recovery.LogDeltaActive owns. It runs only after the commit, so
// a rolled-back refinalize reports nothing it did not do.
func (s RecoveryStore) reportDeltaActive(
	ctx context.Context,
	report recovery.DeltaActiveScopes,
	delta []recovery.DeltaActiveGeneration,
) {
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.Int(contract.SpanAttrRecoveryDeltaActiveScopes, report.Total()),
	)
	recovery.LogDeltaActive(ctx, s.logger, report, delta)
	if s.instruments == nil || s.instruments.RecoveryDeltaActiveScopes == nil {
		return
	}
	for _, outcome := range report.Outcomes() {
		s.instruments.RecoveryDeltaActiveScopes.Add(
			context.WithoutCancel(ctx), int64(report.ByOutcome[outcome]),
			metric.WithAttributes(telemetry.AttrOutcome(outcome)),
		)
	}
}
