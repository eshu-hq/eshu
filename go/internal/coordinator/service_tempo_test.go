package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakeTempoPlanner struct {
	requests []TempoPlanRequest
	run      workflow.Run
	items    []workflow.WorkItem
	err      error
}

func (f *fakeTempoPlanner) PlanTempoWork(
	_ context.Context,
	request TempoPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return workflow.Run{}, nil, f.err
	}
	return f.run, append([]workflow.WorkItem(nil), f.items...), nil
}

func TestServiceRunActiveModeSchedulesTempoWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 5, 18, 30, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "tempo:tempo-primary:schedule:continuous-20260605T180000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorTempo),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "tempo-item-1",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorTempo,
		CollectorInstanceID: "tempo-primary",
		SourceSystem:        string(scope.CollectorTempo),
		ScopeID:             "tempo:source:platform-prod",
		AcceptanceUnitID:    "tempo:source:platform-prod",
		SourceRunID:         "tempo:generation-1",
		GenerationID:        "tempo:generation-1",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	planner := &fakeTempoPlanner{run: run, items: []workflow.WorkItem{item}}
	instance := testServiceTempoInstance(now)
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
				InstanceID:    instance.InstanceID,
				CollectorKind: instance.CollectorKind,
				Mode:          instance.Mode,
				Enabled:       instance.Enabled,
				ClaimsEnabled: instance.ClaimsEnabled,
				Configuration: instance.Configuration,
			}},
		},
		Store:        &fakeStore{instances: []workflow.CollectorInstance{instance}},
		TempoPlanner: planner,
		Clock:        func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(planner.requests), 1; got != want {
		t.Fatalf("planner requests = %d, want %d", got, want)
	}
	if got, want := planner.requests[0].PlanKey, "continuous-20260605T180000Z"; got != want {
		t.Fatalf("planner PlanKey = %q, want %q", got, want)
	}
	if got, want := len(service.Store.(*fakeStore).createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
}

func testServiceTempoInstance(observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "tempo-primary",
		CollectorKind:  scope.CollectorTempo,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testTempoConfigWithTwoEnabledTargets(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}
