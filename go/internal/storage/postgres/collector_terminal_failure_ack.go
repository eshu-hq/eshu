// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// MaxTerminalFailureAckReasonLength bounds the operator's acknowledgement
// reason in characters. It matches the CHECK on migration 231.
const MaxTerminalFailureAckReasonLength = 512

var (
	// ErrTerminalFailureAckInstanceNotFound reports an instance that is not
	// registered, is disabled, or was deactivated. The status read never
	// reports such an instance, so there is nothing to acknowledge.
	ErrTerminalFailureAckInstanceNotFound = errors.New("collector instance is not registered or not active")
	// ErrTerminalFailureAckNoFailure reports that the instance has no
	// unresolved failed_terminal work item on the scope: it never failed
	// there, or a later completed item already resolved the failure.
	ErrTerminalFailureAckNoFailure = errors.New("no unresolved terminal failure on the instance and scope")
	// ErrTerminalFailureAckClassChanged reports that the newest unresolved
	// failure on the scope has a different class than the operator named, so
	// the operator would be acknowledging a failure they have not seen.
	ErrTerminalFailureAckClassChanged = errors.New("the unresolved terminal failure changed class since it was read")
)

// TerminalFailureAckRequest is one operator acknowledgement. FailureClass is
// the bounded class the operator read from collector readiness; the write
// refuses when the failure on the scope has since changed class. ActorClass and
// ActorIDHash are the audit actor, never a raw identity.
type TerminalFailureAckRequest struct {
	InstanceID   string
	ScopeID      string
	FailureClass string
	Reason       string
	ActorClass   string
	ActorIDHash  string
}

// TerminalFailureAckResult is the recorded acknowledgement. Unchanged is true
// when an earlier acknowledgement already covered the failure, so nothing was
// written and AcknowledgedAt, FailureAt, and Reason describe that earlier one.
type TerminalFailureAckResult struct {
	CollectorKind  string
	InstanceID     string
	ScopeID        string
	FailureClass   string
	FailureAt      time.Time
	AcknowledgedAt time.Time
	Reason         string
	Unchanged      bool
}

// TerminalFailureAckStore writes operator acknowledgements of collector
// terminal failures (#8830). The status read in
// workflowCollectorTerminalFailuresQuery consumes the rows.
type TerminalFailureAckStore struct {
	db  db.ExecQueryer
	now func() time.Time
}

// NewTerminalFailureAckStore returns a store over database that stamps
// acknowledgements with the wall clock.
func NewTerminalFailureAckStore(database db.ExecQueryer) *TerminalFailureAckStore {
	return &TerminalFailureAckStore{db: database, now: func() time.Time { return time.Now().UTC() }}
}

// terminalFailureAckTargetQuery reads, for one instance and scope, the
// instance's liveness, the newest unresolved failed_terminal row on the scope
// (the failure an operator can acknowledge), and the acknowledgement that
// already covers that row. The ack join reuses workflowTerminalFailureAckCovers,
// the predicate the status read uses, so "already acknowledged" here means
// exactly "the status read suppresses it". The lateral keeps the status read's
// shape: newest first, stopping at the first row no completed item of the
// target supersedes.
const terminalFailureAckTargetQuery = `
SELECT
    i.collector_kind,
    i.enabled AND i.deactivated_at IS NULL,
    t.updated_at,
    COALESCE(t.last_failure_class, ''),
    k.failure_at,
    k.acknowledged_at,
    k.reason
FROM collector_instances AS i
LEFT JOIN LATERAL (
    SELECT
        r.collector_kind,
        r.collector_instance_id,
        r.scope_id,
        r.last_failure_class,
        r.updated_at
    FROM workflow_work_items AS r
    WHERE r.collector_kind = i.collector_kind
      AND r.collector_instance_id = i.instance_id
      AND r.scope_id = $2
      AND r.status = 'failed_terminal'
      AND NOT EXISTS (
          SELECT 1
          FROM workflow_work_items AS s
          WHERE s.collector_kind = r.collector_kind
            AND s.collector_instance_id = r.collector_instance_id
            AND s.scope_id = r.scope_id
            AND s.status = 'completed'
            AND s.updated_at > r.updated_at
      )
    ORDER BY r.updated_at DESC, r.work_item_id ASC
    LIMIT 1
) AS t ON TRUE
LEFT JOIN collector_terminal_failure_acknowledgements AS k
  ON ` + workflowTerminalFailureAckCovers + `
WHERE i.instance_id = $1
`

// terminalFailureAckUpsertQuery records the acknowledgement. The row is keyed
// by (kind, instance, scope), so a second acknowledgement of a later episode on
// the same target replaces the first: only the newest acknowledged episode can
// suppress. Concurrent acknowledgements of the same failure converge on one row
// and both are valid; the later writer's reason and actor win.
const terminalFailureAckUpsertQuery = `
INSERT INTO collector_terminal_failure_acknowledgements (
    collector_kind, collector_instance_id, scope_id, failure_class, failure_at,
    reason, actor_class, actor_id_hash, acknowledged_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (collector_kind, collector_instance_id, scope_id) DO UPDATE SET
    failure_class = EXCLUDED.failure_class,
    failure_at = EXCLUDED.failure_at,
    reason = EXCLUDED.reason,
    actor_class = EXCLUDED.actor_class,
    actor_id_hash = EXCLUDED.actor_id_hash,
    acknowledged_at = EXCLUDED.acknowledged_at
`

