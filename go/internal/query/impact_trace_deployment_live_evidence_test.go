// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"testing"
)

func TestDeploymentOverallConfidenceLiveEvidence(t *testing.T) {
	t.Parallel()

	confidence, reason := deploymentOverallConfidence(nil, nil, nil, true)
	if confidence != 0.95 {
		t.Fatalf("deploymentOverallConfidence(live=true) confidence = %v, want 0.95", confidence)
	}
	if reason != "live_runtime_observation" {
		t.Fatalf("deploymentOverallConfidence(live=true) reason = %q, want %q", reason, "live_runtime_observation")
	}
}

func TestDeploymentOverallConfidenceLiveEvidenceOverridesInstances(t *testing.T) {
	t.Parallel()

	instances := []map[string]any{
		{"materialization_confidence": 0.9},
	}
	confidence, reason := deploymentOverallConfidence(instances, nil, nil, true)
	if confidence != 0.95 {
		t.Fatalf("confidence = %v, want 0.95", confidence)
	}
	if reason != "live_runtime_observation" {
		t.Fatalf("reason = %q, want %q", reason, "live_runtime_observation")
	}
}

func TestDeploymentOverallConfidenceNoEvidence(t *testing.T) {
	t.Parallel()

	confidence, reason := deploymentOverallConfidence(nil, nil, nil, false)
	if confidence != 0 {
		t.Fatalf("confidence = %v, want 0", confidence)
	}
	if reason != "no_deployment_evidence" {
		t.Fatalf("reason = %q, want %q", reason, "no_deployment_evidence")
	}
}

func TestBuildDeploymentFactSummaryTierLiveEvidence(t *testing.T) {
	t.Parallel()

	ctx := sampleServiceDossierContext()
	instances, _ := ctx["instances"].([]map[string]any)
	summary := buildDeploymentFactSummary(
		ctx,
		instances,
		[]string{"production", "qa"},
		nil,
		[]string{"eks-prod", "ecs-qa"},
		nil,
		nil,
		nil,
		nil,
		nil,
		"controller",
		true, // hasLiveEvidence
	)
	if tier, ok := summary["deployment_truth_tier"]; !ok {
		t.Fatal("deployment_truth_tier missing from summary")
	} else if tier != "runtime_confirmed" {
		t.Fatalf("deployment_truth_tier = %q, want %q", tier, "runtime_confirmed")
	}
	if confidence := summary["overall_confidence"]; confidence != 0.95 {
		t.Fatalf("overall_confidence = %v, want 0.95", confidence)
	}
	if reason := summary["overall_confidence_reason"]; reason != "live_runtime_observation" {
		t.Fatalf("overall_confidence_reason = %q, want %q", reason, "live_runtime_observation")
	}
}

func TestBuildDeploymentFactSummaryTierConfigOnly(t *testing.T) {
	t.Parallel()

	ctx := sampleServiceDossierContext()
	// #5638 TIER GUARDRAIL (the non-negotiable): a live_instance_count CAN be
	// present on the context (a matched fact carried a replica observation)
	// while hasLiveEvidence is STILL false (e.g. the identity-bound match
	// exists but some other live-evidence precondition did not hold). The
	// count must never leak into the tier or confidence-reason decision --
	// only hasLiveEvidence does that.
	ctx["_live_instance_count"] = 3
	instances, _ := ctx["instances"].([]map[string]any)
	summary := buildDeploymentFactSummary(
		ctx,
		instances,
		[]string{"production", "qa"},
		nil,
		[]string{"eks-prod", "ecs-qa"},
		nil,
		nil,
		nil,
		nil,
		nil,
		"controller",
		false,
	)
	if tier, ok := summary["deployment_truth_tier"]; !ok {
		t.Fatal("deployment_truth_tier missing")
	} else if tier != "config_only" {
		t.Fatalf("deployment_truth_tier = %q, want %q (count present must not promote the tier)", tier, "config_only")
	}
	if reason := summary["overall_confidence_reason"]; reason != "materialized_runtime_instances" {
		t.Fatalf("overall_confidence_reason = %q, want %q (count present must not promote the confidence reason)", reason, "materialized_runtime_instances")
	}
	if count := summary["live_instance_count"]; count != 3 {
		t.Fatalf("live_instance_count = %v, want 3 (the count itself still surfaces even though the tier stays config_only)", count)
	}
}

