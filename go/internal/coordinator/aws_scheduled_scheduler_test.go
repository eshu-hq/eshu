package coordinator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestAWSScheduledWorkPlannerPlansConfiguredTargets(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 20, 22, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "collector-aws",
		CollectorKind:  scope.CollectorAWS,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testServiceAWSScheduledConfiguration(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := AWSScheduledWorkPlanner{}.PlanAWSScheduledWork(context.Background(), AWSScheduledPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260520T220000Z",
	})
	if err != nil {
		t.Fatalf("PlanAWSScheduledWork() error = %v, want nil", err)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindSchedule; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorAWS); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	item := items[0]
	if got, want := item.ScopeID, "aws:123456789012:us-east-1:"+awscloud.ServiceLambda; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	var claimTarget struct {
		AccountID   string `json:"account_id"`
		Region      string `json:"region"`
		ServiceKind string `json:"service_kind"`
	}
	if err := json.Unmarshal([]byte(item.AcceptanceUnitID), &claimTarget); err != nil {
		t.Fatalf("AcceptanceUnitID JSON = %q: %v", item.AcceptanceUnitID, err)
	}
	if got, want := claimTarget.AccountID, "123456789012"; got != want {
		t.Fatalf("claim account_id = %q, want %q", got, want)
	}
	if got, want := claimTarget.ServiceKind, awscloud.ServiceLambda; got != want {
		t.Fatalf("claim service_kind = %q, want %q", got, want)
	}
}

func TestServiceRunActiveModeSchedulesAWSWorkWithoutFreshnessTriggers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 20, 22, 5, 0, 0, time.UTC)
	store := &fakeStore{
		instances: []workflow.CollectorInstance{testServiceAWSScheduledInstance(now)},
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
				InstanceID:    "collector-aws",
				CollectorKind: scope.CollectorAWS,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: testServiceAWSScheduledConfiguration(),
			}},
		},
		Store:               store,
		AWSScheduledPlanner: AWSScheduledWorkPlanner{},
		Clock:               func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
	if got, want := store.enqueuedItems[0].CollectorKind, scope.CollectorAWS; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
}

func testServiceAWSScheduledInstance(observedAt time.Time) workflow.CollectorInstance {
	instance := testServiceAWSInstance(observedAt)
	instance.Configuration = testServiceAWSScheduledConfiguration()
	return instance
}

func testServiceAWSScheduledConfiguration() string {
	return `{
		"scheduled_scan_enabled": true,
		"target_scopes": [{
			"account_id": "123456789012",
			"allowed_regions": ["us-east-1"],
			"allowed_services": ["lambda"]
		}]
	}`
}
