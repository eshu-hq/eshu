// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"encoding/json"
	"slices"
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

func TestAWSScheduledWorkPlannerSkipsInvalidGlobalRegionPairs(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 21, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:    "collector-aws",
		CollectorKind: scope.CollectorAWS,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{
			"scheduled_scan_enabled": true,
			"target_scopes": [{
				"account_id": "123456789012",
				"allowed_regions": ["us-east-1", "aws-global"],
				"allowed_services": ["lambda", "s3", "iam", "route53", "cloudfront"]
			}]
		}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := AWSScheduledWorkPlanner{}.PlanAWSScheduledWork(context.Background(), AWSScheduledPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260521T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanAWSScheduledWork() error = %v, want nil", err)
	}

	gotScopeIDs := make([]string, 0, len(items))
	for _, item := range items {
		gotScopeIDs = append(gotScopeIDs, item.ScopeID)
	}
	wantScopeIDs := []string{
		"aws:123456789012:aws-global:" + awscloud.ServiceCloudFront,
		"aws:123456789012:aws-global:" + awscloud.ServiceIAM,
		"aws:123456789012:aws-global:" + awscloud.ServiceRoute53,
		"aws:123456789012:us-east-1:" + awscloud.ServiceLambda,
		"aws:123456789012:us-east-1:" + awscloud.ServiceS3,
	}
	slices.Sort(gotScopeIDs)
	if !slices.Equal(gotScopeIDs, wantScopeIDs) {
		t.Fatalf("planned scope IDs = %#v, want %#v", gotScopeIDs, wantScopeIDs)
	}
	if slices.Contains(gotScopeIDs, "aws:123456789012:aws-global:"+awscloud.ServiceLambda) {
		t.Fatalf("planned aws-global lambda target; regional services must not run against aws-global")
	}
	if slices.Contains(gotScopeIDs, "aws:123456789012:us-east-1:"+awscloud.ServiceIAM) {
		t.Fatalf("planned regional IAM target; global services must stay on aws-global")
	}

	var requested struct {
		SkippedTargets []struct {
			Region      string `json:"region"`
			ServiceKind string `json:"service_kind"`
			Reason      string `json:"reason"`
		} `json:"skipped_targets"`
	}
	if err := json.Unmarshal([]byte(run.RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", run.RequestedScopeSet, err)
	}
	if got, want := len(requested.SkippedTargets), 5; got != want {
		t.Fatalf("skipped targets = %d, want %d in RequestedScopeSet %s", got, want, run.RequestedScopeSet)
	}
	wantSkipped := map[string]string{
		"aws-global/" + awscloud.ServiceLambda:    "regional_service_aws_global",
		"aws-global/" + awscloud.ServiceS3:        "regional_service_aws_global",
		"us-east-1/" + awscloud.ServiceCloudFront: "global_service_regional_region",
		"us-east-1/" + awscloud.ServiceIAM:        "global_service_regional_region",
		"us-east-1/" + awscloud.ServiceRoute53:    "global_service_regional_region",
	}
	for _, skipped := range requested.SkippedTargets {
		key := skipped.Region + "/" + skipped.ServiceKind
		if got, want := skipped.Reason, wantSkipped[key]; got != want {
			t.Fatalf("skipped reason for %s = %q, want %q", key, got, want)
		}
		delete(wantSkipped, key)
	}
	if len(wantSkipped) > 0 {
		t.Fatalf("missing skipped targets: %#v", wantSkipped)
	}
}

func TestAWSScheduledWorkPlannerRecordsAuditOnlySkippedRun(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 21, 16, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "collector-aws",
		CollectorKind:  scope.CollectorAWS,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testServiceAWSInvalidScheduledConfiguration(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := AWSScheduledWorkPlanner{}.PlanAWSScheduledWork(context.Background(), AWSScheduledPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260521T160000Z",
	})
	if err != nil {
		t.Fatalf("PlanAWSScheduledWork() error = %v, want nil", err)
	}
	if got := len(items); got != 0 {
		t.Fatalf("len(items) = %d, want 0", got)
	}
	if got, want := run.Status, workflow.RunStatusComplete; got != want {
		t.Fatalf("run Status = %q, want %q", got, want)
	}
	if !run.FinishedAt.Equal(observedAt) {
		t.Fatalf("run FinishedAt = %s, want %s", run.FinishedAt, observedAt)
	}

	var requested struct {
		Targets        []json.RawMessage `json:"targets"`
		SkippedTargets []struct {
			Region      string `json:"region"`
			ServiceKind string `json:"service_kind"`
			Reason      string `json:"reason"`
		} `json:"skipped_targets"`
	}
	if err := json.Unmarshal([]byte(run.RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", run.RequestedScopeSet, err)
	}
	if got := len(requested.Targets); got != 0 {
		t.Fatalf("requested targets = %d, want 0", got)
	}
	if got, want := len(requested.SkippedTargets), 2; got != want {
		t.Fatalf("skipped targets = %d, want %d in RequestedScopeSet %s", got, want, run.RequestedScopeSet)
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

func TestServiceRunActiveModeSkipsAWSWorkWhenPriorScheduledTargetIsOpen(t *testing.T) {
	t.Parallel()

	first := time.Date(2026, time.May, 20, 22, 5, 0, 0, time.UTC)
	second := first.Add(5 * time.Minute)
	current := first
	store := &fakeStore{
		instances: []workflow.CollectorInstance{testServiceAWSScheduledInstance(first)},
	}
	service := Service{
		Config: Config{
			DeploymentMode:           deploymentModeActive,
			ClaimsEnabled:            true,
			ReconcileInterval:        5 * time.Minute,
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
		Clock:               func() time.Time { return current },
	}

	if err := service.runReconcile(context.Background()); err != nil {
		t.Fatalf("first runReconcile() error = %v, want nil", err)
	}
	current = second
	if err := service.runReconcile(context.Background()); err != nil {
		t.Fatalf("second runReconcile() error = %v, want nil", err)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
}

func TestServiceRunActiveModePersistsAuditOnlyAWSScheduledRun(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 21, 16, 5, 0, 0, time.UTC)
	instance := testServiceAWSScheduledInstance(now)
	instance.Configuration = testServiceAWSInvalidScheduledConfiguration()
	store := &fakeStore{
		instances: []workflow.CollectorInstance{instance},
	}
	service := Service{
		Config: Config{
			DeploymentMode:    deploymentModeActive,
			ClaimsEnabled:     true,
			ReconcileInterval: time.Hour,
			ReapInterval:      time.Hour,
			ClaimLeaseTTL:     time.Minute,
			HeartbeatInterval: 20 * time.Second,
			ExpiredClaimLimit: 10,
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    "collector-aws",
				CollectorKind: scope.CollectorAWS,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: testServiceAWSInvalidScheduledConfiguration(),
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
	if got := len(store.enqueuedItems); got != 0 {
		t.Fatalf("enqueued items = %d, want 0", got)
	}
	if got, want := store.createdRuns[0].Status, workflow.RunStatusComplete; got != want {
		t.Fatalf("created run Status = %q, want %q", got, want)
	}
}

func TestAWSScheduledScanEnabledNormalizesBlankConfiguration(t *testing.T) {
	t.Parallel()

	enabled, err := awsScheduledScanEnabled(" \n\t ")
	if err != nil {
		t.Fatalf("awsScheduledScanEnabled() error = %v, want nil", err)
	}
	if enabled {
		t.Fatalf("awsScheduledScanEnabled() = true, want false")
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

func testServiceAWSInvalidScheduledConfiguration() string {
	return `{
		"scheduled_scan_enabled": true,
		"target_scopes": [{
			"account_id": "123456789012",
			"allowed_regions": ["aws-global"],
			"allowed_services": ["lambda", "s3"]
		}]
	}`
}
