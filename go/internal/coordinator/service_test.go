// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestServiceRunReconcilesImmediately(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		instances: []workflow.CollectorInstance{{
			InstanceID:    "collector-git-primary",
			CollectorKind: scope.CollectorGit,
			Mode:          workflow.CollectorModeContinuous,
			Enabled:       true,
		}},
	}
	metrics := &fakeMetrics{}
	now := time.Date(2026, time.April, 20, 20, 0, 0, 0, time.UTC)
	service := Service{
		Config: Config{
			DeploymentMode:    "dark",
			ReconcileInterval: time.Hour,
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    "collector-git-primary",
				CollectorKind: scope.CollectorGit,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
			}},
		},
		Store:   store,
		Metrics: metrics,
		Clock:   func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(store.observed), 1; got != want {
		t.Fatalf("reconcile calls = %d, want %d", got, want)
	}
	if got, want := len(metrics.observations), 1; got != want {
		t.Fatalf("metrics observations = %d, want %d", got, want)
	}
	if got, want := metrics.observations[0].Outcome, reconcileOutcomeSuccess; got != want {
		t.Fatalf("metrics outcome = %q, want %q", got, want)
	}
	if got, want := metrics.observations[0].DesiredCount, 1; got != want {
		t.Fatalf("metrics desired count = %d, want %d", got, want)
	}
	if got, want := metrics.observations[0].DurableCount, 1; got != want {
		t.Fatalf("metrics durable count = %d, want %d", got, want)
	}
}

func TestServiceRunRejectsNilStore(t *testing.T) {
	t.Parallel()

	service := Service{
		Config: Config{DeploymentMode: "dark", ReconcileInterval: time.Second},
	}

	if err := service.Run(context.Background()); err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}
}

func TestServiceRunReturnsInitialReconcileErrorAndRecordsFailure(t *testing.T) {
	t.Parallel()

	metrics := &fakeMetrics{}
	service := Service{
		Config: Config{DeploymentMode: "dark", ReconcileInterval: time.Second},
		Store: &fakeStore{
			reconcileErr: errors.New("boom"),
		},
		Metrics: metrics,
	}

	err := service.Run(context.Background())
	if err == nil || err.Error() != "initial collector reconciliation: boom" {
		t.Fatalf("Run() error = %v, want initial collector reconciliation: boom", err)
	}
	if got, want := len(metrics.observations), 1; got != want {
		t.Fatalf("metrics observations = %d, want %d", got, want)
	}
	if got, want := metrics.observations[0].Outcome, reconcileOutcomeReconcileError; got != want {
		t.Fatalf("metrics outcome = %q, want %q", got, want)
	}
}

func TestServiceRunReturnsDurableStateReadErrorAndRecordsFailure(t *testing.T) {
	t.Parallel()

	metrics := &fakeMetrics{}
	service := Service{
		Config: Config{
			DeploymentMode:     "dark",
			ReconcileInterval:  time.Second,
			CollectorInstances: []workflow.DesiredCollectorInstance{{InstanceID: "collector-git-primary", CollectorKind: scope.CollectorGit, Mode: workflow.CollectorModeContinuous, Enabled: true}},
		},
		Store: &fakeStore{
			listErr: errors.New("state read failed"),
		},
		Metrics: metrics,
	}

	err := service.Run(context.Background())
	if err == nil || err.Error() != "initial collector reconciliation: list durable collector instances: state read failed" {
		t.Fatalf("Run() error = %v, want durable state read error", err)
	}
	if got, want := len(metrics.observations), 1; got != want {
		t.Fatalf("metrics observations = %d, want %d", got, want)
	}
	if got, want := metrics.observations[0].Outcome, reconcileOutcomeStateReadError; got != want {
		t.Fatalf("metrics outcome = %q, want %q", got, want)
	}
}

func TestServiceRunLogsDriftWarning(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	service := Service{
		Config: Config{
			DeploymentMode:     "dark",
			ReconcileInterval:  time.Hour,
			CollectorInstances: []workflow.DesiredCollectorInstance{{InstanceID: "collector-git-primary", CollectorKind: scope.CollectorGit, Mode: workflow.CollectorModeContinuous, Enabled: true}},
		},
		Store:  &fakeStore{},
		Logger: logger,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := logs.String(); !bytes.Contains([]byte(got), []byte(`"msg":"workflow coordinator collector instance drift detected"`)) {
		t.Fatalf("logs = %s, want drift warning", got)
	}
}

func TestServiceRunActiveModeExecutesReaperAndWorkflowReconciliation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 20, 30, 0, 0, time.UTC)
	store := &fakeStore{
		instances: []workflow.CollectorInstance{{
			InstanceID:    "collector-git-primary",
			CollectorKind: scope.CollectorGit,
			Mode:          workflow.CollectorModeContinuous,
			Enabled:       true,
		}},
		reapedClaims: []workflow.Claim{{ClaimID: "claim-1", WorkItemID: "item-1", FencingToken: 1, OwnerID: "owner-a", Status: workflow.ClaimStatusExpired, ClaimedAt: now.Add(-time.Minute), HeartbeatAt: now.Add(-time.Minute), LeaseExpiresAt: now.Add(-30 * time.Second), CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-30 * time.Second)}},
	}
	service := Service{
		Config: Config{
			DeploymentMode:           deploymentModeActive,
			ClaimsEnabled:            true,
			ReconcileInterval:        time.Hour,
			ReapInterval:             time.Hour,
			ClaimLeaseTTL:            time.Minute,
			HeartbeatInterval:        20 * time.Second,
			ExpiredClaimLimit:        10,
			ExpiredClaimRequeueDelay: 5 * time.Second,
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    "collector-git-primary",
				CollectorKind: scope.CollectorGit,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
			}},
		},
		Store: store,
		Clock: func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := store.reapCalls, 1; got != want {
		t.Fatalf("reap calls = %d, want %d", got, want)
	}
	if got, want := store.runReconcileCalls, 1; got != want {
		t.Fatalf("run reconcile calls = %d, want %d", got, want)
	}
}

