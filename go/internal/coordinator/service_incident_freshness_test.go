// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/pagerdutyplanner"
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
		PagerDutyPlanner:          pagerdutyplanner.WorkPlanner{},
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
		PagerDutyPlanner:          pagerdutyplanner.WorkPlanner{},
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

func TestPagerDutyFreshnessHandoffForwardsExactRequestAndSkipsEmptyAdmission(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 18, 37, 0, 0, time.UTC)
	instance := testServicePagerDutyInstance(now)
	triggers := []webhook.StoredIncidentFreshnessTrigger{
		incidentFreshnessStoredTrigger("trigger-zeta", webhook.ProviderPagerDuty, " pagerduty:service:zeta ", now),
		incidentFreshnessStoredTrigger("trigger-alpha", webhook.ProviderPagerDuty, "pagerduty:service:alpha", now),
		incidentFreshnessStoredTrigger("trigger-blank", webhook.ProviderPagerDuty, " \t ", now),
		incidentFreshnessStoredTrigger("trigger-zeta-duplicate", webhook.ProviderPagerDuty, "pagerduty:service:zeta", now),
	}
	planner := &fakePagerDutyPlanner{run: workflow.Run{
		RunID:              "pagerduty-empty-freshness",
		TriggerKind:        workflow.TriggerKindWebhook,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorPagerDuty),
		CreatedAt:          now,
		UpdatedAt:          now,
	}}
	store := &pagerDutyAdmissionSpyStore{fakeStore: &fakeStore{instances: []workflow.CollectorInstance{instance}}}
	triggerStore := &fakeIncidentFreshnessTriggerStore{}
	service := Service{
		Config:                    Config{ReconcileInterval: time.Hour},
		Store:                     store,
		PagerDutyPlanner:          planner,
		IncidentFreshnessTriggers: triggerStore,
	}
	assignment := incidentFreshnessAssignment{instance: instance, triggers: triggers}

	if err := service.handoffPagerDutyFreshnessAssignment(context.Background(), now, assignment); err != nil {
		t.Fatalf("handoffPagerDutyFreshnessAssignment() error = %v, want nil", err)
	}
	wantRequest := pagerdutyplanner.PlanRequest{
		Instance:    instance,
		ObservedAt:  now,
		PlanKey:     "freshness-20260531T180000Z",
		TriggerKind: workflow.TriggerKindWebhook,
		ScopeIDs:    []string{"pagerduty:service:alpha", "pagerduty:service:zeta"},
	}
	if !reflect.DeepEqual(planner.requests, []pagerdutyplanner.PlanRequest{wantRequest}) {
		t.Fatalf("planner requests = %#v, want %#v", planner.requests, []pagerdutyplanner.PlanRequest{wantRequest})
	}
	if got := store.admissionCalls; got != 0 {
		t.Fatalf("Store admission calls = %d, want 0", got)
	}
	wantHandedOff := []string{"trigger-alpha", "trigger-blank", "trigger-zeta", "trigger-zeta-duplicate"}
	if !reflect.DeepEqual(triggerStore.handedOff, wantHandedOff) {
		t.Fatalf("handedOff = %#v, want %#v", triggerStore.handedOff, wantHandedOff)
	}
}

func TestPagerDutyFreshnessHandoffClassifiesPlanningFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 18, 37, 0, 0, time.UTC)
	trigger := incidentFreshnessStoredTrigger(
		"trigger-plan-failure",
		webhook.ProviderPagerDuty,
		"pagerduty:account:example",
		now,
	)
	triggerStore := &fakeIncidentFreshnessTriggerStore{}
	service := Service{
		Config:                    Config{ReconcileInterval: time.Hour},
		Store:                     &fakeStore{},
		PagerDutyPlanner:          &fakePagerDutyPlanner{err: errors.New("planner unavailable")},
		IncidentFreshnessTriggers: triggerStore,
	}
	assignment := incidentFreshnessAssignment{
		instance: testServicePagerDutyInstance(now),
		triggers: []webhook.StoredIncidentFreshnessTrigger{trigger},
	}

	err := service.handoffPagerDutyFreshnessAssignment(context.Background(), now, assignment)
	if err == nil || !strings.Contains(err.Error(), "planner unavailable") {
		t.Fatalf("handoffPagerDutyFreshnessAssignment() error = %v, want planner failure", err)
	}
	if got, want := triggerStore.failedCall("plan_failed"), []string{"trigger-plan-failure"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("plan_failed triggers = %#v, want %#v", got, want)
	}
	if got := triggerStore.failedCall("workflow_handoff_failed"); len(got) != 0 {
		t.Fatalf("workflow_handoff_failed triggers = %#v, want none", got)
	}
	if len(triggerStore.handedOff) != 0 {
		t.Fatalf("handedOff = %#v, want none", triggerStore.handedOff)
	}
}

