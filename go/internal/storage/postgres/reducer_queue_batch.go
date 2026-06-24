// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// ClaimBatch claims up to limit reducer work items in a single Postgres
// round-trip using FOR UPDATE SKIP LOCKED. Implements reducer.BatchWorkSource.
func (q ReducerQueue) ClaimBatch(ctx context.Context, limit int) ([]reducer.Intent, error) {
	if err := q.validateClaim(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 16
	}

	now := q.now()
	rows, err := q.db.QueryContext(
		ctx,
		claimReducerWorkBatchQuery,
		now,
		q.claimDomainFilters(),
		q.LeaseOwner,
		now.Add(q.LeaseDuration),
		q.RequireProjectorDrainBeforeClaim,
		q.ExpectedSourceLocalProjectors,
		q.semanticEntityClaimLimit(),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("batch claim reducer work: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var intents []reducer.Intent
	for rows.Next() {
		intent, err := scanReducerIntent(rows)
		if err != nil {
			return nil, fmt.Errorf("batch claim scan: %w", err)
		}
		intents = append(intents, intent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("batch claim reducer work: %w", err)
	}

	return intents, nil
}

// AckBatch acknowledges multiple claimed reducer work items in a single
// round-trip. Implements reducer.BatchWorkSink.
func (q ReducerQueue) AckBatch(ctx context.Context, intents []reducer.Intent, _ []reducer.Result) error {
	if err := q.validateClaim(); err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	now := q.now()

	ids := make([]string, len(intents))
	for i, intent := range intents {
		ids[i] = intent.IntentID
	}

	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	args = append(args, now, q.LeaseOwner)
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+3)
		args = append(args, id)
	}

	query := fmt.Sprintf(`
UPDATE fact_work_items
SET status = 'succeeded',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE work_item_id IN (%s)
  AND stage = 'reducer'
  AND lease_owner = $2
  AND status IN ('claimed', 'running')
`, strings.Join(placeholders, ", "))

	if _, err := q.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("batch ack reducer work (%d items): %w", len(intents), err)
	}

	return nil
}

// FailBatch marks multiple claimed reducer work items as failed in a single
// round-trip. Each intent is failed with its corresponding error.
func (q ReducerQueue) FailBatch(ctx context.Context, intents []reducer.Intent, causes []error) error {
	if err := q.validateClaim(); err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	now := q.now()
	for i, intent := range intents {
		cause := causes[i]
		if cause == nil {
			continue
		}
		if err := q.failIntent(ctx, intent, cause); err != nil {
			return fmt.Errorf("batch fail item %d (%s): %w", i, intent.IntentID, err)
		}
	}
	_ = now // used by individual failIntent calls via q.now()
	return nil
}
