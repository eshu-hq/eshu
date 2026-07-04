// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	awsfreshness "github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	gcpfreshness "github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// fakeAWSFreshnessPlanner fails PlanAWSFreshnessWork for one configured
// instance ID and otherwise delegates to the real AWSFreshnessWorkPlanner, so
// tests can prove a single bad assignment does not strand its batch-mates.
type fakeAWSFreshnessPlanner struct {
	failForInstanceID string
	calls             []string
}

func (p *fakeAWSFreshnessPlanner) PlanAWSFreshnessWork(
	ctx context.Context,
	request AWSFreshnessPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	p.calls = append(p.calls, request.Instance.InstanceID)
	if request.Instance.InstanceID == p.failForInstanceID {
		return workflow.Run{}, nil, errors.New("simulated plan failure for " + request.Instance.InstanceID)
	}
	return AWSFreshnessWorkPlanner{}.PlanAWSFreshnessWork(ctx, request)
}

// fakeGCPFreshnessPlanner is fakeAWSFreshnessPlanner's GCP counterpart.
type fakeGCPFreshnessPlanner struct {
	failForInstanceID string
	calls             []string
}

func (p *fakeGCPFreshnessPlanner) PlanGCPWork(
	ctx context.Context,
	request GCPPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	p.calls = append(p.calls, request.Instance.InstanceID)
	if request.Instance.InstanceID == p.failForInstanceID {
		return workflow.Run{}, nil, errors.New("simulated plan failure for " + request.Instance.InstanceID)
	}
	return GCPWorkPlanner{}.PlanGCPWork(ctx, request)
}

// TestScheduleAWSFreshnessWorkContinuesPastOneAssignmentFailure proves the
// #4576 fix: a mid-batch failure planning/handing off ONE instance's
// assignment must not abandon every remaining claimed batch-mate. Two
// instances each authorize a distinct trigger claimed in the same batch; the
// first instance's plan fails, but the second instance's trigger must still
// be planned, enqueued, and handed off.
func TestScheduleAWSFreshnessWorkContinuesPastOneAssignmentFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 4, 9, 0, 0, 0, time.UTC)
	failingTrigger, err := awsfreshness.NewStoredTrigger(awsfreshness.Trigger{
		EventID:     "event-fail",
		Kind:        awsfreshness.EventKindConfigChange,
		AccountID:   "111111111111",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceLambda,
		ObservedAt:  now,
	}, now)
	if err != nil {
		t.Fatalf("NewStoredTrigger(failing) error = %v", err)
	}
	okTrigger, err := awsfreshness.NewStoredTrigger(awsfreshness.Trigger{
		EventID:     "event-ok",
		Kind:        awsfreshness.EventKindConfigChange,
		AccountID:   "222222222222",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceLambda,
		ObservedAt:  now,
	}, now)
	if err != nil {
		t.Fatalf("NewStoredTrigger(ok) error = %v", err)
	}

	freshnessStore := &fakeAWSFreshnessTriggerStore{
		claimed: []awsfreshness.StoredTrigger{failingTrigger, okTrigger},
	}
	planner := &fakeAWSFreshnessPlanner{failForInstanceID: "collector-fail"}
	store := &fakeStore{
		instances: []workflow.CollectorInstance{
			{
				InstanceID:     "collector-fail",
				CollectorKind:  scope.CollectorAWS,
				Mode:           workflow.CollectorModeContinuous,
				Enabled:        true,
				ClaimsEnabled:  true,
				Configuration:  `{"target_scopes":[{"account_id":"111111111111","allowed_regions":["us-east-1"],"allowed_services":["lambda"]}]}`,
				LastObservedAt: now,
				CreatedAt:      now,
				UpdatedAt:      now,
			},
			{
				InstanceID:     "collector-ok",
				CollectorKind:  scope.CollectorAWS,
				Mode:           workflow.CollectorModeContinuous,
				Enabled:        true,
				ClaimsEnabled:  true,
				Configuration:  `{"target_scopes":[{"account_id":"222222222222","allowed_regions":["us-east-1"],"allowed_services":["lambda"]}]}`,
				LastObservedAt: now,
				CreatedAt:      now,
				UpdatedAt:      now,
			},
		},
	}
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive,
			ClaimsEnabled:  true,
		},
		Store:                store,
		AWSFreshnessTriggers: freshnessStore,
		AWSFreshnessPlanner:  planner,
		Clock:                func() time.Time { return now },
	}

	err = service.scheduleAWSFreshnessWork(context.Background(), now, store.instances)
	if err == nil {
		t.Fatal("scheduleAWSFreshnessWork() error = nil, want the aggregated failing-instance error")
	}
	t.Logf("DEBUG err=%v planner.calls=%v failedIDs=%v handedOffIDs=%v", err, planner.calls, freshnessStore.failedIDs, freshnessStore.handedOffIDs)

	// The batch-mate on collector-ok must still have been planned, enqueued,
	// and handed off, proving the failing assignment did not strand it.
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d (collector-ok's run only)", got, want)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
	if len(store.enqueuedItems) == 1 && store.enqueuedItems[0].CollectorInstanceID != "collector-ok" {
		t.Fatalf("enqueued item instance = %q, want %q", store.enqueuedItems[0].CollectorInstanceID, "collector-ok")
	}
	if got, want := len(freshnessStore.handedOffIDs), 1; got != want {
		t.Fatalf("handed off ids = %d, want %d (collector-ok's trigger only)", got, want)
	}
	if got, want := len(freshnessStore.failedIDs), 1; got != want {
		t.Fatalf("failed ids = %d, want %d (collector-fail's trigger marked failed, not stranded at claimed)", got, want)
	}
	if got, want := len(planner.calls), 2; got != want {
		t.Fatalf("planner calls = %d, want %d (both instances must be attempted)", got, want)
	}
}

