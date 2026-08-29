// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jiraplanner

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
	observedAt := time.Date(2026, time.June, 8, 18, 30, 0, 0, location)
	instance := validJiraInstance(observedAt, `{
		"targets": [{
			"provider": " jira_cloud ",
			"scope_id": " jira:site:zeta ",
			"site_id": " zeta.atlassian.example ",
			"base_url": "https://zeta.atlassian.example",
			"email_env": "JIRA_ZETA_EMAIL",
			"token_env": "JIRA_ZETA_TOKEN",
			"jql": "project = ZETA ORDER BY updated ASC",
			"issue_limit": 25,
			"updated_lookback": "6h",
			"changelog_limit": 25,
			"remote_link_limit": 25
		}, {
			"provider": "jira_cloud",
			"scope_id": "jira:site:alpha",
			"site_id": "alpha.atlassian.example",
			"base_url": "https://alpha.atlassian.example",
			"email_env": "JIRA_ALPHA_EMAIL",
			"token_env": "JIRA_ALPHA_TOKEN",
			"jql": "project = ALPHA ORDER BY updated ASC",
			"issue_limit": 50,
			"updated_lookback": "12h",
			"changelog_limit": 50,
			"remote_link_limit": 50
		}]
	}`)
	instance.Bootstrap = true
	planKey := "freshness-20260608T183000-0400"
	request := PlanRequest{
		Instance:    instance,
		ObservedAt:  observedAt,
		PlanKey:     planKey,
		TriggerKind: workflow.TriggerKindWebhook,
		ScopeIDs: []string{
			"", " jira:site:alpha ", " jira:site:zeta ", "\tjira:site:alpha\t",
		},
	}

	run, items, err := (WorkPlanner{}).PlanJiraWork(t.Context(), request)
	if err != nil {
		t.Fatalf("PlanJiraWork() error = %v, want nil", err)
	}
	runAgain, itemsAgain, err := (WorkPlanner{}).PlanJiraWork(t.Context(), request)
	if err != nil {
		t.Fatalf("second PlanJiraWork() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(run, runAgain) || !reflect.DeepEqual(items, itemsAgain) {
		t.Fatalf("same request produced different rows\nfirst:  %#v %#v\nsecond: %#v %#v", run, items, runAgain, itemsAgain)
	}

	wantRun := workflow.Run{
		RunID:              "jira:jira-primary:webhook:" + planKey,
		TriggerKind:        workflow.TriggerKindWebhook,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  `{"collector_instance_id":"jira-primary","targets":[{"scope_id":"jira:site:alpha","provider":"jira_cloud","site_id":"alpha.atlassian.example"},{"scope_id":"jira:site:zeta","provider":"jira_cloud","site_id":"zeta.atlassian.example"}]}`,
		RequestedCollector: string(scope.CollectorJira),
		CreatedAt:          observedAt.UTC(),
		UpdatedAt:          observedAt.UTC(),
		FinishedAt:         time.Time{},
	}
	for _, forbidden := range []string{
		"JIRA_ZETA_EMAIL", "JIRA_ALPHA_EMAIL", "JIRA_ZETA_TOKEN", "JIRA_ALPHA_TOKEN",
		"base_url", "jql", "project = ZETA", "project = ALPHA", "issue_limit",
		"updated_lookback", "6h", "12h", "changelog_limit", "remote_link_limit",
	} {
		if strings.Contains(run.RequestedScopeSet, forbidden) {
			t.Fatalf("RequestedScopeSet = %q, must not contain %q", run.RequestedScopeSet, forbidden)
		}
	}
	if !reflect.DeepEqual(run, wantRun) {
		t.Fatalf("run = %#v, want %#v", run, wantRun)
	}
	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if got, want := []string{items[0].ScopeID, items[1].ScopeID}, []string{"jira:site:zeta", "jira:site:alpha"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("item scope order = %#v, want %#v", got, want)
	}
	wantGenerationIDs := []string{
		"jira:1b91de730c442859f4ebbce88a37d3c51a6aed5e9a064870578869f6477a4766",
		"jira:1aa4bd232f6a61a2a1e46ed2e60b19c47a6a14fb5e94c04f85805e72d4558066",
	}
	if got := []string{items[0].GenerationID, items[1].GenerationID}; !reflect.DeepEqual(got, wantGenerationIDs) {
		t.Fatalf("generation IDs = %#v, want %#v", got, wantGenerationIDs)
	}
	for index, item := range items {
		if item.CreatedAt != observedAt.UTC() || item.UpdatedAt != observedAt.UTC() {
			t.Fatalf("item timestamps = %s/%s, want %s", item.CreatedAt, item.UpdatedAt, observedAt.UTC())
		}
		if got, want := item.CollectorInstanceID, instance.InstanceID; got != want {
			t.Fatalf("CollectorInstanceID = %q, want %q", got, want)
		}
		if got, want := item.Status, workflow.WorkItemStatusPending; got != want {
			t.Fatalf("Status = %q, want %q", got, want)
		}
		if item.RunID != wantRun.RunID || item.CollectorKind != scope.CollectorJira || item.SourceSystem != string(scope.CollectorJira) {
			t.Fatalf("item identity = %#v, want Jira item for run %q", item, wantRun.RunID)
		}
		if item.SourceRunID != item.GenerationID || item.AcceptanceUnitID != item.ScopeID {
			t.Fatalf("item durable identity = %#v, want source run/generation and scope/acceptance parity", item)
		}
		if got, want := item.WorkItemID, "jira:jira-primary:"+wantGenerationIDs[index]; got != want {
			t.Fatalf("WorkItemID = %q, want %q", got, want)
		}
	}
	if got, want := items[0].FairnessKey, "jira:jira-primary:zeta.atlassian.example"; got != want {
		t.Fatalf("zeta FairnessKey = %q, want %q", got, want)
	}
	if got, want := items[1].FairnessKey, "jira:jira-primary:alpha.atlassian.example"; got != want {
		t.Fatalf("alpha FairnessKey = %q, want %q", got, want)
	}
}

func validJiraInstance(observedAt time.Time, configuration string) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "jira-primary",
		CollectorKind:  scope.CollectorJira,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  configuration,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}
