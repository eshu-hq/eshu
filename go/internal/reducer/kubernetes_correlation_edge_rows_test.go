// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// k8sSourceManifestWithNode builds an oci_registry.image_manifest fact that
// satisfies BOTH join paths the edge slice needs: registry+repository+digest so
// the correlation classifier resolves the live digest to an exact source, and a
// descriptor_id so the SourceImageDigestJoinIndex resolves the same digest to the
// canonical OCI node uid the projector committed. Without descriptor_id (or an
// oci-registry:// repository_id) the join index has no node uid to anchor the
// edge target on, which is exactly the digest->uid bridge #1105 added.
func k8sSourceManifestWithNode(factID, registry, repository, digest, descriptorID string, tombstone bool) facts.Envelope {
	return facts.Envelope{
		FactID:      factID,
		FactKind:    facts.OCIImageManifestFactKind,
		IsTombstone: tombstone,
		Payload: map[string]any{
			"registry":      registry,
			"repository":    repository,
			"repository_id": "oci-registry://" + registry + "/" + repository,
			"digest":        digest,
			"descriptor_id": descriptorID,
		},
	}
}

func testCheckoutObjectID() string {
	return "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/checkout"
}

func testCheckoutDescriptorID() string {
	return "oci-descriptor://" + testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
}

// TestExtractKubernetesCorrelationEdgeRowsExactDigestMaterializes proves that a
// live workload whose image digest matches an active deployment-source digest
// materializes exactly one RUNS_IMAGE edge from the workload node to the resolved
// OCI source node, with a deterministic, idempotent key.
func TestExtractKubernetesCorrelationEdgeRowsExactDigestMaterializes(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	envelopes := []facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false),
		k8sSourceManifestWithNode("oci-1", testK8sRegistry, testK8sRepository, testK8sDigest, testCheckoutDescriptorID(), false),
	}

	rows, tally := ExtractKubernetesCorrelationEdgeRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 RUNS_IMAGE edge; tally=%+v", len(rows), tally)
	}
	row := rows[0]
	if got := anyToString(row["workload_uid"]); got != testCheckoutObjectID() {
		t.Fatalf("workload_uid = %q, want %q (the KubernetesWorkload node uid = object_id)", got, testCheckoutObjectID())
	}
	if got := anyToString(row["source_uid"]); got != testCheckoutDescriptorID() {
		t.Fatalf("source_uid = %q, want %q (the resolved OCI node uid)", got, testCheckoutDescriptorID())
	}
	if got := anyToString(row["rel_type"]); got != kubernetesRunsImageRelType {
		t.Fatalf("rel_type = %q, want %q", got, kubernetesRunsImageRelType)
	}
	if got := anyToString(row["source_label"]); got != sourceImageNodeLabelManifest {
		t.Fatalf("source_label = %q, want %q", got, sourceImageNodeLabelManifest)
	}
	if tally.materialized[joinModeDigest] != 1 {
		t.Fatalf("materialized[digest] = %d, want 1; tally=%+v", tally.materialized[joinModeDigest], tally)
	}
}

// TestExtractKubernetesCorrelationEdgeRowsExactOnly proves only exact digest
// outcomes promote to edges. Derived (tag->single digest), ambiguous, unresolved,
// stale, and rejected stay provenance-only and fabricate no edge.
func TestExtractKubernetesCorrelationEdgeRowsExactOnly(t *testing.T) {
	t.Parallel()

	derivedRef := testK8sRegistry + "/" + testK8sRepository + ":v1.2.3"
	ambiguousRef := testK8sRegistry + "/" + testK8sRepository + ":latest"
	unresolvedRef := testK8sRegistry + "/" + testK8sRepository + ":v9.9.9"
	rejectedRef := "::::not-a-ref"
	envelopes := []facts.Envelope{
		podTemplateFact("pod-derived", "derived", "uid-d", []string{derivedRef}, map[string]string{"app": "d"}, false),
		k8sSourceTagFact("tag-derived", testK8sRegistry, testK8sRepository, "v1.2.3", testK8sDigest, "", false),
		podTemplateFact("pod-ambig", "ambig", "uid-a", []string{ambiguousRef}, map[string]string{"app": "a"}, false),
		k8sSourceTagFact("tag-a", testK8sRegistry, testK8sRepository, "latest", testK8sDigest, "", true),
		k8sSourceTagFact("tag-b", testK8sRegistry, testK8sRepository, "latest", testK8sDigest2, "", true),
		podTemplateFact("pod-unres", "unres", "uid-u", []string{unresolvedRef}, map[string]string{"app": "u"}, false),
		podTemplateFact("pod-reject", "reject", "uid-r", []string{rejectedRef}, map[string]string{"app": "r"}, false),
	}

	rows, _ := ExtractKubernetesCorrelationEdgeRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 (only exact digest outcomes promote to edges)", len(rows))
	}
}

