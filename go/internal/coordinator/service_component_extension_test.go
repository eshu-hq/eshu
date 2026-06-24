// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestServiceRunSchedulesComponentExtensionWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 13, 30, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:    "scorecard-primary",
		CollectorKind: scope.CollectorKind("scorecard"),
		Mode:          workflow.CollectorModeScheduled,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{
			"schema_version":"eshu.component.instance.v1",
			"component_id":"dev.eshu.examples.scorecard",
			"component_version":"0.1.0",
			"manifest_digest":"sha256:1234",
			"config_handle":"component-config:abcd",
			"runtime":{"sdk_protocol":"collector-sdk/v1alpha1","adapter":"oci"}
		}`,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastObservedAt: now,
	}
	store := &fakeStore{
		instances: []workflow.CollectorInstance{instance},
	}
	audit := &fakeGovernanceAuditAppender{}
	service := Service{
		Config: Config{
			DeploymentMode:    deploymentModeActive,
			ClaimsEnabled:     true,
			ReconcileInterval: time.Hour,
			ExtensionEgressPolicy: mustParseExtensionEgressPolicy(t, `{
				"mode":"restricted",
				"extensions":[{"component_id":"dev.eshu.examples.scorecard","instance_id":"scorecard-primary","decision":"allow"}]
			}`),
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    instance.InstanceID,
				CollectorKind: instance.CollectorKind,
				Mode:          instance.Mode,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: instance.Configuration,
			}},
		},
		Store:                     store,
		GovernanceAudit:           audit,
		ComponentExtensionPlanner: ComponentExtensionWorkPlanner{},
		Clock:                     func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
	if got, want := store.enqueuedItems[0].CollectorKind, scope.CollectorKind("scorecard"); got != want {
		t.Fatalf("collector kind = %q, want %q", got, want)
	}
	if got := len(audit.events); got != 0 {
		t.Fatalf("audit events = %d, want 0 for allowed extension work represented by workflow rows", got)
	}
}

func TestServiceRunSchedulesPagerDutyComponentExtensionThroughGenericPlanner(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 14, 17, 45, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:    "pagerduty-reference",
		CollectorKind: scope.CollectorPagerDuty,
		Mode:          workflow.CollectorModeScheduled,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{
			"schema_version":"eshu.component.instance.v1",
			"component_id":"dev.eshu.examples.pagerduty",
			"component_version":"0.1.0",
			"publisher":"eshu-hq",
			"manifest_digest":"sha256:1234",
			"config_handle":"component-config:abcd",
			"host":{
				"source_system":"pagerduty",
				"scope":{"id":"pagerduty:account:synthetic-reference","kind":"pagerduty_account"}
			},
			"runtime":{"sdk_protocol":"collector-sdk/v1alpha1","adapter":"process"}
		}`,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastObservedAt: now,
	}
	pagerDutyPlanner := &fakePagerDutyPlanner{}
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	service := Service{
		Config: Config{
			DeploymentMode:        deploymentModeActive,
			ClaimsEnabled:         true,
			ReconcileInterval:     time.Hour,
			ExtensionEgressPolicy: mustParseExtensionEgressPolicy(t, `{"mode":"broad"}`),
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    instance.InstanceID,
				CollectorKind: instance.CollectorKind,
				Mode:          instance.Mode,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: instance.Configuration,
			}},
		},
		Store:                     store,
		PagerDutyPlanner:          pagerDutyPlanner,
		ComponentExtensionPlanner: ComponentExtensionWorkPlanner{},
		Clock:                     func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := len(pagerDutyPlanner.requests); got != 0 {
		t.Fatalf("pagerduty planner requests = %d, want 0 for component activation config", got)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
	if got, want := store.enqueuedItems[0].CollectorKind, scope.CollectorPagerDuty; got != want {
		t.Fatalf("collector kind = %q, want %q", got, want)
	}
	if got, want := store.enqueuedItems[0].ScopeID, "pagerduty:account:synthetic-reference"; got != want {
		t.Fatalf("scope id = %q, want %q", got, want)
	}
}

func TestServiceRunSkipsComponentExtensionWithoutEgressPolicy(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 13, 45, 0, 0, time.UTC)
	instance := testScorecardComponentInstance(now)
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	audit := &fakeGovernanceAuditAppender{}
	service := Service{
		Config: Config{
			DeploymentMode:    deploymentModeActive,
			ClaimsEnabled:     true,
			ReconcileInterval: time.Hour,
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    instance.InstanceID,
				CollectorKind: instance.CollectorKind,
				Mode:          instance.Mode,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: instance.Configuration,
			}},
		},
		Store:                     store,
		GovernanceAudit:           audit,
		ComponentExtensionPlanner: ComponentExtensionWorkPlanner{},
		Clock:                     func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := len(store.enqueuedItems); got != 0 {
		t.Fatalf("enqueued items = %d, want 0 without extension egress policy", got)
	}
	if got, want := len(audit.events), 1; got != want {
		t.Fatalf("audit events = %d, want %d", got, want)
	}
	event := audit.events[0]
	if got, want := event.Type, governanceaudit.EventTypeExtensionActivation; got != want {
		t.Fatalf("event.Type = %q, want %q", got, want)
	}
	if got, want := event.ActorClass, governanceaudit.ActorClassServicePrincipal; got != want {
		t.Fatalf("event.ActorClass = %q, want %q", got, want)
	}
	if got, want := event.ServicePrincipalID, "svc:workflow-coordinator"; got != want {
		t.Fatalf("event.ServicePrincipalID = %q, want %q", got, want)
	}
	if got, want := event.ScopeClass, governanceaudit.ScopeClassExtensionComponent; got != want {
		t.Fatalf("event.ScopeClass = %q, want %q", got, want)
	}
	if got, want := event.Decision, governanceaudit.DecisionUnavailable; got != want {
		t.Fatalf("event.Decision = %q, want %q", got, want)
	}
	if got, want := event.ReasonCode, ExtensionEgressReasonMissing; got != want {
		t.Fatalf("event.ReasonCode = %q, want %q", got, want)
	}
	if event.ScopeIDHash == "" {
		t.Fatal("event.ScopeIDHash is empty, want hashed component identity")
	}
	if _, err := governanceaudit.NormalizeEvent(event); err != nil {
		t.Fatalf("NormalizeEvent() error = %v, want nil", err)
	}
}

