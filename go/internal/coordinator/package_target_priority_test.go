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

func TestPackageRegistryWorkPlannerPrioritizesDirectOwnedBeforeBroadTargets(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 26, 18, 0, 0, 0, time.UTC)
	instance := packageRegistryPriorityInstance(observedAt, `{"targets":[
		{"provider":"npm","ecosystem":"npm","registry":"https://registry.npmjs.org","scope_id":"npm://registry.npmjs.org/_all","package_limit":100,"metadata_url":"https://registry.npmjs.org/-/all"},
		{"provider":"npm","ecosystem":"npm","registry":"https://registry.npmjs.org","scope_id":"npm://registry.npmjs.org/z-direct","packages":["z-direct"],"package_limit":1,"metadata_url":"https://registry.npmjs.org/z-direct"}
	],"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm"],"target_limit":1,"version_limit":50}}`)

	run, items, err := PackageRegistryWorkPlanner{}.PlanPackageRegistryWork(context.Background(), PackageRegistryPlanRequest{
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
	assertPackageRegistryTargetClasses(t, run.RequestedScopeSet, map[string]string{
		"npm://registry.npmjs.org/z-direct": packageRegistryTargetClassConfiguredDirect,
		"npm://registry.npmjs.org/m-owned":  packageRegistryTargetClassOwnedPackage,
		"npm://registry.npmjs.org/_all":     packageRegistryTargetClassBroad,
	})
}

func TestPackageRegistryWorkPlannerPreservesOwnedTargetReaderOrder(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 26, 18, 5, 0, 0, time.UTC)
	instance := packageRegistryPriorityInstance(observedAt, `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm"],"target_limit":3,"version_limit":50}}`)

	_, items, err := PackageRegistryWorkPlanner{}.PlanPackageRegistryWork(context.Background(), PackageRegistryPlanRequest{
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

func TestVulnerabilityIntelligenceWorkPlannerPreservesOwnedPriorityAcrossBatches(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 26, 18, 10, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "collector-vulnerability-intelligence",
		CollectorKind:  scope.CollectorVulnerabilityIntelligence,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"target_limit":101}}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
	owned := []workflow.OwnedPackageDependencyTarget{{
		Ecosystem:   "npm",
		PackageName: "zz-provider-alert-package",
		Version:     "1.0.0",
		Lockfile:    true,
	}}
	for i := 0; i < 100; i++ {
		owned = append(owned, workflow.OwnedPackageDependencyTarget{
			Ecosystem:   "npm",
			PackageName: fmt.Sprintf("aa-fanout-%03d", i),
			Version:     fmt.Sprintf("1.0.%d", i),
			Lockfile:    true,
		})
	}

	run, items, err := VulnerabilityIntelligenceWorkPlanner{}.PlanVulnerabilityIntelligenceWork(context.Background(), VulnerabilityIntelligencePlanRequest{
		Instance:            instance,
		ObservedAt:          observedAt,
		PlanKey:             "continuous-20260526T181000Z",
		OwnedPackageTargets: owned,
	})
	if err != nil {
		t.Fatalf("PlanVulnerabilityIntelligenceWork() error = %v", err)
	}
	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if !workItemsContainDerivedOSVQuery(items[:1], "zz-provider-alert-package", "1.0.0") {
		t.Fatalf("first derived OSV batch does not contain the priority package: %q", items[0].ScopeID)
	}
	assertWorkItemCreatedAtOrder(t, items)
	assertVulnerabilityTargetClass(t, run.RequestedScopeSet, items[0].ScopeID, vulnerabilityTargetClassOwnedPackage)
}

func TestTargetCreatedAtSpacingSurvivesPostgresTimestampPrecision(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 26, 18, 15, 0, 0, time.UTC)
	first := targetCreatedAt(observedAt, 0).Truncate(time.Microsecond)
	second := targetCreatedAt(observedAt, 1).Truncate(time.Microsecond)
	if !first.Before(second) {
		t.Fatalf("targetCreatedAt spacing collapses at Postgres precision: first=%s second=%s",
			first.Format(time.RFC3339Nano), second.Format(time.RFC3339Nano))
	}
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

func assertPackageRegistryTargetClasses(t *testing.T, requestedScopeSet string, want map[string]string) {
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

func assertVulnerabilityTargetClass(t *testing.T, requestedScopeSet string, scopeID string, want string) {
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
		if target.ScopeID == scopeID {
			if target.TargetClass != want {
				t.Fatalf("target_class for %q = %q, want %q", scopeID, target.TargetClass, want)
			}
			return
		}
	}
	t.Fatalf("RequestedScopeSet missing scope_id %q", scopeID)
}
