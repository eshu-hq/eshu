// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestServiceRunActiveModeSkipsDeniedCollectorEgress(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 18, 0, 0, 0, time.UTC)
	instance := testServicePagerDutyInstance(now)
	run := workflow.Run{
		RunID:              "pagerduty:pagerduty-primary:schedule:continuous-20260609T180000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorPagerDuty),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "pagerduty-item-1",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorPagerDuty,
		CollectorInstanceID: instance.InstanceID,
		SourceSystem:        string(scope.CollectorPagerDuty),
		ScopeID:             "pagerduty:account:example",
		AcceptanceUnitID:    "pagerduty:account:example",
		SourceRunID:         "pagerduty:generation-1",
		GenerationID:        "pagerduty:generation-1",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	planner := &fakePagerDutyPlanner{run: run, items: []workflow.WorkItem{item}}
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	audit := &fakeGovernanceAuditAppender{}
	var logs bytes.Buffer
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
			CollectorEgressPolicy: mustParseCollectorEgressPolicy(t, `{
				"mode": "restricted",
				"collectors": [{"collector_kind": "pagerduty", "decision": "deny"}]
			}`),
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    instance.InstanceID,
				CollectorKind: instance.CollectorKind,
				Mode:          instance.Mode,
				Enabled:       instance.Enabled,
				ClaimsEnabled: instance.ClaimsEnabled,
				Configuration: instance.Configuration,
			}},
		},
		Store:            store,
		GovernanceAudit:  audit,
		Logger:           slog.New(slog.NewTextHandler(&logs, nil)),
		PagerDutyPlanner: planner,
		Clock:            func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := len(planner.requests); got != 0 {
		t.Fatalf("planner requests = %d, want 0", got)
	}
	if got := len(store.enqueuedItems); got != 0 {
		t.Fatalf("enqueued items = %d, want 0", got)
	}
	if got := logs.String(); !strings.Contains(got, `reason=egress_provider_denied`) {
		t.Fatalf("logs = %q, want denied egress reason", got)
	}
	if got, want := len(audit.events), 1; got != want {
		t.Fatalf("audit events = %d, want %d", got, want)
	}
	event := audit.events[0]
	if got, want := event.Type, governanceaudit.EventTypeCollectorActivation; got != want {
		t.Fatalf("event.Type = %q, want %q", got, want)
	}
	if got, want := event.ActorClass, governanceaudit.ActorClassServicePrincipal; got != want {
		t.Fatalf("event.ActorClass = %q, want %q", got, want)
	}
	if got, want := event.ServicePrincipalID, "svc:workflow-coordinator"; got != want {
		t.Fatalf("event.ServicePrincipalID = %q, want %q", got, want)
	}
	if got, want := event.ScopeClass, governanceaudit.ScopeClassCollectorKind; got != want {
		t.Fatalf("event.ScopeClass = %q, want %q", got, want)
	}
	if got, want := event.Decision, governanceaudit.DecisionDenied; got != want {
		t.Fatalf("event.Decision = %q, want %q", got, want)
	}
	if got, want := event.ReasonCode, CollectorEgressReasonDenied; got != want {
		t.Fatalf("event.ReasonCode = %q, want %q", got, want)
	}
	if event.ScopeIDHash == "" {
		t.Fatal("event.ScopeIDHash is empty, want hashed collector kind")
	}
	if _, err := governanceaudit.NormalizeEvent(event); err != nil {
		t.Fatalf("NormalizeEvent() error = %v, want nil", err)
	}
}

func TestServiceRunActiveModeAuditsMissingCollectorEgressRule(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 18, 5, 0, 0, time.UTC)
	instance := testServicePagerDutyInstance(now)
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	audit := &fakeGovernanceAuditAppender{}
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
			CollectorEgressPolicy: mustParseCollectorEgressPolicy(t, `{
				"mode": "restricted",
				"collectors": []
			}`),
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    instance.InstanceID,
				CollectorKind: instance.CollectorKind,
				Mode:          instance.Mode,
				Enabled:       instance.Enabled,
				ClaimsEnabled: instance.ClaimsEnabled,
				Configuration: instance.Configuration,
			}},
		},
		Store:           store,
		GovernanceAudit: audit,
		PagerDutyPlanner: &fakePagerDutyPlanner{
			run: workflow.Run{
				RunID:              "pagerduty:pagerduty-primary:schedule:continuous-20260609T180500Z",
				TriggerKind:        workflow.TriggerKindSchedule,
				Status:             workflow.RunStatusCollectionPending,
				RequestedScopeSet:  "{}",
				RequestedCollector: string(scope.CollectorPagerDuty),
				CreatedAt:          now,
				UpdatedAt:          now,
			},
		},
		Clock: func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := len(store.enqueuedItems); got != 0 {
		t.Fatalf("enqueued items = %d, want 0", got)
	}
	if got, want := len(audit.events), 1; got != want {
		t.Fatalf("audit events = %d, want %d", got, want)
	}
	event := audit.events[0]
	if got, want := event.Decision, governanceaudit.DecisionUnavailable; got != want {
		t.Fatalf("event.Decision = %q, want %q", got, want)
	}
	if got, want := event.ReasonCode, CollectorEgressReasonMissing; got != want {
		t.Fatalf("event.ReasonCode = %q, want %q", got, want)
	}
	if _, err := governanceaudit.NormalizeEvent(event); err != nil {
		t.Fatalf("NormalizeEvent() error = %v, want nil", err)
	}
}

func TestServiceIncidentFreshnessSkipsDeniedCollectorEgress(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 18, 0, 0, 0, time.UTC)
	triggerStore := &fakeIncidentFreshnessTriggerStore{
		claimed: []webhook.StoredIncidentFreshnessTrigger{
			incidentFreshnessStoredTrigger("trigger-pd", webhook.ProviderPagerDuty, "pagerduty:account:example", now),
		},
	}
	store := &fakeStore{instances: []workflow.CollectorInstance{testServicePagerDutyInstance(now)}}
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive,
			ClaimsEnabled:  true,
			CollectorEgressPolicy: mustParseCollectorEgressPolicy(t, `{
				"mode": "restricted",
				"collectors": [{"collector_kind": "pagerduty", "decision": "deny"}]
			}`),
		},
		Store:                     store,
		PagerDutyPlanner:          PagerDutyWorkPlanner{},
		IncidentFreshnessTriggers: triggerStore,
		Clock:                     func() time.Time { return now },
	}

	if err := service.runIncidentFreshnessHandoff(context.Background()); err != nil {
		t.Fatalf("runIncidentFreshnessHandoff() error = %v, want nil", err)
	}
	if got := len(store.enqueuedItems); got != 0 {
		t.Fatalf("enqueued items = %d, want 0", got)
	}
	if !reflect.DeepEqual(triggerStore.failed, []string{"trigger-pd"}) {
		t.Fatalf("failed = %#v, want trigger-pd", triggerStore.failed)
	}
	if got := triggerStore.failedCall("unauthorized_target"); !reflect.DeepEqual(got, []string{"trigger-pd"}) {
		t.Fatalf("failed unauthorized_target = %#v, want trigger-pd", got)
	}
}

func mustParseCollectorEgressPolicy(t *testing.T, raw string) CollectorEgressPolicy {
	t.Helper()

	policy, err := ParseCollectorEgressPolicyJSON(raw)
	if err != nil {
		t.Fatalf("ParseCollectorEgressPolicyJSON() error = %v, want nil", err)
	}
	return policy
}