func TestServiceRunSkipsDeniedComponentExtensionEgress(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 13, 50, 0, 0, time.UTC)
	instance := testScorecardComponentInstance(now)
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	audit := &fakeGovernanceAuditAppender{}
	service := Service{
		Config: Config{
			DeploymentMode:        deploymentModeActive,
			ClaimsEnabled:         true,
			ReconcileInterval:     time.Hour,
			ExtensionEgressPolicy: mustParseExtensionEgressPolicy(t, `{"mode":"restricted","extensions":[{"component_id":"dev.eshu.examples.scorecard","decision":"deny"}]}`),
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    instance.InstanceID,
				CollectorKind: instance.CollectorKind,
				Mode:          instance.Mode,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: instance.Configuration,
			}},
		},
		Store:                     store,
		GovernanceAudit:           audit,
		ComponentExtensionPlanner: ComponentExtensionWorkPlanner{},
		Clock:                     func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := len(store.enqueuedItems); got != 0 {
		t.Fatalf("enqueued items = %d, want 0 for denied extension egress", got)
	}
	if got, want := len(audit.events), 1; got != want {
		t.Fatalf("audit events = %d, want %d", got, want)
	}
	event := audit.events[0]
	if got, want := event.Decision, governanceaudit.DecisionDenied; got != want {
		t.Fatalf("event.Decision = %q, want %q", got, want)
	}
	if got, want := event.ReasonCode, ExtensionEgressReasonDenied; got != want {
		t.Fatalf("event.ReasonCode = %q, want %q", got, want)
	}
	if _, err := governanceaudit.NormalizeEvent(event); err != nil {
		t.Fatalf("NormalizeEvent() error = %v, want nil", err)
	}
}

func TestServiceComponentExtensionReconcileIsIdempotentAcrossRestart(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 14, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:    "scorecard-primary",
		CollectorKind: scope.CollectorKind("scorecard"),
		Mode:          workflow.CollectorModeScheduled,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{
			"schema_version":"eshu.component.instance.v1",
			"component_id":"dev.eshu.examples.scorecard",
			"component_version":"0.1.0",
			"manifest_digest":"sha256:1234",
			"config_handle":"component-config:abcd",
			"runtime":{"sdk_protocol":"collector-sdk/v1alpha1","adapter":"oci"}
		}`,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastObservedAt: now,
	}
	store := &fakeStore{
		instances: []workflow.CollectorInstance{instance},
	}
	service := Service{
		Config: Config{
			DeploymentMode:        deploymentModeActive,
			ClaimsEnabled:         true,
			ReconcileInterval:     time.Hour,
			ExtensionEgressPolicy: mustParseExtensionEgressPolicy(t, `{"mode":"broad"}`),
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    instance.InstanceID,
				CollectorKind: instance.CollectorKind,
				Mode:          instance.Mode,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: instance.Configuration,
			}},
		},
		Store:                     store,
		ComponentExtensionPlanner: ComponentExtensionWorkPlanner{},
		Clock:                     func() time.Time { return now },
	}

	if err := service.runReconcile(context.Background()); err != nil {
		t.Fatalf("first runReconcile() error = %v, want nil", err)
	}
	if err := service.runReconcile(context.Background()); err != nil {
		t.Fatalf("second runReconcile() error = %v, want nil", err)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d after duplicate reconcile", got, want)
	}
}

func testScorecardComponentInstance(now time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:    "scorecard-primary",
		CollectorKind: scope.CollectorKind("scorecard"),
		Mode:          workflow.CollectorModeScheduled,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{
			"schema_version":"eshu.component.instance.v1",
			"component_id":"dev.eshu.examples.scorecard",
			"component_version":"0.1.0",
			"manifest_digest":"sha256:1234",
			"config_handle":"component-config:abcd",
			"runtime":{"sdk_protocol":"collector-sdk/v1alpha1","adapter":"oci"}
		}`,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastObservedAt: now,
	}
}

func mustParseExtensionEgressPolicy(t *testing.T, raw string) ExtensionEgressPolicy {
	t.Helper()

	policy, err := ParseExtensionEgressPolicyJSON(raw)
	if err != nil {
		t.Fatalf("ParseExtensionEgressPolicyJSON() error = %v, want nil", err)
	}
	return policy
}
