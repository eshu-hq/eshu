// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildKubernetesCorrelationDecisionsExactDigest proves a live image
// reference that already names a digest observed in deployment-source registry
// facts is classified exact/in_sync, not provenance-only.
func TestBuildKubernetesCorrelationDecisionsExactDigest(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false),
		k8sSourceManifestFact("oci-1", testK8sRegistry, testK8sRepository, testK8sDigest, false),
	})

	byRef := kubernetesCorrelationByImageRef(decisions)
	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	decision := byRef[objectID+"|"+imageRef+"|"]
	assertKubernetesOutcome(t, decision, KubernetesCorrelationExact, driftInSync)
	if decision.ProvenanceOnly {
		t.Fatal("exact digest ProvenanceOnly = true, want false")
	}
	if decision.SourceDigest != testK8sDigest {
		t.Fatalf("source_digest = %q, want %q", decision.SourceDigest, testK8sDigest)
	}
}

// TestBuildKubernetesCorrelationDecisionsDerivedTagToDigest proves a live
// repository:tag reference that resolves to exactly one deployment-source digest
// is derived/provenance-only and in_sync (digest-first precedence falls through
// to the weaker repository+tag join).
func TestBuildKubernetesCorrelationDecisionsDerivedTagToDigest(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + ":v1.2.3"
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false),
		k8sSourceTagFact("oci-tag-1", testK8sRegistry, testK8sRepository, "v1.2.3", testK8sDigest, "", false),
	})

	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	decision := kubernetesCorrelationByImageRef(decisions)[objectID+"|"+imageRef+"|"]
	assertKubernetesOutcome(t, decision, KubernetesCorrelationDerived, driftInSync)
	if !decision.ProvenanceOnly {
		t.Fatal("derived tag ProvenanceOnly = false, want true")
	}
	if decision.SourceDigest != testK8sDigest {
		t.Fatalf("source_digest = %q, want %q", decision.SourceDigest, testK8sDigest)
	}
}

// TestBuildKubernetesCorrelationDecisionsAmbiguousTag proves a live tag that
// resolves to multiple deployment-source digests stays ambiguous with both
// candidates recorded and no single source pick.
func TestBuildKubernetesCorrelationDecisionsAmbiguousTag(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + ":latest"
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false),
		k8sSourceTagFact("oci-tag-a", testK8sRegistry, testK8sRepository, "latest", testK8sDigest, "", true),
		k8sSourceTagFact("oci-tag-b", testK8sRegistry, testK8sRepository, "latest", testK8sDigest2, "", true),
	})

	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	decision := kubernetesCorrelationByImageRef(decisions)[objectID+"|"+imageRef+"|"]
	assertKubernetesOutcome(t, decision, KubernetesCorrelationAmbiguous, driftUnknown)
	if !decision.ProvenanceOnly {
		t.Fatal("ambiguous tag ProvenanceOnly = false, want true")
	}
	if decision.SourceDigest != "" {
		t.Fatalf("ambiguous SourceDigest = %q, want empty (no single pick)", decision.SourceDigest)
	}
	if len(decision.CandidateSourceDigests) != 2 {
		t.Fatalf("candidate_source_digests = %v, want 2 candidates", decision.CandidateSourceDigests)
	}
}

// TestBuildKubernetesCorrelationDecisionsUnresolvedMissingSource proves a live
// image reference with no matching deployment-source evidence is unresolved and
// classified missing_source (the live cluster runs an image Eshu has no source
// for).
func TestBuildKubernetesCorrelationDecisionsUnresolvedMissingSource(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + ":v9.9.9"
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false),
	})

	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	decision := kubernetesCorrelationByImageRef(decisions)[objectID+"|"+imageRef+"|"]
	assertKubernetesOutcome(t, decision, KubernetesCorrelationUnresolved, driftMissingSource)
	if !decision.ProvenanceOnly {
		t.Fatal("unresolved ProvenanceOnly = false, want true")
	}
}

// TestBuildKubernetesCorrelationDecisionsStaleTombstonedSource proves a live
// digest that resolves only to a tombstoned deployment-source manifest is
// classified stale/stale_source, never exact.
func TestBuildKubernetesCorrelationDecisionsStaleTombstonedSource(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false),
		k8sSourceManifestFact("oci-dead", testK8sRegistry, testK8sRepository, testK8sDigest, true),
	})

	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	decision := kubernetesCorrelationByImageRef(decisions)[objectID+"|"+imageRef+"|"]
	assertKubernetesOutcome(t, decision, KubernetesCorrelationStale, driftStaleSource)
	if !decision.ProvenanceOnly {
		t.Fatal("stale ProvenanceOnly = false, want true")
	}
	if decision.Outcome == KubernetesCorrelationExact {
		t.Fatal("tombstoned source must never classify exact")
	}
}

