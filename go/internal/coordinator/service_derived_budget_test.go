// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	packages "github.com/eshu-hq/eshu/go/internal/coordinator/registry/package"
	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestServiceRunActiveModeSurfacesPackageRegistryDerivedBudgetExhaustion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 28, 15, 30, 0, 0, time.UTC)
	instance := testServicePackageRegistryInstance(now)
	instance.Configuration = `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm"],"target_limit":2,"version_limit":1}}`
	targetReader := &fakeOwnedPackageTargetReader{targets: makeOwnedNPMTargets(5)}
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	service := Service{
		Config:                   activeDerivedTargetConfig(scope.CollectorPackageRegistry, "collector-package-registry", instance.Configuration),
		Store:                    store,
		PackageRegistryPlanner:   packages.WorkPlanner{},
		OwnedPackageTargetReader: targetReader,
		Clock:                    func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(targetReader.requests), 1; got != want {
		t.Fatalf("target reader requests = %d, want %d", got, want)
	}
	if got, want := targetReader.requests[0].Limit, 3; got != want {
		t.Fatalf("target reader limit = %d, want budget plus exhaustion lookahead %d", got, want)
	}
	if got, want := targetReader.requests[0].RotationOffset, schedule.DerivedTargetRotationOffset(now, time.Hour, 2); got != want {
		t.Fatalf("target reader rotation offset = %d, want %d", got, want)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}

	var requested struct {
		SkippedTargets []schedule.DerivedTargetSkipEvidence `json:"skipped_targets"`
	}
	if err := json.Unmarshal([]byte(store.createdRuns[0].RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", store.createdRuns[0].RequestedScopeSet, err)
	}
	assertDerivedBudgetSkipEvidence(t, requested.SkippedTargets, "package_registry", 2, 2, 1)
}

func TestServiceRunActiveModeSurfacesVulnerabilityDerivedBudgetExhaustion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 28, 15, 45, 0, 0, time.UTC)
	instance := testServiceVulnerabilityIntelligenceInstance(now)
	instance.Configuration = `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"target_limit":2}}`
	targetReader := &fakeOwnedPackageTargetReader{targets: makeOwnedNPMTargets(5)}
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	service := Service{
		Config:                           activeDerivedTargetConfig(scope.CollectorVulnerabilityIntelligence, "collector-vulnerability-intelligence", instance.Configuration),
		Store:                            store,
		VulnerabilityIntelligencePlanner: vulnerability.IntelligenceWorkPlanner{},
		OwnedPackageTargetReader:         targetReader,
		Clock:                            func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(targetReader.requests), 1; got != want {
		t.Fatalf("target reader requests = %d, want %d", got, want)
	}
	if got, want := targetReader.requests[0].Limit, 3; got != want {
		t.Fatalf("target reader limit = %d, want budget plus exhaustion lookahead %d", got, want)
	}
	if got, want := targetReader.requests[0].RotationOffset, schedule.DerivedTargetRotationOffset(now, time.Hour, 2); got != want {
		t.Fatalf("target reader rotation offset = %d, want %d", got, want)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}

	var requested struct {
		SkippedTargets []schedule.DerivedTargetSkipEvidence `json:"skipped_targets"`
	}
	if err := json.Unmarshal([]byte(store.createdRuns[0].RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", store.createdRuns[0].RequestedScopeSet, err)
	}
	assertDerivedBudgetSkipEvidence(t, requested.SkippedTargets, "vulnerability_intelligence", 2, 2, 1)
}

func TestServiceRunActiveModeSinglePassPackageRegistryDerivedBudgetDoesNotAdmitNextBucket(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.May, 31, 16, 0, 0, 0, time.UTC)
	instance := testServicePackageRegistryInstance(start)
	instance.Configuration = `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm"],"target_limit":2,"version_limit":1,"planning_mode":"single_pass"}}`
	targetReader := &rotatingOwnedPackageTargetReader{targets: makeOwnedNPMTargets(6)}
	store := &terminalizingWorkflowStore{fakeStore: fakeStore{instances: []workflow.CollectorInstance{instance}}}
	service := Service{
		Config:                   activeDerivedTargetConfig(scope.CollectorPackageRegistry, "collector-package-registry", instance.Configuration),
		Store:                    store,
		PackageRegistryPlanner:   packages.WorkPlanner{},
		OwnedPackageTargetReader: targetReader,
	}

	ctx := context.Background()
	service.Clock = func() time.Time { return start }
	if err := service.runReconcile(ctx); err != nil {
		t.Fatalf("first runReconcile() error = %v, want nil", err)
	}
	service.Clock = func() time.Time { return start.Add(time.Hour) }
	if err := service.runReconcile(ctx); err != nil {
		t.Fatalf("second runReconcile() error = %v, want nil", err)
	}

	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want one stable representative run", got)
	}
	if got, want := len(store.enqueuedItems), 2; got != want {
		t.Fatalf("enqueued package-registry derived targets across representative proof = %d, want configured budget %d", got, want)
	}
	for i, request := range targetReader.requests {
		if request.RotationOffset != 0 {
			t.Fatalf("target reader request %d rotation offset = %d, want 0 for single-pass proof", i, request.RotationOffset)
		}
	}
}

