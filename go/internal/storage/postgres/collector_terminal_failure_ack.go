// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
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

// Default and maximum row counts of ListTerminalFailureAcks.
const (
	defaultTerminalFailureAckListLimit = 100
	maxTerminalFailureAckListLimit     = 500
)

var (
	// ErrTerminalFailureAckInstanceNotFound reports an instance that is not
	// registered, does not match the named collector kind, is disabled, or was
	// deactivated. The status read never reports such an instance, so there is
	// nothing to acknowledge.
	ErrTerminalFailureAckInstanceNotFound = errors.New("collector instance is not registered or not active")
	// ErrTerminalFailureAckNoFailure reports that the instance has no
	// unresolved failed_terminal work item inside its window on the scope: it
	// never failed there, a later completed item resolved the failure, or the
	// failure aged out. An acknowledgement names one existing failure; a blind
	// pre-acknowledgement would silence a future, different one.
	ErrTerminalFailureAckNoFailure = errors.New("no unresolved terminal failure inside the window on the instance and scope")
	// ErrTerminalFailureAckClassChanged reports that the newest unresolved
	// failure on the scope has a different bounded class than the one the
	// caller named, so the operator would be acknowledging a failure they have
	// not seen.
	ErrTerminalFailureAckClassChanged = errors.New("the unresolved terminal failure has a different class than the one named")
)

// TerminalFailureAckRequest is one operator acknowledgement. FailureClass is
// optional: when set it must equal the bounded class of the failure being
// acknowledged, so a failure that changed class since the operator read it is
// refused. ActorClass, ActorIDHash, and CorrelationID are the audit actor and
// request, never a raw identity.
type TerminalFailureAckRequest struct {
	CollectorKind string
	InstanceID    string
	ScopeID       string
	FailureClass  string
	Reason        string
	ActorClass    string
	ActorIDHash   string
	CorrelationID string
}

// TerminalFailureAckTarget is the failure an acknowledgement names: the newest
// unresolved failed_terminal work item on one instance and scope. FailureClass
// and FailureOperation are bounded tokens; the raw persisted class is what the
// row stores.
type TerminalFailureAckTarget struct {
	CollectorKind    string
	InstanceID       string
	ScopeID          string
	WorkItemID       string
	FailureClass     string
	FailureOperation string
	FailureAt        time.Time
	rawClass         string
}

// TerminalFailureAckResult is the recorded acknowledgement.
type TerminalFailureAckResult struct {
	TerminalFailureAckTarget
	AcknowledgedAt time.Time
}

// TerminalFailureAckRecord is a stored acknowledgement as an admin reads it.
// Unlike every status surface, it carries the operator's reason.
type TerminalFailureAckRecord struct {
	CollectorKind  string
	InstanceID     string
	ScopeID        string
	FailureClass   string
	FailureAt      time.Time
	WorkItemID     string
	Reason         string
	ActorClass     string
	CorrelationID  string
	AcknowledgedAt time.Time
}

// TerminalFailureAckListFilter narrows ListTerminalFailureAcks. Empty fields
// match everything. Limit defaults to 100 and is capped at 500.
type TerminalFailureAckListFilter struct {
	CollectorKind string
	InstanceID    string
	ScopeID       string
	Limit         int
}

// TerminalFailureAckStore writes operator acknowledgements of collector
// terminal failures (#8830). The status read in
// workflowCollectorTerminalFailuresQuery consumes the rows.
type TerminalFailureAckStore struct {
	db       db.ExecQueryer
	beginner db.Beginner
	now      func() time.Time
}

// NewTerminalFailureAckStore returns a store that reads through database and
// writes in a transaction opened by beginner, stamping acknowledgements with
// the wall clock.
func NewTerminalFailureAckStore(database db.ExecQueryer, beginner db.Beginner) *TerminalFailureAckStore {
	return &TerminalFailureAckStore{db: database, beginner: beginner, now: func() time.Time { return time.Now().UTC() }}
}

// terminalFailureAckTargetQuery reads the newest unresolved failed_terminal row
// on one instance and scope inside the instance's window ($4 is the cutoff): the
// failure an operator can acknowledge. The ordering and the unresolved probe
// match the status read's, so "acknowledgeable" and "reported" agree.
const terminalFailureAckTargetQuery = `
SELECT
    t.work_item_id,
    COALESCE(t.last_failure_class, ''),
    COALESCE(t.last_failure_operation, ''),
    t.updated_at
FROM workflow_work_items AS t
WHERE t.collector_kind = $1
  AND t.collector_instance_id = $2
  AND t.scope_id = $3
  AND t.status = 'failed_terminal'
  AND t.updated_at >= $4
  AND NOT EXISTS (
      SELECT 1
      FROM workflow_work_items AS s
      WHERE s.collector_kind = t.collector_kind
        AND s.collector_instance_id = t.collector_instance_id
        AND s.scope_id = t.scope_id
        AND s.status = 'completed'
        AND s.updated_at > t.updated_at
  )
ORDER BY t.updated_at DESC, t.work_item_id ASC
LIMIT 1
`