func TestServiceRunActiveModeReconcilesRunsOnDedicatedInterval(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 21, 12, 45, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	store := &fakeStore{
		instances: []workflow.CollectorInstance{{
			InstanceID:    "collector-git-primary",
			CollectorKind: scope.CollectorGit,
			Mode:          workflow.CollectorModeContinuous,
			Enabled:       true,
		}},
		runReconcileHook: func(count int) {
			if count >= 2 {
				cancel()
			}
		},
	}
	service := Service{
		Config: Config{
			DeploymentMode:           deploymentModeActive,
			ClaimsEnabled:            true,
			ReconcileInterval:        time.Hour,
			RunReconcileInterval:     time.Millisecond,
			ReapInterval:             time.Hour,
			ClaimLeaseTTL:            time.Minute,
			HeartbeatInterval:        20 * time.Second,
			ExpiredClaimLimit:        10,
			ExpiredClaimRequeueDelay: 5 * time.Second,
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    "collector-git-primary",
				CollectorKind: scope.CollectorGit,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
			}},
		},
		Store: store,
		Clock: func() time.Time { return now },
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(store.desired), 1; got != want {
		t.Fatalf("collector reconciles = %d, want %d; run reconciliation should not wait for collector reconcile", got, want)
	}
	if got := store.runReconcileCalls; got < 2 {
		t.Fatalf("run reconcile calls = %d, want at least 2", got)
	}
}

func TestRunActiveMaintenanceReconcilesWorkflowRunsBetweenReconciles(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 20, 30, 0, 0, time.UTC)
	store := &fakeStore{
		instances: []workflow.CollectorInstance{{
			InstanceID:    "collector-git-primary",
			CollectorKind: scope.CollectorGit,
			Mode:          workflow.CollectorModeContinuous,
			Enabled:       true,
		}},
	}
	service := Service{
		Config: Config{
			DeploymentMode:           deploymentModeActive,
			ClaimsEnabled:            true,
			ReconcileInterval:        time.Hour,
			ReapInterval:             20 * time.Second,
			ClaimLeaseTTL:            time.Minute,
			HeartbeatInterval:        20 * time.Second,
			ExpiredClaimLimit:        10,
			ExpiredClaimRequeueDelay: 5 * time.Second,
		},
		Store: store,
		Clock: func() time.Time { return now },
	}

	if err := service.runActiveMaintenance(context.Background()); err != nil {
		t.Fatalf("runActiveMaintenance() error = %v, want nil", err)
	}
	if got, want := store.reapCalls, 1; got != want {
		t.Fatalf("reap calls = %d, want %d", got, want)
	}
	if got, want := store.runReconcileCalls, 1; got != want {
		t.Fatalf("run reconcile calls = %d, want %d", got, want)
	}
}

func TestServiceRunActiveModeReturnsReaperError(t *testing.T) {
	t.Parallel()

	service := Service{
		Config: Config{
			DeploymentMode:           deploymentModeActive,
			ClaimsEnabled:            true,
			ReconcileInterval:        time.Second,
			ReapInterval:             time.Second,
			ClaimLeaseTTL:            time.Minute,
			HeartbeatInterval:        20 * time.Second,
			ExpiredClaimLimit:        10,
			ExpiredClaimRequeueDelay: 5 * time.Second,
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    "collector-git-primary",
				CollectorKind: scope.CollectorGit,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
			}},
		},
		Store: &fakeStore{
			reapErr: errors.New("reaper failed"),
		},
	}

	err := service.Run(context.Background())
	if err == nil || err.Error() != "initial expired-claim reap: reaper failed" {
		t.Fatalf("Run() error = %v, want initial expired-claim reap: reaper failed", err)
	}
}

func TestServiceRunActiveModeReturnsRunReconcileError(t *testing.T) {
	t.Parallel()

	service := Service{
		Config: Config{
			DeploymentMode:           deploymentModeActive,
			ClaimsEnabled:            true,
			ReconcileInterval:        time.Second,
			ReapInterval:             time.Second,
			ClaimLeaseTTL:            time.Minute,
			HeartbeatInterval:        20 * time.Second,
			ExpiredClaimLimit:        10,
			ExpiredClaimRequeueDelay: 5 * time.Second,
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    "collector-git-primary",
				CollectorKind: scope.CollectorGit,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
			}},
		},
		Store: &fakeStore{
			runReconcileErr: errors.New("workflow reconcile failed"),
		},
	}

	err := service.Run(context.Background())
	if err == nil || err.Error() != "initial workflow run reconciliation: workflow reconcile failed" {
		t.Fatalf("Run() error = %v, want initial workflow run reconciliation: workflow reconcile failed", err)
	}
}
