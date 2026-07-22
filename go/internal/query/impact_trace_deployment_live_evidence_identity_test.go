// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestApiVersionGroupAppsGroup proves a namespaced apiVersion ("apps/v1")
// derives its API group as the segment before the "/", matching Kubernetes
// API-group conventions.
func TestApiVersionGroupAppsGroup(t *testing.T) {
	t.Parallel()

	if got, want := apiVersionGroup("apps/v1"), "apps"; got != want {
		t.Fatalf("apiVersionGroup(%q) = %q, want %q", "apps/v1", got, want)
	}
}

// TestApiVersionGroupCoreGroup proves a bare-version apiVersion ("v1") --
// the Kubernetes core group -- derives an empty group string, not "v1"
// itself. This is the case ArgoCD's own tracking-id format leaves as an
// empty segment (":/Kind:...").
func TestApiVersionGroupCoreGroup(t *testing.T) {
	t.Parallel()

	if got, want := apiVersionGroup("v1"), ""; got != want {
		t.Fatalf("apiVersionGroup(%q) = %q, want %q", "v1", got, want)
	}
}

// TestApiVersionGroupEmpty proves an absent/empty apiVersion also derives an
// empty group, the fail-safe default for a resource whose api_version was
// never projected (see collectDeploymentSourceK8sResources, k8sResourceWireRow).
func TestApiVersionGroupEmpty(t *testing.T) {
	t.Parallel()

	if got, want := apiVersionGroup(""), ""; got != want {
		t.Fatalf("apiVersionGroup(%q) = %q, want %q", "", got, want)
	}
}

// TestBuildArgoCDTrackingIDAppsGroup proves the tracking-id format for a
// namespaced, apps-group resource (e.g. Deployment) matches ArgoCD's
// BuildAppInstanceValue convention: "<app>:<group>/<kind>:<namespace>/<name>".
func TestBuildArgoCDTrackingIDAppsGroup(t *testing.T) {
	t.Parallel()

	got := buildArgoCDTrackingID("deployable-source", "apps", "Deployment", "production", "deployable-source")
	want := "deployable-source:apps/Deployment:production/deployable-source"
	if got != want {
		t.Fatalf("buildArgoCDTrackingID() = %q, want %q", got, want)
	}
}

// TestBuildArgoCDTrackingIDCoreGroup proves the core-API-group case: an
// empty group leaves an empty segment before the "/", per ArgoCD's own
// format string (util/argo/resource_tracking.go BuildAppInstanceValue),
// which does not special-case an empty Group field.
func TestBuildArgoCDTrackingIDCoreGroup(t *testing.T) {
	t.Parallel()

	got := buildArgoCDTrackingID("deployable-source", "", "Service", "production", "deployable-source")
	want := "deployable-source:/Service:production/deployable-source"
	if got != want {
		t.Fatalf("buildArgoCDTrackingID() = %q, want %q", got, want)
	}
}

// TestArgoCDApplicationNamesFiltersToApplicationControllerKind proves only
// controller_kind == "argocd_application" controllers contribute an app
// name: an ApplicationSet or Flux controller carries no ArgoCD
// annotation-based tracking-id and must be excluded.
func TestArgoCDApplicationNamesFiltersToApplicationControllerKind(t *testing.T) {
	t.Parallel()

	controllers := []map[string]any{
		{"controller_kind": "argocd_application", "entity_name": "deployable-source"},
		{"controller_kind": "argocd_applicationset", "entity_name": "not-an-app"},
		{"controller_kind": "flux_kustomization", "entity_name": "also-not-an-app"},
		{"controller_kind": "argocd_application", "entity_name": "deployable-source"}, // duplicate
	}
	names := argoCDApplicationNames(controllers)
	if len(names) != 1 || names[0] != "deployable-source" {
		t.Fatalf("argoCDApplicationNames() = %v, want [deployable-source]", names)
	}
}

// TestExpectedArgoCDTrackingIDsAppsGroupResource is the end-to-end positive
// case: one ArgoCD Application controller plus one apps-group (Deployment)
// declared resource produces exactly the expected tracking-id.
func TestExpectedArgoCDTrackingIDsAppsGroupResource(t *testing.T) {
	t.Parallel()

	controllers := []map[string]any{
		{"controller_kind": "argocd_application", "entity_name": "deployable-source"},
	}
	k8sResources := []map[string]any{
		{
			"kind":        "Deployment",
			"entity_name": "deployable-source",
			"namespace":   "production",
			"api_version": "apps/v1",
		},
	}
	ids := expectedArgoCDTrackingIDs(controllers, k8sResources)
	if len(ids) != 1 || ids[0] != "deployable-source:apps/Deployment:production/deployable-source" {
		t.Fatalf("expectedArgoCDTrackingIDs() = %v, want [deployable-source:apps/Deployment:production/deployable-source]", ids)
	}
}

