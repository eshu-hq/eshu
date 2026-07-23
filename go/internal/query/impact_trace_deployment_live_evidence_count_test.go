// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"testing"
)

// stubKubernetesPodTemplateListStore is a test fake implementing
// KubernetesPodTemplateStore for fetchWorkloadLiveInstanceSummary's tests: it
// records every filter ListLiveIdentityMatches was called with and returns
// canned matches keyed by TrackingID for ArgoCD tracking-id anchor filters,
// or by declaredObjectMatchKey (GroupVersionResource|Namespace|Name) for
// declared-object anchor filters (#5639) -- the two anchor kinds never share
// a keyspace, mirroring how the real store dispatches on
// filter.AnchorKind. HasLiveIdentityMatch is never called by the probe under
// test here; it panics if reached so a wiring mistake fails loudly instead of
// silently returning a wrong bool.
type stubKubernetesPodTemplateListStore struct {
	matchesByTrackingID        map[string][]LiveIdentityMatch
	matchesByDeclaredObjectKey map[string][]LiveIdentityMatch
	err                        error
	calls                      []KubernetesPodTemplateFilter
}

// declaredObjectMatchKey builds the stub's lookup key for a declared-object
// anchor filter, matching declaredObjectAnchors' own key shape
// (impact_trace_deployment_live_evidence_identity_declared.go:84).
func declaredObjectMatchKey(gvr, namespace, name string) string {
	return gvr + "|" + namespace + "|" + name
}

func (s *stubKubernetesPodTemplateListStore) HasLiveIdentityMatch(
	context.Context,
	KubernetesPodTemplateFilter,
) (bool, error) {
	panic("HasLiveIdentityMatch must not be called by fetchWorkloadLiveInstanceSummary")
}

func (s *stubKubernetesPodTemplateListStore) ListLiveIdentityMatches(
	_ context.Context,
	filter KubernetesPodTemplateFilter,
) ([]LiveIdentityMatch, error) {
	s.calls = append(s.calls, filter)
	if s.err != nil {
		return nil, s.err
	}
	if filter.AnchorKind == liveIdentityAnchorDeclaredObject {
		key := declaredObjectMatchKey(filter.GroupVersionResource, filter.Namespace, filter.Name)
		return s.matchesByDeclaredObjectKey[key], nil
	}
	return s.matchesByTrackingID[filter.TrackingID], nil
}

// singleTrackingIDFixture builds the controllers/k8sResources pair that
// expectedArgoCDTrackingIDs resolves to exactly one tracking-id, matching the
// argoCDControllerFixture/k8sResourceFixture helpers
// (impact_trace_deployment_live_evidence_test.go).
func singleTrackingIDFixture(appName, kind, name, namespace, apiVersion string) (
	[]map[string]any, []map[string]any, string,
) {
	controllers := []map[string]any{argoCDControllerFixture(appName)}
	resources := []map[string]any{k8sResourceFixture(kind, name, namespace, apiVersion)}
	trackingIDs := expectedArgoCDTrackingIDs(controllers, resources)
	if len(trackingIDs) != 1 {
		panic(fmt.Sprintf("test fixture bug: want exactly 1 tracking id, got %d", len(trackingIDs)))
	}
	return controllers, resources, trackingIDs[0]
}

func int32Ptr(v int32) *int32 { return &v }

func TestFetchWorkloadLiveInstanceSummaryNilHandler(t *testing.T) {
	t.Parallel()

	var h *ImpactHandler
	summary, err := h.fetchWorkloadLiveInstanceSummary(t.Context(), nil, nil, nil, repositoryAccessFilter{})
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary != nil {
		t.Fatal("nil handler returned a non-nil summary, want nil")
	}
}

