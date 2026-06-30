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
	markFailedErr error
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
	return f.markFailedErr
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

func TestServiceRunActiveModeSkipsAWSFreshnessWhenPriorTargetIsOpen(t *testing.T) {
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
		enqueuedItems: []workflow.WorkItem{{
			CollectorKind:       scope.CollectorAWS,
			CollectorInstanceID: "collector-aws",
			ScopeID:             "aws:123456789012:us-east-1:lambda",
			AcceptanceUnitID:    `{"account_id":"123456789012","region":"us-east-1","service_kind":"lambda"}`,
		}},
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
	if got, want := len(store.createdRuns), 0; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want original open item only", got)
	}
	if got, want := len(freshnessStore.handedOffIDs), 1; got != want {
		t.Fatalf("handed off ids = %d, want %d", got, want)
	}
	if got, want := len(freshnessStore.failedIDs), 0; got != want {
		t.Fatalf("failed ids = %d, want %d", got, want)
	}
	if got, want := counter.events, 2; got != want {
		t.Fatalf("AWS freshness metric events = %d, want %d", got, want)
	}
}

func TestRunAWSFreshnessHandoffUsesDurableInstancesBetweenReconciles(t *testing.T) {
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
	store := &fakeStore{
		instances: []workflow.CollectorInstance{testServiceAWSInstance(now)},
	}
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive,
			ClaimsEnabled:  true,
		},
		Store:                store,
		AWSFreshnessTriggers: freshnessStore,
		AWSFreshnessPlanner:  AWSFreshnessWorkPlanner{},
		Clock:                func() time.Time { return now },
	}

	if err := service.runAWSFreshnessHandoff(context.Background()); err != nil {
		t.Fatalf("runAWSFreshnessHandoff() error = %v, want nil", err)
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
}

func TestScheduleAWSFreshnessWorkRequiresPlannerBeforeClaim(t *testing.T) {
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
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive,
			ClaimsEnabled:  true,
		},
		AWSFreshnessTriggers: freshnessStore,
	}

	err = service.scheduleAWSFreshnessWork(context.Background(), now, []workflow.CollectorInstance{
		testServiceAWSInstance(now),
	})

	if err == nil {
		t.Fatalf("scheduleAWSFreshnessWork() error = nil, want planner error")
	}
	if freshnessStore.claimCalls != 0 {
		t.Fatalf("claim calls = %d, want 0", freshnessStore.claimCalls)
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

// TestMarkAWSFreshnessFailedLogsWhenMarkErrors proves the best-effort
// MarkTriggersFailed call is observable: when persisting the failure-marking
// itself errors, the operator gets a WARN rather than a silent drop (#3793).
func TestMarkAWSFreshnessFailedLogsWhenMarkErrors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	var logs bytes.Buffer
	triggerStore := &fakeAWSFreshnessTriggerStore{markFailedErr: errors.New("postgres write failed")}
	service := Service{
		AWSFreshnessTriggers: triggerStore,
		Logger:               slog.New(slog.NewTextHandler(&logs, nil)),
	}

	service.markAWSFreshnessFailed(
		context.Background(),
		[]freshness.StoredTrigger{{TriggerID: "aws-trigger-1"}},
		now,
		"aws_freshness_handoff_error",
		"boom",
	)

	out := logs.String()
	if !strings.Contains(out, "did not persist") {
		t.Fatalf("expected a WARN that the failure marking did not persist, got: %q", out)
	}
	if !strings.Contains(out, "postgres write failed") {
		t.Fatalf("expected the underlying error in the log, got: %q", out)
	}
}