// TestExpectedArgoCDTrackingIDsCoreGroupResource is the end-to-end
// core-API-group case: a bare "v1" apiVersion (e.g. Service) must derive
// the empty-group tracking-id shape, not silently drop the resource or
// treat "v1" itself as the group.
func TestExpectedArgoCDTrackingIDsCoreGroupResource(t *testing.T) {
	t.Parallel()

	controllers := []map[string]any{
		{"controller_kind": "argocd_application", "entity_name": "deployable-source"},
	}
	k8sResources := []map[string]any{
		{
			"kind":        "Service",
			"entity_name": "deployable-source",
			"namespace":   "production",
			"api_version": "v1",
		},
	}
	ids := expectedArgoCDTrackingIDs(controllers, k8sResources)
	if len(ids) != 1 || ids[0] != "deployable-source:/Service:production/deployable-source" {
		t.Fatalf("expectedArgoCDTrackingIDs() = %v, want [deployable-source:/Service:production/deployable-source]", ids)
	}
}

// TestExpectedArgoCDTrackingIDsNoArgoCDControllerIsEmpty proves the
// fail-closed contract: no argocd_application controller (e.g. only Flux)
// means no ArgoCD identity is computable, so the set must be empty even
// though declared k8sResources exist.
func TestExpectedArgoCDTrackingIDsNoArgoCDControllerIsEmpty(t *testing.T) {
	t.Parallel()

	controllers := []map[string]any{
		{"controller_kind": "flux_kustomization", "entity_name": "some-kustomization"},
	}
	k8sResources := []map[string]any{
		{"kind": "Deployment", "entity_name": "deployable-source", "namespace": "production", "api_version": "apps/v1"},
	}
	ids := expectedArgoCDTrackingIDs(controllers, k8sResources)
	if len(ids) != 0 {
		t.Fatalf("expectedArgoCDTrackingIDs() = %v, want empty", ids)
	}
}

// TestExpectedArgoCDTrackingIDsNoControllersIsEmpty proves an empty
// controllers slice (no GitOps controller at all) yields an empty set.
func TestExpectedArgoCDTrackingIDsNoControllersIsEmpty(t *testing.T) {
	t.Parallel()

	k8sResources := []map[string]any{
		{"kind": "Deployment", "entity_name": "deployable-source", "namespace": "production", "api_version": "apps/v1"},
	}
	ids := expectedArgoCDTrackingIDs(nil, k8sResources)
	if len(ids) != 0 {
		t.Fatalf("expectedArgoCDTrackingIDs() = %v, want empty", ids)
	}
}

// TestExpectedArgoCDTrackingIDsNoResourcesIsEmpty proves an empty
// k8sResources slice yields an empty set even with a valid ArgoCD
// Application controller present.
func TestExpectedArgoCDTrackingIDsNoResourcesIsEmpty(t *testing.T) {
	t.Parallel()

	controllers := []map[string]any{
		{"controller_kind": "argocd_application", "entity_name": "deployable-source"},
	}
	ids := expectedArgoCDTrackingIDs(controllers, nil)
	if len(ids) != 0 {
		t.Fatalf("expectedArgoCDTrackingIDs() = %v, want empty", ids)
	}
}

// TestExpectedArgoCDTrackingIDsDistinctWorkloadsProduceDistinctIDs is the
// #5471 codex P1 regression proof at the identity-computation layer: two
// workloads sharing a namespace and kind but with DIFFERENT declared names
// and DIFFERENT ArgoCD Application names must never produce overlapping
// tracking-id sets, even though a shared image digest would have let the
// pre-fix probe conflate them.
func TestExpectedArgoCDTrackingIDsDistinctWorkloadsProduceDistinctIDs(t *testing.T) {
	t.Parallel()

	controllersA := []map[string]any{{"controller_kind": "argocd_application", "entity_name": "app-a"}}
	resourcesA := []map[string]any{
		{"kind": "Deployment", "entity_name": "workload-a", "namespace": "shared-ns", "api_version": "apps/v1"},
	}
	controllersB := []map[string]any{{"controller_kind": "argocd_application", "entity_name": "app-b"}}
	resourcesB := []map[string]any{
		{"kind": "Deployment", "entity_name": "workload-b", "namespace": "shared-ns", "api_version": "apps/v1"},
	}

	idsA := expectedArgoCDTrackingIDs(controllersA, resourcesA)
	idsB := expectedArgoCDTrackingIDs(controllersB, resourcesB)
	if len(idsA) != 1 || len(idsB) != 1 {
		t.Fatalf("idsA = %v, idsB = %v, want exactly one id each", idsA, idsB)
	}
	if idsA[0] == idsB[0] {
		t.Fatalf("distinct workloads produced the SAME tracking id %q -- identity binding collapsed", idsA[0])
	}
}
