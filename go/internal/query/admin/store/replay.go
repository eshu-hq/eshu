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
	queuestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
	"github.com/eshu-hq/eshu/go/internal/telemetry"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
)

// replayUpdateClause is the admin replay's SET list. It carries
// queuestore.ReplayRetryAllowanceAssignments, the replay retry allowance the
// recovery replay templates share (#11182): the projector baseline takes the
// row's attempt_count, admission_wait_attempts goes back to 0, and
// attempt_count itself is kept (never lowered) because it is the claim fence.
// Reducer rows take the admin_replay reason; projector rows keep NULL by
// retaining their prior value, which no projector writer ever sets (#11615
// decision 3).
var replayUpdateClause = `
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
    updated_at = $1,
    reopened_at = $1,
    last_requeue_reason = ` + queuestore.RequeueReasonStageGatedValue(
	telemetry.RequeueReasonAdminReplay.SQLLiteral(), "work.") + `,
    ` + queuestore.ReplayRetryAllowanceAssignments + `
`

// ReplayFailedWorkItems requeues terminal work and records replay events while
// preserving required identity status authorizations and newer worker claims.
// Every replayed row was dead_letter or failed, so the replay stamps
// reopened_at with the replay time (#9320) and the row's backlog age and
// reducer readiness grace restart from the replay.
// The replay clears failure_class on the work row, so it returns the
// selection-time class as a trailing column and records that in the replay
// event (#7387); the returned items carry post-update truth.
// A replayed projector row gets a new row's retry allowance (#11182): the
// returned items carry the replay_attempt_baseline the replay set, and each
// event records that baseline and the admission_wait_attempts the row had
// before the replay reset them.
func (s *postgresStore) ReplayFailedWorkItems(ctx context.Context, f admin.ReplayWorkItemFilter) ([]admin.WorkItem, error) {
	now := s.time()
	query, args := buildMutatingWorkItemsQuery(f.WorkItemIDs, f.ScopeID, f.Stage, f.FailureClass, f.Limit, 1, true, true, replayUpdateClause, f.ExcludeFailureClasses...)
	args = append([]any{now}, args...)
	items, prior, err := scanReplayedWorkItems(ctx, s.database, query, args...)
	if err != nil {
		return nil, err
	}
	// #11615: the replay UPDATE matched only dead_letter/failed rows, so
	// every returned reducer row transitioned (admin_replay); projector
	// rows keep NULL by the stage gate and are not counted. The UPDATE ran
	// autocommit, so this emission is post-commit by construction.
	var reducerRequeues int64
	for _, item := range items {
		if item.Stage == "reducer" {
			reducerRequeues++
		}
	}
	telemetry.RecordReducerRequeue(ctx, s.instruments, telemetry.RequeueReasonAdminReplay, reducerRequeues)
	if err := s.insertReplayEvents(ctx, items, prior, strings.TrimSpace(f.OperatorNote), now); err != nil {
		return nil, err
	}
	return items, nil
}

// replayPrior is what a replayed row carried at selection time, before the
// replay cleared or reset it.
type replayPrior struct {
	failureClass          *string
	admissionWaitAttempts int64
}

// scanReplayedWorkItems scans the replay query's rows: the shared work columns
// carry post-update truth (failure_class cleared), and the trailing
// replayed_failure_class and prior_admission_wait_attempts columns carry the
// selection-time values per row for the replay event (#7387, #11182).
func scanReplayedWorkItems(ctx context.Context, database db.ExecQueryer, query string, args ...any) ([]admin.WorkItem, map[string]replayPrior, error) {
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("query work items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []admin.WorkItem
	prior := make(map[string]replayPrior)
	for rows.Next() {
		var item admin.WorkItem
		var leaseOwner sql.NullString
		var failureClass sql.NullString
		var failureMessage sql.NullString
		var visibleAt sql.NullTime
		var replayedClass sql.NullString
		var priorAdmissionWaits int64
		var failureDetails sql.NullString
		if err := rows.Scan(
			&item.WorkItemID,
			&item.ScopeID,
			&item.GenerationID,
			&item.Stage,
			&item.Domain,
			&item.Status,
			&item.AttemptCount,
			&item.ReplayAttemptBaseline,
			&leaseOwner,
			&failureClass,
			&failureMessage,
			&item.CreatedAt,
			&item.UpdatedAt,
			&visibleAt,
			&failureDetails,
			&replayedClass,
			&priorAdmissionWaits,
		); err != nil {
			return nil, nil, fmt.Errorf("scan replayed work item: %w", err)
		}
		applyWorkItemDetails(&item, failureDetails)
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
		rowPrior := replayPrior{admissionWaitAttempts: priorAdmissionWaits}
		if replayedClass.Valid {
			class := replayedClass.String
			rowPrior.failureClass = &class
		}
		prior[item.WorkItemID] = rowPrior
		items = append(items, item)
	}
	return items, prior, rows.Err()
}

// insertReplayEvents writes one fact_replay_events row per replayed item. For
// a projector row the event records the replay_attempt_baseline the replay set
// and the admission_wait_attempts the row had spent before the replay reset
// it (#11182). A reducer row gets no baseline from the replay yet, so both
// columns stay NULL, as they do on events written before migration 334.
func (s *postgresStore) insertReplayEvents(ctx context.Context, items []admin.WorkItem, prior map[string]replayPrior, operatorNote string, now time.Time) error {
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
    created_at,
    replay_attempt_baseline,
    prior_admission_wait_attempts
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
`
	for _, item := range items {
		id, err := newStoreID("replay")
		if err != nil {
			return err
		}
		rowPrior := prior[item.WorkItemID]
		// The replay clears failure_class on the work row, so the event
		// records the selection-time class the row carried when it was
		// replayed (#7387), not the cleared post-update value.
		var failureClass any
		if rowPrior.failureClass != nil {
			failureClass = *rowPrior.failureClass
		}
		var note any
		if operatorNote != "" {
			note = operatorNote
		}
		var baseline, priorAdmissionWaits any
		if item.Stage == "projector" {
			baseline = int64(item.ReplayAttemptBaseline)
			priorAdmissionWaits = rowPrior.admissionWaitAttempts
		}
		if _, err := s.database.ExecContext(ctx, query, id, item.WorkItemID, item.ScopeID, item.GenerationID, failureClass, note, now, baseline, priorAdmissionWaits); err != nil {
			return fmt.Errorf("insert replay event: %w", err)
		}
	}
	return nil
}
