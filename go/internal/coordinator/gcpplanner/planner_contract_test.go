// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpplanner

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// testGCPConfigThreeScopesOutOfOrder configures three enabled scopes whose
// JSON array order does not match their ScopeID sort order, so ordering
// assertions actually exercise the sort in gcpEnabledScopes rather than
// passing by coincidence.
func testGCPConfigThreeScopesOutOfOrder() string {
	return `{
		"live_collection_enabled": true,
		"scopes": [{
			"enabled": true,
			"parent_scope_kind": "project",
			"parent_scope_id": "project-zeta",
			"asset_type_family": "compute",
			"content_family": "resource",
			"location_bucket": "global",
			"credential_ref": "credential-zeta"
		}, {
			"enabled": true,
			"parent_scope_kind": "project",
			"parent_scope_id": "project-alpha",
			"asset_type_family": "compute",
			"content_family": "resource",
			"location_bucket": "global",
			"credential_ref": "credential-alpha"
		}, {
			"enabled": true,
			"parent_scope_kind": "project",
			"parent_scope_id": "project-mu",
			"asset_type_family": "compute",
			"content_family": "resource",
			"location_bucket": "global",
			"credential_ref": "credential-mu"
		}]
	}`
}

func testGCPInstance(instanceID string, observedAt time.Time, configuration string) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     instanceID,
		CollectorKind:  scope.CollectorGCP,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  configuration,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}

func TestPlanGCPWorkDeterministicIDsForFixedRequest(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	request := PlanRequest{
		Instance:   testGCPInstance("gcp-primary", observedAt, testGCPConfigWithTwoEnabledScopes()),
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260701T120000Z",
	}

	firstRun, firstItems, err := (WorkPlanner{}).PlanGCPWork(t.Context(), request)
	if err != nil {
		t.Fatalf("PlanGCPWork() first call error = %v, want nil", err)
	}
	secondRun, secondItems, err := (WorkPlanner{}).PlanGCPWork(t.Context(), request)
	if err != nil {
		t.Fatalf("PlanGCPWork() second call error = %v, want nil", err)
	}

	if firstRun.RunID != secondRun.RunID {
		t.Fatalf("RunID = %q, want stable RunID %q for a fixed request", secondRun.RunID, firstRun.RunID)
	}
	if len(firstItems) != len(secondItems) {
		t.Fatalf("item count = %d, want stable count %d", len(secondItems), len(firstItems))
	}
	for i := range firstItems {
		if firstItems[i].WorkItemID != secondItems[i].WorkItemID {
			t.Fatalf("WorkItemID[%d] = %q, want stable %q", i, secondItems[i].WorkItemID, firstItems[i].WorkItemID)
		}
		if firstItems[i].GenerationID != secondItems[i].GenerationID {
			t.Fatalf("GenerationID[%d] = %q, want stable %q", i, secondItems[i].GenerationID, firstItems[i].GenerationID)
		}
		if firstItems[i].FairnessKey != secondItems[i].FairnessKey {
			t.Fatalf("FairnessKey[%d] = %q, want stable %q", i, secondItems[i].FairnessKey, firstItems[i].FairnessKey)
		}
	}
}

func TestPlanGCPWorkPreservesSortedScopeOrder(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	_, items, err := (WorkPlanner{}).PlanGCPWork(t.Context(), PlanRequest{
		Instance:   testGCPInstance("gcp-primary", observedAt, testGCPConfigThreeScopesOutOfOrder()),
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260701T120000Z",
	})
	if err != nil {
		t.Fatalf("PlanGCPWork() error = %v, want nil", err)
	}
	if got, want := len(items), 3; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	scopeIDs := make([]string, len(items))
	for i, item := range items {
		scopeIDs[i] = item.ScopeID
	}
	sorted := append([]string(nil), scopeIDs...)
	sort.Strings(sorted)
	for i := range scopeIDs {
		if scopeIDs[i] != sorted[i] {
			t.Fatalf("item scope order = %v, want sorted %v (gcpEnabledScopes must sort before planning)", scopeIDs, sorted)
		}
	}
}

func TestPlanGCPWorkFiltersByScopeIDs(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	instance := testGCPInstance("gcp-primary", observedAt, testGCPConfigWithTwoEnabledScopes())

	run, items, err := (WorkPlanner{}).PlanGCPWork(t.Context(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "freshness-20260701T120000Z",
		ScopeIDs:   []string{items0ScopeID(t, instance, observedAt)},
	})
	if err != nil {
		t.Fatalf("PlanGCPWork() error = %v, want nil", err)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d (freshness scope filter must narrow planning)", got, want)
	}
	if strings.Contains(run.RequestedScopeSet, "project-beta") {
		t.Fatalf("RequestedScopeSet = %q, must not include the filtered-out scope", run.RequestedScopeSet)
	}
}