func TestServiceRunActiveModeSinglePassVulnerabilityDerivedBudgetDoesNotAdmitNextBucket(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.May, 31, 16, 30, 0, 0, time.UTC)
	instance := testServiceVulnerabilityIntelligenceInstance(start)
	instance.Configuration = `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"target_limit":2,"planning_mode":"single_pass"}}`
	targetReader := &rotatingOwnedPackageTargetReader{targets: makeOwnedNPMTargets(6)}
	store := &terminalizingWorkflowStore{fakeStore: fakeStore{instances: []workflow.CollectorInstance{instance}}}
	service := Service{
		Config:                           activeDerivedTargetConfig(scope.CollectorVulnerabilityIntelligence, "collector-vulnerability-intelligence", instance.Configuration),
		Store:                            store,
		VulnerabilityIntelligencePlanner: vulnerability.IntelligenceWorkPlanner{},
		OwnedPackageTargetReader:         targetReader,
	}

	ctx := context.Background()
	service.Clock = func() time.Time { return start }
	if err := service.runReconcile(ctx); err != nil {
		t.Fatalf("first runReconcile() error = %v, want nil", err)
	}
	service.Clock = func() time.Time { return start.Add(time.Hour) }
	if err := service.runReconcile(ctx); err != nil {
		t.Fatalf("second runReconcile() error = %v, want nil", err)
	}

	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want one stable representative run", got)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued vulnerability-intelligence derived query batches across representative proof = %d, want first bounded batch only", got)
	}
	for i, request := range targetReader.requests {
		if request.RotationOffset != 0 {
			t.Fatalf("target reader request %d rotation offset = %d, want 0 for single-pass proof", i, request.RotationOffset)
		}
	}
}

func activeDerivedTargetConfig(
	collectorKind scope.CollectorKind,
	instanceID string,
	configuration string,
) Config {
	return Config{
		DeploymentMode:           deploymentModeActive,
		ClaimsEnabled:            true,
		ReconcileInterval:        time.Hour,
		ReapInterval:             time.Hour,
		ClaimLeaseTTL:            time.Minute,
		HeartbeatInterval:        20 * time.Second,
		ExpiredClaimLimit:        10,
		ExpiredClaimRequeueDelay: 5 * time.Second,
		CollectorInstances: []workflow.DesiredCollectorInstance{{
			InstanceID:    instanceID,
			CollectorKind: collectorKind,
			Mode:          workflow.CollectorModeContinuous,
			Enabled:       true,
			ClaimsEnabled: true,
			Configuration: configuration,
		}},
	}
}

type rotatingOwnedPackageTargetReader struct {
	requests []workflow.OwnedPackageDependencyTargetFilter
	targets  []workflow.OwnedPackageDependencyTarget
}

func (r *rotatingOwnedPackageTargetReader) ListOwnedPackageDependencyTargets(
	_ context.Context,
	filter workflow.OwnedPackageDependencyTargetFilter,
) ([]workflow.OwnedPackageDependencyTarget, error) {
	r.requests = append(r.requests, filter)
	if len(r.targets) == 0 {
		return nil, nil
	}
	limit := filter.Limit
	if limit <= 0 || limit > len(r.targets) {
		limit = len(r.targets)
	}
	offset := int(filter.RotationOffset % int64(len(r.targets)))
	if offset < 0 {
		offset += len(r.targets)
	}
	out := make([]workflow.OwnedPackageDependencyTarget, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, r.targets[(offset+i)%len(r.targets)])
	}
	return out, nil
}

type terminalizingWorkflowStore struct {
	fakeStore
	terminalRuns map[string]struct{}
}

func (s *terminalizingWorkflowStore) CreateRunWithWorkItemsIfNoOpenTargets(
	ctx context.Context,
	run workflow.Run,
	items []workflow.WorkItem,
) (workflow.RunAdmission, error) {
	if s.terminalRuns == nil {
		s.terminalRuns = make(map[string]struct{})
	}
	if _, terminal := s.terminalRuns[run.RunID]; terminal {
		return workflow.RunAdmission{}, nil
	}
	if err := s.CreateRun(ctx, run); err != nil {
		return workflow.RunAdmission{}, err
	}
	if err := s.EnqueueWorkItems(ctx, items); err != nil {
		return workflow.RunAdmission{}, err
	}
	s.terminalRuns[run.RunID] = struct{}{}
	return workflow.RunAdmission{EligibleTargets: len(items), InsertedWorkItems: len(items)}, nil
}

func makeOwnedNPMTargets(count int) []workflow.OwnedPackageDependencyTarget {
	targets := make([]workflow.OwnedPackageDependencyTarget, 0, count)
	for i := 0; i < count; i++ {
		targets = append(targets, workflow.OwnedPackageDependencyTarget{
			Ecosystem:    "npm",
			PackageName:  fmt.Sprintf("pkg-%03d", i),
			Version:      fmt.Sprintf("1.0.%d", i),
			Lockfile:     true,
			RepositoryID: "repo-representative",
		})
	}
	return targets
}

func assertDerivedBudgetSkipEvidence(
	t *testing.T,
	skipped []schedule.DerivedTargetSkipEvidence,
	collectorKind string,
	targetLimit int,
	selectedCount int,
	skippedCount int,
) {
	t.Helper()

	if got, want := len(skipped), 1; got != want {
		t.Fatalf("len(skipped_targets) = %d, want %d: %#v", got, want, skipped)
	}
	row := skipped[0]
	if row.CollectorKind != collectorKind {
		t.Fatalf("CollectorKind = %q, want %q", row.CollectorKind, collectorKind)
	}
	if row.Reason != schedule.DerivedTargetSkipReasonBudgetExhausted {
		t.Fatalf("Reason = %q, want %q", row.Reason, schedule.DerivedTargetSkipReasonBudgetExhausted)
	}
	if row.TargetLimit != targetLimit {
		t.Fatalf("TargetLimit = %d, want %d", row.TargetLimit, targetLimit)
	}
	if row.SelectedCount != selectedCount {
		t.Fatalf("SelectedCount = %d, want %d", row.SelectedCount, selectedCount)
	}
	if row.SkippedCount != skippedCount {
		t.Fatalf("SkippedCount = %d, want %d", row.SkippedCount, skippedCount)
	}
}
