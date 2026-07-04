// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	"go.opentelemetry.io/otel/metric"
)

type fakeGCPFreshnessTriggerStore struct {
	claimed         []freshness.StoredTrigger
	claimCalls      int
	claimedLeases   []time.Duration
	handedOffIDs    []string
	failedIDs       []string
	failureClass    string
	failureReason   string
	markFailedErr   error
	reclaimed       []freshness.StoredTrigger
	reclaimCalls    int
	reclaimErr      error
	reclaimAsOfSeen []time.Time
}

func (f *fakeGCPFreshnessTriggerStore) ClaimQueuedTriggers(
	_ context.Context,
	_ string,
	_ time.Time,
	_ int,
	leaseDuration time.Duration,
) ([]freshness.StoredTrigger, error) {
	f.claimCalls++
	f.claimedLeases = append(f.claimedLeases, leaseDuration)
	return append([]freshness.StoredTrigger(nil), f.claimed...), nil
}

func (f *fakeGCPFreshnessTriggerStore) ReapExpiredTriggerClaims(
	_ context.Context,
	asOf time.Time,
	_ int,
) ([]freshness.StoredTrigger, error) {
	f.reclaimCalls++
	f.reclaimAsOfSeen = append(f.reclaimAsOfSeen, asOf)
	if f.reclaimErr != nil {
		return nil, f.reclaimErr
	}
	return append([]freshness.StoredTrigger(nil), f.reclaimed...), nil
}

func (f *fakeGCPFreshnessTriggerStore) MarkTriggersHandedOff(
	_ context.Context,
	triggerIDs []string,
	_ time.Time,
) error {
	f.handedOffIDs = append(f.handedOffIDs, triggerIDs...)
	return nil
}

func (f *fakeGCPFreshnessTriggerStore) MarkTriggersFailed(
	_ context.Context,
	triggerIDs []string,
	_ time.Time,
	failureClass string,
	failureReason string,
) error {
	f.failedIDs = append(f.failedIDs, triggerIDs...)
	f.failureClass = failureClass
	f.failureReason = failureReason
	return f.markFailedErr
}

type fakeGCPFreshnessCounter struct {
	events int
}

func (f *fakeGCPFreshnessCounter) Add(context.Context, int64, ...metric.AddOption) {
	f.events++
}

type fakeGCPFreshnessFanOutRecorder struct {
	values []int64
}

func (f *fakeGCPFreshnessFanOutRecorder) Record(_ context.Context, value int64, _ ...metric.RecordOption) {
	f.values = append(f.values, value)
}

// TestServiceRunActiveModeFansOutGCPFreshnessTriggerToMultipleContentFamilies
// proves the fan-out decision (#4338): a GCP freshness trigger carries no
// content_family signal (Kind/ParentScopeKind/ParentScopeID/AssetType/Location
// only), so it must resolve to every configured scope sharing
// (parent_scope_kind, parent_scope_id, asset_type_family, location_bucket)
// regardless of content_family. Guessing a single content_family would
// silently under-scan the others. This test configures two scopes that share
// that tuple but differ only in content_family and proves both get scheduled
// from one trigger.
func TestServiceRunActiveModeFansOutGCPFreshnessTriggerToMultipleContentFamilies(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
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
	instance := workflow.CollectorInstance{
		InstanceID:     "gcp-primary",
		CollectorKind:  scope.CollectorGCP,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testGCPConfigWithSharedTupleTwoContentFamilies(),
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
	if freshnessStore.claimCalls != 1 {
		t.Fatalf("claim calls = %d, want 1", freshnessStore.claimCalls)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
	if got, want := len(store.enqueuedItems), 2; got != want {
		t.Fatalf("enqueued items = %d, want %d (fan-out to both content families)", got, want)
	}
	sawResource, sawIAMPolicy := false, false
	for _, item := range store.enqueuedItems {
		if strings.Contains(item.ScopeID, "resource") {
			sawResource = true
		}
		if strings.Contains(item.ScopeID, "iam_policy") {
			sawIAMPolicy = true
		}
	}
	if !sawResource || !sawIAMPolicy {
		t.Fatalf("enqueued items = %+v, want fan-out to both resource and iam_policy content families", store.enqueuedItems)
	}
	if got, want := len(freshnessStore.handedOffIDs), 1; got != want {
		t.Fatalf("handed off ids = %d, want %d", got, want)
	}
	if got, want := counter.events, 2; got != want {
		t.Fatalf("GCP freshness metric events = %d, want %d", got, want)
	}
	if got, want := len(fanOut.values), 1; got != want {
		t.Fatalf("fan-out cardinality observations = %d, want %d", got, want)
	}
	if got, want := fanOut.values[0], int64(2); got != want {
		t.Fatalf("fan-out cardinality = %d, want %d scopes resolved for one trigger", got, want)
	}
}

// TestServiceRunActiveModeSkipsGCPFreshnessWhenPriorTargetIsOpen proves
// idempotent re-handoff behavior mirrors the AWS freshness precedent: a
// trigger whose resolved scope already has an open work item is marked
// handed off without creating a duplicate run.
func TestServiceRunActiveModeSkipsGCPFreshnessWhenPriorTargetIsOpen(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 12, 30, 0, 0, time.UTC)
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
	instance := workflow.CollectorInstance{
		InstanceID:     "gcp-primary",
		CollectorKind:  scope.CollectorGCP,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testGCPConfigWithSharedTupleTwoContentFamilies(),
		LastObservedAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	store := &fakeStore{
		instances: []workflow.CollectorInstance{instance},
		enqueuedItems: []workflow.WorkItem{
			{
				CollectorKind:       scope.CollectorGCP,
				CollectorInstanceID: "gcp-primary",
				ScopeID:             "gcp:project:project-alpha:compute:resource:us-central1",
				AcceptanceUnitID:    "gcp:project:project-alpha:compute:resource:us-central1",
			},
			{
				CollectorKind:       scope.CollectorGCP,
				CollectorInstanceID: "gcp-primary",
				ScopeID:             "gcp:project:project-alpha:compute:iam_policy:us-central1",
				AcceptanceUnitID:    "gcp:project:project-alpha:compute:iam_policy:us-central1",
			},
		},
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
		Clock:                func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(store.createdRuns), 0; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
	if got, want := len(store.enqueuedItems), 2; got != want {
		t.Fatalf("enqueued items = %d, want original open items only %d", got, want)
	}
	if got, want := len(freshnessStore.handedOffIDs), 1; got != want {
		t.Fatalf("handed off ids = %d, want %d", got, want)
	}
	if got, want := len(freshnessStore.failedIDs), 0; got != want {
		t.Fatalf("failed ids = %d, want %d", got, want)
	}
}

// TestScheduleGCPFreshnessWorkRequiresPlannerBeforeClaim mirrors the AWS
// precedent: the scheduler must fail fast when misconfigured rather than
// silently drop claimed triggers.
func TestScheduleGCPFreshnessWorkRequiresPlannerBeforeClaim(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 12, 45, 0, 0, time.UTC)
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
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive,
			ClaimsEnabled:  true,
		},
		GCPFreshnessTriggers: freshnessStore,
	}

	err = service.scheduleGCPFreshnessWork(context.Background(), now, []workflow.CollectorInstance{{
		InstanceID:    "gcp-primary",
		CollectorKind: scope.CollectorGCP,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: testGCPConfigWithSharedTupleTwoContentFamilies(),
	}})

	if err == nil {
		t.Fatalf("scheduleGCPFreshnessWork() error = nil, want planner error")
	}
	if freshnessStore.claimCalls != 0 {
		t.Fatalf("claim calls = %d, want 0", freshnessStore.claimCalls)
	}
}

