// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// #5663: fetchWorkloadLiveInstanceSummary's truncation-signal tests, split
// out of impact_trace_deployment_live_evidence_count_test.go to stay under
// the repo's 500-line-per-file cap. Shares that file's stub
// (stubKubernetesPodTemplateListStore), fixtures (singleTrackingIDFixture,
// argoCDControllerFixture, k8sResourceFixture), and int32Ptr helper.

package query

import (
	"fmt"
	"testing"
)

// atLimitMatches builds serviceStoryItemLimit distinct LiveIdentityMatch rows
// (distinct ClusterID+ObjectID so the cross-anchor dedup never collapses
// them), modelling a single anchor's ListLiveIdentityMatches read landing
// exactly on the store's LIMIT -- the store cannot tell "exactly N matched
// objects" from "N or more exist, truncated at N" (#5663).
func atLimitMatches() []LiveIdentityMatch {
	matches := make([]LiveIdentityMatch, serviceStoryItemLimit)
	for i := range matches {
		matches[i] = LiveIdentityMatch{
			ClusterID:     fmt.Sprintf("cluster-%d", i),
			ObjectID:      fmt.Sprintf("obj-%d", i),
			ReadyReplicas: int32Ptr(1),
		}
	}
	return matches
}

// TestFetchWorkloadLiveInstanceSummaryAnchorAtLimitTruncated is the #5663 RED
// case (a): an anchor's ListLiveIdentityMatches read returning exactly
// serviceStoryItemLimit rows must report truncated=true -- a full page is
// indistinguishable from "more rows exist beyond the cap" without fetching
// N+1, so the summary must disclose the count as a conservative lower bound.
func TestFetchWorkloadLiveInstanceSummaryAnchorAtLimitTruncated(t *testing.T) {
	t.Parallel()

	controllers, resources, trackingID := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingID: atLimitMatches(),
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
		t.Fatal("summary = nil, want non-nil (an observation was made)")
	}
	if !summary.truncated {
		t.Fatal("truncated = false, want true (anchor read hit serviceStoryItemLimit rows)")
	}
}

// TestFetchWorkloadLiveInstanceSummaryUnderLimitNotTruncated is the #5663 RED
// case (b): an anchor read that returns fewer than serviceStoryItemLimit rows
// is a complete observation, not a lower bound.
func TestFetchWorkloadLiveInstanceSummaryUnderLimitNotTruncated(t *testing.T) {
	t.Parallel()

	controllers, resources, trackingID := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingID: {{ClusterID: "c", ObjectID: "obj-a", ReadyReplicas: int32Ptr(3)}},
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
		t.Fatal("summary = nil, want non-nil (an observation was made)")
	}
	if summary.truncated {
		t.Fatal("truncated = true, want false (anchor read returned fewer than serviceStoryItemLimit rows)")
	}
}

// TestFetchWorkloadLiveInstanceSummaryAnyAnchorAtLimitTruncatesWholeSummary
// proves the "ANY anchor" aggregation rule: a workload with two distinct
// tracking-id anchors where only one hits the limit still reports
// truncated=true at the summary level -- a partial undercount on any single
// anchor makes the whole live_instance_count a lower bound.
func TestFetchWorkloadLiveInstanceSummaryAnyAnchorAtLimitTruncatesWholeSummary(t *testing.T) {
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
			trackingIDs[0]: atLimitMatches(),
			trackingIDs[1]: {{ObjectID: "obj-under", ReadyReplicas: int32Ptr(2)}},
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
		t.Fatal("summary = nil, want non-nil (an observation was made)")
	}
	if !summary.truncated {
		t.Fatal("truncated = false, want true (one of two anchors hit the limit)")
	}
}

// TestFetchWorkloadLiveInstanceSummaryNilSummaryStaysNilAtLimit is the #5663
// RED case (c): a nil-summary short circuit (no anchor of any kind) must stay
// nil even though it never gets a chance to observe truncation -- there is no
// count to attach a truncated flag to, so the caller must keep treating this
// exactly like every other "no countable observation" case.
func TestFetchWorkloadLiveInstanceSummaryNilSummaryStaysNilAtLimit(t *testing.T) {
	t.Parallel()

	store := &stubKubernetesPodTemplateListStore{}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	summary, err := h.fetchWorkloadLiveInstanceSummary(
		t.Context(), nil, nil, nil, repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if summary != nil {
		t.Fatal("summary != nil, want nil (no anchor of any kind)")
	}
}

// TestFetchWorkloadLiveInstanceSummaryAllNilReadyReplicasAtLimitStaysNil is
// the #5663 RED case (d): every matched fact across an at-limit anchor read
// carries a nil ReadyReplicas (e.g. bare Pod objects) -- the summary must
// still be nil (no countable observation), never a fabricated zero with a
// leaked truncated=true, even though the anchor's row count hit the limit.
func TestFetchWorkloadLiveInstanceSummaryAllNilReadyReplicasAtLimitStaysNil(t *testing.T) {
	t.Parallel()

	controllers, resources, trackingID := singleTrackingIDFixture("app-a", "Deployment", "workload-a", "ns", "apps/v1")
	matches := make([]LiveIdentityMatch, serviceStoryItemLimit)
	for i := range matches {
		matches[i] = LiveIdentityMatch{ClusterID: fmt.Sprintf("cluster-%d", i), ObjectID: fmt.Sprintf("obj-%d", i)}
	}
	store := &stubKubernetesPodTemplateListStore{
		matchesByTrackingID: map[string][]LiveIdentityMatch{
			trackingID: matches,
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
		t.Fatal("all-nil ready_replicas at the limit returned a non-nil summary, want nil (absent, never fabricated)")
	}
}