// TestBuildKubernetesCorrelationDecisionsRejectsUnparseableImage proves a live
// image reference that cannot be parsed into a repository (no tag, no digest) is
// rejected and suppressed, never promoted.
func TestBuildKubernetesCorrelationDecisionsRejectsUnparseableImage(t *testing.T) {
	t.Parallel()

	// A bare repository reference with no tag and no digest carries no resolvable
	// identity: parseContainerImageRef cannot split it, so it is a weak signal
	// that is rejected, never promoted.
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{"barerepo-no-tag"}, map[string]string{"app": "checkout"}, false),
	})

	var rejected *KubernetesCorrelationDecision
	for i := range decisions {
		if decisions[i].Outcome == KubernetesCorrelationRejected {
			rejected = &decisions[i]
		}
	}
	if rejected == nil {
		t.Fatalf("expected a rejected decision for an unparseable image; decisions=%+v", decisions)
	}
	assertKubernetesOutcome(t, *rejected, KubernetesCorrelationRejected, driftUnknown)
	if !rejected.ProvenanceOnly {
		t.Fatal("rejected ProvenanceOnly = false, want true")
	}
}

// TestBuildKubernetesCorrelationDecisionsOwnerReferenceExactIdentity proves a
// kubernetes_live owner_reference edge is structural exact ownership.
func TestBuildKubernetesCorrelationDecisionsOwnerReferenceExactIdentity(t *testing.T) {
	t.Parallel()

	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", nil, map[string]string{"app": "checkout"}, false),
		k8sRelationshipFact("rel-owner", "owner_reference", "checkout", "checkout-abc"),
	})

	var identity *KubernetesCorrelationDecision
	for i := range decisions {
		if decisions[i].IdentityEdgeKey != "" && decisions[i].Outcome == KubernetesCorrelationExact {
			identity = &decisions[i]
		}
	}
	if identity == nil {
		t.Fatalf("expected an exact owner_reference identity decision; decisions=%+v", decisions)
	}
	if identity.ProvenanceOnly {
		t.Fatal("owner_reference identity ProvenanceOnly = true, want false")
	}
	if identity.NonPromotion != "" {
		t.Fatalf("owner_reference NonPromotion = %q, want empty", identity.NonPromotion)
	}
}

// TestBuildKubernetesCorrelationDecisionsSelectorAmbiguityNeverPromoted is the
// issue #388 acceptance criterion: a label-selector-derived edge that cannot
// prove exact ownership stays ambiguous, is never promoted to exact, and records
// the explicit non-promotion.
func TestBuildKubernetesCorrelationDecisionsSelectorAmbiguityNeverPromoted(t *testing.T) {
	t.Parallel()

	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", nil, map[string]string{"app": "checkout"}, false),
		k8sRelationshipFact("rel-sel", relKubernetesSelectorMatch, "checkout", "checkout-pod-xyz"),
	})

	var selector *KubernetesCorrelationDecision
	for i := range decisions {
		if decisions[i].IdentityEdgeKey != "" && decisions[i].RelationshipType == relKubernetesSelectorMatch {
			selector = &decisions[i]
		}
	}
	if selector == nil {
		t.Fatalf("expected a selector identity decision; decisions=%+v", decisions)
	}
	if selector.Outcome == KubernetesCorrelationExact {
		t.Fatal("selector match must never be promoted to exact")
	}
	assertKubernetesOutcome(t, *selector, KubernetesCorrelationAmbiguous, driftUnknown)
	if !selector.ProvenanceOnly {
		t.Fatal("selector ambiguous ProvenanceOnly = false, want true")
	}
	if selector.NonPromotion == "" {
		t.Fatal("selector ambiguity must record an explicit non_promotion reason")
	}
}

// TestBuildKubernetesCorrelationDecisionsImageDriftSupersededDigest proves a
// live tag that resolves to a source digest the source reports as superseded is
// image_drift (the cluster runs a digest the source tag no longer points at).
func TestBuildKubernetesCorrelationDecisionsImageDriftSupersededDigest(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + ":v1.2.3"
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false),
		// The tag now points at digest2 but previously pointed at digest1; a
		// mutated tag observation carries the previous digest.
		k8sSourceTagFact("oci-tag-mut", testK8sRegistry, testK8sRepository, "v1.2.3", testK8sDigest2, testK8sDigest, true),
	})

	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	decision := kubernetesCorrelationByImageRef(decisions)[objectID+"|"+imageRef+"|"]
	if decision.DriftKind != driftImageDrift {
		t.Fatalf("drift_kind = %q, want %q; outcome=%s reason=%s", decision.DriftKind, driftImageDrift, decision.Outcome, decision.Reason)
	}
	if !decision.ProvenanceOnly {
		t.Fatal("image_drift ProvenanceOnly = false, want true")
	}
}

