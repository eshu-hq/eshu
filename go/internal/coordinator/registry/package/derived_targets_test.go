// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packages

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestPackageRegistryWorkPlannerDerivesNPMTargetsFromOwnedPackageEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 23, 21, 30, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "collector-package-registry",
		CollectorKind:  scope.CollectorPackageRegistry,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm"],"target_limit":10,"version_limit":50}}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := WorkPlanner{}.PlanPackageRegistryWork(context.Background(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260523T213000Z",
		OwnedPackageTargets: []workflow.OwnedPackageDependencyTarget{
			{
				Ecosystem:    "npm",
				PackageName:  "vite",
				Version:      "^5.4.11",
				RepositoryID: "repo-eshu",
			},
			{
				Ecosystem:    "npm",
				PackageName:  "@scope/widget",
				Version:      "1.2.3",
				Lockfile:     true,
				RepositoryID: "repo-eshu",
			},
			{
				Ecosystem:    "composer",
				PackageName:  "symfony/console",
				Version:      "7.0.0",
				Lockfile:     true,
				RepositoryID: "repo-eshu",
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanPackageRegistryWork() error = %v", err)
	}
	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if got, want := items[0].ScopeID, "npm://registry.npmjs.org/vite"; got != want {
		t.Fatalf("items[0].ScopeID = %q, want %q", got, want)
	}
	if got, want := items[1].ScopeID, "npm://registry.npmjs.org/@scope/widget"; got != want {
		t.Fatalf("items[1].ScopeID = %q, want %q", got, want)
	}

	var requested struct {
		Targets []struct {
			ScopeID string  `json:"scope_id"`
			Derived bool    `json:"derived"`
			Version *string `json:"version"`
		} `json:"targets"`
	}
	if err := json.Unmarshal([]byte(run.RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", run.RequestedScopeSet, err)
	}
	if got, want := len(requested.Targets), 2; got != want {
		t.Fatalf("len(RequestedScopeSet.targets) = %d, want %d", got, want)
	}
	for _, target := range requested.Targets {
		if !target.Derived {
			t.Fatalf("requested target %#v is not marked derived", target)
		}
		if target.Version != nil {
			t.Fatalf("requested package-registry target leaked version metadata: %#v", target)
		}
	}
}

func TestPackageRegistryWorkPlannerHonorsFullCorpusDerivedTargetLimit(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 26, 13, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "collector-package-registry",
		CollectorKind:  scope.CollectorPackageRegistry,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm"],"target_limit":125,"version_limit":50}}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
	owned := make([]workflow.OwnedPackageDependencyTarget, 0, 125)
	for i := 0; i < 124; i++ {
		owned = append(owned, workflow.OwnedPackageDependencyTarget{
			Ecosystem:    "npm",
			PackageName:  fmt.Sprintf("pkg-%03d", i),
			Version:      "1.0.0",
			Lockfile:     true,
			RepositoryID: "repo-large",
		})
	}
	owned = append(owned, workflow.OwnedPackageDependencyTarget{
		Ecosystem:    "npm",
		PackageName:  "zz-after-one-hundred",
		Version:      "1.0.0",
		Lockfile:     true,
		RepositoryID: "repo-large",
	})

	_, items, err := WorkPlanner{}.PlanPackageRegistryWork(context.Background(), PlanRequest{
		Instance:            instance,
		ObservedAt:          observedAt,
		PlanKey:             "continuous-20260526T130000Z",
		OwnedPackageTargets: owned,
	})
	if err != nil {
		t.Fatalf("PlanPackageRegistryWork() error = %v, want nil", err)
	}
	if got, want := len(items), 125; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if !workItemsContainScope(items, "npm://registry.npmjs.org/zz-after-one-hundred") {
		t.Fatalf("derived package-registry targets did not include package after the historical 100-target cap")
	}
}

func TestPackageRegistryDerivedTargetUsesNormalizedMetadataURL(t *testing.T) {
	t.Parallel()

	target, ok := npmPackageRegistryTarget(
		workflow.OwnedPackageDependencyTarget{
			Ecosystem:   "npm",
			PackageName: "Vite",
		},
		1,
		200,
	)
	if !ok {
		t.Fatal("npmPackageRegistryTarget() ok = false, want true")
	}
	if got, want := target.ScopeID, "npm://registry.npmjs.org/vite"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := target.SourceURI, "https://registry.npmjs.org/vite"; got != want {
		t.Fatalf("SourceURI = %q, want normalized metadata URL %q", got, want)
	}
	if got, want := target.MetadataURL, "https://registry.npmjs.org/vite"; got != want {
		t.Fatalf("MetadataURL = %q, want normalized metadata URL %q", got, want)
	}
}

func workItemsContainScope(items []workflow.WorkItem, scopeID string) bool {
	for _, item := range items {
		if item.ScopeID == scopeID {
			return true
		}
	}
	return false
}
