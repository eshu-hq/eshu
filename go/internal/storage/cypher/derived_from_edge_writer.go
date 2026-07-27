// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// canonicalPhaseProvenanceDerivedFromEdges names the write phase for the
// base-image lineage projection (#5460).
const canonicalPhaseProvenanceDerivedFromEdges = "provenance_derived_from_edges"

// provenanceDerivedFromEdgeLabel tags the bounded entity-label
// statement-metadata dimension for this writer's edge type.
const provenanceDerivedFromEdgeLabel = "DERIVED_FROM"

// canonicalProvenanceDerivedFromCypher upserts a DERIVED_FROM edge from a built
// ContainerImage to the base ContainerImage its repository's Dockerfile
// declared.
//
// BOTH endpoints are matched by digest. ContainerImageIdentityDecision
// (go/internal/reducer/container_image_identity.go) carries only ImageRef and
// Digest, never the OciImageManifest node uid, and
// oci_registry_canonical_writer.go SETs digest on every ContainerImage node it
// writes, so digest is the correct and only available join key on both sides.
// The two MATCHes precede the MERGE so a row whose child or base node is absent
// produces no edge and no fabricated node (the missing-endpoint no-op contract,
// #5472).
//
// attribution_basis records HOW the child was attributed to the base, so a
// later CI/SLSA per-image link can be admitted on this same edge type and
// distinguished from today's Dockerfile-only inference without a schema change.
const canonicalProvenanceDerivedFromCypher = `UNWIND $rows AS row
MATCH (img:ContainerImage {digest: row.digest})
MATCH (base:ContainerImage {digest: row.base_digest})
MERGE (img)-[rel:DERIVED_FROM]->(base)
SET rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source,
    rel.evidence_kinds = row.evidence_kinds,
    rel.attribution_basis = row.attribution_basis,
    rel.source_tool = row.source_tool`

// retractProvenanceDerivedFromEdgesCypher removes this writer's DERIVED_FROM
// edges for one scope+evidence_source before a fresh generation reprojects
// them. Only container_image_identity writes DERIVED_FROM today, so this
// retract has no other domain's edges to avoid. It still filters on
// evidence_source rather than matching every DERIVED_FROM edge, but that
// filter is not a proven cross-domain isolation guarantee: the canonical MERGE
// above matches on (start, end, type) alone and ignores evidence_source, so a
// second writer sharing this edge type would collapse onto the same edge and
// this retract could delete an assertion the other writer still supports
// (#5827, which names DERIVED_FROM explicitly). A second DERIVED_FROM writer
// MUST NOT land until #5827 is fixed.
//
// The MERGE identity also drops scope_id, but unlike BUILT_FROM that does not
// bite here today: containerImageDerivedFromRows takes an owning repository and
// returns nil when it is empty, so a scope that merely observes both endpoints
// (an OCI or cloud scope) projects nothing, and each edge has one deterministic
// owning scope. That restriction is the fix for exactly this failure mode and
// is pinned by TestProjectContainerImageDerivedFromEdgesNonRepoScopeWritesNothing
// -- do not loosen it. The residual case is narrower: two repository scopes both
// credited with building the same digest, both resolving to the same base.
const retractProvenanceDerivedFromEdgesCypher = `MATCH (:ContainerImage)-[rel:DERIVED_FROM]->(:ContainerImage)
WHERE rel.scope_id = $scope_id
  AND rel.evidence_source = $evidence_source
DELETE rel`

// WriteDerivedFromEdges upserts DERIVED_FROM edges for the given rows. Each row
// MUST carry digest, base_digest, and attribution_basis. scopeID, generationID,
// and evidenceSource are stamped onto every edge.
func (w *ProvenanceEdgeWriter) WriteDerivedFromEdges(
	ctx context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("provenance edge writer executor is required")
	}

	cloned := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		cloned = append(cloned, cloneProvenanceRow(row, scopeID, generationID, evidenceSource))
	}

	stmts := tagProvenanceStatements(
		buildBatchedStatements(canonicalProvenanceDerivedFromCypher, cloned, w.batchSize),
		canonicalPhaseProvenanceDerivedFromEdges, provenanceDerivedFromEdgeLabel, "target=ContainerImage",
	)
	// Sequential auto-commit, never ExecuteGroup: this same-label two-MATCH-MERGE
	// trips NornicDB's UnwindMergeChain fast-path, which throws instead of
	// no-op'ing under a managed transaction when an endpoint node is not yet
	// committed (dispatchSequential documents the full rationale, #5460).
	return w.dispatchSequential(ctx, stmts)
}

// RetractDerivedFromEdges removes this writer's DERIVED_FROM edges for one
// scope+evidence_source. See RetractPublishesEdges for the
// sequential-autocommit dispatch rationale.
func (w *ProvenanceEdgeWriter) RetractDerivedFromEdges(
	ctx context.Context,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	return w.retract(
		ctx, retractProvenanceDerivedFromEdgesCypher,
		canonicalPhaseProvenanceDerivedFromEdges, provenanceDerivedFromEdgeLabel,
		scopeID, generationID, evidenceSource,
	)
}
