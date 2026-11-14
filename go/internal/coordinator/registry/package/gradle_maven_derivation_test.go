// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packages

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestPackageRegistryWorkPlannerDerivesMavenTargetFromGradleOnlyCoordinate
// (#9314) proves a Gradle-declared Maven coordinate derives one Maven
// package-registry target. Today the planner matches the raw ecosystem, so a
// Gradle-only row derives nothing.
func TestPackageRegistryWorkPlannerDerivesMavenTargetFromGradleOnlyCoordinate(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.October, 29, 12, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "collector-package-registry",
		CollectorKind:  scope.CollectorPackageRegistry,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["maven"],"target_limit":10,"package_limit":1,"version_limit":3}}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	_, items, err := WorkPlanner{}.PlanPackageRegistryWork(context.Background(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20261029T120000Z",
		OwnedPackageTargets: []workflow.OwnedPackageDependencyTarget{
			{
				Ecosystem:    "gradle",
				PackageName:  "org.example:gradle-only",
				Version:      "1.0.0",
				RepositoryID: "repo-eshu",
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanPackageRegistryWork() error = %v", err)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d (gradle-only coordinate derives one maven target)", got, want)
	}
	if got, want := items[0].ScopeID, "maven://repo.maven.apache.org/maven2/org.example:gradle-only"; got != want {
		t.Fatalf("items[0].ScopeID = %q, want %q", got, want)
	}
}

// TestPackageRegistryWorkPlannerDerivesOneTargetForMavenAndGradlePair
// (#9314) proves a coordinate declared by both Maven and Gradle manifests
// derives exactly one target, keeping today's order.
func TestPackageRegistryWorkPlannerDerivesOneTargetForMavenAndGradlePair(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.October, 29, 12, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "collector-package-registry",
		CollectorKind:  scope.CollectorPackageRegistry,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["maven"],"target_limit":10,"package_limit":1,"version_limit":3}}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	_, items, err := WorkPlanner{}.PlanPackageRegistryWork(context.Background(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20261029T120000Z",
		OwnedPackageTargets: []workflow.OwnedPackageDependencyTarget{
			{
				Ecosystem:    "maven",
				PackageName:  "org.example:shared",
				Version:      "2.0.0",
				RepositoryID: "repo-eshu",
			},
			{
				Ecosystem:    "gradle",
				PackageName:  "org.example:shared",
				Version:      "2.0.0",
				RepositoryID: "repo-eshu",
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanPackageRegistryWork() error = %v", err)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d (maven+gradle pair derives one target)", got, want)
	}
	if got, want := items[0].ScopeID, "maven://repo.maven.apache.org/maven2/org.example:shared"; got != want {
		t.Fatalf("items[0].ScopeID = %q, want %q", got, want)
	}
}

// The derived Maven target's acceptance by the metadata parser moved to the
// extracted satellite with #10620: the satellite owns its parser registry
// and proves the routing there, so this package carries no parser case. The
// planner still proves the derived target shape (registry, scope ID) above.