// TestBuildKubernetesCorrelationDecisionsEmpty proves empty input yields no
// decisions and never panics.
func TestBuildKubernetesCorrelationDecisionsEmpty(t *testing.T) {
	t.Parallel()

	if decisions := BuildKubernetesCorrelationDecisions(nil); len(decisions) != 0 {
		t.Fatalf("decisions = %v, want empty", decisions)
	}
}

// TestBuildKubernetesCorrelationDecisionsDuplicateImageRefDeduplicates proves a
// workload declaring the same image in two containers yields one decision.
func TestBuildKubernetesCorrelationDecisionsDuplicateImageRefDeduplicates(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef, imageRef}, map[string]string{"app": "checkout"}, false),
		k8sSourceManifestFact("oci-1", testK8sRegistry, testK8sRepository, testK8sDigest, false),
	})

	count := 0
	for _, decision := range decisions {
		if decision.ImageRef == imageRef {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicate image ref produced %d decisions, want 1", count)
	}
}

// TestBuildKubernetesCorrelationDecisionsTombstonedWorkloadSkipped proves a
// tombstoned live workload (a deleted Deployment) does not produce image
// correlation decisions.
func TestBuildKubernetesCorrelationDecisionsTombstonedWorkloadSkipped(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFact("pod-dead", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, true),
		k8sSourceManifestFact("oci-1", testK8sRegistry, testK8sRepository, testK8sDigest, false),
	})

	for _, decision := range decisions {
		if decision.ImageRef == imageRef {
			t.Fatalf("tombstoned workload produced an image decision: %+v", decision)
		}
	}
}

// TestBuildKubernetesCorrelationDecisionsWarningAttachedAsEvidence proves a
// kubernetes_live.warning for a workload's resource scope is attached so a
// partial snapshot is visible, not silently read as in_sync.
func TestBuildKubernetesCorrelationDecisionsWarningAttachedAsEvidence(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	decisions := BuildKubernetesCorrelationDecisions([]facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false),
		k8sSourceManifestFact("oci-1", testK8sRegistry, testK8sRepository, testK8sDigest, false),
		k8sWarningFact("warn-1", "ambiguous_selector", "apps/v1/deployments"),
	})

	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
	decision := kubernetesCorrelationByImageRef(decisions)[objectID+"|"+imageRef+"|"]
	if !slices.Contains(decision.Warnings, "ambiguous_selector") {
		t.Fatalf("warnings = %v, want it to contain ambiguous_selector", decision.Warnings)
	}
}

// TestBuildKubernetesCorrelationDecisionsDeterministicOrder proves repeated runs
// over the same facts produce a stable ordering for idempotent batch writes.
func TestBuildKubernetesCorrelationDecisionsDeterministicOrder(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		podTemplateFact("pod-b", "billing", "uid-b", []string{testK8sRegistry + "/team/billing@" + testK8sDigest2}, map[string]string{"app": "billing"}, false),
		podTemplateFact("pod-a", "checkout", "uid-a", []string{testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest}, map[string]string{"app": "checkout"}, false),
		k8sSourceManifestFact("oci-1", testK8sRegistry, testK8sRepository, testK8sDigest, false),
		k8sSourceManifestFact("oci-2", testK8sRegistry, "team/billing", testK8sDigest2, false),
	}
	first := BuildKubernetesCorrelationDecisions(envelopes)
	second := BuildKubernetesCorrelationDecisions(envelopes)
	if len(first) != len(second) {
		t.Fatalf("decision counts differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].WorkloadObjectID != second[i].WorkloadObjectID ||
			first[i].ImageRef != second[i].ImageRef ||
			first[i].Outcome != second[i].Outcome {
			t.Fatalf("non-deterministic ordering at %d: %+v vs %+v", i, first[i], second[i])
		}
	}
}

