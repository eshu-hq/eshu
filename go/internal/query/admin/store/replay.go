// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
)

// ReplayFailedWorkItems requeues terminal work and records replay events while
// preserving required identity status authorizations and newer worker claims.
// The replay clears failure_class on the work row, so it returns the
// selection-time class as a trailing column and records that in the replay
// event (#7387); the returned items carry post-update truth.
func (s *postgresStore) ReplayFailedWorkItems(ctx context.Context, f admin.ReplayWorkItemFilter) ([]admin.WorkItem, error) {
	now := s.time()
	query, args := buildMutatingWorkItemsQuery(f.WorkItemIDs, f.ScopeID, f.Stage, f.FailureClass, f.Limit, 1, true, true, `
SET status = 'pending',
    attempt_count = GREATEST(work.attempt_count, 1),
    container_image_identity_v2_authorized_status = CASE
        WHEN work.container_image_identity_v2_required THEN 'pending' ELSE ''
    END,
    container_image_identity_v3_authorized_status = CASE
        WHEN work.container_image_identity_v3_required THEN 'pending' ELSE ''
    END,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $1,
    next_attempt_at = NULL,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL,
    updated_at = $1
`, f.ExcludeFailureClasses...)
	args = append([]any{now}, args...)
	items, priorClasses, err := scanReplayedWorkItems(ctx, s.database, query, args...)
	if err != nil {
		return nil, err
	}
	if err := s.insertReplayEvents(ctx, items, priorClasses, strings.TrimSpace(f.OperatorNote), now); err != nil {
		return nil, err
	}
	return items, nil
}

// scanReplayedWorkItems scans the replay query's rows: the shared work columns
// carry post-update truth (failure_class cleared), and the trailing
// replayed_failure_class column carries the selection-time class per row for
// the replay event (#7387).
func scanReplayedWorkItems(ctx context.Context, database db.ExecQueryer, query string, args ...any) ([]admin.WorkItem, map[string]*string, error) {
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("query work items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []admin.WorkItem
	priorClasses := make(map[string]*string)
	for rows.Next() {
		var item admin.WorkItem
		var leaseOwner sql.NullString
		var failureClass sql.NullString
		var failureMessage sql.NullString
		var visibleAt sql.NullTime
		var replayedClass sql.NullString
		if err := rows.Scan(
			&item.WorkItemID,
			&item.ScopeID,
			&item.GenerationID,
			&item.Stage,
			&item.Domain,
			&item.Status,
			&item.AttemptCount,
			&leaseOwner,
			&failureClass,
			&failureMessage,
			&item.CreatedAt,
			&item.UpdatedAt,
			&visibleAt,
			&replayedClass,
		); err != nil {
			return nil, nil, fmt.Errorf("scan replayed work item: %w", err)
		}
		if leaseOwner.Valid {
			item.LeaseOwner = &leaseOwner.String
		}
		if failureClass.Valid {
			item.FailureClass = &failureClass.String
		}
		if failureMessage.Valid {
			item.FailureMessage = &failureMessage.String
		}
		if visibleAt.Valid {
			item.VisibleAt = &visibleAt.Time
		}
		if replayedClass.Valid {
			class := replayedClass.String
			priorClasses[item.WorkItemID] = &class
		}
		items = append(items, item)
	}
	return items, priorClasses, rows.Err()
}

func (s *postgresStore) insertReplayEvents(ctx context.Context, items []admin.WorkItem, priorClasses map[string]*string, operatorNote string, now time.Time) error {
	if len(items) == 0 {
		return nil
	}

	const query = `
INSERT INTO fact_replay_events (
    replay_event_id,
    work_item_id,
    scope_id,
    generation_id,
    failure_class,
    operator_note,
    created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
`
	for _, item := range items {
		id, err := newStoreID("replay")
		if err != nil {
			return err
		}
		// The replay clears failure_class on the work row, so the event
		// records the selection-time class the row carried when it was
		// replayed (#7387), not the cleared post-update value.
		var failureClass any
		if class := priorClasses[item.WorkItemID]; class != nil {
			failureClass = *class
		}
		var note any
		if operatorNote != "" {
			note = operatorNote
		}
		if _, err := s.database.ExecContext(ctx, query, id, item.WorkItemID, item.ScopeID, item.GenerationID, failureClass, note, now); err != nil {
			return fmt.Errorf("insert replay event: %w", err)
		}
	}
	return nil
}