// TestFetchWorkloadLiveInstanceSummaryNoAnchorOfAnyKindNeverQueriesStore
// proves the same fail-closed anchor-first discipline as
// TestFetchWorkloadLiveEvidenceNoAnchorOfAnyKindNeverQueriesStore, widened
// for #5639: no resolvable ArgoCD identity AND no mappable declared-object
// anchor (ConfigMap is outside the closed kind map) means the store is never
// queried and the count is absent. Renamed from
// TestFetchWorkloadLiveInstanceSummaryNoIdentityNeverQueriesStore: its
// meaning changed from "no ArgoCD controller" to "no anchor of ANY kind".
func TestFetchWorkloadLiveInstanceSummaryNoAnchorOfAnyKindNeverQueriesStore(t *testing.T) {
	t.Parallel()

	store := &stubKubernetesPodTemplateListStore{}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(),
		nil, // no controllers at all
		[]map[string]any{k8sResourceFixture("ConfigMap", "workload-a", "shared-ns", "v1")},
		[]string{"ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"},
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary != nil {
		t.Fatal("no anchor of any kind returned a non-nil summary, want nil")
	}
	if got := len(store.calls); got != 0 {
		t.Fatalf("store was queried %d times, want 0 (fail-closed at the identity layer)", got)
	}
}

func TestFetchWorkloadLiveInstanceSummaryNilStore(t *testing.T) {
	t.Parallel()

	controllers, resources, _ := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	h := &ImpactHandler{} // KubernetesPodTemplates is nil
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, []string{"img:latest"}, repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary != nil {
		t.Fatal("nil store returned a non-nil summary, want nil")
	}
}

func TestFetchWorkloadLiveInstanceSummaryEmptyImageRefs(t *testing.T) {
	t.Parallel()

	controllers, resources, _ := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, nil, repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary != nil {
		t.Fatal("empty image_refs returned a non-nil summary, want nil")
	}
	if got := len(store.calls); got != 0 {
		t.Fatalf("store was queried %d times, want 0", got)
	}
}

func TestFetchWorkloadLiveInstanceSummaryEmptyAccess(t *testing.T) {
	t.Parallel()

	controllers, resources, trackingID := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingID: {{ReadyReplicas: int32Ptr(3)}},
		},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, []string{"img@sha256:a"}, repositoryAccessFilter{}, // scoped, no grants
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary != nil {
		t.Fatal("empty access filter returned a non-nil summary, want nil")
	}
	if got := len(store.calls); got != 0 {
		t.Fatalf("store was queried %d times, want 0", got)
	}
}

// TestFetchWorkloadLiveInstanceSummaryMaxNotSum is the anti-double-count RED
// test: a Deployment and its ReplicaSet share ONE tracking-id (the
// Deployment copies its annotations onto the ReplicaSet it owns) and both
// report ready_replicas=3. The correct aggregation is MAX (one running
// Deployment observed twice, from two different object kinds) yielding 3;
// summing the two matched facts would wrongly double-count to 6.
func TestFetchWorkloadLiveInstanceSummaryMaxNotSum(t *testing.T) {
	t.Parallel()

	controllers, resources, trackingID := singleTrackingIDFixture("deployable-source", "Deployment", "deployable-source", "production", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingID: {
				{ClusterID: "prod-cluster", ReadyReplicas: int32Ptr(3)}, // Deployment
				{ClusterID: "prod-cluster", ReadyReplicas: int32Ptr(3)}, // ReplicaSet, same tracking-id, same cluster
			},
		},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources,
		[]string{"ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"},
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary == nil {
		t.Fatal("summary = nil, want non-nil (an observation was made)")
	}
	if summary.count != 3 {
		t.Fatalf("count = %d, want 3 (MAX across same-tracking-id, same-cluster matches, not SUM which would give 6)", summary.count)
	}
}

// TestFetchWorkloadLiveInstanceSummaryMultiClusterSumsAcrossClusters is the
// codex #5661 P1 regression: one ArgoCD Application deployed to two clusters
// shares ONE tracking-id, but each cluster is a separate running deployment.
// The count MAXes within each cluster (dedup the Deployment/ReplicaSet copies)
// then SUMs across clusters -- clusters at 3 and 5 ready replicas make 8, never
// 5 (which a cross-cluster MAX would wrongly report).
func TestFetchWorkloadLiveInstanceSummaryMultiClusterSumsAcrossClusters(t *testing.T) {
	t.Parallel()

	controllers, resources, trackingID := singleTrackingIDFixture("deployable-source", "Deployment", "deployable-source", "production", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingID: {
				{ClusterID: "cluster-a", ReadyReplicas: int32Ptr(3)}, // cluster A: Deployment
				{ClusterID: "cluster-a", ReadyReplicas: int32Ptr(3)}, // cluster A: ReplicaSet copy (same tracking-id)
				{ClusterID: "cluster-b", ReadyReplicas: int32Ptr(5)}, // cluster B: a separate running deployment
			},
		},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources,
		[]string{"ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"},
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary == nil || summary.count != 8 {
		t.Fatalf("summary = %v, want count 8 (MAX 3 in cluster-a + 5 in cluster-b; a cross-cluster MAX gives 5)", summary)
	}
}

