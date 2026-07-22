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
// canned matches keyed by TrackingID. HasLiveIdentityMatch is never called by
// the probe under test here; it panics if reached so a wiring mistake fails
// loudly instead of silently returning a wrong bool.
type stubKubernetesPodTemplateListStore struct {
	matchesByTrackingID map[string][]LiveIdentityMatch
	err                 error
	calls               []KubernetesPodTemplateFilter
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

// TestFetchWorkloadLiveInstanceSummaryNoIdentityNeverQueriesStore proves the
// same fail-closed anchor-first discipline as
// TestFetchWorkloadLiveEvidenceNoArgoCDControllerNeverQueriesStore: no
// resolvable ArgoCD identity means the store is never queried and the count
// is absent.
func TestFetchWorkloadLiveInstanceSummaryNoIdentityNeverQueriesStore(t *testing.T) {
	t.Parallel()

	store := &stubKubernetesPodTemplateListStore{}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(),
		nil, // no controllers at all
		[]map[string]any{k8sResourceFixture("Deployment", "workload-a", "shared-ns", "apps/v1")},
		[]string{"ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"},
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary != nil {
		t.Fatal("no ArgoCD controller returned a non-nil summary, want nil")
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
// max, and the totals are summed across tracking-ids.
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
			trackingIDs[0]: {{ReadyReplicas: int32Ptr(2)}},
			trackingIDs[1]: {{ReadyReplicas: int32Ptr(5)}},
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
}

// TestFetchWorkloadLiveInstanceSummaryStoreError proves a store failure
// returns a nil summary and the error, so the call site can log-and-continue
// without setting the count/environment response fields.
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
