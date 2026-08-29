// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerdutyplanner

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkPlannerPreservesOrderingIdentityAndPrivacyContract(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("EDT", -4*60*60)
	observedAt := time.Date(2026, time.June, 6, 18, 30, 0, 0, location)
	instance := validPagerDutyInstance(observedAt, `{
		"targets": [{
			"provider": " pagerduty ",
			"scope_id": " pagerduty:service:zeta ",
			"account_id": " account-zeta ",
			"token_env": "PAGERDUTY_ZETA_TOKEN",
			"api_base_url": "https://api-zeta.pagerduty.example.com",
			"source_uri": "https://zeta.pagerduty.example.com/incidents",
			"incident_limit": 25,
			"incident_lookback": "6h",
			"log_entry_limit": 25,
			"change_event_limit": 25,
			"config_validation_enabled": true,
			"config_resource_limit": 40,
			"pagination_max_pages": 5,
			"pagination_max_records": 500,
			"allowed_service_ids": ["PZETA"]
		}, {
			"provider": "pagerduty",
			"scope_id": "pagerduty:service:alpha",
			"account_id": "account-alpha",
			"token_env": "PAGERDUTY_ALPHA_TOKEN",
			"api_base_url": "https://api-alpha.pagerduty.example.com",
			"source_uri": "https://alpha.pagerduty.example.com/incidents",
			"incident_limit": 50,
			"incident_lookback": "12h",
			"log_entry_limit": 50,
			"change_event_limit": 50,
			"config_validation_enabled": true,
			"config_resource_limit": 60,
			"pagination_max_pages": 6,
			"pagination_max_records": 600,
			"allowed_service_ids": ["PALPHA"]
		}]
	}`)
	instance.Bootstrap = true
	planKey := "freshness-20260606T183000-0400"
	request := PlanRequest{
		Instance:    instance,
		ObservedAt:  observedAt,
		PlanKey:     planKey,
		TriggerKind: workflow.TriggerKindWebhook,
		ScopeIDs: []string{
			"", " pagerduty:service:alpha ", " pagerduty:service:zeta ",
			"\tpagerduty:service:alpha\t",
		},
	}

	run, items, err := (WorkPlanner{}).PlanPagerDutyWork(t.Context(), request)
	if err != nil {
		t.Fatalf("PlanPagerDutyWork() error = %v, want nil", err)
	}
	runAgain, itemsAgain, err := (WorkPlanner{}).PlanPagerDutyWork(t.Context(), request)
	if err != nil {
		t.Fatalf("second PlanPagerDutyWork() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(run, runAgain) || !reflect.DeepEqual(items, itemsAgain) {
		t.Fatalf("same request produced different rows\nfirst:  %#v %#v\nsecond: %#v %#v", run, items, runAgain, itemsAgain)
	}

	wantRun := workflow.Run{
		RunID:              "pagerduty:pagerduty-primary:webhook:" + planKey,
		TriggerKind:        workflow.TriggerKindWebhook,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  `{"collector_instance_id":"pagerduty-primary","targets":[{"scope_id":"pagerduty:service:alpha","provider":"pagerduty","account_id":"account-alpha"},{"scope_id":"pagerduty:service:zeta","provider":"pagerduty","account_id":"account-zeta"}]}`,
		RequestedCollector: string(scope.CollectorPagerDuty),
		CreatedAt:          observedAt.UTC(),
		UpdatedAt:          observedAt.UTC(),
		FinishedAt:         time.Time{},
	}
	for _, forbidden := range []string{
		"PAGERDUTY_ZETA_TOKEN",
		"PAGERDUTY_ALPHA_TOKEN",
		"pagerduty.example.com",
		"allowed_service_ids",
		"PZETA",
		"PALPHA",
		"incident_limit",
		"incident_lookback",
		"6h",
		"12h",
		"log_entry_limit",
		"change_event_limit",
		"config_validation_enabled",
		"config_resource_limit",
		"pagination_max_pages",
		"pagination_max_records",
	} {
		if strings.Contains(run.RequestedScopeSet, forbidden) {
			t.Fatalf("RequestedScopeSet = %q, must not contain %q", run.RequestedScopeSet, forbidden)
		}
	}
	if !reflect.DeepEqual(run, wantRun) {
		t.Fatalf("run = %#v, want %#v", run, wantRun)
	}
	zetaGenerationID := "pagerduty:67dd19891c6b86557653cdc47823ee8b10037edbc3cd827f47391f294491d70f"
	alphaGenerationID := "pagerduty:1367c9c56206a6ba1f1538f657c534706e4b5b07a44fcb3f9c169e5c2eda1d9c"
	wantItems := []workflow.WorkItem{
		exactPagerDutyWorkItem(wantRun.RunID, "pagerduty:service:zeta", zetaGenerationID, observedAt.UTC()),
		exactPagerDutyWorkItem(wantRun.RunID, "pagerduty:service:alpha", alphaGenerationID, observedAt.UTC()),
	}
	if !reflect.DeepEqual(items, wantItems) {
		t.Fatalf("items = %#v, want %#v", items, wantItems)
	}
}

func exactPagerDutyWorkItem(runID, scopeID, generationID string, observedAt time.Time) workflow.WorkItem {
	return workflow.WorkItem{
		WorkItemID:          "pagerduty:pagerduty-primary:" + generationID,
		RunID:               runID,
		CollectorKind:       scope.CollectorPagerDuty,
		CollectorInstanceID: "pagerduty-primary",
		SourceSystem:        string(scope.CollectorPagerDuty),
		ScopeID:             scopeID,
		TenantID:            "",
		WorkspaceID:         "",
		SubjectClass:        "",
		PolicyRevisionHash:  "",
		AcceptanceUnitID:    scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey:         "pagerduty:pagerduty-primary:pagerduty",
		Status:              workflow.WorkItemStatusPending,
		AttemptCount:        0,
		CurrentClaimID:      "",
		CurrentFencingToken: 0,
		CurrentOwnerID:      "",
		LeaseExpiresAt:      time.Time{},
		VisibleAt:           time.Time{},
		LastClaimedAt:       time.Time{},
		LastCompletedAt:     time.Time{},
		LastFailureClass:    "",
		LastFailureMessage:  "",
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
	}
}

func validPagerDutyInstance(observedAt time.Time, configuration string) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "pagerduty-primary",
		CollectorKind:  scope.CollectorPagerDuty,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  configuration,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}
