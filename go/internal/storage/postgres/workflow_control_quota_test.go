// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// quotaUnitStore builds a store that never touches a database: the quota
// validation and floor-budget paths under test fail or decide before any
// statement runs.
func quotaUnitStore() *WorkflowControlStore {
	return NewWorkflowControlStore(SQLDB{DB: nil})
}

func TestClaimQuotaInvalidLimitFailsClosed(t *testing.T) {
	t.Parallel()

	store := quotaUnitStore()
	store.QuotaLookup = func(scope.CollectorKind) (workflow.ClaimQuota, bool) {
		return workflow.ClaimQuota{MaxGenerationsPerHour: 0}, true
	}
	_, _, _, err := store.ClaimNextEligible(context.Background(), workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "quota-unit-instance",
		OwnerID:             "quota-unit-owner",
		ClaimID:             "quota-unit-claim",
	}, time.Now().UTC(), time.Minute)
	if err == nil {
		t.Fatal("ClaimNextEligible() with zero limit error = nil, want fail-closed configuration error")
	}
}

func TestClaimQuotaAbsentLookupTakesPlainPath(t *testing.T) {
	t.Parallel()

	store := quotaUnitStore()
	if _, ok := store.effectiveClaimQuota(scope.CollectorGit); ok {
		t.Fatal("effectiveClaimQuota() = (_, true) with nil lookup, want (_, false)")
	}
	store.QuotaLookup = func(scope.CollectorKind) (workflow.ClaimQuota, bool) {
		return workflow.ClaimQuota{}, false
	}
	if _, ok := store.effectiveClaimQuota(scope.CollectorGit); ok {
		t.Fatal("effectiveClaimQuota() = (_, true) with negative lookup, want (_, false)")
	}
}

func TestIsQuotaStateUnreadable(t *testing.T) {
	t.Parallel()

	missing := &pgconn.PgError{Code: "42P01", Message: `relation "component_quota_windows" does not exist`}
	if !isQuotaStateUnreadable(missing) {
		t.Fatal("isQuotaStateUnreadable(missing quota table) = false, want true")
	}
	denied := &pgconn.PgError{Code: "42501", Message: `permission denied for table component_quota_windows`}
	if !isQuotaStateUnreadable(denied) {
		t.Fatal("isQuotaStateUnreadable(quota permission denied) = false, want true")
	}
	otherMissing := &pgconn.PgError{Code: "42P01", Message: `relation "workflow_work_items" does not exist`}
	if isQuotaStateUnreadable(otherMissing) {
		t.Fatal("isQuotaStateUnreadable(missing claim table) = true, want false (surface the real error)")
	}
	if isQuotaStateUnreadable(context.DeadlineExceeded) {
		t.Fatal("isQuotaStateUnreadable(context error) = true, want false")
	}
}

func TestQuotaFloorResetsOnHourRoll(t *testing.T) {
	t.Parallel()

	store := quotaUnitStore()
	store.QuotaFloorGrantsPerHour = 1
	first := time.Date(2026, 10, 19, 6, 10, 0, 0, time.UTC)
	if !store.floorAllowLocked("git", first) {
		t.Fatal("floorAllowLocked() = false with fresh budget, want true")
	}
	store.floorConsumeLocked("git", first)
	if store.floorAllowLocked("git", first) {
		t.Fatal("floorAllowLocked() = true after exhausting floor 1, want false")
	}
	rolled := first.Add(time.Hour)
	if !store.floorAllowLocked("git", rolled) {
		t.Fatal("floorAllowLocked() = false after hour roll, want true (budget resets)")
	}
}

func TestQuotaFloorDefaultsWhenUnconfigured(t *testing.T) {
	t.Parallel()

	store := quotaUnitStore()
	if got := store.effectiveFloorGrantsPerHour(); got != DefaultQuotaFloorGrantsPerHour {
		t.Fatalf("effectiveFloorGrantsPerHour() = %d, want default %d", got, DefaultQuotaFloorGrantsPerHour)
	}
	store.QuotaFloorGrantsPerHour = 7
	if got := store.effectiveFloorGrantsPerHour(); got != 7 {
		t.Fatalf("effectiveFloorGrantsPerHour() = %d, want 7", got)
	}
}
