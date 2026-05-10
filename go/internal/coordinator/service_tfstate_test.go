package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakeTerraformStatePlanner struct {
	requests []TerraformStatePlanRequest
	run      workflow.Run
	items    []workflow.WorkItem
	err      error
}

func (f *fakeTerraformStatePlanner) PlanTerraformStateWork(
	_ context.Context,
	request TerraformStatePlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return workflow.Run{}, nil, f.err
	}
	return f.run, append([]workflow.WorkItem(nil), f.items...), nil
}

func TestServiceRunDarkModeDoesNotScheduleTerraformStateWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	planner := &fakeTerraformStatePlanner{}
	store := &fakeStore{
		instances: []workflow.CollectorInstance{testServiceTerraformStateInstance(now)},
	}
	service := Service{
		Config: Config{
			DeploymentMode:    deploymentModeDark,
			ClaimsEnabled:     true,
			ReconcileInterval: time.Hour,
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    "collector-tfstate-primary",
				CollectorKind: scope.CollectorTerraformState,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				Bootstrap:     true,
				ClaimsEnabled: true,
				Configuration: testServiceTerraformStateConfiguration(),
			}},
		},
		Store:                 store,
		TerraformStatePlanner: planner,
		Clock:                 func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := len(planner.requests); got != 0 {
		t.Fatalf("planner requests = %d, want 0 in dark mode", got)
	}
	if got := len(store.createdRuns); got != 0 {
		t.Fatalf("created runs = %d, want 0 in dark mode", got)
	}
}

func TestServiceRunActiveModeSchedulesTerraformStateWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 10, 12, 5, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "terraform_state:collector-tfstate-primary:bootstrap:bootstrap",
		TriggerKind:        workflow.TriggerKindBootstrap,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorTerraformState),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "tfstate-item-1",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             "state_snapshot:s3:locator-hash",
		AcceptanceUnitID:    "platform-infra",
		SourceRunID:         "terraform_state_candidate:s3:abc",
		GenerationID:        "terraform_state_candidate:s3:abc",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	planner := &fakeTerraformStatePlanner{run: run, items: []workflow.WorkItem{item}}
	store := &fakeStore{
		instances: []workflow.CollectorInstance{testServiceTerraformStateInstance(now)},
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
				InstanceID:    "collector-tfstate-primary",
				CollectorKind: scope.CollectorTerraformState,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				Bootstrap:     true,
				ClaimsEnabled: true,
				Configuration: testServiceTerraformStateConfiguration(),
			}},
		},
		Store:                 store,
		TerraformStatePlanner: planner,
		Clock:                 func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(planner.requests), 1; got != want {
		t.Fatalf("planner requests = %d, want %d", got, want)
	}
	if got, want := planner.requests[0].PlanKey, "bootstrap"; got != want {
		t.Fatalf("planner PlanKey = %q, want %q", got, want)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
}

func TestServiceRunActiveModeSkipsTerraformStateGraphWait(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 10, 12, 10, 0, 0, time.UTC)
	planner := &fakeTerraformStatePlanner{
		err: terraformstate.WaitingOnGitGenerationError{RepoIDs: []string{"platform-infra"}},
	}
	store := &fakeStore{
		instances: []workflow.CollectorInstance{testServiceTerraformStateInstance(now)},
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
				InstanceID:    "collector-tfstate-primary",
				CollectorKind: scope.CollectorTerraformState,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				Bootstrap:     true,
				ClaimsEnabled: true,
				Configuration: testServiceTerraformStateConfiguration(),
			}},
		},
		Store:                 store,
		TerraformStatePlanner: planner,
		Clock:                 func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := len(store.createdRuns); got != 0 {
		t.Fatalf("created runs = %d, want 0 while waiting on git", got)
	}
	if got := len(store.enqueuedItems); got != 0 {
		t.Fatalf("enqueued items = %d, want 0 while waiting on git", got)
	}
}

func testServiceTerraformStateInstance(observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "collector-tfstate-primary",
		CollectorKind:  scope.CollectorTerraformState,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		Bootstrap:      true,
		ClaimsEnabled:  true,
		Configuration:  testServiceTerraformStateConfiguration(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}

func testServiceTerraformStateConfiguration() string {
	return `{
		"discovery": {
			"seeds": [{
				"kind": "s3",
				"bucket": "tfstate-prod",
				"key": "services/api/terraform.tfstate",
				"region": "us-east-1",
				"repo_id": "platform-infra"
			}]
		},
		"aws": {"role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-reader"}
	}`
}
