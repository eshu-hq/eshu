// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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

// quotaFloorUnitFixture builds a store over a fake database with real
// instruments on a manual reader, so floor-path tests observe the shipped
// fallback counter without a live database.
func quotaFloorUnitFixture(
	t *testing.T,
	database *fake.ExecQueryer,
	floor int,
) (*WorkflowControlStore, context.Context, sdkmetric.Reader) {
	t.Helper()
	store := NewWorkflowControlStore(database)
	store.QuotaFloorGrantsPerHour = floor
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("quota-floor-unit-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	store.Instruments = inst
	return store, context.Background(), reader
}

func quotaFloorUnitSelector(nonce int) workflow.ClaimSelector {
	return workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "quota-floor-unit-instance",
		OwnerID:             "quota-floor-unit-owner",
		ClaimID:             time.Now().UTC().Format("150405.000000000") + string(rune('a'+nonce)),
	}
}

// TestQuotaFloorExhaustedStaysSilent proves an exhausted floor emits no
// fallback signal and issues no statement: once the budget is spent, further
// floor calls throttle quietly instead of ticking
// quota_unreadable_fallback_total and WARN-spamming per poll.
func TestQuotaFloorExhaustedStaysSilent(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{{}, {}, {}},
	}
	store, ctx, reader := quotaFloorUnitFixture(t, database, 1)
	now := time.Now().UTC()
	// Spend the single floor grant without touching the database, so the
	// floor is exhausted before the first call under test.
	store.quotaFloorMu.Lock()
	store.floorConsumeLocked(string(scope.CollectorGit), now)
	store.quotaFloorMu.Unlock()

	for i := 0; i < 3; i++ {
		_, _, found, err := store.claimNextEligibleFloor(ctx, quotaFloorUnitSelector(i), now, time.Minute)
		if err != nil {
			t.Fatalf("claimNextEligibleFloor() error = %v, want nil (quiet throttle)", err)
		}
		if found {
			t.Fatal("claimNextEligibleFloor() found = true, want false (floor exhausted)")
		}
	}
	if got := len(database.Queries); got != 0 {
		t.Fatalf("exhausted floor issued %d statements, want 0 (no work past the budget check)", got)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if got := counterTotal(rm, "eshu_dp_workflow_claim_quota_unreadable_fallback_total"); got != 0 {
		t.Fatalf("quota_unreadable_fallback_total = %d, want 0 (exhausted floor ticks nothing)", got)
	}
}

// TestQuotaFloorReleasesLockDuringClaimQuery proves the floor mutex is not
// held across the plain-claim round-trip: while one degraded claim is inside
// QueryContext, the budget lock must be acquirable, so a slow database
// cannot serialize degraded claims across all components on one lock.
func TestQuotaFloorReleasesLockDuringClaimQuery(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once bool
	database := &fake.ExecQueryer{
		Routes: []fake.Route{func(_ string, _ []any) (*fake.Rows, bool) {
			if !once {
				once = true
				close(entered)
			}
			<-release
			return &fake.Rows{}, true
		}},
	}
	store, ctx, _ := quotaFloorUnitFixture(t, database, 60)
	done := make(chan error, 1)
	go func() {
		_, _, _, err := store.claimNextEligibleFloor(
			ctx, quotaFloorUnitSelector(0), time.Now().UTC(), time.Minute)
		done <- err
	}()
	<-entered
	if !store.quotaFloorMu.TryLock() {
		t.Fatal("quotaFloorMu still held inside the floor claim QueryContext, want it released before I/O")
	}
	store.quotaFloorMu.Unlock()
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("claimNextEligibleFloor() error = %v, want nil", err)
	}
}
