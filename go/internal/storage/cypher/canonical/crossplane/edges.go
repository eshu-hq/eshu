// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplane

import (
	"fmt"
)

// Phase names the Crossplane Claim -> XRD SATISFIED_BY edge projection phase
// for grouped-backend statement metadata and diagnostics.
const Phase = "crossplane_satisfied_by_edge"

// EdgeLabel is the fixed relationship type this writer produces, doubling as
// the bounded entity-label tag for statement metadata and the
// edge-projection counter dimension.
const EdgeLabel = "SATISFIED_BY"

// EvidenceSource tags SATISFIED_BY edges written by this reducer domain so
// the prior-generation retract path scopes its delete to this writer's edges
// and never touches an edge owned by another writer.
const EvidenceSource = "reducer/crossplane-satisfied-by"

// SatisfiedByEdgeUpsertCypher batches SATISFIED_BY edge upserts between an
// already-materialized K8sResource node (the Claim) and the CrossplaneXRD
// node it resolved against. Both node labels are fixed, closed-vocabulary
// constants (never data-driven), so unlike the RUNS_IMAGE writer this
// template needs no runtime label validation. The relationship type is the
// static SATISFIED_BY token, kept out of the MERGE property map so NornicDB
// uses its relationship hot path. Two MATCHes precede the MERGE so a row
// whose Claim or XRD node is absent produces no edge and no fabricated node
// — the edge self-heals on a later generation once both endpoints commit.
const SatisfiedByEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (claim:K8sResource {uid: row.claim_uid})
MATCH (xrd:CrossplaneXRD {uid: row.xrd_uid})
MERGE (claim)-[rel:SATISFIED_BY]->(xrd)
SET rel.resolution_mode = row.resolution_mode,
    rel.claim_group = row.claim_group,
    rel.claim_kind = row.claim_kind,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// RetractSatisfiedByEdgesCypher removes this reducer's SATISFIED_BY edges for
// a set of scopes before a fresh generation reprojects them. The scope
// predicate filters on the edge's own scope_id, not either endpoint node's:
// K8sResource and CrossplaneXRD nodes are cross-generation canonical and
// carry no reducer scope_id, so a node-scoped predicate would make the
// retract a silent no-op that leaks stale edges across generations.
const RetractSatisfiedByEdgesCypher = `MATCH (:K8sResource)-[rel:SATISFIED_BY]->(:CrossplaneXRD)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// writeReasons is the single source of truth for the relationship types
// this writer accepts, mirroring sqlRelationshipWriteReasons
// (edge/writer/sql.go). It backs MaterializedEdgeTypes, the
// registry-derived accessor the blast-radius edge-materialization coverage
// registry (go/internal/query/querycontract/edge_materialization_coverage.go)
// merges in, so SATISFIED_BY flips from "no_writer" to materialized:true
// automatically once this writer is wired — no second hand-maintained list.
var writeReasons = map[string]string{
	"SATISFIED_BY": "Crossplane Claim (K8sResource) resolved against exactly one CrossplaneXRD by (group, kind) == (spec.group, spec.claimNames.kind)",
}

// MaterializedEdgeTypes returns a defensive copy of writeReasons: the graph
// relationship types the Crossplane edge writer actually accepts, mapped to
// the write reason recorded on each MERGEd edge. Mirrors
// SQLRelationshipMaterializedEdgeTypes and the canonical/flux leaf's
// MaterializedEdgeTypes.
//
// It returns a copy so a caller cannot mutate the package's source of truth.
func MaterializedEdgeTypes() map[string]string {
	out := make(map[string]string, len(writeReasons))
	for edgeType, reason := range writeReasons {
		out[edgeType] = reason
	}
	return out
}

// CloneRowsWithScope returns a copy of rows with scopeID, generationID, and
// evidenceSource injected onto every row as scope_id / generation_id /
// evidence_source. scope_id is what the prior-generation retract filters on
// (the endpoint nodes carry no reducer scope_id), so omitting it would make
// scope-scoped retract a silent no-op. The input maps are never mutated.
func CloneRowsWithScope(rows []map[string]any, scopeID, generationID, evidenceSource string) []map[string]any {
	cloned := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		clone := make(map[string]any, len(row)+3)
		for key, value := range row {
			clone[key] = value
		}
		clone["scope_id"] = scopeID
		clone["generation_id"] = generationID
		clone["evidence_source"] = evidenceSource
		cloned = append(cloned, clone)
	}
	return cloned
}

// BuildUpsertParts splits scope-tagged row clones into one batched
// MATCH-MATCH-MERGE upsert part per batch, each carrying the phase,
// entity-label, and bounded-summary statement metadata the root adapter
// copies onto its Statements untouched. A batchSize of 0 or less puts all
// rows in a single part. It returns nil for empty rows, so the writer emits
// no statement. The write is idempotent: the same (claim_uid, SATISFIED_BY,
// xrd_uid) converges on one edge across batches, retries, and generations,
// and a missing endpoint is a no-op rather than a fabricated node.
func BuildUpsertParts(rows []map[string]any, scopeID, generationID, evidenceSource string, batchSize int) []Part {
	cloned := CloneRowsWithScope(rows, scopeID, generationID, evidenceSource)
	if len(cloned) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = len(cloned)
	}
	var parts []Part
	for start := 0; start < len(cloned); start += batchSize {
		end := start + batchSize
		if end > len(cloned) {
			end = len(cloned)
		}
		batch := cloned[start:end]
		parts = append(parts, Part{
			Cypher: SatisfiedByEdgeUpsertCypher,
			Parameters: map[string]any{
				"rows":         batch,
				phaseKey:       Phase,
				entityLabelKey: EdgeLabel,
				summaryKey: fmt.Sprintf(
					"edge=%s rows=%d",
					EdgeLabel,
					len(batch),
				),
			},
		})
	}
	return parts
}

// BuildRetractPart returns the single statement-free part that removes this
// writer's SATISFIED_BY edges for the given scopes before a fresh generation
// reprojects them. The delete is scoped to this writer's evidence_source and
// never touches endpoint nodes. The root adapter dispatches it through
// sequential Execute calls, never ExecuteGroup: on the pinned NornicDB
// v1.1.11 a DELETE dispatched through ExecuteGroup / a managed transaction
// under-applies — even a single statement — while the identical statement
// run as an auto-commit transaction (Execute) deletes correctly. See
// docs/public/reference/nornicdb-pitfalls.md.
func BuildRetractPart(scopeIDs []string, generationID, evidenceSource string) Part {
	return Part{
		Cypher:    RetractSatisfiedByEdgesCypher,
		IsRetract: true,
		Parameters: map[string]any{
			"scope_ids":       scopeIDs,
			"evidence_source": evidenceSource,
			phaseKey:          Phase,
			entityLabelKey:    EdgeLabel,
			summaryKey: fmt.Sprintf(
				"edge=%s retract scopes=%d generation=%s",
				EdgeLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}
}