func TestPagerDutyFreshnessHandoffClassifiesWorkflowAdmissionFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 18, 37, 0, 0, time.UTC)
	instance := testServicePagerDutyInstance(now)
	trigger := incidentFreshnessStoredTrigger(
		"trigger-workflow-failure",
		webhook.ProviderPagerDuty,
		"pagerduty:account:example",
		now,
	)
	run := workflow.Run{
		RunID:              "pagerduty-freshness-run",
		TriggerKind:        workflow.TriggerKindWebhook,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorPagerDuty),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "pagerduty-freshness-item",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorPagerDuty,
		CollectorInstanceID: instance.InstanceID,
		SourceSystem:        string(scope.CollectorPagerDuty),
		ScopeID:             "pagerduty:account:example",
		AcceptanceUnitID:    "pagerduty:account:example",
		SourceRunID:         "pagerduty:freshness-generation",
		GenerationID:        "pagerduty:freshness-generation",
		FairnessKey:         "pagerduty:pagerduty-primary:pagerduty",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	triggerStore := &fakeIncidentFreshnessTriggerStore{}
	service := Service{
		Config: Config{ReconcileInterval: time.Hour},
		Store: &fakeStore{
			createRunErr: errors.New("admission unavailable"),
		},
		PagerDutyPlanner:          &fakePagerDutyPlanner{run: run, items: []workflow.WorkItem{item}},
		IncidentFreshnessTriggers: triggerStore,
	}
	assignment := incidentFreshnessAssignment{instance: instance, triggers: []webhook.StoredIncidentFreshnessTrigger{trigger}}

	err := service.handoffPagerDutyFreshnessAssignment(context.Background(), now, assignment)
	if err == nil || !strings.Contains(err.Error(), "admission unavailable") {
		t.Fatalf("handoffPagerDutyFreshnessAssignment() error = %v, want admission failure", err)
	}
	if got, want := triggerStore.failedCall("workflow_handoff_failed"), []string{"trigger-workflow-failure"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("workflow_handoff_failed triggers = %#v, want %#v", got, want)
	}
	if got := triggerStore.failedCall("plan_failed"); len(got) != 0 {
		t.Fatalf("plan_failed triggers = %#v, want none", got)
	}
	if len(triggerStore.handedOff) != 0 {
		t.Fatalf("handedOff = %#v, want none", triggerStore.handedOff)
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
	claimed       []webhook.StoredIncidentFreshnessTrigger
	handedOff     []string
	failed        []string
	failedCalls   []incidentFreshnessFailureCall
	markFailedErr error
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
	return s.markFailedErr
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

// TestMarkIncidentFreshnessFailedLogsWhenMarkErrors proves the best-effort
// MarkTriggersFailed call is observable: when persisting the failure-marking
// itself errors, the operator gets a WARN rather than a silent drop (#3793).
func TestMarkIncidentFreshnessFailedLogsWhenMarkErrors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	var logs bytes.Buffer
	triggerStore := &fakeIncidentFreshnessTriggerStore{markFailedErr: errors.New("postgres write failed")}
	service := Service{
		IncidentFreshnessTriggers: triggerStore,
		Logger:                    slog.New(slog.NewTextHandler(&logs, nil)),
	}

	service.markIncidentFreshnessFailed(
		context.Background(),
		[]webhook.StoredIncidentFreshnessTrigger{
			incidentFreshnessStoredTrigger("trigger-pd", webhook.ProviderPagerDuty, "pagerduty:account:example", now),
		},
		now,
		"incident_freshness_handoff_error",
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
