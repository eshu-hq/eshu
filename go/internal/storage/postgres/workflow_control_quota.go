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

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// DefaultQuotaFloorGrantsPerHour bounds degraded-mode grants per component
// per coordinator-hour when the quota state is unreadable. It is enough for
// a healthy component to make progress and far below what a runaway needs;
// it is a deliberately coarse fail-open budget, not a second quota.
const DefaultQuotaFloorGrantsPerHour = 60

// quotaFloorKey scopes the degraded-mode floor budget. The scope is unknown
// until a grant exists, so the floor is per component, coarser than the
// per-(component, scope) quota state by design.
type quotaFloorKey struct {
	component string
	window    time.Time
}

// claimNextEligibleWithQuota runs the quota-gated claim statement: one
// statement reserves budget and grants the claim, so concurrent replicas
// cannot overshoot. A non-positive configured limit is a configuration
// error and fails closed before any statement runs.
func (s *WorkflowControlStore) claimNextEligibleWithQuota(
	ctx context.Context,
	selector workflow.ClaimSelector,
	now time.Time,
	leaseTTL time.Duration,
	quota workflow.ClaimQuota,
) (workflow.WorkItem, workflow.Claim, bool, error) {
	if quota.MaxGenerationsPerHour < 1 {
		return workflow.WorkItem{}, workflow.Claim{}, false, fmt.Errorf(
			"claim quota for collector kind %q must be at least 1 generation per hour, got %d",
			string(selector.CollectorKind), quota.MaxGenerationsPerHour)
	}
	rows, err := s.database.QueryContext(
		ctx,
		claimNextWorkflowWorkItemQuotaQuery,
		string(selector.CollectorKind),
		selector.CollectorInstanceID,
		now.UTC(),
		selector.OwnerID,
		selector.ClaimID,
		now.UTC().Add(leaseTTL),
		selector.RotationSalt,
		quota.MaxGenerationsPerHour,
	)
	if err != nil {
		if isQuotaStateUnreadable(err) {
			return s.claimNextEligibleFloor(ctx, selector, now, leaseTTL)
		}
		return workflow.WorkItem{}, workflow.Claim{}, false, fmt.Errorf("claim workflow work item: %w", err)
	}
	item, claim, found, err := finishWorkflowClaim(rows, err)
	if err != nil {
		return workflow.WorkItem{}, workflow.Claim{}, false, err
	}
	if !found {
		s.recordQuotaThrottle(ctx, selector, now, quota)
	}
	return item, claim, found, nil
}

// claimNextEligibleFloor is the unreadable-quota fallback: the gated
// statement named component_quota_windows as the failure, so the quota
// protection is degraded, not gone. Grants flow through the plain claim
// statement at the floor budget with one fallback signal per call; once the
// floor is exhausted the claim throttles (found=false) instead of erroring.
// The floor mutex serializes degraded claims within one coordinator; the
// gated path never takes it, so healthy-path concurrency is unaffected.
func (s *WorkflowControlStore) claimNextEligibleFloor(
	ctx context.Context,
	selector workflow.ClaimSelector,
	now time.Time,
	leaseTTL time.Duration,
) (workflow.WorkItem, workflow.Claim, bool, error) {
	kind := string(selector.CollectorKind)
	s.recordQuotaFallback(ctx, selector)
	s.quotaFloorMu.Lock()
	defer s.quotaFloorMu.Unlock()
	if !s.floorAllowLocked(kind, now) {
		return workflow.WorkItem{}, workflow.Claim{}, false, nil
	}
	rows, err := s.database.QueryContext(
		ctx,
		claimNextWorkflowWorkItemQuery,
		kind,
		selector.CollectorInstanceID,
		now.UTC(),
		selector.OwnerID,
		selector.ClaimID,
		now.UTC().Add(leaseTTL),
		selector.RotationSalt,
	)
	item, claim, found, err := finishWorkflowClaim(rows, err)
	if err != nil {
		return workflow.WorkItem{}, workflow.Claim{}, false, err
	}
	if found {
		s.floorConsumeLocked(kind, now)
	}
	return item, claim, found, nil
}

// effectiveClaimQuota resolves the enforced quota for a collector kind. A
// nil lookup means quotas are not configured: the plain claim path runs
// unchanged.
func (s *WorkflowControlStore) effectiveClaimQuota(kind scope.CollectorKind) (workflow.ClaimQuota, bool) {
	if s.QuotaLookup == nil {
		return workflow.ClaimQuota{}, false
	}
	return s.QuotaLookup(kind)
}