// Acknowledge records an acknowledgement for the newest unresolved terminal
// failure of one instance and scope. It refuses an unknown or inactive
// instance, a scope with no unresolved failure, and a failure whose class is
// not the one the operator named. A repeat of an acknowledgement that already
// covers the failure writes nothing and returns Unchanged.
//
// The read and the write are two autocommit statements on purpose: the write
// stores the failure the read saw, so a failure that lands in between is
// either the same episode (still covered) or a different class (reported
// again); neither is ever suppressed unseen. A client cancel after the upsert
// committed answers with an error while the acknowledgement is durable, and a
// repeat returns Unchanged.
func (s *TerminalFailureAckStore) Acknowledge(ctx context.Context, req TerminalFailureAckRequest) (TerminalFailureAckResult, error) {
	if s == nil || s.db == nil {
		return TerminalFailureAckResult{}, errors.New("terminal failure acknowledgement store is not configured")
	}
	req.InstanceID = strings.TrimSpace(req.InstanceID)
	req.ScopeID = strings.TrimSpace(req.ScopeID)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.InstanceID == "" || req.ScopeID == "" || req.Reason == "" {
		return TerminalFailureAckResult{}, errors.New("instance id, scope id, and reason are required")
	}
	if utf8.RuneCountInString(req.Reason) > MaxTerminalFailureAckReasonLength {
		return TerminalFailureAckResult{}, fmt.Errorf("reason is longer than %d characters", MaxTerminalFailureAckReasonLength)
	}

	target, err := s.readTarget(ctx, req)
	if err != nil {
		return TerminalFailureAckResult{}, err
	}
	result := TerminalFailureAckResult{
		CollectorKind: target.kind, InstanceID: req.InstanceID, ScopeID: req.ScopeID,
		FailureClass: collector.BoundedFailureClassForKind(target.kind, target.persistedClass),
	}
	if target.coveredAt.Valid {
		result.FailureAt = target.coveredFailureAt.Time.UTC()
		result.AcknowledgedAt = target.coveredAt.Time.UTC()
		result.Reason = target.coveredReason
		result.Unchanged = true
		return result, nil
	}

	acknowledgedAt := s.now().UTC()
	if _, err := s.db.ExecContext(ctx, terminalFailureAckUpsertQuery,
		target.kind, req.InstanceID, req.ScopeID, target.persistedClass, target.failureAt.UTC(),
		req.Reason, req.ActorClass, req.ActorIDHash, acknowledgedAt,
	); err != nil {
		return TerminalFailureAckResult{}, fmt.Errorf("record collector terminal failure acknowledgement: %w", err)
	}
	result.FailureAt = target.failureAt.UTC()
	result.AcknowledgedAt = acknowledgedAt
	result.Reason = req.Reason
	return result, nil
}

// terminalFailureAckTarget is the row terminalFailureAckTargetQuery returns.
type terminalFailureAckTarget struct {
	kind             string
	failureAt        time.Time
	persistedClass   string
	coveredFailureAt sql.NullTime
	coveredAt        sql.NullTime
	coveredReason    string
}

// readTarget resolves the instance and its newest unresolved failure on the
// scope, and applies the three refusals.
func (s *TerminalFailureAckStore) readTarget(ctx context.Context, req TerminalFailureAckRequest) (terminalFailureAckTarget, error) {
	rows, err := s.db.QueryContext(ctx, terminalFailureAckTargetQuery, req.InstanceID, req.ScopeID)
	if err != nil {
		return terminalFailureAckTarget{}, fmt.Errorf("read collector terminal failure acknowledgement target: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return terminalFailureAckTarget{}, fmt.Errorf("read collector terminal failure acknowledgement target: %w", err)
		}
		return terminalFailureAckTarget{}, ErrTerminalFailureAckInstanceNotFound
	}
	var (
		target    terminalFailureAckTarget
		active    bool
		failureAt sql.NullTime
		reason    sql.NullString
	)
	if err := rows.Scan(&target.kind, &active, &failureAt, &target.persistedClass,
		&target.coveredFailureAt, &target.coveredAt, &reason); err != nil {
		return terminalFailureAckTarget{}, fmt.Errorf("read collector terminal failure acknowledgement target: %w", err)
	}
	if err := rows.Err(); err != nil {
		return terminalFailureAckTarget{}, fmt.Errorf("read collector terminal failure acknowledgement target: %w", err)
	}
	switch {
	case !active:
		return terminalFailureAckTarget{}, ErrTerminalFailureAckInstanceNotFound
	case !failureAt.Valid:
		return terminalFailureAckTarget{}, ErrTerminalFailureAckNoFailure
	}
	target.failureAt = failureAt.Time
	target.coveredReason = reason.String
	named := strings.TrimSpace(req.FailureClass)
	if named == "" || named != collector.BoundedFailureClassForKind(target.kind, target.persistedClass) {
		return terminalFailureAckTarget{}, ErrTerminalFailureAckClassChanged
	}
	return target, nil
}