// TestScheduleGCPFreshnessWorkContinuesPastOneAssignmentFailure is
// TestScheduleAWSFreshnessWorkContinuesPastOneAssignmentFailure's GCP
// counterpart (#4576).
func TestScheduleGCPFreshnessWorkContinuesPastOneAssignmentFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 4, 9, 30, 0, 0, time.UTC)
	failingTrigger, err := gcpfreshness.NewStoredTrigger(gcpfreshness.Trigger{
		EventID:         "event-fail",
		Kind:            gcpfreshness.EventKindAssetChange,
		ParentScopeKind: gcpcloud.ParentScopeProject,
		ParentScopeID:   "project-fail",
		AssetType:       "compute.googleapis.com/Instance",
		Location:        "us-central1-a",
		ObservedAt:      now,
	}, now)
	if err != nil {
		t.Fatalf("NewStoredTrigger(failing) error = %v", err)
	}
	okTrigger, err := gcpfreshness.NewStoredTrigger(gcpfreshness.Trigger{
		EventID:         "event-ok",
		Kind:            gcpfreshness.EventKindAssetChange,
		ParentScopeKind: gcpcloud.ParentScopeProject,
		ParentScopeID:   "project-ok",
		AssetType:       "compute.googleapis.com/Instance",
		Location:        "us-central1-a",
		ObservedAt:      now,
	}, now)
	if err != nil {
		t.Fatalf("NewStoredTrigger(ok) error = %v", err)
	}

	freshnessStore := &fakeGCPFreshnessTriggerStore{
		claimed: []gcpfreshness.StoredTrigger{failingTrigger, okTrigger},
	}
	planner := &fakeGCPFreshnessPlanner{failForInstanceID: "gcp-fail"}
	store := &fakeStore{
		instances: []workflow.CollectorInstance{
			{
				InstanceID:    "gcp-fail",
				CollectorKind: scope.CollectorGCP,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: testGCPConfigWithSingleContentFamily("project-fail", "resource"),
			},
			{
				InstanceID:    "gcp-ok",
				CollectorKind: scope.CollectorGCP,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: testGCPConfigWithSingleContentFamily("project-ok", "resource"),
			},
		},
	}
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive,
			ClaimsEnabled:  true,
		},
		Store:                store,
		GCPFreshnessTriggers: freshnessStore,
		GCPPlanner:           planner,
		Clock:                func() time.Time { return now },
	}

	err = service.scheduleGCPFreshnessWork(context.Background(), now, store.instances)
	if err == nil {
		t.Fatal("scheduleGCPFreshnessWork() error = nil, want the aggregated failing-instance error")
	}

	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d (gcp-ok's run only)", got, want)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
	if len(store.enqueuedItems) == 1 && store.enqueuedItems[0].CollectorInstanceID != "gcp-ok" {
		t.Fatalf("enqueued item instance = %q, want %q", store.enqueuedItems[0].CollectorInstanceID, "gcp-ok")
	}
	if got, want := len(freshnessStore.handedOffIDs), 1; got != want {
		t.Fatalf("handed off ids = %d, want %d (gcp-ok's trigger only)", got, want)
	}
	if got, want := len(freshnessStore.failedIDs), 1; got != want {
		t.Fatalf("failed ids = %d, want %d (gcp-fail's trigger marked failed, not stranded at claimed)", got, want)
	}
	if got, want := len(planner.calls), 2; got != want {
		t.Fatalf("planner calls = %d, want %d (both instances must be attempted)", got, want)
	}
}