// TestExtractKubernetesCorrelationEdgeRowsStaleNoEdge proves a live digest that
// resolves only to a tombstoned source produces no edge and is not a dangling
// edge: the source node no longer exists, so the join index does not resolve it.
func TestExtractKubernetesCorrelationEdgeRowsStaleNoEdge(t *testing.T) {
	t.Parallel()

	imageRef := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	envelopes := []facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false),
		k8sSourceManifestWithNode("oci-1", testK8sRegistry, testK8sRepository, testK8sDigest, testCheckoutDescriptorID(), true),
	}

	rows, tally := ExtractKubernetesCorrelationEdgeRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 (tombstoned source is stale, never exact)", len(rows))
	}
	if tally.materialized[joinModeDigest] != 0 {
		t.Fatalf("materialized[digest] = %d, want 0", tally.materialized[joinModeDigest])
	}
}

// TestExtractKubernetesCorrelationEdgeRowsExactButDigestUnresolvableSkips proves
// the no-dangle contract: an exact correlation decision whose source digest has
// no resolvable canonical node (the classifier matched via a tag-only digest
// observation that carries no node uid) is counted skipped, never written as an
// edge to a non-existent node.
func TestExtractKubernetesCorrelationEdgeRowsExactButDigestUnresolvableSkips(t *testing.T) {
	t.Parallel()

	// The live ref names a digest. A tag observation carries that digest (so the
	// classifier resolves it exact), but tag observations are not digest-addressed
	// node facts, so the SourceImageDigestJoinIndex has no node uid for it.
	imageRef := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	envelopes := []facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false),
		k8sSourceTagFact("tag-1", testK8sRegistry, testK8sRepository, "v1", testK8sDigest, "", false),
	}

	rows, tally := ExtractKubernetesCorrelationEdgeRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 (no resolvable source node = no dangling edge)", len(rows))
	}
	if tally.skipped[joinModeDigest] != 1 {
		t.Fatalf("skipped[digest] = %d, want 1 (exact decision with unresolvable source node)", tally.skipped[joinModeDigest])
	}
}

// TestExtractKubernetesCorrelationEdgeRowsOwnerReferenceNoImageEdge proves an
// owner_reference exact identity decision does NOT materialize an image edge: its
// endpoints are two K8s objects (object_id->object_id), not a workload->image
// pair, and the owner target is not guaranteed to have a KubernetesWorkload node.
// This slice edges only the resolved image; owner_reference is deferred.
func TestExtractKubernetesCorrelationEdgeRowsOwnerReferenceNoImageEdge(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		k8sRelationshipFact("rel-1", relKubernetesOwnerReference, "checkout", "checkout-pod-abc"),
	}

	rows, _ := ExtractKubernetesCorrelationEdgeRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 (owner_reference is not an image edge in this slice)", len(rows))
	}
}

// TestExtractKubernetesCorrelationEdgeRowsDeduplicatesAndSorts proves duplicate
// exact decisions for the same (workload, source) collapse to one edge and the
// rows are deterministically ordered so the batched write is byte-stable across
// retries and reprojections.
func TestExtractKubernetesCorrelationEdgeRowsDeduplicatesAndSorts(t *testing.T) {
	t.Parallel()

	refA := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	repoB := "team/api"
	digestB := testK8sDigest2
	descB := "oci-descriptor://" + testK8sRegistry + "/" + repoB + "@" + digestB
	refB := testK8sRegistry + "/" + repoB + "@" + digestB
	envelopes := []facts.Envelope{
		// Two workloads referencing image B, one referencing A; the second B
		// container is a duplicate of the first within the same workload.
		podTemplateFact("pod-z", "zeta", "uid-z", []string{refB, refB}, map[string]string{"app": "z"}, false),
		podTemplateFact("pod-a", "alpha", "uid-a", []string{refA}, map[string]string{"app": "a"}, false),
		k8sSourceManifestWithNode("oci-a", testK8sRegistry, testK8sRepository, testK8sDigest, testCheckoutDescriptorID(), false),
		k8sSourceManifestWithNode("oci-b", testK8sRegistry, repoB, digestB, descB, false),
	}

	rows, _ := ExtractKubernetesCorrelationEdgeRows(envelopes)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 distinct edges (duplicate B container collapsed)", len(rows))
	}
	// Deterministic order: sorted by (rel_type, workload_uid, source_uid). The
	// alpha workload's object_id sorts before zeta's.
	first := anyToString(rows[0]["workload_uid"])
	second := anyToString(rows[1]["workload_uid"])
	if first >= second {
		t.Fatalf("rows not sorted by workload_uid: %q then %q", first, second)
	}
}

// TestExtractKubernetesCorrelationEdgeRowsEmpty proves empty input yields no rows
// and no panic.
func TestExtractKubernetesCorrelationEdgeRowsEmpty(t *testing.T) {
	t.Parallel()

	rows, tally := ExtractKubernetesCorrelationEdgeRows(nil)
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 for empty input", len(rows))
	}
	if len(tally.materialized) != 0 || len(tally.skipped) != 0 {
		t.Fatalf("tally not empty for empty input: %+v", tally)
	}
}
