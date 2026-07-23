// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

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