// TestBuildDeploymentFactSummaryLiveInstanceCountAbsentWhenNoObservation
// proves live_instance_count is omitted entirely (not a fabricated 0) when
// the handler never set workloadContext["_live_instance_count"] at all --
// the no-countable-observation case fetchWorkloadLiveInstanceSummary reports
// via a nil *liveInstanceSummary.
func TestBuildDeploymentFactSummaryLiveInstanceCountAbsentWhenNoObservation(t *testing.T) {
	t.Parallel()

	ctx := sampleServiceDossierContext()
	instances, _ := ctx["instances"].([]map[string]any)
	summary := buildDeploymentFactSummary(
		ctx, instances, []string{"production"}, nil, []string{"eks-prod"},
		nil, nil, nil, nil, nil, "controller", false,
	)
	if _, ok := summary["live_instance_count"]; ok {
		t.Fatalf("live_instance_count = %v, want absent when no observation was made", summary["live_instance_count"])
	}
}

func TestBuildDeploymentFactSummaryTierEmptyWhenNoEvidence(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{}
	summary := buildDeploymentFactSummary(
		ctx,
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
		"",
		false,
	)
	if _, ok := summary["deployment_truth_tier"]; ok {
		t.Fatal("deployment_truth_tier must be absent when no evidence exists")
	}
}

// stubKubernetesPodTemplateStore is a test fake implementing
// KubernetesPodTemplateStore: it records every filter it was called with and
// reports a match only for filters whose TrackingID is in
// matchingTrackingIDs (ArgoCD anchor filters) or whose
// group_version_resource|namespace|name key
// (declaredObjectStubMatchKey) is in matchingDeclaredObjects (declared-object
// anchor filters, #5639).
type stubKubernetesPodTemplateStore struct {
	matchingTrackingIDs     map[string]struct{}
	matchingDeclaredObjects map[string]struct{}
	err                     error
	// calls records every filter passed to HasLiveIdentityMatch, so tests
	// can assert the probe never queried the store (call-count = 0) and
	// inspect access-scoping fields.
	calls []KubernetesPodTemplateFilter
}

// declaredObjectStubMatchKey builds the stub's match key for a
// declared-object anchor filter, mirroring the identity tuple
// declaredObjectAnchors binds on (group_version_resource, namespace, name).
func declaredObjectStubMatchKey(gvr, namespace, name string) string {
	return gvr + "|" + namespace + "|" + name
}

func (s *stubKubernetesPodTemplateStore) HasLiveIdentityMatch(
	_ context.Context,
	filter KubernetesPodTemplateFilter,
) (bool, error) {
	s.calls = append(s.calls, filter)
	if s.err != nil {
		return false, s.err
	}
	if filter.AnchorKind == liveIdentityAnchorDeclaredObject {
		key := declaredObjectStubMatchKey(filter.GroupVersionResource, filter.Namespace, filter.Name)
		_, matched := s.matchingDeclaredObjects[key]
		return matched, nil
	}
	_, matched := s.matchingTrackingIDs[filter.TrackingID]
	return matched, nil
}

// ListLiveIdentityMatches satisfies KubernetesPodTemplateStore for tests that
// only exercise HasLiveIdentityMatch. It is never called by
// fetchWorkloadLiveEvidence; fetchWorkloadLiveInstanceSummary's own tests use
// the richer stubKubernetesPodTemplateListStore instead
// (impact_trace_deployment_live_evidence_count_test.go).
func (s *stubKubernetesPodTemplateStore) ListLiveIdentityMatches(
	context.Context,
	KubernetesPodTemplateFilter,
) ([]LiveIdentityMatch, error) {
	return nil, nil
}

// argoCDControllerFixture builds a minimal argocd_application controller map
// as buildDeploymentSourceControllerEntity would produce it.
func argoCDControllerFixture(appName string) map[string]any {
	return map[string]any{"controller_kind": "argocd_application", "entity_name": appName}
}

// k8sResourceFixture builds a minimal declared k8sResource map as
// collectDeploymentSourceK8sResources/k8sResourceWireRow would produce it.
func k8sResourceFixture(kind, name, namespace, apiVersion string) map[string]any {
	return map[string]any{
		"kind":        kind,
		"entity_name": name,
		"namespace":   namespace,
		"api_version": apiVersion,
	}
}

