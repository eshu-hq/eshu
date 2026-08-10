// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "strings"

// The repo_dependency family's materialized-edge registry (#5999).
//
// It lives beside the other materialized-edge registries rather than in
// canonical_relationships.go, which owns the write and retract templates and had
// reached the repository's 500-line cap.

// RepoDependencyMaterializedEdgeTypes returns the relationship types the
// repo_dependency family materializes, keyed by type and mapped to a short
// reason (#5999).
//
// Seven types. Six are SPLIT OUT OF repoDependencyRelationshipEdgeTypes rather
// than re-listed, so the set cannot drift from the alternation the live retract
// actually deletes. RUNS_ON is added explicitly because it is reaped by a
// separate retract role (retractRepoRunsOnEdgesCypher) against a different
// topology: the intent row is repo-scoped, but the edge itself is
// (WorkloadInstance)-[:RUNS_ON]->(Platform), reached through DEFINES/INSTANCE_OF
// edges that workload materialization writes. A missing RUNS_ON can therefore
// root-cause in that other domain rather than this one.
//
// Do not read retractRepoDependencyEdgesCypher as this family's retract. It
// names DEPENDS_ON alone and looks authoritative, but its only user,
// BuildRetractRepoDependencyEdges, has no production caller — it is dead. The
// live path is buildRepoDependencySplitRetractStatements, whose role 1 deletes
// the six-type alternation above and whose role 2 deletes RUNS_ON.
//
// Deliberately excluded, both written under this domain:
//
//   - HAS_DEPLOYMENT_EVIDENCE and EVIDENCES_REPOSITORY_RELATIONSHIP. Their
//     EvidenceArtifact endpoint uid embeds the generation id and ordinal, so it
//     changes on every reprojection and no exact hand-derived edge set can pin
//     them. Covering them needs an id-normalized assertion, not this exact-set
//     gate. Stated residual gap, not an oversight.
//   - TARGETS_ENVIRONMENT. It is written here from an EvidenceArtifact source
//     and by kubernetes_namespace_node_writer.go from a KubernetesNamespace one,
//     so the two ARE label-separable and materialized_edge_endpoints.go could now
//     express that. It stays excluded on the same ground as the two edges below:
//     its source EvidenceArtifact is MERGEd with a generation-embedded id, so its
//     endpoint identity changes on every reprojection and no exact hand-derived
//     set can pin it. Do not lift this exclusion on the strength of the endpoint
//     table alone — the blocker is endpoint identity, not label ambiguity.
func RepoDependencyMaterializedEdgeTypes() map[string]string {
	alternatives := strings.Split(repoDependencyRelationshipEdgeTypes, "|")
	out := make(map[string]string, len(alternatives)+1)
	for _, edgeType := range alternatives {
		if edgeType = strings.TrimSpace(edgeType); edgeType != "" {
			out[edgeType] = "repo-to-repo relationship (retractRepoRelationshipEdgesCypher role 1)"
		}
	}
	out["RUNS_ON"] = "workload-instance-to-platform edge (batchCanonicalRunsOnUpsertCypher, retractRepoRunsOnEdgesCypher role 2)"
	return out
}
