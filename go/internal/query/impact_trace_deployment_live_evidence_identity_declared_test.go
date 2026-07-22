// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestApiVersionVersionAppsGroup proves a namespaced apiVersion ("apps/v1")
// derives its version as the segment after the "/".
func TestApiVersionVersionAppsGroup(t *testing.T) {
	t.Parallel()

	if got, want := apiVersionVersion("apps/v1"), "v1"; got != want {
		t.Fatalf("apiVersionVersion(%q) = %q, want %q", "apps/v1", got, want)
	}
}

// TestApiVersionVersionCoreGroup proves a bare-version apiVersion ("v1") --
// the Kubernetes core group -- derives the whole string as the version, since
// there is no group segment to cut away.
func TestApiVersionVersionCoreGroup(t *testing.T) {
	t.Parallel()

	if got, want := apiVersionVersion("v1"), "v1"; got != want {
		t.Fatalf("apiVersionVersion(%q) = %q, want %q", "v1", got, want)
	}
}

// TestApiVersionVersionEmpty proves an absent/empty apiVersion derives an
// empty version.
func TestApiVersionVersionEmpty(t *testing.T) {
	t.Parallel()

	if got, want := apiVersionVersion(""), ""; got != want {
		t.Fatalf("apiVersionVersion(%q) = %q, want %q", "", got, want)
	}
}

// TestDeclaredObjectAnchorsMappableKindProducesAnchor is the positive case:
// a Deployment with a non-empty namespace and name produces exactly one
// declared-object anchor with the expected group_version_resource.
func TestDeclaredObjectAnchorsMappableKindProducesAnchor(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{
		k8sResourceFixture("Deployment", "deployable-source", "production", "apps/v1"),
	}
	anchors := declaredObjectAnchors(resources)
	if len(anchors) != 1 {
		t.Fatalf("declaredObjectAnchors() = %d anchors, want 1", len(anchors))
	}
	got := anchors[0]
	if got.Kind != liveIdentityAnchorDeclaredObject {
		t.Fatalf("anchor.Kind = %q, want %q", got.Kind, liveIdentityAnchorDeclaredObject)
	}
	if got.GroupVersionResource != "apps/v1/deployments" {
		t.Fatalf("anchor.GroupVersionResource = %q, want %q", got.GroupVersionResource, "apps/v1/deployments")
	}
	if got.Namespace != "production" {
		t.Fatalf("anchor.Namespace = %q, want %q", got.Namespace, "production")
	}
	if got.Name != "deployable-source" {
		t.Fatalf("anchor.Name = %q, want %q", got.Name, "deployable-source")
	}
}

// TestDeclaredObjectAnchorsCoreGroupKind proves a bare Pod (core API group,
// apiVersion "v1") produces the empty-group GVR shape ("/v1/pods"), matching
// the collector's own group_version_resource convention.
func TestDeclaredObjectAnchorsCoreGroupKind(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{
		k8sResourceFixture("Pod", "worker-pod", "default", "v1"),
	}
	anchors := declaredObjectAnchors(resources)
	if len(anchors) != 1 || anchors[0].GroupVersionResource != "/v1/pods" {
		t.Fatalf("declaredObjectAnchors() = %+v, want one anchor with GVR \"/v1/pods\"", anchors)
	}
}

// TestDeclaredObjectAnchorsEveryMappedKind proves every kind in the closed
// kind->resource map produces its expected resource plural.
func TestDeclaredObjectAnchorsEveryMappedKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind     string
		resource string
	}{
		{"Deployment", "deployments"},
		{"ReplicaSet", "replicasets"},
		{"StatefulSet", "statefulsets"},
		{"DaemonSet", "daemonsets"},
		{"CronJob", "cronjobs"},
		{"Job", "jobs"},
		{"Pod", "pods"},
	}
	for _, tc := range cases {
		resources := []map[string]any{
			k8sResourceFixture(tc.kind, "workload-a", "ns", "apps/v1"),
		}
		anchors := declaredObjectAnchors(resources)
		if len(anchors) != 1 {
			t.Fatalf("kind %q: declaredObjectAnchors() = %d anchors, want 1", tc.kind, len(anchors))
		}
		want := "apps/v1/" + tc.resource
		if anchors[0].GroupVersionResource != want {
			t.Fatalf("kind %q: GroupVersionResource = %q, want %q", tc.kind, anchors[0].GroupVersionResource, want)
		}
	}
}