// TestScheduleGCPFreshnessWorkMarksFailedWhenNoScopeMatchesTuple proves an
// unauthorized/unmatched target (no configured scope shares the trigger's
// (parent_scope_kind, parent_scope_id, asset_type_family, location_bucket)
// tuple) is marked failed rather than silently dropped.
func TestScheduleGCPFreshnessWorkMarksFailedWhenNoScopeMatchesTuple(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 13, 0, 0, 0, time.UTC)
	trigger, err := freshness.NewStoredTrigger(freshness.Trigger{
		EventID:         "event-1",
		Kind:            freshness.EventKindAssetChange,
		ParentScopeKind: gcpcloud.ParentScopeProject,
		ParentScopeID:   "project-unconfigured",
		AssetType:       "compute.googleapis.com/Instance",
		Location:        "us-central1",
		ObservedAt:      now,
	}, now)
	if err != nil {
		t.Fatalf("NewStoredTrigger() error = %v", err)
	}
	freshnessStore := &fakeGCPFreshnessTriggerStore{claimed: []freshness.StoredTrigger{trigger}}
	counter := &fakeGCPFreshnessCounter{}
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive,
			ClaimsEnabled:  true,
		},
		GCPFreshnessTriggers: freshnessStore,
		GCPPlanner:           GCPWorkPlanner{},
		GCPFreshnessEvents:   counter,
	}

	err = service.scheduleGCPFreshnessWork(context.Background(), now, []workflow.CollectorInstance{{
		InstanceID:    "gcp-primary",
		CollectorKind: scope.CollectorGCP,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: testGCPConfigWithSharedTupleTwoContentFamilies(),
	}})
	if err != nil {
		t.Fatalf("scheduleGCPFreshnessWork() error = %v, want nil (failure is recorded, not propagated)", err)
	}
	if got, want := len(freshnessStore.failedIDs), 1; got != want {
		t.Fatalf("failed ids = %d, want %d", got, want)
	}
	if got, want := freshnessStore.failureClass, "unauthorized_target"; got != want {
		t.Fatalf("failure class = %q, want %q", got, want)
	}
}

// TestMarkGCPFreshnessFailedLogsWhenMarkErrors mirrors the AWS precedent
// (#3793): a best-effort MarkTriggersFailed failure must be observable via a
// WARN log, not silently dropped.
func TestMarkGCPFreshnessFailedLogsWhenMarkErrors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 13, 15, 0, 0, time.UTC)
	var logs bytes.Buffer
	triggerStore := &fakeGCPFreshnessTriggerStore{markFailedErr: errors.New("postgres write failed")}
	service := Service{
		GCPFreshnessTriggers: triggerStore,
		Logger:               slog.New(slog.NewTextHandler(&logs, nil)),
	}

	service.markGCPFreshnessFailed(
		context.Background(),
		[]freshness.StoredTrigger{{TriggerID: "gcp-trigger-1"}},
		now,
		"gcp_freshness_handoff_error",
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

func testGCPConfigWithSharedTupleTwoContentFamilies() string {
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
			"parent_scope_id": "project-alpha",
			"asset_type_family": "compute",
			"content_family": "iam_policy",
			"location_bucket": "us-central1",
			"credential_ref": "credential-handle"
		}]
	}`
}
