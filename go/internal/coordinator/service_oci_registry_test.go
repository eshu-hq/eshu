package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakeOCIRegistryPlanner struct {
	requests []OCIRegistryPlanRequest
	run      workflow.Run
	items    []workflow.WorkItem
	err      error
}

func (f *fakeOCIRegistryPlanner) PlanOCIRegistryWork(
	_ context.Context,
	request OCIRegistryPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return workflow.Run{}, nil, f.err
	}
	return f.run, append([]workflow.WorkItem(nil), f.items...), nil
}

func TestServiceRunActiveModeSchedulesOCIRegistryWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 13, 15, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "oci_registry:collector-oci-registry:schedule:continuous-20260513T150000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorOCIRegistry),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "oci-item-1",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorOCIRegistry,
		CollectorInstanceID: "collector-oci-registry",
		SourceSystem:        string(scope.CollectorOCIRegistry),
		ScopeID:             "oci-registry://registry-1.docker.io/library/busybox",
		AcceptanceUnitID:    "oci-registry://registry-1.docker.io/library/busybox",
		SourceRunID:         "oci_registry:collector-oci-registry:schedule:continuous-20260513T150000Z:registry-1.docker.io/library/busybox",
		GenerationID:        "oci_registry:collector-oci-registry:schedule:continuous-20260513T150000Z:registry-1.docker.io/library/busybox",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	planner := &fakeOCIRegistryPlanner{run: run, items: []workflow.WorkItem{item}}
	store := &fakeStore{
		instances: []workflow.CollectorInstance{testServiceOCIRegistryInstance(now)},
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
				InstanceID:    "collector-oci-registry",
				CollectorKind: scope.CollectorOCIRegistry,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: testServiceOCIRegistryConfiguration(),
			}},
		},
		Store:              store,
		OCIRegistryPlanner: planner,
		Clock:              func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(planner.requests), 1; got != want {
		t.Fatalf("planner requests = %d, want %d", got, want)
	}
	if got, want := planner.requests[0].PlanKey, "continuous-20260513T150000Z"; got != want {
		t.Fatalf("planner PlanKey = %q, want %q", got, want)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
}

func testServiceOCIRegistryInstance(observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "collector-oci-registry",
		CollectorKind:  scope.CollectorOCIRegistry,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testServiceOCIRegistryConfiguration(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}

func testServiceOCIRegistryConfiguration() string {
	return `{
		"targets": [{
			"provider": "dockerhub",
			"registry": "registry-1.docker.io",
			"repository": "library/busybox",
			"references": ["latest"],
			"tag_limit": 1
		}]
	}`
}
