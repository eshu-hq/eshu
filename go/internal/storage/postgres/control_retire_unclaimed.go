// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ErrWorkflowRetireUnknownInstance fails a never-claimed retirement when no
// collector_instances row names the (kind, instance) pair. The sanctioned
// disposition is valid only once the instance is deconfigured, and an
// unknown pair is a typo to fail closed on, never a silent no-op.
var ErrWorkflowRetireUnknownInstance = errors.New("retire unclaimed workflow work items: unknown collector instance")

// ErrWorkflowRetireInstanceActive fails a never-claimed retirement while
// the instance row is still enabled and not deactivated. A live collector
// may still claim those pending rows, so the disposition is not valid yet.
var ErrWorkflowRetireInstanceActive = errors.New("retire unclaimed workflow work items: collector instance still configured")

// RetiredUnclaimedItem names one work item the sanctioned never-claimed
// retirement transitioned to failed_terminal. Scope ids ride the response
// for the operator; they never become metric labels.
type RetiredUnclaimedItem struct {
	WorkItemID string
	ScopeID    string
}

// retireUnclaimedOperatorNoteLimit bounds the operator note folded into the
// retired rows' failure message. The note is operator text, not provider
// output, and the bound keeps one long paste from bloating every retired
// row.
const retireUnclaimedOperatorNoteLimit = 500

// RetireUnclaimedWorkItems retires never-claimed pending work items for one
// deconfigured (collector_kind, collector_instance_id) pair: rows with
// status pending, attempt_count 0, and no current claim move to
// failed_terminal with the operator-retired failure class
// (eshu-hq/eshu#10532).
//
// The instance gate runs first: an unknown pair fails closed with
// ErrWorkflowRetireUnknownInstance and a still-enabled, non-deactivated
// pair fails closed with ErrWorkflowRetireInstanceActive. Only a known,
// deconfigured instance proceeds. The gate and the retire are autocommit
// statements, mirroring the admin skip, and the retire statement rechecks
// the deconfiguration in the same snapshot as its row locks: a
// reactivation that commits between the gate and the retire retires
// nothing (the operator retries into a 409) instead of retiring rows
// under a live collector. A concurrent claim and the retire give every
// row exactly one winner through the per-row lock plus predicate recheck
// (a concurrent claim takes the row, the retire skips it, or vice versa).
//
// The retire locks candidates newest first, then locks the still-eligible
// rows in work_item_id COLLATE "C" order under SKIP LOCKED and rechecks
// every predicate on the locked row before the terminal update — the same
// order the claim path locks in, so claim-versus-retire never deadlocks.
// The returned items arrive in work_item_id order; an empty result with a
// nil error means nothing was eligible, so a repeated call is a safe
// no-op (idempotent like the admin skip).
func (s *WorkflowControlStore) RetireUnclaimedWorkItems(
	ctx context.Context,
	kind scope.CollectorKind,
	instanceID string,
	reason string,
) ([]RetiredUnclaimedItem, error) {
	if s.database == nil {
		return nil, fmt.Errorf("workflow control store database is required")
	}
	collectorKind := strings.TrimSpace(string(kind))
	instanceID = strings.TrimSpace(instanceID)
	if collectorKind == "" {
		return nil, fmt.Errorf("retire unclaimed workflow work items: collector kind is required")
	}
	if instanceID == "" {
		return nil, fmt.Errorf("retire unclaimed workflow work items: collector instance id is required")
	}

	known, active, err := s.probeRetireUnclaimedInstance(ctx, collectorKind, instanceID)
	if err != nil {
		return nil, err
	}
	if !known {
		return nil, fmt.Errorf("%w: %s/%s", ErrWorkflowRetireUnknownInstance, collectorKind, instanceID)
	}
	if active {
		return nil, fmt.Errorf("%w: %s/%s", ErrWorkflowRetireInstanceActive, collectorKind, instanceID)
	}

	message := fmt.Sprintf(
		"retired by operator: never-claimed pending item for deconfigured instance %s/%s",
		collectorKind,
		instanceID,
	)
	if note := strings.TrimSpace(reason); note != "" {
		message += ": " + truncateRetireUnclaimedNote(note)
	}
	retired, err := s.retireUnclaimedEligible(ctx, s.database, collectorKind, instanceID, message)
	if err != nil {
		return nil, err
	}

	s.recordUnclaimedRetired(ctx, collectorKind, instanceID, len(retired))
	return retired, nil
}