// TestDeclaredObjectAnchorsUnmappableKindIsEmpty is the fail-closed proof: a
// kind outside the closed collector-listed family (e.g. ConfigMap) produces
// no anchor at all, even with a valid namespace and name.
func TestDeclaredObjectAnchorsUnmappableKindIsEmpty(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{
		k8sResourceFixture("ConfigMap", "workload-a", "production", "v1"),
	}
	anchors := declaredObjectAnchors(resources)
	if len(anchors) != 0 {
		t.Fatalf("declaredObjectAnchors() = %+v, want empty for an unmappable kind", anchors)
	}
}

// TestDeclaredObjectAnchorsEmptyNamespaceIsEmpty is the fail-closed proof: a
// mappable kind with an empty namespace (a cluster-scoped or unprojected
// declared object) never produces a declared-object anchor -- no
// cluster-scoped or wildcard match is ever allowed.
func TestDeclaredObjectAnchorsEmptyNamespaceIsEmpty(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{
		k8sResourceFixture("Deployment", "workload-a", "", "apps/v1"),
	}
	anchors := declaredObjectAnchors(resources)
	if len(anchors) != 0 {
		t.Fatalf("declaredObjectAnchors() = %+v, want empty for an empty namespace", anchors)
	}
}

// TestDeclaredObjectAnchorsEmptyNameIsEmpty proves an empty declared name
// also fails closed.
func TestDeclaredObjectAnchorsEmptyNameIsEmpty(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{
		k8sResourceFixture("Deployment", "", "production", "apps/v1"),
	}
	anchors := declaredObjectAnchors(resources)
	if len(anchors) != 0 {
		t.Fatalf("declaredObjectAnchors() = %+v, want empty for an empty name", anchors)
	}
}

// TestDeclaredObjectAnchorsDeduplicates proves two identical declared
// resources (same kind+namespace+name+api_version) collapse to one anchor.
func TestDeclaredObjectAnchorsDeduplicates(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{
		k8sResourceFixture("Deployment", "workload-a", "ns", "apps/v1"),
		k8sResourceFixture("Deployment", "workload-a", "ns", "apps/v1"),
	}
	anchors := declaredObjectAnchors(resources)
	if len(anchors) != 1 {
		t.Fatalf("declaredObjectAnchors() = %d anchors, want 1 (deduplicated)", len(anchors))
	}
}

// TestDeclaredObjectAnchorsDistinctWorkloadsProduceDistinctAnchors is the
// #5471-style identity proof at the declared-object anchor layer: two
// workloads with different names in the same namespace and kind never
// produce overlapping anchors.
func TestDeclaredObjectAnchorsDistinctWorkloadsProduceDistinctAnchors(t *testing.T) {
	t.Parallel()

	resourcesA := []map[string]any{k8sResourceFixture("Deployment", "workload-a", "shared-ns", "apps/v1")}
	resourcesB := []map[string]any{k8sResourceFixture("Deployment", "workload-b", "shared-ns", "apps/v1")}

	anchorsA := declaredObjectAnchors(resourcesA)
	anchorsB := declaredObjectAnchors(resourcesB)
	if len(anchorsA) != 1 || len(anchorsB) != 1 {
		t.Fatalf("anchorsA = %+v, anchorsB = %+v, want exactly one each", anchorsA, anchorsB)
	}
	if anchorsA[0].Name == anchorsB[0].Name {
		t.Fatalf("distinct workloads produced the SAME declared-object anchor name %q", anchorsA[0].Name)
	}
}