// TestRunReapExpiredAWSFreshnessClaimsRecordsMetricsAndReclaims proves the
// coordinator wiring for #4576's stuck-claim reap: runReapExpiredAWSFreshnessClaims
// calls the store's ReapExpiredTriggerClaims, records a success observation
// with the reclaimed count (the operator-visible stuck-claimed signal), and
// is a no-op when AWS freshness claiming is not configured.
func TestRunReapExpiredAWSFreshnessClaimsRecordsMetricsAndReclaims(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 4, 10, 0, 0, 0, time.UTC)
	reclaimed, err := awsfreshness.NewStoredTrigger(awsfreshness.Trigger{
		EventID:     "event-reclaimed",
		Kind:        awsfreshness.EventKindConfigChange,
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceLambda,
		ObservedAt:  now,
	}, now)
	if err != nil {
		t.Fatalf("NewStoredTrigger() error = %v", err)
	}
	freshnessStore := &fakeAWSFreshnessTriggerStore{reclaimed: []awsfreshness.StoredTrigger{reclaimed}}
	metrics := &fakeMetrics{}
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive,
			ClaimsEnabled:  true,
		},
		Store:                &fakeStore{},
		AWSFreshnessTriggers: freshnessStore,
		Metrics:              metrics,
		Clock:                func() time.Time { return now },
	}

	if err := service.runReapExpiredAWSFreshnessClaims(context.Background()); err != nil {
		t.Fatalf("runReapExpiredAWSFreshnessClaims() error = %v, want nil", err)
	}
	if got, want := freshnessStore.reclaimCalls, 1; got != want {
		t.Fatalf("reclaim calls = %d, want %d", got, want)
	}
	if len(freshnessStore.reclaimAsOfSeen) != 1 || !freshnessStore.reclaimAsOfSeen[0].Equal(now) {
		t.Fatalf("reclaim asOf = %v, want %v", freshnessStore.reclaimAsOfSeen, now)
	}
	if got, want := len(metrics.awsFreshnessReapObserved), 1; got != want {
		t.Fatalf("AWS freshness reap observations = %d, want %d", got, want)
	}
	if got, want := metrics.awsFreshnessReapObserved[0].ReclaimedCount, 1; got != want {
		t.Fatalf("reclaimed count = %d, want %d", got, want)
	}
	if got, want := metrics.awsFreshnessReapObserved[0].Outcome, freshnessReapOutcomeSuccess; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
}

// TestRunReapExpiredAWSFreshnessClaimsIsNoOpWithoutStore proves the reap
// helper does not call a nil AWSFreshnessTriggers store (mirrors the sibling
// runAWSFreshnessHandoff guard).
func TestRunReapExpiredAWSFreshnessClaimsIsNoOpWithoutStore(t *testing.T) {
	t.Parallel()

	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive,
			ClaimsEnabled:  true,
		},
	}

	if err := service.runReapExpiredAWSFreshnessClaims(context.Background()); err != nil {
		t.Fatalf("runReapExpiredAWSFreshnessClaims() error = %v, want nil", err)
	}
}

// TestRunReapExpiredAWSFreshnessClaimsRecordsErrorOutcome proves a reap
// failure is recorded with the error outcome rather than silently dropped.
func TestRunReapExpiredAWSFreshnessClaimsRecordsErrorOutcome(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 4, 10, 30, 0, 0, time.UTC)
	freshnessStore := &fakeAWSFreshnessTriggerStore{reclaimErr: errors.New("reap query failed")}
	metrics := &fakeMetrics{}
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive,
			ClaimsEnabled:  true,
		},
		Store:                &fakeStore{},
		AWSFreshnessTriggers: freshnessStore,
		Metrics:              metrics,
		Clock:                func() time.Time { return now },
	}

	if err := service.runReapExpiredAWSFreshnessClaims(context.Background()); err == nil {
		t.Fatal("runReapExpiredAWSFreshnessClaims() error = nil, want the store error")
	}
	if got, want := len(metrics.awsFreshnessReapObserved), 1; got != want {
		t.Fatalf("AWS freshness reap observations = %d, want %d", got, want)
	}
	if got, want := metrics.awsFreshnessReapObserved[0].Outcome, freshnessReapOutcomeError; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
}

