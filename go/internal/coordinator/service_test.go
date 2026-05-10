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

type fakeStore struct {
	observed             []time.Time
	desired              [][]workflow.DesiredCollectorInstance
	instances            []workflow.CollectorInstance
	createdRuns          []workflow.Run
	enqueuedItems        []workflow.WorkItem
	reapedClaims         []workflow.Claim
	reconcileErr         error
	listErr              error
	createRunErr         error
	enqueueErr           error
	reapErr              error
	runReconcileErr      error
	reapCalls            int
	runReconcileCalls    int
	runReconcileObserved []time.Time
}

func (f *fakeStore) ReconcileCollectorInstances(_ context.Context, observedAt time.Time, desired []workflow.DesiredCollectorInstance) error {
	f.observed = append(f.observed, observedAt)
	f.desired = append(f.desired, desired)
	return f.reconcileErr
}

func (f *fakeStore) ListCollectorInstances(context.Context) ([]workflow.CollectorInstance, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]workflow.CollectorInstance(nil), f.instances...), nil
}

func (f *fakeStore) CreateRun(_ context.Context, run workflow.Run) error {
	if f.createRunErr != nil {
		return f.createRunErr
	}
	f.createdRuns = append(f.createdRuns, run)
	return nil
}

func (f *fakeStore) EnqueueWorkItems(_ context.Context, items []workflow.WorkItem) error {
	if f.enqueueErr != nil {
		return f.enqueueErr
	}
	f.enqueuedItems = append(f.enqueuedItems, items...)
	return nil
}

func (f *fakeStore) ReapExpiredClaims(_ context.Context, observedAt time.Time, limit int, requeueDelay time.Duration) ([]workflow.Claim, error) {
	f.reapCalls++
	f.observed = append(f.observed, observedAt)
	if f.reapErr != nil {
		return nil, f.reapErr
	}
	return append([]workflow.Claim(nil), f.reapedClaims...), nil
}

func (f *fakeStore) ReconcileWorkflowRuns(_ context.Context, observedAt time.Time) (int, error) {
	f.runReconcileCalls++
	f.runReconcileObserved = append(f.runReconcileObserved, observedAt)
	if f.runReconcileErr != nil {
		return 0, f.runReconcileErr
	}
	return 2, nil
}

type fakeMetrics struct {
	observations      []ReconcileObservation
	reapObservations  []ReapObservation
	runReconcilations []RunReconciliationObservation
}

func (f *fakeMetrics) RecordReconcile(_ context.Context, observation ReconcileObservation) {
	f.observations = append(f.observations, observation)
}

func (f *fakeMetrics) RecordReap(_ context.Context, observation ReapObservation) {
	f.reapObservations = append(f.reapObservations, observation)
}

func (f *fakeMetrics) RecordRunReconciliation(_ context.Context, observation RunReconciliationObservation) {
	f.runReconcilations = append(f.runReconcilations, observation)
}

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
