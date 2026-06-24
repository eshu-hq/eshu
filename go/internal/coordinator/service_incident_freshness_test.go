// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestServiceRunActiveModeHandoffsIncidentFreshnessTriggers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	pagerDuty := testServicePagerDutyInstance(now)
	jira := workflow.CollectorInstance{
		InstanceID:     "jira-primary",
		CollectorKind:  scope.CollectorJira,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testJiraConfig(),
		LastObservedAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	triggerStore := &fakeIncidentFreshnessTriggerStore{
		claimed: []webhook.StoredIncidentFreshnessTrigger{
			incidentFreshnessStoredTrigger("trigger-pd", webhook.ProviderPagerDuty, "pagerduty:account:example", now),
			incidentFreshnessStoredTrigger("trigger-jira", webhook.ProviderJira, "jira:site:example", now),
		},
	}
	store := &fakeStore{instances: []workflow.CollectorInstance{pagerDuty, jira}}
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
		},
		Store:                     store,
		PagerDutyPlanner:          PagerDutyWorkPlanner{},
		JiraPlanner:               JiraWorkPlanner{},
		IncidentFreshnessTriggers: triggerStore,
		Clock:                     func() time.Time { return now },
	}

	if err := service.runIncidentFreshnessHandoff(context.Background()); err != nil {
		t.Fatalf("runIncidentFreshnessHandoff() error = %v, want nil", err)
	}
	if got, want := len(store.enqueuedItems), 2; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
	if !reflect.DeepEqual(triggerStore.handedOff, []string{"trigger-jira", "trigger-pd"}) {
		t.Fatalf("handedOff = %#v, want both trigger IDs", triggerStore.handedOff)
	}
	for _, run := range store.createdRuns {
		if run.TriggerKind != workflow.TriggerKindWebhook {
			t.Fatalf("created run %#v has TriggerKind %q, want webhook", run.RunID, run.TriggerKind)
		}
	}
}

func TestServiceRunActiveModeMarksStaleIncidentFreshnessTriggerFailed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	triggerStore := &fakeIncidentFreshnessTriggerStore{
		claimed: []webhook.StoredIncidentFreshnessTrigger{
			incidentFreshnessStoredTrigger("trigger-stale", webhook.ProviderPagerDuty, "pagerduty:account:missing", now),
		},
	}
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive,
			ClaimsEnabled:  true,
		},
		Store:                     &fakeStore{instances: []workflow.CollectorInstance{testServicePagerDutyInstance(now)}},
		PagerDutyPlanner:          PagerDutyWorkPlanner{},
		IncidentFreshnessTriggers: triggerStore,
		Clock:                     func() time.Time { return now },
	}

	if err := service.runIncidentFreshnessHandoff(context.Background()); err != nil {
		t.Fatalf("runIncidentFreshnessHandoff() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(triggerStore.failed, []string{"trigger-stale"}) {
		t.Fatalf("failed = %#v, want stale trigger ID", triggerStore.failed)
	}
	if got := triggerStore.failedCall("unauthorized_target"); !reflect.DeepEqual(got, []string{"trigger-stale"}) {
		t.Fatalf("failed unauthorized_target = %#v, want stale trigger ID", got)
	}
}

func TestServiceRunActiveModeCoalescesRepeatedJiraWebhookClaims(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	jira := workflow.CollectorInstance{
		InstanceID:     "jira-primary",
		CollectorKind:  scope.CollectorJira,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testJiraConfig(),
		LastObservedAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	triggerStore := &fakeIncidentFreshnessTriggerStore{
		claimed: []webhook.StoredIncidentFreshnessTrigger{
			incidentFreshnessStoredTrigger("trigger-jira-retry-1", webhook.ProviderJira, "jira:site:example", now.Add(-15*time.Minute)),
			incidentFreshnessStoredTrigger("trigger-jira-retry-2", webhook.ProviderJira, "jira:site:example", now),
		},
	}
	store := &fakeStore{instances: []workflow.CollectorInstance{jira}}
	service := Service{
		Config: Config{
			DeploymentMode:    deploymentModeActive,
			ClaimsEnabled:     true,
			ReconcileInterval: time.Hour,
		},
		Store:                     store,
		JiraPlanner:               JiraWorkPlanner{},
		IncidentFreshnessTriggers: triggerStore,
		Clock:                     func() time.Time { return now },
	}

	if err := service.runIncidentFreshnessHandoff(context.Background()); err != nil {
		t.Fatalf("runIncidentFreshnessHandoff() error = %v, want nil", err)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
	if got, want := store.enqueuedItems[0].ScopeID, "jira:site:example"; got != want {
		t.Fatalf("enqueued ScopeID = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(triggerStore.handedOff, []string{"trigger-jira-retry-1", "trigger-jira-retry-2"}) {
		t.Fatalf("handedOff = %#v, want both duplicate trigger IDs", triggerStore.handedOff)
	}
}

type fakeIncidentFreshnessTriggerStore struct {
	claimed     []webhook.StoredIncidentFreshnessTrigger
	handedOff   []string
	failed      []string
	failedCalls []incidentFreshnessFailureCall
}

func (s *fakeIncidentFreshnessTriggerStore) ClaimQueuedTriggers(
	context.Context,
	string,
	time.Time,
	int,
) ([]webhook.StoredIncidentFreshnessTrigger, error) {
	return append([]webhook.StoredIncidentFreshnessTrigger(nil), s.claimed...), nil
}

func (s *fakeIncidentFreshnessTriggerStore) MarkTriggersHandedOff(
	_ context.Context,
	triggerIDs []string,
	_ time.Time,
) error {
	s.handedOff = append(s.handedOff, triggerIDs...)
	return nil
}

func (s *fakeIncidentFreshnessTriggerStore) MarkTriggersFailed(
	_ context.Context,
	triggerIDs []string,
	_ time.Time,
	failureClass string,
	_ string,
) error {
	s.failed = append(s.failed, triggerIDs...)
	s.failedCalls = append(s.failedCalls, incidentFreshnessFailureCall{
		triggerIDs:   append([]string(nil), triggerIDs...),
		failureClass: failureClass,
	})
	return nil
}

func (s *fakeIncidentFreshnessTriggerStore) failedCall(failureClass string) []string {
	for _, call := range s.failedCalls {
		if call.failureClass == failureClass {
			return append([]string(nil), call.triggerIDs...)
		}
	}
	return nil
}

func incidentFreshnessStoredTrigger(
	id string,
	provider webhook.Provider,
	scopeID string,
	now time.Time,
) webhook.StoredIncidentFreshnessTrigger {
	return webhook.StoredIncidentFreshnessTrigger{
		IncidentFreshnessTrigger: webhook.IncidentFreshnessTrigger{
			Provider:   provider,
			EventKind:  "freshness",
			EventID:    id,
			ScopeID:    scopeID,
			ResourceID: id + "-resource",
			ObservedAt: now,
		},
		TriggerID:    id,
		DeliveryKey:  string(provider) + ":" + id,
		FreshnessKey: string(provider) + ":" + scopeID,
		Status:       webhook.TriggerStatusClaimed,
		ReceivedAt:   now,
		UpdatedAt:    now,
	}
}

type incidentFreshnessFailureCall struct {
	triggerIDs   []string
	failureClass string
}