// TestFetchWorkloadLiveInstanceSummaryTwoTrackingIDsSum proves the second
// half of the aggregation contract: two DISTINCT tracking-ids (two different
// declared k8sResources under the traced workload) each contribute their own
// max, and the totals are summed across tracking-ids. The two matches carry
// distinct ObjectIDs, as two genuinely different live objects always would in
// production (object_id is never empty on a real fact) -- this keeps the
// cross-anchor cluster_id+object_id dedup (#5639 P1 fix) from mistaking two
// distinct objects for a re-hit of the same one.
func TestFetchWorkloadLiveInstanceSummaryTwoTrackingIDsSum(t *testing.T) {
	t.Parallel()

	controllers := []map[string]any{argoCDControllerFixture("app-a")}
	resources := []map[string]any{
		k8sResourceFixture("Deployment", "workload-a", "ns", "apps/v1"),
		k8sResourceFixture("Deployment", "workload-b", "ns", "apps/v1"),
	}
	trackingIDs := expectedArgoCDTrackingIDs(controllers, resources)
	if len(trackingIDs) != 2 {
		t.Fatalf("test fixture bug: want 2 tracking ids, got %d", len(trackingIDs))
	}
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingIDs[0]: {{ObjectID: "obj-a", ReadyReplicas: int32Ptr(2)}},
			trackingIDs[1]: {{ObjectID: "obj-b", ReadyReplicas: int32Ptr(5)}},
		},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, []string{"img@sha256:shared"}, repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary == nil {
		t.Fatal("summary = nil, want non-nil")
	}
	if summary.count != 7 {
		t.Fatalf("count = %d, want 7 (2 + 5 summed across distinct tracking-ids)", summary.count)
	}
}

// TestFetchWorkloadLiveInstanceSummaryAllNilReadyReplicasOmitsCount proves a
// matched-but-unobserved case (every matched fact carries a nil
// ReadyReplicas, e.g. bare Pod objects) never fabricates a count: absent
// stays absent.
func TestFetchWorkloadLiveInstanceSummaryAllNilReadyReplicasOmitsCount(t *testing.T) {
	t.Parallel()

	controllers, resources, trackingID := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingID: {
				{ReadyReplicas: nil},
				{ReadyReplicas: nil},
			},
		},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, []string{"img@sha256:a"}, repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary != nil {
		t.Fatal("all-nil ready_replicas returned a non-nil summary, want nil (absent, never fabricated)")
	}
}

// TestFetchWorkloadLiveInstanceSummaryReadyZeroIsPresent proves a real
// scaled-to-zero observation (ready_replicas present and 0) is reported as a
// present 0, not treated the same as "no observation".
func TestFetchWorkloadLiveInstanceSummaryReadyZeroIsPresent(t *testing.T) {
	t.Parallel()

	controllers, resources, trackingID := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingID: {{ReadyReplicas: int32Ptr(0)}},
		},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, []string{"img@sha256:a"}, repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary == nil {
		t.Fatal("summary = nil, want non-nil (ready_replicas=0 is a real, present observation)")
	}
	if summary.count != 0 {
		t.Fatalf("count = %d, want 0", summary.count)
	}
	if summary.truncated {
		t.Fatal("truncated = true, want false (single match, far under serviceStoryItemLimit)")
	}
}

