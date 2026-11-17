// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestRunActiveMaintenanceExpiresStaleRunsBeforeReconcile pins the Track B2
// ordering (eshu-hq/eshu#10791): runActiveMaintenance expires stale open
// runs before reconciling them, so the reconcile that closes the run sees
// the terminal rows in the same pass.
func TestRunActiveMaintenanceExpiresStaleRunsBeforeReconcile(t *testing.T) {
	t.Parallel()

	var order []string
	store := &fakeStore{
		expireHook: func() { order = append(order, "expire") },
	}
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive,
			ClaimsEnabled:  true,
			MaxOpenRunAge:  24 * time.Hour,
		},
		Store:   store,
		Metrics: &fakeMetrics{},
		Clock:   func() time.Time { return time.Now().UTC() },
	}
	store.runReconcileHook = func(int) { order = append(order, "reconcile") }

	if err := service.runActiveMaintenance(context.Background()); err != nil {
		t.Fatalf("runActiveMaintenance() error = %v, want nil", err)
	}
	if store.expireCalls != 1 {
		t.Fatalf("expire calls = %d, want 1", store.expireCalls)
	}
	if store.runReconcileCalls != 1 {
		t.Fatalf("reconcile calls = %d, want 1", store.runReconcileCalls)
	}
	if len(order) != 2 || order[0] != "expire" || order[1] != "reconcile" {
		t.Fatalf("call order = %v, want [expire reconcile]", order)
	}
}

// TestRunActiveMaintenanceStaleExpiryErrorAbortsMaintenance proves an expiry
// failure aborts the pass loudly instead of reconciling on a half-expired
// queue.
func TestRunActiveMaintenanceStaleExpiryErrorAbortsMaintenance(t *testing.T) {
	t.Parallel()

	store := &fakeStore{expireErr: errors.New("expiry store boom")}
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive,
			ClaimsEnabled:  true,
			MaxOpenRunAge:  24 * time.Hour,
		},
		Store:   store,
		Metrics: &fakeMetrics{},
		Clock:   func() time.Time { return time.Now().UTC() },
	}

	err := service.runActiveMaintenance(context.Background())
	if err == nil {
		t.Fatal("runActiveMaintenance() error = nil, want expiry failure")
	}
	if store.runReconcileCalls != 0 {
		t.Fatalf("reconcile calls = %d, want 0 (no reconcile after expiry failure)", store.runReconcileCalls)
	}
}

// TestConfigDefaultsMaxOpenRunAge pins the effective validation contract:
// an unset bound defaults to 24h through withDefaults (like the sibling
// knobs), so Validate passes with the bound in force rather than failing
// startup or silently disabling the expiry.
func TestConfigDefaultsMaxOpenRunAge(t *testing.T) {
	t.Parallel()

	instance := desiredSelectorInstance(scope.CollectorGit, testComponentActivationConfig)
	config := activeConfigWithInstance(instance)
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil (unset bound defaults)", err)
	}
}

// TestStaleRunExpiryObservationRecorded proves the operator signal fires:
// an expiry pass records the outcome, duration, and expired-item count.
func TestStaleRunExpiryObservationRecorded(t *testing.T) {
	t.Parallel()

	metrics := &fakeMetrics{}
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive,
			ClaimsEnabled:  true,
			MaxOpenRunAge:  24 * time.Hour,
		},
		Store:   &fakeStore{expiredItems: []workflow.ExpiredStaleRunItem{{RunID: "r1", WorkItemID: "w1", ScopeID: "s1"}}},
		Metrics: metrics,
		Clock:   func() time.Time { return time.Now().UTC() },
	}

	if err := service.runExpireStaleOpenRuns(context.Background()); err != nil {
		t.Fatalf("runExpireStaleOpenRuns() error = %v, want nil", err)
	}
	if got := len(metrics.staleRunExpiryObserved); got != 1 {
		t.Fatalf("stale expiry observations = %d, want 1", got)
	}
	if got := metrics.staleRunExpiryObserved[0].ExpiredItems; got != 1 {
		t.Fatalf("expired items observed = %d, want 1", got)
	}
}
