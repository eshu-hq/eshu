// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func testGCPConfigWithSingleContentFamily(parentScopeID, contentFamily string) string {
	return `{
		"live_collection_enabled": true,
		"scopes": [{
			"enabled": true,
			"parent_scope_kind": "project",
			"parent_scope_id": "` + parentScopeID + `",
			"asset_type_family": "compute",
			"content_family": "` + contentFamily + `",
			"location_bucket": "us-central1",
			"credential_ref": "credential-handle"
		}]
	}`
}

func testGCPConfigWithTwoProjectScopes() string {
	return `{
		"live_collection_enabled": true,
		"scopes": [{
			"enabled": true,
			"parent_scope_kind": "project",
			"parent_scope_id": "project-alpha",
			"asset_type_family": "compute",
			"content_family": "resource",
			"location_bucket": "us-central1",
			"credential_ref": "credential-handle"
		}, {
			"enabled": true,
			"parent_scope_kind": "project",
			"parent_scope_id": "project-beta",
			"asset_type_family": "compute",
			"content_family": "resource",
			"location_bucket": "us-central1",
			"credential_ref": "credential-handle"
		}]
	}`
}

// TestServiceRunActiveModeFansOutGCPFreshnessTriggerAcrossMultipleInstances
// proves bug 2 (codex, PR #4577): resolveGCPFreshnessScopeIDs must not stop
// at the first enabled, claim-enabled GCP collector instance that matches the
// trigger's tuple. Two instances each configure only ONE of the two content
// families sharing that tuple (gcp-primary configures "resource",
// gcp-secondary configures "iam_policy"); together they cover both, but
// neither alone does. Both instances must be planned so neither content
// family is silently under-scanned.
func TestServiceRunActiveModeFansOutGCPFreshnessTriggerAcrossMultipleInstances(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 3, 6, 0, 0, 0, time.UTC)
	trigger, err := freshness.NewStoredTrigger(freshness.Trigger{
		EventID:         "event-1",
		Kind:            freshness.EventKindAssetChange,
		ParentScopeKind: gcpcloud.ParentScopeProject,
		ParentScopeID:   "project-alpha",
		AssetType:       "compute.googleapis.com/Instance",
		Location:        "us-central1",
		ObservedAt:      now,
	}, now)
	if err != nil {
		t.Fatalf("NewStoredTrigger() error = %v", err)
	}
	freshnessStore := &fakeGCPFreshnessTriggerStore{claimed: []freshness.StoredTrigger{trigger}}
	counter := &fakeGCPFreshnessCounter{}
	fanOut := &fakeGCPFreshnessFanOutRecorder{}
	primary := workflow.CollectorInstance{
		InstanceID:     "gcp-primary",
		CollectorKind:  scope.CollectorGCP,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testGCPConfigWithSingleContentFamily("project-alpha", "resource"),
		LastObservedAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	secondary := workflow.CollectorInstance{
		InstanceID:     "gcp-secondary",
		CollectorKind:  scope.CollectorGCP,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testGCPConfigWithSingleContentFamily("project-alpha", "iam_policy"),
		LastObservedAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	store := &fakeStore{instances: []workflow.CollectorInstance{primary, secondary}}
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
			CollectorInstances: []workflow.DesiredCollectorInstance{
				{
					InstanceID:    primary.InstanceID,
					CollectorKind: primary.CollectorKind,
					Mode:          primary.Mode,
					Enabled:       primary.Enabled,
					ClaimsEnabled: primary.ClaimsEnabled,
					Configuration: primary.Configuration,
				},
				{
					InstanceID:    secondary.InstanceID,
					CollectorKind: secondary.CollectorKind,
					Mode:          secondary.Mode,
					Enabled:       secondary.Enabled,
					ClaimsEnabled: secondary.ClaimsEnabled,
					Configuration: secondary.Configuration,
				},
			},
		},
		Store:                store,
		GCPPlanner:           GCPWorkPlanner{},
		GCPFreshnessTriggers: freshnessStore,
		GCPFreshnessEvents:   counter,
		GCPFreshnessFanOut:   fanOut,
		Clock:                func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(store.createdRuns), 2; got != want {
		t.Fatalf("created runs = %d, want %d (one per matching instance)", got, want)
	}
	if got, want := len(store.enqueuedItems), 2; got != want {
		t.Fatalf("enqueued items = %d, want %d (one per instance's own content family)", got, want)
	}
	sawPrimary, sawSecondary := false, false
	for _, item := range store.enqueuedItems {
		switch item.CollectorInstanceID {
		case "gcp-primary":
			sawPrimary = true
		case "gcp-secondary":
			sawSecondary = true
		}
	}
	if !sawPrimary || !sawSecondary {
		t.Fatalf("enqueued items = %+v, want work planned for both gcp-primary and gcp-secondary", store.enqueuedItems)
	}
	if got, want := len(freshnessStore.handedOffIDs), 2; got != want {
		t.Fatalf("handed off ids = %d, want %d (one handoff per matching instance)", got, want)
	}
	if got, want := len(fanOut.values), 2; got != want {
		t.Fatalf("fan-out cardinality observations = %d, want %d (one per instance)", got, want)
	}
}