// TestRunReapExpiredGCPFreshnessClaimsRecordsMetricsAndReclaims is
// TestRunReapExpiredAWSFreshnessClaimsRecordsMetricsAndReclaims's GCP
// counterpart (#4576).
func TestRunReapExpiredGCPFreshnessClaimsRecordsMetricsAndReclaims(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 4, 11, 0, 0, 0, time.UTC)
	reclaimed, err := gcpfreshness.NewStoredTrigger(gcpfreshness.Trigger{
		EventID:         "event-reclaimed",
		Kind:            gcpfreshness.EventKindAssetChange,
		ParentScopeKind: gcpcloud.ParentScopeProject,
		ParentScopeID:   "project-alpha",
		AssetType:       "compute.googleapis.com/Instance",
		Location:        "us-central1-a",
		ObservedAt:      now,
	}, now)
	if err != nil {
		t.Fatalf("NewStoredTrigger() error = %v", err)
	}
	freshnessStore := &fakeGCPFreshnessTriggerStore{reclaimed: []gcpfreshness.StoredTrigger{reclaimed}}
	metrics := &fakeMetrics{}
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive,
			ClaimsEnabled:  true,
		},
		Store:                &fakeStore{},
		GCPFreshnessTriggers: freshnessStore,
		Metrics:              metrics,
		Clock:                func() time.Time { return now },
	}

	if err := service.runReapExpiredGCPFreshnessClaims(context.Background()); err != nil {
		t.Fatalf("runReapExpiredGCPFreshnessClaims() error = %v, want nil", err)
	}
	if got, want := freshnessStore.reclaimCalls, 1; got != want {
		t.Fatalf("reclaim calls = %d, want %d", got, want)
	}
	if got, want := len(metrics.gcpFreshnessReapObserved), 1; got != want {
		t.Fatalf("GCP freshness reap observations = %d, want %d", got, want)
	}
	if got, want := metrics.gcpFreshnessReapObserved[0].ReclaimedCount, 1; got != want {
		t.Fatalf("reclaimed count = %d, want %d", got, want)
	}
}

// TestRunActiveMaintenanceReapsFreshnessClaimsBeforeHandoff proves the
// ordering wired into runActiveMaintenance: stuck AWS/GCP freshness claims
// are reaped before this tick's handoff runs, so a trigger reclaimed this
// tick is eligible for re-claim in the SAME tick rather than only the next
// one.
func TestRunActiveMaintenanceReapsFreshnessClaimsBeforeHandoff(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 4, 11, 30, 0, 0, time.UTC)
	awsStore := &fakeAWSFreshnessTriggerStore{}
	gcpStore := &fakeGCPFreshnessTriggerStore{}
	store := &fakeStore{}
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive,
			ClaimsEnabled:  true,
		},
		Store:                store,
		AWSFreshnessTriggers: awsStore,
		AWSFreshnessPlanner:  AWSFreshnessWorkPlanner{},
		GCPFreshnessTriggers: gcpStore,
		GCPPlanner:           GCPWorkPlanner{},
		Clock:                func() time.Time { return now },
	}

	if err := service.runActiveMaintenance(context.Background()); err != nil {
		t.Fatalf("runActiveMaintenance() error = %v, want nil", err)
	}
	if awsStore.reclaimCalls != 1 {
		t.Fatalf("AWS reclaim calls = %d, want 1", awsStore.reclaimCalls)
	}
	if gcpStore.reclaimCalls != 1 {
		t.Fatalf("GCP reclaim calls = %d, want 1", gcpStore.reclaimCalls)
	}
	// Both handoffs must also have run this tick (claim call observed),
	// proving the reap did not short-circuit or block the rest of maintenance.
	if awsStore.claimCalls != 1 {
		t.Fatalf("AWS claim calls = %d, want 1", awsStore.claimCalls)
	}
	if gcpStore.claimCalls != 1 {
		t.Fatalf("GCP claim calls = %d, want 1", gcpStore.claimCalls)
	}
}
