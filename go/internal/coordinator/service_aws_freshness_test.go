package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	"go.opentelemetry.io/otel/metric"
)

type fakeAWSFreshnessTriggerStore struct {
	claimed       []freshness.StoredTrigger
	claimCalls    int
	handedOffIDs  []string
	failedIDs     []string
	failureClass  string
	failureReason string
}

func (f *fakeAWSFreshnessTriggerStore) ClaimQueuedTriggers(
	context.Context,
	string,
	time.Time,
	int,
) ([]freshness.StoredTrigger, error) {
	f.claimCalls++
	return append([]freshness.StoredTrigger(nil), f.claimed...), nil
}

func (f *fakeAWSFreshnessTriggerStore) MarkTriggersHandedOff(
	_ context.Context,
	triggerIDs []string,
	_ time.Time,
) error {
	f.handedOffIDs = append(f.handedOffIDs, triggerIDs...)
	return nil
}

func (f *fakeAWSFreshnessTriggerStore) MarkTriggersFailed(
	_ context.Context,
	triggerIDs []string,
	_ time.Time,
	failureClass string,
	failureReason string,
) error {
	f.failedIDs = append(f.failedIDs, triggerIDs...)
	f.failureClass = failureClass
	f.failureReason = failureReason
	return nil
}

type fakeAWSFreshnessCounter struct {
	events int
}

func (f *fakeAWSFreshnessCounter) Add(context.Context, int64, ...metric.AddOption) {
	f.events++
}

func TestServiceRunActiveModeHandsOffAWSFreshnessTriggers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 15, 18, 30, 0, 0, time.UTC)
	trigger, err := freshness.NewStoredTrigger(freshness.Trigger{
		EventID:     "event-1",
		Kind:        freshness.EventKindConfigChange,
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceLambda,
		ObservedAt:  now,
	}, now)
	if err != nil {
		t.Fatalf("NewStoredTrigger() error = %v", err)
	}
	freshnessStore := &fakeAWSFreshnessTriggerStore{claimed: []freshness.StoredTrigger{trigger}}
	counter := &fakeAWSFreshnessCounter{}
	store := &fakeStore{
		instances: []workflow.CollectorInstance{testServiceAWSInstance(now)},
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
				Configuration: testServiceAWSConfiguration(),
			}},
		},
		Store:                store,
		AWSFreshnessTriggers: freshnessStore,
		AWSFreshnessPlanner:  AWSFreshnessWorkPlanner{},
		AWSFreshnessEvents:   counter,
		Clock:                func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if freshnessStore.claimCalls != 1 {
		t.Fatalf("claim calls = %d, want 1", freshnessStore.claimCalls)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
	if got, want := len(freshnessStore.handedOffIDs), 1; got != want {
		t.Fatalf("handed off ids = %d, want %d", got, want)
	}
	if got, want := counter.events, 2; got != want {
		t.Fatalf("AWS freshness metric events = %d, want %d", got, want)
	}
}

func testServiceAWSInstance(observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "collector-aws",
		CollectorKind:  scope.CollectorAWS,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testServiceAWSConfiguration(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}

func testServiceAWSConfiguration() string {
	return `{
		"target_scopes": [{
			"account_id": "123456789012",
			"allowed_regions": ["us-east-1"],
			"allowed_services": ["lambda"]
		}]
	}`
}
