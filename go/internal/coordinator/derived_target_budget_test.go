// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestPackageRegistryWorkPlannerReportsDerivedTargetBudgetExhaustion(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 28, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "collector-package-registry",
		CollectorKind:  scope.CollectorPackageRegistry,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm"],"target_limit":2,"version_limit":1}}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := PackageRegistryWorkPlanner{}.PlanPackageRegistryWork(context.Background(), PackageRegistryPlanRequest{
		Instance:            instance,
		ObservedAt:          observedAt,
		PlanKey:             "representative-20260528T150000Z",
		OwnedPackageTargets: makeOwnedNPMTargets(5),
	})
	if err != nil {
		t.Fatalf("PlanPackageRegistryWork() error = %v, want nil", err)
	}
	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want representative budget %d", got, want)
	}

	var requested struct {
		SkippedTargets []derivedTargetSkipEvidence `json:"skipped_targets"`
	}
	if err := json.Unmarshal([]byte(run.RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", run.RequestedScopeSet, err)
	}
	assertDerivedBudgetSkipEvidence(t, requested.SkippedTargets, "package_registry", 2, 2, 3)
}

func TestVulnerabilityIntelligenceWorkPlannerReportsDerivedQueryBudgetExhaustion(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 28, 15, 15, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "collector-vulnerability-intelligence",
		CollectorKind:  scope.CollectorVulnerabilityIntelligence,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"target_limit":2}}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := VulnerabilityIntelligenceWorkPlanner{}.PlanVulnerabilityIntelligenceWork(context.Background(), VulnerabilityIntelligencePlanRequest{
		Instance:            instance,
		ObservedAt:          observedAt,
		PlanKey:             "representative-20260528T151500Z",
		OwnedPackageTargets: makeOwnedNPMTargets(5),
	})
	if err != nil {
		t.Fatalf("PlanVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want one bounded OSV batch for the selected query budget", got)
	}

	var requested struct {
		Targets []struct {
			Queries []vulnerabilityDerivedQuery `json:"queries"`
		} `json:"targets"`
		SkippedTargets []derivedTargetSkipEvidence `json:"skipped_targets"`
	}
	if err := json.Unmarshal([]byte(run.RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", run.RequestedScopeSet, err)
	}
	if got := len(requested.Targets); got == 0 {
		t.Fatalf("RequestedScopeSet targets = 0, want at least one OSV query batch: %s", run.RequestedScopeSet)
	}
	if got, want := len(requested.Targets[0].Queries), 2; got != want {
		t.Fatalf("len(RequestedScopeSet.targets[0].queries) = %d, want selected query budget %d", got, want)
	}
	assertDerivedBudgetSkipEvidence(t, requested.SkippedTargets, "vulnerability_intelligence", 2, 2, 3)
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
	skipped []derivedTargetSkipEvidence,
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
	if row.Reason != derivedTargetSkipReasonBudgetExhausted {
		t.Fatalf("Reason = %q, want %q", row.Reason, derivedTargetSkipReasonBudgetExhausted)
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