// retireUnclaimedEligible runs the retire statement on queryer and returns
// what it transitioned. Queryer is the store's database on the admin path;
// tests pass a transaction or the database directly to prove the statement's
// own rechecks (deconfiguration, eligibility) without racing the gate. The
// statement's NOT EXISTS recheck shares its snapshot with the row locks, so
// a reactivation that commits after the gate probe still retires nothing.
func (s *WorkflowControlStore) retireUnclaimedEligible(
	ctx context.Context,
	queryer db.Queryer,
	collectorKind string,
	instanceID string,
	message string,
) ([]RetiredUnclaimedItem, error) {
	rows, err := queryer.QueryContext(
		ctx,
		retireUnclaimedWorkflowWorkItemsQuery,
		collectorKind,
		instanceID,
		time.Now().UTC(),
		workflow.FailureClassOperatorRetiredUnclaimed,
		message,
		workflow.FailureOperationOperatorRetire,
	)
	if err != nil {
		return nil, fmt.Errorf("retire unclaimed workflow work items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	retired := make([]RetiredUnclaimedItem, 0)
	for rows.Next() {
		var item RetiredUnclaimedItem
		if err := rows.Scan(&item.WorkItemID, &item.ScopeID); err != nil {
			return nil, fmt.Errorf("retire unclaimed workflow work items: %w", err)
		}
		retired = append(retired, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("retire unclaimed workflow work items: %w", err)
	}
	return retired, nil
}

// probeRetireUnclaimedInstance reports whether any collector_instances row
// names the pair (known) and whether that row is still an enabled,
// non-deactivated configuration (active).
func (s *WorkflowControlStore) probeRetireUnclaimedInstance(
	ctx context.Context,
	collectorKind string,
	instanceID string,
) (known bool, active bool, err error) {
	rows, err := s.database.QueryContext(ctx, retireUnclaimedInstanceGateQuery, instanceID, collectorKind)
	if err != nil {
		return false, false, fmt.Errorf("retire unclaimed workflow work items: read collector instance: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, false, fmt.Errorf("retire unclaimed workflow work items: read collector instance: %w", err)
		}
		return false, false, fmt.Errorf("retire unclaimed workflow work items: instance gate returned no rows")
	}
	if err := rows.Scan(&known, &active); err != nil {
		return false, false, fmt.Errorf("retire unclaimed workflow work items: read collector instance: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, false, fmt.Errorf("retire unclaimed workflow work items: read collector instance: %w", err)
	}
	return known, active, nil
}

// recordUnclaimedRetired emits the operator signal for one sanctioned
// retirement: a counter increment by the retired count labeled with the
// operator-supplied collector kind and instance, plus a structured log line
// carrying the same three values. A call that retired nothing logs without
// touching the counter. Scope ids stay out of the metric labels and the
// log; the retired rows carry them for the operator. A nil Instruments
// (binaries without a wired meter provider) skips the counter and keeps
// the log, exactly like the quota throttle path.
func (s *WorkflowControlStore) recordUnclaimedRetired(
	ctx context.Context,
	collectorKind string,
	instanceID string,
	count int,
) {
	// A zero-count call retired nothing, so it logs without touching the
	// counter: a Sum data point with value 0 would claim work moved that
	// never moved.
	if count > 0 && s.Instruments != nil && s.Instruments.WorkflowUnclaimedRetired != nil {
		s.Instruments.WorkflowUnclaimedRetired.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrCollectorKind(collectorKind),
			telemetry.AttrCollectorInstanceID(instanceID),
		))
	}
	slog.InfoContext(ctx, "workflow unclaimed work items retired",
		slog.String(telemetry.LogKeyCollectorKind, collectorKind),
		slog.String("collector_instance_id", instanceID),
		slog.Int("retired_count", count),
	)
}

// truncateRetireUnclaimedNote clamps the operator note folded into retired
// rows' failure message to retireUnclaimedOperatorNoteLimit runes. The cut
// runs on the rune slice so a multi-byte rune is never split: a byte cut
// would fold invalid UTF-8 into every retired row's failure message.
func truncateRetireUnclaimedNote(note string) string {
	runes := []rune(note)
	if len(runes) <= retireUnclaimedOperatorNoteLimit {
		return note
	}
	return string(runes[:retireUnclaimedOperatorNoteLimit])
}
