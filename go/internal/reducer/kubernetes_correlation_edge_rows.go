// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

// kubernetesRunsImageRelType is the canonical relationship type for the live
// Kubernetes workload -> deployment-source image edge (issue #388 PR3). It is a
// single static token from a closed vocabulary; the edge writer interpolates it
// into the relationship-type position (which cannot be parameterized) and
// validates it against this exact constant, so no upstream value can fabricate a
// different relationship type. A live workload "RUNS_IMAGE" the resolved
// digest-addressed source node it was observed running.
const kubernetesRunsImageRelType = string(edgetype.RunsImage)

// kubernetesCorrelationEdgeTally is the bounded, honest accounting surface for
// the RUNS_IMAGE edge projection (issue #388 PR3, mirroring the #805 §6 and #391
// PR3 tallies). Both the metric and the completion log read materialized vs
// skipped counts keyed by the closed image join-mode vocabulary (only digest is
// edge-eligible), so cardinality stays bounded.
type kubernetesCorrelationEdgeTally struct {
	// materialized counts RUNS_IMAGE edges written, keyed by join mode. Only
	// joinModeDigest is edge-eligible (exact, digest-addressed source node), so in
	// practice this carries a single key; keeping it a map mirrors the AWS/COVERS
	// tallies and leaves room for a future exact join mode without a shape change.
	materialized map[string]int
	// skipped counts exact correlation decisions that did NOT produce an edge
	// because the source digest resolved no canonical OCI node uid in this
	// generation (e.g. the digest was only ever observed via a tag observation,
	// which is not a digest-addressed node). Counted, never silently dropped, and
	// never written as a dangling edge to a non-existent node.
	skipped map[string]int
}

func newKubernetesCorrelationEdgeTally() kubernetesCorrelationEdgeTally {
	return kubernetesCorrelationEdgeTally{
		materialized: make(map[string]int),
		skipped:      make(map[string]int),
	}
}

// totalSkipped returns the count of exact correlation decisions that resolved no
// canonical source node and therefore produced no edge.
func (t kubernetesCorrelationEdgeTally) totalSkipped() int {
	total := 0
	for _, count := range t.skipped {
		total += count
	}
	return total
}

// ExtractKubernetesCorrelationEdgeRows builds canonical RUNS_IMAGE edge rows from
// the scope generation's facts. It runs the same pure classifier the PR1
// fact-only read model uses (BuildKubernetesCorrelationDecisions) and promotes to
// a graph edge ONLY the exact image decisions that resolved BOTH endpoints to a
// stable canonical node uid: the live KubernetesWorkload node (uid = the
// collector-emitted object_id) and the digest-addressed OCI source node (uid
// resolved via SourceImageDigestJoinIndex). Everything else stays
// provenance-only and fabricates no edge:
//
//   - derived / ambiguous / unresolved / stale / rejected image outcomes are not
//     exact truth and never reach the graph;
//   - an exact owner_reference identity decision is a workload->workload structural
//     edge, not a workload->image edge, and its owner target is not guaranteed to
//     have a KubernetesWorkload node, so this image-edge slice never anchors on it
//     (it produces no SourceDigest, so it is naturally excluded);
//   - an exact image decision whose source digest resolves no canonical node uid
//     (e.g. a digest only ever observed via a mutable tag observation, which is not
//     a digest-addressed node) is counted skipped, never written as a dangling
//     edge to a non-existent node.
//
// Rows are deduplicated by (workload_uid, rel_type, source_uid) and sorted
// deterministically so the batched MERGE write is byte-stable across retries and
// reprojections.
func ExtractKubernetesCorrelationEdgeRows(
	envelopes []facts.Envelope,
) ([]map[string]any, kubernetesCorrelationEdgeTally) {
	tally := newKubernetesCorrelationEdgeTally()
	decisions := BuildKubernetesCorrelationDecisions(envelopes)
	if len(decisions) == 0 {
		return nil, tally
	}

	sourceIndex := BuildSourceImageDigestJoinIndex(envelopes)

	type edgeKey struct {
		workload string
		relType  string
		source   string
	}
	seen := make(map[edgeKey]struct{}, len(decisions))
	rows := make([]map[string]any, 0, len(decisions))

	for _, decision := range decisions {
		if !kubernetesDecisionIsImageEdgeEligible(decision) {
			continue
		}
		sourceNode, ok := sourceIndex.ResolveDigestNode(decision.SourceDigest)
		if !ok {
			// The classifier proved an exact digest match against source evidence,
			// but no digest-addressed canonical node carries that digest in this
			// generation (e.g. tag-only evidence). Count it so an operator can see
			// exact coverage the graph cannot anchor yet, rather than writing a
			// dangling edge to a node that does not exist.
			tally.skipped[joinModeDigest]++
			continue
		}
		key := edgeKey{
			workload: decision.WorkloadObjectID,
			relType:  kubernetesRunsImageRelType,
			source:   sourceNode.UID,
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		tally.materialized[joinModeDigest]++
		rows = append(rows, map[string]any{
			"workload_uid":    decision.WorkloadObjectID,
			"source_uid":      sourceNode.UID,
			"source_label":    sourceNode.Label,
			"rel_type":        kubernetesRunsImageRelType,
			"resolution_mode": joinModeDigest,
			"image_ref":       decision.ImageRef,
			"source_digest":   decision.SourceDigest,
		})
	}

	if len(rows) == 0 {
		return nil, tally
	}

	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["rel_type"]) + ":" +
			anyToString(rows[a]["workload_uid"]) + "->" +
			anyToString(rows[a]["source_uid"])
		right := anyToString(rows[b]["rel_type"]) + ":" +
			anyToString(rows[b]["workload_uid"]) + "->" +
			anyToString(rows[b]["source_uid"])
		return left < right
	})
	return rows, tally
}

// kubernetesDecisionIsImageEdgeEligible reports whether a correlation decision
// proves a canonical RUNS_IMAGE edge. Only an exact image decision that resolved
// both a live workload node uid (object_id) and a source digest is eligible; the
// source node uid is resolved separately so a digest with no canonical node is
// skipped rather than dangled. An exact owner_reference identity decision carries
// no SourceDigest and is therefore naturally excluded — it is a workload->workload
// structural edge, deferred from this image-edge slice.
func kubernetesDecisionIsImageEdgeEligible(decision KubernetesCorrelationDecision) bool {
	return decision.Outcome == KubernetesCorrelationExact &&
		decision.WorkloadObjectID != "" &&
		decision.SourceDigest != "" &&
		decision.IdentityEdgeKey == ""
}
