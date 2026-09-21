// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packages

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestPackageRegistryWorkPlannerPrioritizesDirectOwnedBeforeBroadTargets(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 26, 18, 0, 0, 0, time.UTC)
	instance := packageRegistryPriorityInstance(observedAt, `{"targets":[
		{"provider":"npm","ecosystem":"npm","registry":"https://registry.npmjs.org","scope_id":"npm://registry.npmjs.org/_all","package_limit":100,"metadata_url":"https://registry.npmjs.org/-/all"},
		{"provider":"npm","ecosystem":"npm","registry":"https://registry.npmjs.org","scope_id":"npm://registry.npmjs.org/z-direct","packages":["z-direct"],"package_limit":1,"metadata_url":"https://registry.npmjs.org/z-direct"}
	],"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm"],"target_limit":1,"version_limit":50}}`)

	run, items, err := WorkPlanner{}.PlanPackageRegistryWork(context.Background(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260526T180000Z",
		OwnedPackageTargets: []workflow.OwnedPackageDependencyTarget{{
			Ecosystem:   "npm",
			PackageName: "m-owned",
			Version:     "1.0.0",
			Lockfile:    true,
		}},
	})
	if err != nil {
		t.Fatalf("PlanPackageRegistryWork() error = %v", err)
	}
	wantScopes := []string{
		"npm://registry.npmjs.org/z-direct",
		"npm://registry.npmjs.org/m-owned",
		"npm://registry.npmjs.org/_all",
	}
	assertWorkItemScopeOrder(t, items, wantScopes)
	assertWorkItemCreatedAtOrder(t, items)
	assertTargetClasses(t, run.RequestedScopeSet, map[string]string{
		"npm://registry.npmjs.org/z-direct": targetClassConfiguredDirect,
		"npm://registry.npmjs.org/m-owned":  targetClassOwnedPackage,
		"npm://registry.npmjs.org/_all":     targetClassBroad,
	})
}

func TestPackageRegistryWorkPlannerPreservesOwnedTargetReaderOrder(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 26, 18, 5, 0, 0, time.UTC)
	instance := packageRegistryPriorityInstance(observedAt, `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm"],"target_limit":3,"version_limit":50}}`)

	_, items, err := WorkPlanner{}.PlanPackageRegistryWork(context.Background(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260526T180500Z",
		OwnedPackageTargets: []workflow.OwnedPackageDependencyTarget{
			{Ecosystem: "npm", PackageName: "z-rotated-first", Version: "1.0.0", Lockfile: true},
			{Ecosystem: "npm", PackageName: "a-lexical-second", Version: "1.0.0", Lockfile: true},
			{Ecosystem: "npm", PackageName: "m-lexical-third", Version: "1.0.0", Lockfile: true},
		},
	})
	if err != nil {
		t.Fatalf("PlanPackageRegistryWork() error = %v", err)
	}
	assertWorkItemScopeOrder(t, items, []string{
		"npm://registry.npmjs.org/z-rotated-first",
		"npm://registry.npmjs.org/a-lexical-second",
		"npm://registry.npmjs.org/m-lexical-third",
	})
	assertWorkItemCreatedAtOrder(t, items)
}

func packageRegistryPriorityInstance(observedAt time.Time, configuration string) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "collector-package-registry",
		CollectorKind:  scope.CollectorPackageRegistry,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  configuration,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}

func assertWorkItemScopeOrder(t *testing.T, items []workflow.WorkItem, want []string) {
	t.Helper()
	if got, wantLen := len(items), len(want); got != wantLen {
		t.Fatalf("len(items) = %d, want %d", got, wantLen)
	}
	for i, wantScope := range want {
		if got := items[i].ScopeID; got != wantScope {
			t.Fatalf("items[%d].ScopeID = %q, want %q", i, got, wantScope)
		}
	}
}

func assertWorkItemCreatedAtOrder(t *testing.T, items []workflow.WorkItem) {
	t.Helper()
	for i := 1; i < len(items); i++ {
		if !items[i-1].CreatedAt.Before(items[i].CreatedAt) {
			t.Fatalf("CreatedAt order did not preserve target priority: item[%d]=%s item[%d]=%s",
				i-1, items[i-1].CreatedAt.Format(time.RFC3339Nano), i, items[i].CreatedAt.Format(time.RFC3339Nano))
		}
	}
}

func assertTargetClasses(t *testing.T, requestedScopeSet string, want map[string]string) {
	t.Helper()
	var requested struct {
		Targets []struct {
			ScopeID     string `json:"scope_id"`
			TargetClass string `json:"target_class"`
		} `json:"targets"`
	}
	if err := json.Unmarshal([]byte(requestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", requestedScopeSet, err)
	}
	for _, target := range requested.Targets {
		if wantClass, ok := want[target.ScopeID]; ok && target.TargetClass != wantClass {
			t.Fatalf("target_class for %q = %q, want %q", target.ScopeID, target.TargetClass, wantClass)
		}
		delete(want, target.ScopeID)
	}
	if len(want) > 0 {
		t.Fatalf("RequestedScopeSet missing target classes for %#v", want)
	}
}
