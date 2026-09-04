// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/awsscheduledplanner"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// These three cases assert Service.Run wiring for scheduled AWS work: they
// construct a Service with fakeStore and therefore stay at root, while the
// pure planner cases moved into awsscheduledplanner with the family.

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
				Configuration: testServiceAWSScheduledRunConfiguration(),
			}},
		},
		Store:               store,
		AWSScheduledPlanner: awsscheduledplanner.WorkPlanner{},
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
				Configuration: testServiceAWSScheduledRunConfiguration(),
			}},
		},
		Store:               store,
		AWSScheduledPlanner: awsscheduledplanner.WorkPlanner{},
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
		AWSScheduledPlanner: awsscheduledplanner.WorkPlanner{},
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

// testServiceAWSScheduledInstance builds a scheduled-AWS collector instance for
// the Service.Run cases below. It stays at root because it composes root's
// testServiceAWSInstance; the planner package carries its own configuration
// fixture rather than importing across the boundary.
func testServiceAWSScheduledInstance(observedAt time.Time) workflow.CollectorInstance {
	instance := testServiceAWSInstance(observedAt)
	instance.Configuration = testServiceAWSScheduledRunConfiguration()
	return instance
}

func testServiceAWSScheduledRunConfiguration() string {
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