// TestKubernetesCorrelationHandlerLoadsFactsAndWrites proves the handler loads
// the bounded scope fact kinds, joins the cross-scope active source facts,
// classifies, and writes durable facts.
func TestKubernetesCorrelationHandlerLoadsFactsAndWrites(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	loader := &stubKubernetesCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false),
		},
		activeFacts: []facts.Envelope{
			k8sSourceManifestFact("oci-1", testK8sRegistry, testK8sRepository, testK8sDigest, false),
		},
	}
	writer := &recordingKubernetesCorrelationWriter{}
	handler := KubernetesCorrelationHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-k8s",
		ScopeID:      "k8s://" + testK8sCluster,
		GenerationID: "generation-k8s",
		Domain:       DomainKubernetesCorrelation,
		SourceSystem: "kubernetes_live",
		Cause:        "kubernetes live facts observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteKubernetesCorrelations() calls = %d, want 1", writer.calls)
	}
	if loader.activeCalls != 1 {
		t.Fatalf("ListActiveContainerImageIdentityFacts() calls = %d, want 1", loader.activeCalls)
	}
	if got, want := loader.kindCalls[0], kubernetesCorrelationFactKinds(); !slices.Equal(got, want) {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
	if got, want := result.Domain, DomainKubernetesCorrelation; got != want {
		t.Fatalf("result.Domain = %q, want %q", got, want)
	}
	if result.CanonicalWrites == 0 {
		t.Fatal("result.CanonicalWrites = 0, want > 0")
	}
	// The exact-digest join only resolves because the cross-scope source fact was
	// loaded and merged; an in_sync decision proves the join happened.
	var sawInSync bool
	for _, decision := range writer.write.Decisions {
		if decision.Outcome == KubernetesCorrelationExact && decision.DriftKind == driftInSync {
			sawInSync = true
		}
	}
	if !sawInSync {
		t.Fatalf("expected an exact/in_sync decision from the cross-scope join; decisions=%+v", writer.write.Decisions)
	}
}

// TestKubernetesCorrelationHandlerRejectsWrongDomain proves the handler guards
// its domain, mirroring the #390/#391 dispatch guard.
func TestKubernetesCorrelationHandlerRejectsWrongDomain(t *testing.T) {
	t.Parallel()

	handler := KubernetesCorrelationHandler{
		FactLoader: &stubKubernetesCorrelationFactLoader{},
		Writer:     &recordingKubernetesCorrelationWriter{},
	}
	if _, err := handler.Handle(context.Background(), Intent{Domain: DomainObservabilityCoverageCorrelation}); err == nil {
		t.Fatal("Handle() error = nil, want domain mismatch error")
	}
}

// TestPostgresKubernetesCorrelationWriterPersistsReducerFacts proves the writer
// stores one provenance-only reducer fact per decision through the shared
// canonical insert path with a stable, retry-idempotent identity.
func TestPostgresKubernetesCorrelationWriterPersistsReducerFacts(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresKubernetesCorrelationWriter{DB: db}

	write := KubernetesCorrelationWrite{
		IntentID:     "intent-k8s",
		ScopeID:      "k8s://" + testK8sCluster,
		GenerationID: "generation-k8s",
		SourceSystem: "kubernetes_live",
		Cause:        "kubernetes live facts observed",
		Decisions: []KubernetesCorrelationDecision{
			{
				ClusterID:        testK8sCluster,
				WorkloadObjectID: "k8s://obj",
				ImageRef:         "registry/repo@" + testK8sDigest,
				SourceDigest:     testK8sDigest,
				Outcome:          KubernetesCorrelationExact,
				DriftKind:        driftInSync,
				Reason:           "live image digest matches a deployment-source digest",
				ProvenanceOnly:   false,
				EvidenceFactIDs:  []string{"pod-1", "oci-1"},
			},
		},
	}
	result, err := writer.WriteKubernetesCorrelations(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteKubernetesCorrelations() error = %v, want nil", err)
	}
	if result.FactsWritten != 1 {
		t.Fatalf("FactsWritten = %d, want 1", result.FactsWritten)
	}
	if got, want := db.execs[0].args[3], kubernetesCorrelationFactKind; got != want {
		t.Fatalf("fact_kind = %v, want %v", got, want)
	}
	payload := unmarshalKubernetesCorrelationPayload(t, db.execs[0].args[14])
	if got, want := payload["outcome"], string(KubernetesCorrelationExact); got != want {
		t.Fatalf("outcome = %#v, want %q", got, want)
	}
	if got, want := payload["drift_kind"], driftInSync; got != want {
		t.Fatalf("drift_kind = %#v, want %q", got, want)
	}

	// Idempotency: a second write of the same decision reuses the same fact_id.
	db2 := &fakeWorkloadIdentityExecer{}
	writer2 := PostgresKubernetesCorrelationWriter{DB: db2}
	if _, err := writer2.WriteKubernetesCorrelations(context.Background(), write); err != nil {
		t.Fatalf("second WriteKubernetesCorrelations() error = %v", err)
	}
	if db.execs[0].args[0] != db2.execs[0].args[0] {
		t.Fatalf("fact_id not stable across writes: %v vs %v", db.execs[0].args[0], db2.execs[0].args[0])
	}
}