// TestServiceRunActiveModeCoalescesGCPFreshnessTriggersForSameInstance proves
// the RunID-collision fix (#4577): two distinct triggers (different target
// tuples) resolving to the SAME GCP collector instance within one reconcile
// batch must be merged into exactly one PlanGCPWork/
// createWorkflowWorkIfNoOpenTargets call, not two independent calls that
// would compute the identical (instance, interval) PlanKey/RunID and race
// each other through workflowRunIsTerminal/workItemsWithoutOpenTargets.
func TestServiceRunActiveModeCoalescesGCPFreshnessTriggersForSameInstance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 3, 6, 30, 0, 0, time.UTC)
	triggerAlpha, err := freshness.NewStoredTrigger(freshness.Trigger{
		EventID:         "event-alpha",
		Kind:            freshness.EventKindAssetChange,
		ParentScopeKind: gcpcloud.ParentScopeProject,
		ParentScopeID:   "project-alpha",
		AssetType:       "compute.googleapis.com/Instance",
		Location:        "us-central1",
		ObservedAt:      now,
	}, now)
	if err != nil {
		t.Fatalf("NewStoredTrigger() error = %v", err)
	}
	triggerBeta, err := freshness.NewStoredTrigger(freshness.Trigger{
		EventID:         "event-beta",
		Kind:            freshness.EventKindAssetChange,
		ParentScopeKind: gcpcloud.ParentScopeProject,
		ParentScopeID:   "project-beta",
		AssetType:       "compute.googleapis.com/Instance",
		Location:        "us-central1",
		ObservedAt:      now,
	}, now)
	if err != nil {
		t.Fatalf("NewStoredTrigger() error = %v", err)
	}
	freshnessStore := &fakeGCPFreshnessTriggerStore{claimed: []freshness.StoredTrigger{triggerAlpha, triggerBeta}}
	counter := &fakeGCPFreshnessCounter{}
	fanOut := &fakeGCPFreshnessFanOutRecorder{}
	instance := workflow.CollectorInstance{
		InstanceID:     "gcp-primary",
		CollectorKind:  scope.CollectorGCP,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testGCPConfigWithTwoProjectScopes(),
		LastObservedAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
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
				InstanceID:    instance.InstanceID,
				CollectorKind: instance.CollectorKind,
				Mode:          instance.Mode,
				Enabled:       instance.Enabled,
				ClaimsEnabled: instance.ClaimsEnabled,
				Configuration: instance.Configuration,
			}},
		},
		Store:                store,
		GCPPlanner:           GCPWorkPlanner{},
		GCPFreshnessTriggers: freshnessStore,
		GCPFreshnessEvents:   counter,
		GCPFreshnessFanOut:   fanOut,
		Clock:                func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d (both triggers coalesced into one run for the shared instance)", got, want)
	}
	if got, want := len(store.enqueuedItems), 2; got != want {
		t.Fatalf("enqueued items = %d, want %d (one per trigger's own scope)", got, want)
	}
	if got, want := len(freshnessStore.handedOffIDs), 2; got != want {
		t.Fatalf("handed off ids = %d, want %d (both triggers marked handed off from the single merged handoff)", got, want)
	}
	if got, want := len(fanOut.values), 1; got != want {
		t.Fatalf("fan-out cardinality observations = %d, want %d (one merged handoff)", got, want)
	}
	if got, want := fanOut.values[0], int64(2); got != want {
		t.Fatalf("fan-out cardinality = %d, want %d (deduped scope ids across both triggers)", got, want)
	}
}
