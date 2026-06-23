package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/scope"
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

const refinalizeScopeProjectionsQuery = `
INSERT INTO fact_work_items (
    work_item_id,
    scope_id,
    generation_id,
    stage,
    domain,
    status,
    attempt_count,
    lease_owner,
    claim_until,
    visible_at,
    last_attempt_at,
    next_attempt_at,
    failure_class,
    failure_message,
    failure_details,
    payload,
    created_at,
    updated_at
)
SELECT
    'refinalize_' || scope.scope_id || '_' || scope.active_generation_id,
    scope.scope_id,
    scope.active_generation_id,
    'projector',
    'source_local',
    'pending',
    0,
    NULL,
    NULL,
    $1,
    NULL,
    NULL,
    NULL,
    NULL,
    NULL,
    '{}'::jsonb,
    $1,
    $1
FROM ingestion_scopes AS scope
WHERE scope.scope_id = ANY($2)
  AND scope.active_generation_id IS NOT NULL
  AND scope.status = 'active'
ON CONFLICT (work_item_id) DO UPDATE
SET status = 'pending',
    attempt_count = 0,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = EXCLUDED.visible_at,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL,
    updated_at = EXCLUDED.updated_at
RETURNING scope_id
`

// RecoveryStore implements recovery.ReplayStore over Postgres.
type RecoveryStore struct {
	db ExecQueryer
}

// NewRecoveryStore constructs a Postgres-backed recovery store.
func NewRecoveryStore(db ExecQueryer) RecoveryStore {
	return RecoveryStore{db: db}
}

// replayPredicate is the dynamic WHERE tail (after the terminal-status clause)
// for one replay filter, plus the positional args that fill its placeholders.
// startPlaceholder is the next $N to use, so a query with a leading timestamp
// arg ($1) starts the predicate at $2 while a count query starts it at $1.
type replayPredicate struct {
	clause string
	args   []any
}

// buildReplayPredicate renders the stage, scope, failure-class, and
// manual-review-exclusion predicates shared by the replay UPDATE and the backlog
// COUNT. Both call it so a count can never select a different row set than the
// replay it precedes, and the exclusion is applied uniformly: an unscoped drain
// that selects a broad set still cannot move a manual-review (poison) row.
func buildReplayPredicate(filter recovery.ReplayFilter, startPlaceholder int) replayPredicate {
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
	if s.db == nil {
		return recovery.ReplayResult{}, fmt.Errorf("recovery store database is required")
	}

	query, args := buildReplayFailedWorkItemsQuery(filter, now)

	rows, err := s.db.QueryContext(ctx, query, args...)
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

	return recovery.ReplayResult{
		Stage:       filter.Stage,
		Replayed:    len(workItemIDs),
		WorkItemIDs: workItemIDs,
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
	if s.db == nil {
		return 0, fmt.Errorf("recovery store database is required")
	}

	predicate := buildReplayPredicate(filter, 1)
	query := fmt.Sprintf(countDeadLetterBacklogTemplate, predicate.clause)

	rows, err := s.db.QueryContext(ctx, query, predicate.args...)
	if err != nil {
		return 0, fmt.Errorf("count dead letter backlog: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var depth int
	if rows.Next() {
		if scanErr := rows.Scan(&depth); scanErr != nil {
			return 0, fmt.Errorf("count dead letter backlog: %w", scanErr)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("count dead letter backlog: %w", err)
	}

	return depth, nil
}

// ReplayCollectorGenerations marks collector generation commit failures for
// source-level replay.
func (s RecoveryStore) ReplayCollectorGenerations(
	ctx context.Context,
	filter recovery.CollectorGenerationReplayFilter,
	now time.Time,
) (recovery.CollectorGenerationReplayResult, error) {
	if s.db == nil {
		return recovery.CollectorGenerationReplayResult{}, fmt.Errorf("recovery store database is required")
	}

	result, err := NewCollectorGenerationDeadLetterStore(s.db).ReplayGenerationDeadLetters(ctx, collector.GenerationDeadLetterReplayFilter{
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

// RefinalizeScopeProjections re-enqueues projector work for the given scope
// IDs by inserting new pending work items for their active generations.
func (s RecoveryStore) RefinalizeScopeProjections(
	ctx context.Context,
	filter recovery.RefinalizeFilter,
	now time.Time,
) (recovery.RefinalizeResult, error) {
	if s.db == nil {
		return recovery.RefinalizeResult{}, fmt.Errorf("recovery store database is required")
	}

	rows, err := s.db.QueryContext(ctx, refinalizeScopeProjectionsQuery, now.UTC(), filter.ScopeIDs)
	if err != nil {
		return recovery.RefinalizeResult{}, fmt.Errorf("refinalize scope projections: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var scopeIDs []string
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			return recovery.RefinalizeResult{}, fmt.Errorf("refinalize scope projections: %w", scanErr)
		}
		scopeIDs = append(scopeIDs, id)
	}
	if err := rows.Err(); err != nil {
		return recovery.RefinalizeResult{}, fmt.Errorf("refinalize scope projections: %w", err)
	}

	return recovery.RefinalizeResult{
		Enqueued: len(scopeIDs),
		ScopeIDs: scopeIDs,
	}, nil
}
