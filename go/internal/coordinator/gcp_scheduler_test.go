// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestGCPWorkPlannerPlansOneClaimPerEnabledScope(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 18, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "gcp-primary",
		CollectorKind:  scope.CollectorGCP,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testGCPConfigWithTwoEnabledScopes(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (GCPWorkPlanner{}).PlanGCPWork(t.Context(), GCPPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260618T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanGCPWork() error = %v, want nil", err)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorGCP); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindSchedule; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if items[0].FairnessKey == items[1].FairnessKey {
		t.Fatalf("FairnessKey collision = %q, want distinct per-scope keys", items[0].FairnessKey)
	}
	for _, item := range items {
		if got, want := item.CollectorKind, scope.CollectorGCP; got != want {
			t.Fatalf("CollectorKind = %q, want %q", got, want)
		}
		if got, want := item.CollectorInstanceID, "gcp-primary"; got != want {
			t.Fatalf("CollectorInstanceID = %q, want %q", got, want)
		}
		if item.ScopeID == "" {
			t.Fatal("ScopeID is empty, want derived bounded GCP scope")
		}
	}
	for _, want := range []string{"project-alpha", "project-beta"} {
		if !strings.Contains(run.RequestedScopeSet, want) {
			t.Fatalf("RequestedScopeSet = %q, want scope marker %q", run.RequestedScopeSet, want)
		}
	}
	for _, forbidden := range []string{"credential-handle", "credential-ref-two"} {
		if strings.Contains(run.RequestedScopeSet, forbidden) {
			t.Fatalf("RequestedScopeSet = %q, must not expose credential ref %q", run.RequestedScopeSet, forbidden)
		}
	}
}

func TestGCPWorkPlannerSkipsDisabledScopes(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 18, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "gcp-primary",
		CollectorKind:  scope.CollectorGCP,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testGCPConfigWithOneDisabledScope(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (GCPWorkPlanner{}).PlanGCPWork(t.Context(), GCPPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260618T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanGCPWork() error = %v, want nil", err)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if !strings.Contains(items[0].ScopeID, "project-alpha") {
		t.Fatalf("ScopeID = %q, want enabled project scope", items[0].ScopeID)
	}
	if strings.Contains(run.RequestedScopeSet, "project-beta") {
		t.Fatalf("RequestedScopeSet = %q, must not include disabled scope", run.RequestedScopeSet)
	}
}

func TestGCPWorkPlannerRejectsConfigWithoutLiveMode(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 18, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "gcp-primary",
		CollectorKind:  scope.CollectorGCP,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"scopes":[{"enabled":true,"parent_scope_kind":"project","parent_scope_id":"project-alpha","credential_ref":"credential-handle"}]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	if _, _, err := (GCPWorkPlanner{}).PlanGCPWork(t.Context(), GCPPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260618T150000Z",
	}); err == nil {
		t.Fatal("PlanGCPWork() error = nil, want live-mode rejection")
	}
}

func TestGCPWorkPlannerRejectsLiveModeWithoutEnabledScopes(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 18, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "gcp-primary",
		CollectorKind:  scope.CollectorGCP,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"live_collection_enabled":true,"scopes":[{"enabled":false}]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	if _, _, err := (GCPWorkPlanner{}).PlanGCPWork(t.Context(), GCPPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260618T150000Z",
	}); err == nil {
		t.Fatal("PlanGCPWork() error = nil, want enabled-scope rejection")
	}
}

func testGCPConfigWithTwoEnabledScopes() string {
	return `{
		"live_collection_enabled": true,
		"scopes": [{
			"enabled": true,
			"parent_scope_kind": "project",
			"parent_scope_id": "project-alpha",
			"asset_type_family": "compute",
			"content_family": "resource",
			"location_bucket": "global",
			"credential_ref": "credential-handle"
		}, {
			"enabled": true,
			"parent_scope_kind": "project",
			"parent_scope_id": "project-beta",
			"asset_type_family": "storage",
			"content_family": "iam_policy",
			"location_bucket": "us",
			"credential_ref": "credential-ref-two"
		}]
	}`
}

func testGCPConfigWithOneDisabledScope() string {
	return `{
		"live_collection_enabled": true,
		"scopes": [{
			"enabled": true,
			"parent_scope_kind": "project",
			"parent_scope_id": "project-alpha",
			"asset_type_family": "compute",
			"content_family": "resource",
			"location_bucket": "global",
			"credential_ref": "credential-handle"
		}, {
			"enabled": false,
			"parent_scope_kind": "project",
			"parent_scope_id": "project-beta",
			"asset_type_family": "storage",
			"content_family": "iam_policy",
			"location_bucket": "us",
			"credential_ref": "credential-ref-two"
		}]
	}`
}
