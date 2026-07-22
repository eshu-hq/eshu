// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// canonicalTerraformStateMatchesConfigEdgeRetractCypher deletes a stale
// MATCHES_STATE edge whose TerraformStateResource endpoint survived this
// generation's refresh (s.generation_id = $generation_id -- it was NOT swept
// by terraformStateResourceRetractStatements) but whose edge was not
// rewritten by canonicalTerraformStateMatchesConfigEdgeCypher this cycle
// (#5443 P1 review finding: no statement previously deleted a pre-existing
// MATCHES_STATE edge). Two production cases produce exactly this shape, and
// neither is caught by node retraction because both endpoints survive --
// only the relationship between them is now wrong:
//
//  1. The state resource's resolved OwningRepoID changed to a different
//     config repo between generations (resolveTerraformStateOwnership).
//     terraformStateMatchesConfigEdgeStatements' MERGE anchors the NEW
//     (owning_repo_id, address) pair this cycle, leaving the OLD edge to the
//     now-orphaned TerraformResource untouched.
//  2. A previously-unique (repo_id, address) pair became ambiguous
//     (resolveTerraformStateConfigMatchAmbiguity): the row is flagged so
//     terraformStateMatchesConfigEdgeStatements excludes it from this
//     cycle's MERGE rows entirely, again leaving the OLD edge in place.
//
// Anchored on TerraformStateResource (s), not the config TerraformResource
// side: s.uid is the only endpoint with a stable per-generation identity
// across an OwningRepoID change -- the config endpoint a stale edge points
// at is, by definition, no longer the row's current match target, so it
// cannot be resolved from this cycle's row data at all. TerraformStateResource's
// uid uniqueness constraint indexes this label; a TerraformResource-anchored
// MATCH here would have no equivalent anchor for the "no longer matches"
// case. Direction is preserved from canonicalTerraformStateMatchesConfigEdgeCypher's
// own (c)-[e:MATCHES_STATE]->(s): matching s first with the reversed `<-`
// arrow selects the identical edge, never a different one.
const canonicalTerraformStateMatchesConfigEdgeRetractCypher = `MATCH (s:TerraformStateResource)<-[e:MATCHES_STATE]-(:TerraformResource)
WHERE s.scope_id = $scope_id
  AND s.generation_id = $generation_id
  AND e.evidence_source = 'projector/tfstate'
  AND e.generation_id <> $generation_id
DELETE e`

// terraformStateMatchesConfigEdgeRetractStatements builds the generation-gated
// stale-MATCHES_STATE-edge retraction described on
// canonicalTerraformStateMatchesConfigEdgeRetractCypher.
//
// Skipped on the scope's first generation, matching every other tfstate
// retraction in this package (terraformStateResourceRetractStatements): no
// prior generation ever wrote an edge for this scope, so nothing can be
// stale yet. Also skipped on a delta cycle (mat.DeltaProjection), for the
// exact reason terraformStateResourceRetractStatements documents:
// mat.TerraformStateResources is populated only from terraform_state
// envelopes present in THIS materialization's input
// (tfstate_canonical.go's extractTerraformStateRows), so a delta cycle
// triggered by an unrelated file edit carries none, and the
// `s.generation_id = $generation_id` anchor would then never select any
// state resource -- correctly a no-op, but the DeltaProjection guard is kept
// explicit anyway to match this package's own established precedent rather
// than rely on that incidental emptiness. Genuine MATCHES_STATE edge changes
// missed by a delta cycle are still caught by the periodic full
// reconciliation generation (mat.ReconciliationProjection forces
// DeltaProjection=false), the same mechanism the node retraction relies on.
//
// Emitted with Drain=true and an empty DrainVar: this is a relationship
// DELETE mixed into the terraform_state phase alongside sibling MERGE
// upserts, the same NornicDB grouped-ExecuteWrite silent-no-op class (#4476)
// the Atlantis, Flux, GitLab, and Helm structural-edge retractions already
// guard against (canonical_atlantis_edges.go et al. -- see writer.go's
// Statement.DrainVar doc comment for the bounded-mixed-phase-relationship-
// retract convention this follows). buildTerraformStateStatements runs this
// BEFORE terraformStateMatchesConfigEdgeStatements' MERGE, matching that
// precedent's retract-then-MERGE ordering.
func (w *CanonicalNodeWriter) terraformStateMatchesConfigEdgeRetractStatements(mat projector.CanonicalMaterialization) []Statement {
	if mat.FirstGeneration || mat.DeltaProjection {
		return nil
	}
	return []Statement{
		{
			Operation: OperationCanonicalRetract,
			Cypher:    canonicalTerraformStateMatchesConfigEdgeRetractCypher,
			Parameters: map[string]any{
				"scope_id":                       mat.ScopeID,
				"generation_id":                  mat.GenerationID,
				StatementMetadataPhaseKey:        canonicalPhaseTerraformState,
				StatementMetadataEntityLabelKey:  "MATCHES_STATE",
				StatementMetadataScopeIDKey:      mat.ScopeID,
				StatementMetadataGenerationIDKey: mat.GenerationID,
				StatementMetadataSummaryKey:      "retract_stale_matches_state_edge",
			},
			Drain: true,
		},
	}
}