func TestFetchWorkloadLiveEvidenceNilHandler(t *testing.T) {
	t.Parallel()

	var h *ImpactHandler
	live, err := h.fetchWorkloadLiveEvidence(t.Context(), nil, nil, nil, repositoryAccessFilter{})
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if live {
		t.Fatal("nil handler returned true, want false")
	}
}

// TestFetchWorkloadLiveEvidenceNoAnchorOfAnyKindNeverQueriesStore is the
// #5471 codex P1 core fail-closed proof, widened for #5639: a workload with
// no argocd_application controller AND no mappable declared-object anchor
// (an unmappable kind, here ConfigMap) has no computable identity ANCHOR OF
// ANY KIND, so the probe must return config_only WITHOUT ever calling the
// store, even when the store is wired and would have returned a match for
// every anchor. Renamed from
// TestFetchWorkloadLiveEvidenceNoArgoCDControllerNeverQueriesStore: its
// meaning changed from "no ArgoCD controller" to "no anchor of ANY kind" --
// a workload with only a declared-object anchor now legitimately queries
// (see TestFetchWorkloadLiveEvidenceDeclaredObjectAnchorMatchPromotesToRuntimeConfirmed).
func TestFetchWorkloadLiveEvidenceNoAnchorOfAnyKindNeverQueriesStore(t *testing.T) {
	t.Parallel()

	store := &stubKubernetesPodTemplateStore{matchingTrackingIDs: map[string]struct{}{}}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	live, err := h.fetchWorkloadLiveEvidence(
		t.Context(),
		nil, // no controllers at all
		// ConfigMap is outside the closed declared-object-anchor kind map
		// (declaredObjectAnchorResourceByKind), so this resource resolves
		// NO declared-object anchor either -- proving the fail-closed
		// absence, not merely the absence of an ArgoCD controller.
		[]map[string]any{k8sResourceFixture("ConfigMap", "workload-a", "shared-ns", "v1")},
		[]string{"ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"},
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if live {
		t.Fatal("no anchor of any kind returned true, want false (config_only)")
	}
	if got := len(store.calls); got != 0 {
		t.Fatalf("store was queried %d times, want 0 (fail-closed at the identity layer)", got)
	}
}

func TestFetchWorkloadLiveEvidenceNilStore(t *testing.T) {
	t.Parallel()

	h := &ImpactHandler{} // KubernetesPodTemplates is nil
	live, err := h.fetchWorkloadLiveEvidence(
		t.Context(),
		[]map[string]any{argoCDControllerFixture("app-a")},
		[]map[string]any{k8sResourceFixture("Deployment", "workload-a", "ns", "apps/v1")},
		[]string{"img:latest"},
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if live {
		t.Fatal("nil store returned true, want false")
	}
}

func TestFetchWorkloadLiveEvidenceEmptyImageRefs(t *testing.T) {
	t.Parallel()

	store := &stubKubernetesPodTemplateStore{}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	live, err := h.fetchWorkloadLiveEvidence(
		t.Context(),
		[]map[string]any{argoCDControllerFixture("app-a")},
		[]map[string]any{k8sResourceFixture("Deployment", "workload-a", "ns", "apps/v1")},
		nil,
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if live {
		t.Fatal("empty image_refs returned true, want false")
	}
	if got := len(store.calls); got != 0 {
		t.Fatalf("store was queried %d times, want 0", got)
	}
}

// TestFetchWorkloadLiveEvidenceDistinctWorkloadsSharedDigest is the #5471
// codex P1 end-to-end regression proof, THE NON-NEGOTIABLE hazard test that
// MUST stay green across the #5639 anchor widening: workloads A and B
// declare DIFFERENT ArgoCD Applications (and therefore different ArgoCD
// identities) but happen to declare the SAME image digest. A fake store that
// only matches B's ArgoCD tracking-id must promote trace(B) to
// runtime_confirmed and must NOT promote trace(A), even though A's
// image_refs are identical to B's.
//
// #5639 widened resolveLiveIdentityAnchors to also try a declared-object
// anchor after the ArgoCD anchor, so each trace now issues UP TO 2 store
// calls instead of exactly 1 -- trace(B) still short-circuits after its
// FIRST (ArgoCD) anchor matches, but trace(A)'s ArgoCD anchor does not match,
// so it falls through to try its (also non-matching, since A and B have
// different declared names) declared-object anchor before giving up. The
// widening does not weaken the hazard proof: workload A's declared-object
// anchor (shared-ns/workload-a) is a GENUINE identity distinct from
// workload B's (shared-ns/workload-b), so it is asserted here too.
func TestFetchWorkloadLiveEvidenceDistinctWorkloadsSharedDigest(t *testing.T) {
	t.Parallel()

	sharedDigest := "ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"
	controllersA := []map[string]any{argoCDControllerFixture("app-a")}
	resourcesA := []map[string]any{k8sResourceFixture("Deployment", "workload-a", "shared-ns", "apps/v1")}
	controllersB := []map[string]any{argoCDControllerFixture("app-b")}
	resourcesB := []map[string]any{k8sResourceFixture("Deployment", "workload-b", "shared-ns", "apps/v1")}

	trackingIDB := "app-b:apps/Deployment:shared-ns/workload-b"
	trackingIDA := "app-a:apps/Deployment:shared-ns/workload-a"
	if trackingIDA == trackingIDB {
		t.Fatal("test fixture bug: tracking ids must differ")
	}

	store := &stubKubernetesPodTemplateStore{
		matchingTrackingIDs: map[string]struct{}{trackingIDB: {}},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}

	liveA, err := h.fetchWorkloadLiveEvidence(t.Context(), controllersA, resourcesA, []string{sharedDigest}, repositoryAccessFilter{allScopes: true})
	if err != nil {
		t.Fatalf("trace(A) error = %v, want nil", err)
	}
	if liveA {
		t.Fatal("trace(A) promoted to runtime_confirmed on workload B's live row -- #5471 codex P1 regression")
	}

	liveB, err := h.fetchWorkloadLiveEvidence(t.Context(), controllersB, resourcesB, []string{sharedDigest}, repositoryAccessFilter{allScopes: true})
	if err != nil {
		t.Fatalf("trace(B) error = %v, want nil", err)
	}
	if !liveB {
		t.Fatal("trace(B) did not promote on its OWN identity-bound live row, want true")
	}

	// Trace(A) tries both its ArgoCD anchor and its declared-object anchor
	// (neither matches); trace(B) short-circuits on its first (ArgoCD)
	// anchor. 2 (A) + 1 (B) = 3.
	if got := len(store.calls); got != 3 {
		t.Fatalf("store was queried %d times, want exactly 3 (2 for trace(A): ArgoCD + declared-object; 1 for trace(B): ArgoCD short-circuit)", got)
	}
	if got := store.calls[0].TrackingID; got != trackingIDA {
		t.Fatalf("trace(A) first (ArgoCD) call queried tracking id %q, want %q", got, trackingIDA)
	}
	if got := store.calls[1].AnchorKind; got != liveIdentityAnchorDeclaredObject {
		t.Fatalf("trace(A) second call AnchorKind = %q, want declared-object", got)
	}
	if got := store.calls[1].Namespace; got != "shared-ns" {
		t.Fatalf("trace(A) declared-object call Namespace = %q, want shared-ns", got)
	}
	if got := store.calls[1].Name; got != "workload-a" {
		t.Fatalf("trace(A) declared-object call Name = %q, want workload-a (A's OWN declared name, never B's)", got)
	}
	if got := store.calls[2].TrackingID; got != trackingIDB {
		t.Fatalf("trace(B) queried tracking id %q, want %q", got, trackingIDB)
	}
}

// TestFetchWorkloadLiveEvidenceDeclaredObjectAnchorMatchPromotesToRuntimeConfirmed
// is the #5639 positive case: a workload with NO ArgoCD controller (so no
// ArgoCD tracking-id annotation is possible) but a mappable declared
// Deployment matches an active pod_template fact by kind+namespace+name plus
// an intersecting image ref. The probe must promote to runtime_confirmed
// purely on the declared-object anchor.
func TestFetchWorkloadLiveEvidenceDeclaredObjectAnchorMatchPromotesToRuntimeConfirmed(t *testing.T) {
	t.Parallel()

	resources := []map[string]any{k8sResourceFixture("Deployment", "deployable-source", "production", "apps/v1")}
	matchKey := declaredObjectStubMatchKey("apps/v1/deployments", "production", "deployable-source")
	store := &stubKubernetesPodTemplateStore{
		matchingDeclaredObjects: map[string]struct{}{matchKey: {}},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}

	live, err := h.fetchWorkloadLiveEvidence(
		t.Context(),
		nil, // no ArgoCD controller at all
		resources,
		[]string{"ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"},
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if !live {
		t.Fatal("declared-object anchor match returned false, want true (runtime_confirmed)")
	}
	if len(store.calls) != 1 {
		t.Fatalf("store queried %d times, want 1 (the declared-object anchor is the only anchor when there is no ArgoCD controller)", len(store.calls))
	}
	// Assert the exact filter shape received (#5639 design requirement):
	// AnchorKind, GroupVersionResource, Namespace, Name, and ImageRefs must
	// all thread through liveIdentityAnchorFilter unchanged.
	got := store.calls[0]
	if got.AnchorKind != liveIdentityAnchorDeclaredObject {
		t.Fatalf("store.calls[0].AnchorKind = %q, want declared-object", got.AnchorKind)
	}
	if got.GroupVersionResource != "apps/v1/deployments" {
		t.Fatalf("store.calls[0].GroupVersionResource = %q, want %q", got.GroupVersionResource, "apps/v1/deployments")
	}
	if got.Namespace != "production" {
		t.Fatalf("store.calls[0].Namespace = %q, want %q", got.Namespace, "production")
	}
	if got.Name != "deployable-source" {
		t.Fatalf("store.calls[0].Name = %q, want %q", got.Name, "deployable-source")
	}
	if len(got.ImageRefs) != 1 || got.ImageRefs[0] != "ghcr.io/eshu-hq/supply-chain-demo@sha256:shared" {
		t.Fatalf("store.calls[0].ImageRefs = %v, want the traced workload's image refs", got.ImageRefs)
	}
	if !got.AllScopes {
		t.Fatal("store.calls[0].AllScopes = false, want true for an unscoped caller")
	}
}

// TestFetchWorkloadLiveEvidenceDeclaredObjectAnchorSharedDigestDistinctWorkloadsNoPromotion
// is the REQUIRED #5639 declared-object shared-digest hazard proof -- the
// codex-P1-clone this widening must never reintroduce: workloads A and B
// share an image digest and NEITHER declares an ArgoCD Application (so only
// the declared-object anchor is exercised). The live fact carries ONLY B's
// kind+namespace+name. A's declared-object anchor must NOT match B's live
// row -- a shared digest alone must never promote A.
//
// This test is RED-provable: if the name/GVR equality in the declared-object
// stub match key (declaredObjectStubMatchKey) or in the production identity
// binding (declaredObjectAnchors / hasLiveKubernetesPodTemplateDeclaredObjectIdentityQuery)
// were dropped in favor of matching on image digest alone, trace(A) would
// wrongly promote here.
func TestFetchWorkloadLiveEvidenceDeclaredObjectAnchorSharedDigestDistinctWorkloadsNoPromotion(t *testing.T) {
	t.Parallel()

	sharedDigest := "ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"
	resourcesA := []map[string]any{k8sResourceFixture("Deployment", "workload-a", "shared-ns", "apps/v1")}
	resourcesB := []map[string]any{k8sResourceFixture("Deployment", "workload-b", "shared-ns", "apps/v1")}

	// The live fact carries ONLY workload-b's declared identity.
	matchKeyB := declaredObjectStubMatchKey("apps/v1/deployments", "shared-ns", "workload-b")
	store := &stubKubernetesPodTemplateStore{
		matchingDeclaredObjects: map[string]struct{}{matchKeyB: {}},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}

	liveA, err := h.fetchWorkloadLiveEvidence(t.Context(), nil, resourcesA, []string{sharedDigest}, repositoryAccessFilter{allScopes: true})
	if err != nil {
		t.Fatalf("trace(A) error = %v, want nil", err)
	}
	if liveA {
		t.Fatal("trace(A) promoted to runtime_confirmed on workload B's declared-object live row via a shared digest -- #5639 codex-P1-clone regression")
	}

	liveB, err := h.fetchWorkloadLiveEvidence(t.Context(), nil, resourcesB, []string{sharedDigest}, repositoryAccessFilter{allScopes: true})
	if err != nil {
		t.Fatalf("trace(B) error = %v, want nil", err)
	}
	if !liveB {
		t.Fatal("trace(B) did not promote on its OWN declared-object identity, want true")
	}
}

// TestFetchWorkloadLiveEvidenceDeclaredObjectAnchorNamespaceGuard is the
// REQUIRED #5639 namespace-guard proof: the SAME kind and name in a
// DIFFERENT namespace must never match -- no cluster-scoped or
// cross-namespace wildcard match is ever allowed. RED-provable by dropping
// the namespace equality from declaredObjectAnchors/the declared-object SQL
// predicate.
func TestFetchWorkloadLiveEvidenceDeclaredObjectAnchorNamespaceGuard(t *testing.T) {
	t.Parallel()

	// The live fact carries the identity in "staging", but the traced
	// workload declares "production".
	matchKey := declaredObjectStubMatchKey("apps/v1/deployments", "staging", "deployable-source")
	store := &stubKubernetesPodTemplateStore{
		matchingDeclaredObjects: map[string]struct{}{matchKey: {}},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}

	resources := []map[string]any{k8sResourceFixture("Deployment", "deployable-source", "production", "apps/v1")}
	live, err := h.fetchWorkloadLiveEvidence(
		t.Context(), nil, resources,
		[]string{"ghcr.io/eshu-hq/supply-chain-demo@sha256:shared"},
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if live {
		t.Fatal("cross-namespace match promoted to runtime_confirmed -- namespace guard regression")
	}
}

func TestFetchWorkloadLiveEvidenceStoreError(t *testing.T) {
	t.Parallel()

	store := &stubKubernetesPodTemplateStore{
		err: fmt.Errorf("postgres offline"),
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	_, err := h.fetchWorkloadLiveEvidence(
		t.Context(),
		[]map[string]any{argoCDControllerFixture("app-a")},
		[]map[string]any{k8sResourceFixture("Deployment", "workload-a", "ns", "apps/v1")},
		[]string{"img:latest"},
		repositoryAccessFilter{allScopes: true},
	)
	if err == nil {
		t.Fatal("store error must be surfaced, got nil")
	}
}

func TestFetchWorkloadLiveEvidenceScopedAccessFilter(t *testing.T) {
	t.Parallel()

	trackingID := "app-a:apps/Deployment:ns/workload-a"
	store := &stubKubernetesPodTemplateStore{
		matchingTrackingIDs: map[string]struct{}{trackingID: {}},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	access := repositoryAccessFilter{
		allScopes:            false,
		allowedRepositoryIDs: []string{"repo:sample-service-api"},
		allowedScopeIDs:      []string{"scope:test"},
		allowed: map[string]struct{}{
			"repo:sample-service-api": {},
			"scope:test":              {},
		},
	}
	live, err := h.fetchWorkloadLiveEvidence(
		t.Context(),
		[]map[string]any{argoCDControllerFixture("app-a")},
		[]map[string]any{k8sResourceFixture("Deployment", "workload-a", "ns", "apps/v1")},
		[]string{"img@sha256:a"},
		access,
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if !live {
		t.Fatal("exact match with scoped access returned false")
	}
	// The store must have received the access-scoping fields.
	if len(store.calls) != 1 {
		t.Fatalf("store queried %d times, want 1", len(store.calls))
	}
	if store.calls[0].AllScopes {
		t.Fatal("AllScopes must be false for scoped access")
	}
	if len(store.calls[0].AllowedRepositoryIDs) == 0 {
		t.Fatal("AllowedRepositoryIDs must be populated for scoped access")
	}
	if len(store.calls[0].AllowedScopeIDs) == 0 {
		t.Fatal("AllowedScopeIDs must be populated for scoped access")
	}
}

func TestFetchWorkloadLiveEvidenceEmptyAccess(t *testing.T) {
	t.Parallel()

	// An empty access filter (no grants, not all-scopes) means a scoped
	// caller with zero grants. The store must never be called.
	trackingID := "app-a:apps/Deployment:ns/workload-a"
	store := &stubKubernetesPodTemplateStore{
		matchingTrackingIDs: map[string]struct{}{trackingID: {}},
	}
	h := &ImpactHandler{KubernetesPodTemplates: store}
	live, err := h.fetchWorkloadLiveEvidence(
		t.Context(),
		[]map[string]any{argoCDControllerFixture("app-a")},
		[]map[string]any{k8sResourceFixture("Deployment", "workload-a", "ns", "apps/v1")},
		[]string{"img@sha256:a"},
		repositoryAccessFilter{}, // empty: no grants, scoped
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if live {
		t.Fatal("empty access filter must return false without querying")
	}
	if got := len(store.calls); got != 0 {
		t.Fatalf("store was queried %d times, want 0", got)
	}
}
