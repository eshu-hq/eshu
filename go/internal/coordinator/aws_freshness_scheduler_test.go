package coordinator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestAWSFreshnessWorkPlannerPlansTargetedAWSClaims(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	trigger, err := freshness.NewStoredTrigger(freshness.Trigger{
		EventID:      "evt-123",
		Kind:         freshness.EventKindConfigChange,
		AccountID:    "123456789012",
		Region:       "us-east-1",
		ServiceKind:  awscloud.ServiceLambda,
		ResourceType: awscloud.ResourceTypeLambdaFunction,
		ResourceID:   "function-1",
		ObservedAt:   observedAt,
	}, observedAt)
	if err != nil {
		t.Fatalf("NewStoredTrigger() error = %v, want nil", err)
	}
	instance := testAWSCollectorInstance(observedAt)

	run, items, err := AWSFreshnessWorkPlanner{}.PlanAWSFreshnessWork(context.Background(), AWSFreshnessPlanRequest{
		Instance:   instance,
		Triggers:   []freshness.StoredTrigger{trigger},
		ObservedAt: observedAt,
		PlanKey:    "freshness-20260515T100000Z",
	})
	if err != nil {
		t.Fatalf("PlanAWSFreshnessWork() error = %v, want nil", err)
	}
	if run.TriggerKind != workflow.TriggerKindWebhook {
		t.Fatalf("TriggerKind = %q, want %q", run.TriggerKind, workflow.TriggerKindWebhook)
	}
	if run.RequestedCollector != string(scope.CollectorAWS) {
		t.Fatalf("RequestedCollector = %q, want aws", run.RequestedCollector)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	item := items[0]
	if item.CollectorKind != scope.CollectorAWS {
		t.Fatalf("CollectorKind = %q, want %q", item.CollectorKind, scope.CollectorAWS)
	}
	if got, want := item.ScopeID, "aws:123456789012:us-east-1:lambda"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	var acceptance map[string]string
	if err := json.Unmarshal([]byte(item.AcceptanceUnitID), &acceptance); err != nil {
		t.Fatalf("AcceptanceUnitID decode error = %v", err)
	}
	if got, want := acceptance["account_id"], "123456789012"; got != want {
		t.Fatalf("account_id = %q, want %q", got, want)
	}
	if item.GenerationID != item.SourceRunID {
		t.Fatalf("GenerationID = %q, SourceRunID = %q, want equal", item.GenerationID, item.SourceRunID)
	}
}

func TestAWSFreshnessWorkPlannerCoalescesDuplicateTargetTriggers(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	first, err := freshness.NewStoredTrigger(testAWSPlannerTrigger("evt-1", observedAt), observedAt)
	if err != nil {
		t.Fatalf("NewStoredTrigger(first) error = %v, want nil", err)
	}
	second, err := freshness.NewStoredTrigger(testAWSPlannerTrigger("evt-2", observedAt), observedAt)
	if err != nil {
		t.Fatalf("NewStoredTrigger(second) error = %v, want nil", err)
	}

	_, items, err := AWSFreshnessWorkPlanner{}.PlanAWSFreshnessWork(context.Background(), AWSFreshnessPlanRequest{
		Instance:   testAWSCollectorInstance(observedAt),
		Triggers:   []freshness.StoredTrigger{first, second},
		ObservedAt: observedAt,
		PlanKey:    "freshness-20260515T100000Z",
	})
	if err != nil {
		t.Fatalf("PlanAWSFreshnessWork() error = %v, want nil", err)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want one coalesced target", got)
	}
}

func TestAWSFreshnessWorkPlannerRejectsUnauthorizedTarget(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	trigger, err := freshness.NewStoredTrigger(freshness.Trigger{
		EventID:     "evt-123",
		Kind:        freshness.EventKindConfigChange,
		AccountID:   "123456789012",
		Region:      "us-west-2",
		ServiceKind: awscloud.ServiceLambda,
		ObservedAt:  observedAt,
	}, observedAt)
	if err != nil {
		t.Fatalf("NewStoredTrigger() error = %v, want nil", err)
	}

	_, _, err = AWSFreshnessWorkPlanner{}.PlanAWSFreshnessWork(context.Background(), AWSFreshnessPlanRequest{
		Instance:   testAWSCollectorInstance(observedAt),
		Triggers:   []freshness.StoredTrigger{trigger},
		ObservedAt: observedAt,
		PlanKey:    "freshness-20260515T100000Z",
	})
	if err == nil {
		t.Fatal("PlanAWSFreshnessWork() error = nil, want unauthorized target error")
	}
	if got, want := err.Error(), "not authorized"; !strings.Contains(got, want) {
		t.Fatalf("PlanAWSFreshnessWork() error = %q, want substring %q", got, want)
	}
}

func testAWSPlannerTrigger(eventID string, observedAt time.Time) freshness.Trigger {
	return freshness.Trigger{
		EventID:      eventID,
		Kind:         freshness.EventKindConfigChange,
		AccountID:    "123456789012",
		Region:       "us-east-1",
		ServiceKind:  awscloud.ServiceLambda,
		ResourceType: awscloud.ResourceTypeLambdaFunction,
		ResourceID:   eventID,
		ObservedAt:   observedAt,
	}
}

func testAWSCollectorInstance(observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "collector-aws-primary",
		CollectorKind:  scope.CollectorAWS,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"target_scopes":[{"account_id":"123456789012","allowed_regions":["us-east-1"],"allowed_services":["lambda","s3"],"credentials":{"mode":"local_workload_identity"}}]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}