// terminalFailureAckUpsertQuery records the acknowledgement. The row is keyed by
// (kind, instance, scope), so a later acknowledgement of a newer episode on the
// same target replaces the first: only the newest acknowledged episode can
// suppress. Two acknowledgements with different idempotency keys both commit and
// the last writer's row stands; both are audited by the caller.
const terminalFailureAckUpsertQuery = `
INSERT INTO collector_terminal_failure_acknowledgements (
    collector_kind, collector_instance_id, scope_id, failure_class, failure_at,
    work_item_id, reason, actor_class, actor_id_hash, correlation_id, acknowledged_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (collector_kind, collector_instance_id, scope_id) DO UPDATE SET
    failure_class = EXCLUDED.failure_class,
    failure_at = EXCLUDED.failure_at,
    work_item_id = EXCLUDED.work_item_id,
    reason = EXCLUDED.reason,
    actor_class = EXCLUDED.actor_class,
    actor_id_hash = EXCLUDED.actor_id_hash,
    correlation_id = EXCLUDED.correlation_id,
    acknowledged_at = EXCLUDED.acknowledged_at
`

const terminalFailureAckListQuery = `
SELECT
    collector_kind, collector_instance_id, scope_id, failure_class, failure_at,
    work_item_id, reason, actor_class, COALESCE(correlation_id, ''), acknowledged_at
FROM collector_terminal_failure_acknowledgements
WHERE ($1 = '' OR collector_kind = $1)
  AND ($2 = '' OR collector_instance_id = $2)
  AND ($3 = '' OR scope_id = $3)
ORDER BY acknowledged_at DESC, collector_kind ASC, collector_instance_id ASC, scope_id ASC
LIMIT $4
`

func (r TerminalFailureAckRequest) normalized() TerminalFailureAckRequest {
	r.CollectorKind = strings.TrimSpace(r.CollectorKind)
	r.InstanceID = strings.TrimSpace(r.InstanceID)
	r.ScopeID = strings.TrimSpace(r.ScopeID)
	r.FailureClass = strings.TrimSpace(r.FailureClass)
	r.Reason = strings.TrimSpace(r.Reason)
	return r
}

func (r TerminalFailureAckRequest) validate() error {
	if r.CollectorKind == "" || r.InstanceID == "" || r.ScopeID == "" || r.Reason == "" {
		return errors.New("collector kind, instance id, scope id, and reason are required")
	}
	if utf8.RuneCountInString(r.Reason) > MaxTerminalFailureAckReasonLength {
		return fmt.Errorf("reason is longer than %d characters", MaxTerminalFailureAckReasonLength)
	}
	return nil
}

// ResolveTarget returns the failure an acknowledgement of the request would
// name, without writing. The handler calls it before it claims the idempotency
// key, so an expected refusal leaves the key unconsumed for a corrected retry.
// It refuses an unknown, mismatched, disabled, or deactivated instance, a scope
// with no unresolved failure inside the instance's window, and a failure whose
// bounded class differs from a non-empty req.FailureClass.
func (s *TerminalFailureAckStore) ResolveTarget(ctx context.Context, req TerminalFailureAckRequest) (TerminalFailureAckTarget, error) {
	if s == nil || s.db == nil {
		return TerminalFailureAckTarget{}, errors.New("terminal failure acknowledgement store is not configured")
	}
	return s.resolveTarget(ctx, s.db, req.normalized())
}

