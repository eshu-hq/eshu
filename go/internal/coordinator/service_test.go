// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
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

// fixedAdmissionStore returns one canned admission from the guarded scheduler so
// the two shortfall shapes can be told apart: targets an open run already
// covered, and rows the database refused after the guard admitted them.
type fixedAdmissionStore struct {
	fakeStore
	admission workflow.RunAdmission
}

func (s *fixedAdmissionStore) CreateRunWithWorkItemsIfNoOpenTargets(
	context.Context,
	workflow.Run,
	[]workflow.WorkItem,
) (workflow.RunAdmission, error) {
	return s.admission, nil
}

func scheduledWorkLogFixture(admission workflow.RunAdmission) (Service, *bytes.Buffer, workflow.CollectorInstance, workflow.Run, []workflow.WorkItem) {
	logs := &bytes.Buffer{}
	service := Service{
		Store:  &fixedAdmissionStore{admission: admission},
		Logger: slog.New(slog.NewJSONHandler(logs, nil)),
	}
	instance := workflow.CollectorInstance{
		InstanceID:    "collector-tfstate-primary",
		CollectorKind: scope.CollectorTerraformState,
	}
	now := time.Date(2026, time.May, 13, 15, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "terraform_state:collector-tfstate-primary:schedule:continuous-20260513T150000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorTerraformState),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	items := []workflow.WorkItem{
		{WorkItemID: run.RunID + ":a", RunID: run.RunID, ScopeID: "state_snapshot:s3:a", AcceptanceUnitID: "repository:a"},
		{WorkItemID: run.RunID + ":b", RunID: run.RunID, ScopeID: "state_snapshot:s3:a", AcceptanceUnitID: "repository:b"},
	}
	return service, logs, instance, run, items
}

// TestCreateWorkflowWorkIfNoOpenTargetsReportsInsertConflictSeparately covers the
// #4586 reporting gap. Both planned targets clear the open-target guard, then the
// database accepts one row because a partial unique index covers a narrower tuple
// than the guard compares. That is planned work that never reached the queue, and
// reporting it as "target_already_planned" tells an operator a duplicate was
// harmlessly skipped when work was actually lost.
func TestCreateWorkflowWorkIfNoOpenTargetsReportsInsertConflictSeparately(t *testing.T) {
	t.Parallel()

	service, logs, instance, run, items := scheduledWorkLogFixture(workflow.RunAdmission{
		EligibleTargets:   2,
		InsertedWorkItems: 1,
	})

	enqueued, err := service.createWorkflowWorkIfNoOpenTargets(context.Background(), instance, run, items)
	if err != nil {
		t.Fatalf("createWorkflowWorkIfNoOpenTargets() error = %v, want nil", err)
	}
	if enqueued != 1 {
		t.Fatalf("enqueued = %d, want 1 row the database accepted", enqueued)
	}

	got := logs.String()
	if !strings.Contains(got, `"reason":"insert_conflict_dropped_row"`) {
		t.Fatalf("logs = %s, want an insert-conflict reason: a row the database refused is lost work, not a skipped duplicate (#4586)", got)
	}
	if strings.Contains(got, `"reason":"target_already_planned"`) {
		t.Fatalf("logs = %s, want no already-planned reason: the guard admitted both targets, so nothing was skipped as a duplicate (#4586)", got)
	}
}

// TestCreateWorkflowWorkIfNoOpenTargetsReportsAlreadyPlannedSkips is the other
// half: when the guard itself drops a target because an open run already covers
// it, that is the benign duplicate skip and must keep its own reason.
func TestCreateWorkflowWorkIfNoOpenTargetsReportsAlreadyPlannedSkips(t *testing.T) {
	t.Parallel()

	service, logs, instance, run, items := scheduledWorkLogFixture(workflow.RunAdmission{
		EligibleTargets:   1,
		InsertedWorkItems: 1,
	})

	if _, err := service.createWorkflowWorkIfNoOpenTargets(context.Background(), instance, run, items); err != nil {
		t.Fatalf("createWorkflowWorkIfNoOpenTargets() error = %v, want nil", err)
	}

	got := logs.String()
	if !strings.Contains(got, `"reason":"target_already_planned"`) {
		t.Fatalf("logs = %s, want the already-planned reason for a target an open run covers", got)
	}
	if strings.Contains(got, `"reason":"insert_conflict_dropped_row"`) {
		t.Fatalf("logs = %s, want no insert-conflict reason: the database accepted every row the guard admitted", got)
	}
}
