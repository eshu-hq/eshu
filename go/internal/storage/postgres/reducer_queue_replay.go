// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	queuestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ReopenSucceededReducerSetClause is the SET list every succeeded-to-pending
// reducer reopen shares: status, attempt_count, the identity authorization
// statuses, lease columns, visible_at, failure evidence, updated_at,
// reopened_at, and the requeue reason -- every state column the reducer claim
// path reads. It is the single source (#7731): reopenSucceededReducerWork,
// replaySucceededReducerDomain, and the admin reopen compose it, so the lists
// cannot drift.
//
// It is a function of the closed-vocabulary reason, not a constant, so the
// compiler forces every composer to declare its own reason (#11615): a
// shared constant could not hardcode one reason for all callers. The reason
// is embedded as a validated literal (never a bind parameter) so composers
// keep their own WHERE arg numbering after $1, the reopen timestamp.
func ReopenSucceededReducerSetClause(reason telemetry.RequeueReason) string {
	return `status = 'pending',
    attempt_count = 0,
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
    updated_at = $1,
    reopened_at = $1,
    ` + queuestore.RequeueReasonSetAssignment(reason.SQLLiteral()) + `,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL`
}

// reopenSucceededReducerWork builds the single-row succeeded-to-pending
// reopen for one reason. It resets reopened_at to the reopen timestamp ($1)
// alongside attempt_count -- both mark the start of a fresh repair cycle --
// but never touches created_at, which stays the row's original enqueue time
// for every other consumer of Intent.EnqueuedAt (see migration 088's doc
// comment).
func reopenSucceededReducerWork(reason telemetry.RequeueReason) string {
	return `
UPDATE fact_work_items
SET ` + ReopenSucceededReducerSetClause(reason) + `
WHERE work_item_id = $2
  AND stage = 'reducer'
  AND status = 'succeeded'
`
}

// replaySucceededReducerDomain builds ReopenSucceeded's domain-wide sibling
// for one reason (see reopenSucceededReducerWork's doc comment for why
// reopened_at resets here too).
func replaySucceededReducerDomain(reason telemetry.RequeueReason) string {
	return `
UPDATE fact_work_items
SET ` + ReopenSucceededReducerSetClause(reason) + `
WHERE scope_id = $2
  AND generation_id = $3
  AND domain = $4
  AND stage = 'reducer'
  AND status = 'succeeded'
`
}

// CountInFlightByDomain returns the number of reducer work items for one domain
// that have not yet reached a terminal status.
func (q ReducerQueue) CountInFlightByDomain(
	ctx context.Context,
	domain reducercontract.Domain,
) (int, error) {
	if err := q.validateDB(); err != nil {
		return 0, err
	}
	if err := domain.Validate(); err != nil {
		return 0, fmt.Errorf("count in-flight reducer work: %w", err)
	}

	rows, err := q.database.QueryContext(
		ctx,
		countInFlightReducerWorkByDomainQuery,
		string(domain),
	)
	if err != nil {
		return 0, fmt.Errorf("count in-flight reducer work: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("count in-flight reducer work: %w", err)
		}
		return 0, errors.New("count in-flight reducer work: missing count row")
	}

	var count int
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("count in-flight reducer work: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("count in-flight reducer work: %w", err)
	}

	return count, nil
}

func (q ReducerQueue) validateDB() error {
	if q.database == nil {
		return errors.New("reducer queue database is required")
	}

	return nil
}