// Acknowledge records an acknowledgement for the newest unresolved terminal
// failure of one instance and scope. It re-reads the target and upserts the row
// in one READ COMMITTED transaction: the write touches only the acknowledgement
// row, so it shares no lock with the failure writers (which update claimed
// workflow_work_items rows) and cannot deadlock with them. A failed_terminal row
// that lands after the read is covered or reported by the status predicate, never
// suppressed unseen. A client cancel after the commit leaves the acknowledgement
// durable; the caller's idempotency ledger answers a retry.
func (s *TerminalFailureAckStore) Acknowledge(ctx context.Context, req TerminalFailureAckRequest) (TerminalFailureAckResult, error) {
	if s == nil || s.db == nil || s.beginner == nil {
		return TerminalFailureAckResult{}, errors.New("terminal failure acknowledgement store is not configured")
	}
	req = req.normalized()
	if err := req.validate(); err != nil {
		return TerminalFailureAckResult{}, err
	}
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return TerminalFailureAckResult{}, fmt.Errorf("begin collector terminal failure acknowledgement: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	target, err := s.resolveTarget(ctx, tx, req)
	if err != nil {
		return TerminalFailureAckResult{}, err
	}
	acknowledgedAt := s.now().UTC()
	var correlationID any
	if req.CorrelationID != "" {
		correlationID = req.CorrelationID
	}
	if _, err := tx.ExecContext(ctx, terminalFailureAckUpsertQuery,
		target.CollectorKind, target.InstanceID, target.ScopeID, target.rawClass, target.FailureAt.UTC(),
		target.WorkItemID, req.Reason, req.ActorClass, req.ActorIDHash, correlationID, acknowledgedAt,
	); err != nil {
		return TerminalFailureAckResult{}, fmt.Errorf("record collector terminal failure acknowledgement: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return TerminalFailureAckResult{}, fmt.Errorf("commit collector terminal failure acknowledgement: %w", err)
	}
	committed = true
	return TerminalFailureAckResult{TerminalFailureAckTarget: target, AcknowledgedAt: acknowledgedAt}, nil
}

// resolveTarget resolves the instance (kind and active) and its newest
// unresolved failure on the scope on queryer, which may be the store's database
// or the acknowledgement transaction.
func (s *TerminalFailureAckStore) resolveTarget(ctx context.Context, queryer db.Queryer, req TerminalFailureAckRequest) (TerminalFailureAckTarget, error) {
	if req.CollectorKind == "" || req.InstanceID == "" || req.ScopeID == "" {
		return TerminalFailureAckTarget{}, errors.New("collector kind, instance id, and scope id are required")
	}
	instances, err := listCoordinatorCollectorInstances(ctx, queryer)
	if err != nil {
		return TerminalFailureAckTarget{}, err
	}
	var scanInterval time.Duration
	found := false
	for _, instance := range instances {
		if instance.InstanceID == req.InstanceID && instance.CollectorKind == req.CollectorKind &&
			instance.Enabled && instance.DeactivatedAt.IsZero() {
			scanInterval, found = instance.ScanInterval, true
			break
		}
	}
	if !found {
		return TerminalFailureAckTarget{}, ErrTerminalFailureAckInstanceNotFound
	}
	cutoff := terminalFailureCutoff(s.now(), scanInterval)
	rows, err := queryer.QueryContext(ctx, terminalFailureAckTargetQuery, req.CollectorKind, req.InstanceID, req.ScopeID, cutoff)
	if err != nil {
		return TerminalFailureAckTarget{}, fmt.Errorf("read collector terminal failure acknowledgement target: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return TerminalFailureAckTarget{}, fmt.Errorf("read collector terminal failure acknowledgement target: %w", err)
		}
		return TerminalFailureAckTarget{}, ErrTerminalFailureAckNoFailure
	}
	target := TerminalFailureAckTarget{CollectorKind: req.CollectorKind, InstanceID: req.InstanceID, ScopeID: req.ScopeID}
	var operation string
	if err := rows.Scan(&target.WorkItemID, &target.rawClass, &operation, &target.FailureAt); err != nil {
		return TerminalFailureAckTarget{}, fmt.Errorf("read collector terminal failure acknowledgement target: %w", err)
	}
	if err := rows.Err(); err != nil {
		return TerminalFailureAckTarget{}, fmt.Errorf("read collector terminal failure acknowledgement target: %w", err)
	}
	target.FailureAt = target.FailureAt.UTC()
	target.FailureClass = collector.BoundedFailureClassForKind(req.CollectorKind, target.rawClass)
	target.FailureOperation = collector.BoundedFailureOperation(operation)
	if req.FailureClass != "" && req.FailureClass != target.FailureClass {
		return TerminalFailureAckTarget{}, ErrTerminalFailureAckClassChanged
	}
	return target, nil
}

// ListTerminalFailureAcks returns stored acknowledgements, newest first, for an
// admin read. It is the only read that returns the operator's reason; no status
// surface does.
func (s *TerminalFailureAckStore) ListTerminalFailureAcks(ctx context.Context, filter TerminalFailureAckListFilter) ([]TerminalFailureAckRecord, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("terminal failure acknowledgement store is not configured")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultTerminalFailureAckListLimit
	}
	limit = min(limit, maxTerminalFailureAckListLimit)
	rows, err := s.db.QueryContext(ctx, terminalFailureAckListQuery,
		strings.TrimSpace(filter.CollectorKind), strings.TrimSpace(filter.InstanceID), strings.TrimSpace(filter.ScopeID), limit)
	if err != nil {
		return nil, fmt.Errorf("list collector terminal failure acknowledgements: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var records []TerminalFailureAckRecord
	for rows.Next() {
		var record TerminalFailureAckRecord
		var rawClass string
		if err := rows.Scan(&record.CollectorKind, &record.InstanceID, &record.ScopeID, &rawClass, &record.FailureAt,
			&record.WorkItemID, &record.Reason, &record.ActorClass, &record.CorrelationID, &record.AcknowledgedAt); err != nil {
			return nil, fmt.Errorf("list collector terminal failure acknowledgements: %w", err)
		}
		record.FailureClass = collector.BoundedFailureClassForKind(record.CollectorKind, rawClass)
		record.FailureAt = record.FailureAt.UTC()
		record.AcknowledgedAt = record.AcknowledgedAt.UTC()
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list collector terminal failure acknowledgements: %w", err)
	}
	return records, nil
}