// TestDeclaredObjectAnchorsNamespaceDistinguishesIdentity proves the
// namespace field participates in the anchor identity: same kind+name,
// different namespace, must never collapse to the same anchor.
func TestDeclaredObjectAnchorsNamespaceDistinguishesIdentity(t *testing.T) {
	t.Parallel()

	resourcesProd := []map[string]any{k8sResourceFixture("Deployment", "workload-a", "production", "apps/v1")}
	resourcesStaging := []map[string]any{k8sResourceFixture("Deployment", "workload-a", "staging", "apps/v1")}

	anchorsProd := declaredObjectAnchors(resourcesProd)
	anchorsStaging := declaredObjectAnchors(resourcesStaging)
	if len(anchorsProd) != 1 || len(anchorsStaging) != 1 {
		t.Fatalf("anchorsProd = %+v, anchorsStaging = %+v, want exactly one each", anchorsProd, anchorsStaging)
	}
	if anchorsProd[0].Namespace == anchorsStaging[0].Namespace {
		t.Fatal("distinct namespaces produced the same anchor namespace field")
	}
}

// TestResolveLiveIdentityAnchorsOrdersArgoCDFirst proves the combined
// resolver puts every ArgoCD tracking-id anchor before any declared-object
// anchor -- ArgoCD identity is the stronger anchor and must be tried first.
func TestResolveLiveIdentityAnchorsOrdersArgoCDFirst(t *testing.T) {
	t.Parallel()

	controllers := []map[string]any{argoCDControllerFixture("app-a")}
	resources := []map[string]any{k8sResourceFixture("Deployment", "workload-a", "ns", "apps/v1")}

	anchors := resolveLiveIdentityAnchors(controllers, resources)
	if len(anchors) != 2 {
		t.Fatalf("resolveLiveIdentityAnchors() = %d anchors, want 2 (one ArgoCD, one declared-object)", len(anchors))
	}
	if anchors[0].Kind != liveIdentityAnchorArgoCDTrackingID {
		t.Fatalf("anchors[0].Kind = %q, want ArgoCD tracking-id first", anchors[0].Kind)
	}
	if anchors[1].Kind != liveIdentityAnchorDeclaredObject {
		t.Fatalf("anchors[1].Kind = %q, want declared-object last", anchors[1].Kind)
	}
}

// TestResolveLiveIdentityAnchorsDeclaredOnlyWhenNoArgoCD proves a workload
// with no ArgoCD Application controller but a mappable declared k8sResource
// still resolves a (weaker) declared-object anchor -- this is the core #5639
// widening.
func TestResolveLiveIdentityAnchorsDeclaredOnlyWhenNoArgoCD(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{k8sResourceFixture("Deployment", "workload-a", "ns", "apps/v1")}
	anchors := resolveLiveIdentityAnchors(nil, resources)
	if len(anchors) != 1 {
		t.Fatalf("resolveLiveIdentityAnchors() = %d anchors, want 1 (declared-object only)", len(anchors))
	}
	if anchors[0].Kind != liveIdentityAnchorDeclaredObject {
		t.Fatalf("anchors[0].Kind = %q, want declared-object", anchors[0].Kind)
	}
}

// TestResolveLiveIdentityAnchorsEmptyWhenNeitherPresent proves the
// fail-closed contract: no ArgoCD controller and no mappable declared
// resource yields an empty anchor list.
func TestResolveLiveIdentityAnchorsEmptyWhenNeitherPresent(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{k8sResourceFixture("ConfigMap", "workload-a", "ns", "v1")}
	anchors := resolveLiveIdentityAnchors(nil, resources)
	if len(anchors) != 0 {
		t.Fatalf("resolveLiveIdentityAnchors() = %+v, want empty", anchors)
	}
}