// TestFetchWorkloadLiveInstanceSummaryStoreError proves a store failure
// returns a nil summary and the error, so the call site can log-and-continue
// without setting the count/environment response fields.
// TestFetchWorkloadLiveInstanceSummaryDeclaredObjectAnchorContributesCount is
// the #5639 positive case for the count probe: a workload with no ArgoCD
// controller but a mappable declared Deployment counts its live replicas
// purely from the declared-object anchor's matched facts.
func TestFetchWorkloadLiveInstanceSummaryDeclaredObjectAnchorContributesCount(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{k8sResourceFixture("Deployment", "deployable-source", "production", "apps/v1")}
	anchors := declaredObjectAnchors(resources)
	if len(anchors) != 1 {
		t.Fatalf("test fixture bug: want exactly 1 declared-object anchor, got %d", len(anchors))
	}
	key := declaredObjectMatchKey(anchors[0].GroupVersionResource, anchors[0].Namespace, anchors[0].Name)
	store := &stubKubernetesPodTemplateListStore{
		matchesByDeclaredObjectKey: map[string][]LiveIdentityMatch{
			key: {{ClusterID: "prod-cluster", ReadyReplicas: int32Ptr(4)}},
		},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), nil, resources,
		[]string{"ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"},
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary == nil {
		t.Fatal("summary = nil, want non-nil (declared-object anchor observed a match)")
	}
	if summary.count != 4 {
		t.Fatalf("count = %d, want 4", summary.count)
	}
	if len(store.calls) != 1 {
		t.Fatalf("store queried %d times, want 1 (declared-object anchor only, no ArgoCD controller)", len(store.calls))
	}
	if got := store.calls[0].AnchorKind; got != liveIdentityAnchorDeclaredObject {
		t.Fatalf("store.calls[0].AnchorKind = %q, want declared-object", got)
	}
}

// TestFetchWorkloadLiveInstanceSummaryArgoCDAndDeclaredObjectAnchorsNoDoubleCount
// is the #5639 P1 regression: an ArgoCD-managed workload also has a mappable
// declared Deployment, so resolveLiveIdentityAnchors produces BOTH a
// tracking-id anchor and a declared-object anchor for it
// (impact_trace_deployment_live_evidence_identity.go). In production the SAME
// live pod-template fact (same cluster_id + object_id) is matched by both the
// tracking-id query (via the ArgoCD annotation) and the declared-object query
// (via group_version_resource/namespace/name), because it is one running
// Deployment observed through two independent identity paths. The
// aggregation MUST dedup that re-hit by (cluster_id, object_id) across anchor
// families -- summing both anchors' per-cluster maxima would double the
// count (3 + 3 = 6) even though only one Deployment with 3 ready replicas
// exists.
func TestFetchWorkloadLiveInstanceSummaryArgoCDAndDeclaredObjectAnchorsNoDoubleCount(t *testing.T) {
	t.Parallel()

	controllers, resources, trackingID := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	anchors := declaredObjectAnchors(resources)
	if len(anchors) != 1 {
		t.Fatalf("test fixture bug: want exactly 1 declared-object anchor, got %d", len(anchors))
	}
	declaredKey := declaredObjectMatchKey(anchors[0].GroupVersionResource, anchors[0].Namespace, anchors[0].Name)

	// The same live fact (object-D, prod-cluster, 3 ready replicas) is
	// returned for BOTH the tracking-id anchor and the declared-object
	// anchor, modelling the real store where the fact matches both queries.
	sharedMatch := LiveIdentityMatch{ObjectID: "obj-D", ClusterID: "c", ReadyReplicas: int32Ptr(3)}
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingID: {sharedMatch},
		},
		matchesByDeclaredObjectKey: map[string][]LiveIdentityMatch{
			declaredKey: {sharedMatch},
		},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources,
		[]string{"ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"},
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary == nil {
		t.Fatal("summary = nil, want non-nil (an observation was made)")
	}
	if summary.count != 3 {
		t.Fatalf("count = %d, want 3 (same live object matched by both an ArgoCD and a declared-object anchor must be counted once, not summed to 6)", summary.count)
	}
}

func TestFetchWorkloadLiveInstanceSummaryStoreError(t *testing.T) {
	t.Parallel()

	controllers, resources, _ := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{err: fmt.Errorf("postgres offline")}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), controllers, resources, []string{"img@sha256:a"}, repositoryAccessFilter{allScopes: true},
	)
	if err == nil {
		t.Fatal("store error must be surfaced, got nil")
	}
	if summary != nil {
		t.Fatal("store error returned a non-nil summary, want nil")
	}
}