// items0ScopeID resolves the first enabled scope's ScopeID via EnabledScopes
// so the filter test targets a real, derived scope id rather than a guessed
// literal.
func items0ScopeID(t *testing.T, instance workflow.CollectorInstance, observedAt time.Time) string {
	t.Helper()
	scopes, err := EnabledScopes(instance.Configuration)
	if err != nil {
		t.Fatalf("EnabledScopes() error = %v, want nil", err)
	}
	if len(scopes) == 0 {
		t.Fatal("EnabledScopes() returned no scopes, want at least one")
	}
	return scopes[0].ScopeID
}

func TestPlanGCPWorkEmptySelectionReturnsPopulatedRunNoItems(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	run, items, err := (WorkPlanner{}).PlanGCPWork(t.Context(), PlanRequest{
		Instance:   testGCPInstance("gcp-primary", observedAt, testGCPConfigWithTwoEnabledScopes()),
		ObservedAt: observedAt,
		PlanKey:    "freshness-20260701T120000Z",
		ScopeIDs:   []string{"gcp:project:project-does-not-exist:compute:resource:global"},
	})
	if err != nil {
		t.Fatalf("PlanGCPWork() error = %v, want nil for a valid empty selection", err)
	}
	if got, want := len(items), 0; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if run.RunID == "" {
		t.Fatal("RunID is empty, want a populated pending run even with no selected items")
	}
	if run.Status != workflow.RunStatusCollectionPending {
		t.Fatalf("Status = %q, want %q", run.Status, workflow.RunStatusCollectionPending)
	}
}

func TestPlanGCPWorkFairnessKeysDistinctPerInstance(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	config := testGCPConfigWithOneDisabledScope()
	_, itemsA, err := (WorkPlanner{}).PlanGCPWork(t.Context(), PlanRequest{
		Instance:   testGCPInstance("gcp-instance-a", observedAt, config),
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260701T120000Z",
	})
	if err != nil {
		t.Fatalf("PlanGCPWork() instance A error = %v, want nil", err)
	}
	_, itemsB, err := (WorkPlanner{}).PlanGCPWork(t.Context(), PlanRequest{
		Instance:   testGCPInstance("gcp-instance-b", observedAt, config),
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260701T120000Z",
	})
	if err != nil {
		t.Fatalf("PlanGCPWork() instance B error = %v, want nil", err)
	}
	if itemsA[0].FairnessKey == itemsB[0].FairnessKey {
		t.Fatalf("FairnessKey collision across instances = %q, want distinct per-instance fairness partitions", itemsA[0].FairnessKey)
	}
}

func TestPlanGCPWorkTimestampsAreUTC(t *testing.T) {
	t.Parallel()

	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	observedAt := time.Date(2026, time.July, 1, 8, 0, 0, 0, loc)
	run, items, err := (WorkPlanner{}).PlanGCPWork(t.Context(), PlanRequest{
		Instance:   testGCPInstance("gcp-primary", observedAt, testGCPConfigWithOneDisabledScope()),
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260701T120000Z",
	})
	if err != nil {
		t.Fatalf("PlanGCPWork() error = %v, want nil", err)
	}
	if run.CreatedAt.Location() != time.UTC {
		t.Fatalf("run.CreatedAt location = %v, want UTC", run.CreatedAt.Location())
	}
	for _, item := range items {
		if item.CreatedAt.Location() != time.UTC || item.UpdatedAt.Location() != time.UTC {
			t.Fatalf("work item timestamps not UTC: created=%v updated=%v", item.CreatedAt.Location(), item.UpdatedAt.Location())
		}
	}
}

func TestEnabledScopesMatchesPlanGCPWorkScopes(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	instance := testGCPInstance("gcp-primary", observedAt, testGCPConfigThreeScopesOutOfOrder())

	_, items, err := (WorkPlanner{}).PlanGCPWork(t.Context(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260701T120000Z",
	})
	if err != nil {
		t.Fatalf("PlanGCPWork() error = %v, want nil", err)
	}
	scopes, err := EnabledScopes(instance.Configuration)
	if err != nil {
		t.Fatalf("EnabledScopes() error = %v, want nil", err)
	}
	if got, want := len(scopes), len(items); got != want {
		t.Fatalf("EnabledScopes() returned %d scopes, want %d matching PlanGCPWork items", got, want)
	}
	for i, item := range items {
		if scopes[i].ScopeID != item.ScopeID {
			t.Fatalf("EnabledScopes()[%d].ScopeID = %q, want %q matching PlanGCPWork item order", i, scopes[i].ScopeID, item.ScopeID)
		}
	}
}

func TestValidateClaimSchedulerConfigurationAcceptsPlannableConfiguration(t *testing.T) {
	t.Parallel()

	desired := workflow.DesiredCollectorInstance{
		InstanceID:    "gcp-primary",
		CollectorKind: scope.CollectorGCP,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: testGCPConfigWithTwoEnabledScopes(),
	}
	if err := ValidateClaimSchedulerConfiguration(desired); err != nil {
		t.Fatalf("ValidateClaimSchedulerConfiguration() error = %v, want nil for the same configuration PlanGCPWork accepts", err)
	}
}