// floorAllowLocked reports whether the degraded budget has room. Callers
// hold quotaFloorMu.
func (s *WorkflowControlStore) floorAllowLocked(component string, now time.Time) bool {
	s.floorRollLocked(now)
	return s.quotaFloorUsed[quotaFloorKey{component: component, window: s.quotaFloorWindow}] < s.effectiveFloorGrantsPerHour()
}

// floorConsumeLocked spends one floor grant. Callers hold quotaFloorMu.
func (s *WorkflowControlStore) floorConsumeLocked(component string, now time.Time) {
	s.floorRollLocked(now)
	key := quotaFloorKey{component: component, window: s.quotaFloorWindow}
	s.quotaFloorUsed[key]++
}

// floorRollLocked resets the degraded budget on the coordinator-hour roll.
// Callers hold quotaFloorMu.
func (s *WorkflowControlStore) floorRollLocked(now time.Time) {
	window := now.UTC().Truncate(time.Hour)
	if s.quotaFloorUsed == nil || !window.Equal(s.quotaFloorWindow) {
		s.quotaFloorUsed = make(map[quotaFloorKey]int)
		s.quotaFloorWindow = window
	}
}

// effectiveFloorGrantsPerHour applies the configured floor, defaulting a
// non-positive value to DefaultQuotaFloorGrantsPerHour.
func (s *WorkflowControlStore) effectiveFloorGrantsPerHour() int {
	if s.QuotaFloorGrantsPerHour < 1 {
		return DefaultQuotaFloorGrantsPerHour
	}
	return s.QuotaFloorGrantsPerHour
}

// isQuotaStateUnreadable reports whether err is the quota state (not the
// claim) being unreadable: the statement named component_quota_windows in
// an undefined-table or insufficient-privilege failure. Any other error —
// including a missing claim table or a connection failure — returns false
// so the caller surfaces the real problem instead of degrading.
func isQuotaStateUnreadable(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.Code {
	case "42P01", "42501":
		return strings.Contains(pgErr.Message, "component_quota_windows")
	default:
		return false
	}
}

// recordQuotaThrottle probes whether an empty gated claim hid eligible work
// behind an exhausted budget, and counts it when so. Observability only:
// the probe never admits or denies a grant.
func (s *WorkflowControlStore) recordQuotaThrottle(
	ctx context.Context,
	selector workflow.ClaimSelector,
	now time.Time,
	quota workflow.ClaimQuota,
) {
	rows, err := s.database.QueryContext(
		ctx,
		quotaThrottleProbeQuery,
		string(selector.CollectorKind),
		selector.CollectorInstanceID,
		now.UTC(),
		quota.MaxGenerationsPerHour,
	)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	var hasWork, throttled bool
	if !rows.Next() {
		return
	}
	if err := rows.Scan(&hasWork, &throttled); err != nil {
		return
	}
	if !hasWork || !throttled {
		return
	}
	if s.Instruments != nil && s.Instruments.WorkflowClaimQuotaExceeded != nil {
		s.Instruments.WorkflowClaimQuotaExceeded.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrCollectorKind(string(selector.CollectorKind)),
			telemetry.AttrQuotaKind(telemetry.QuotaKindGenerationsPerHour),
		))
	}
	slog.DebugContext(ctx, "workflow claim throttled by component quota",
		slog.String(telemetry.LogKeyQuotaCollectorKind, string(selector.CollectorKind)),
		slog.String(telemetry.LogKeyQuotaKind, telemetry.QuotaKindGenerationsPerHour),
		slog.Int(telemetry.LogKeyQuotaLimit, quota.MaxGenerationsPerHour),
	)
}

// recordQuotaFallback emits the distinct unreadable-quota signal: a counter
// tick plus a warning log line carrying the scope-free identifiers. The
// scope is intentionally absent from the counter and present on the log.
func (s *WorkflowControlStore) recordQuotaFallback(ctx context.Context, selector workflow.ClaimSelector) {
	if s.Instruments != nil && s.Instruments.WorkflowClaimQuotaUnreadableFallback != nil {
		s.Instruments.WorkflowClaimQuotaUnreadableFallback.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrCollectorKind(string(selector.CollectorKind)),
		))
	}
	slog.WarnContext(ctx, "workflow claim quota state unreadable, using floor rate",
		slog.String(telemetry.LogKeyQuotaCollectorKind, string(selector.CollectorKind)),
		slog.Int(telemetry.LogKeyQuotaLimit, s.effectiveFloorGrantsPerHour()),
	)
}
