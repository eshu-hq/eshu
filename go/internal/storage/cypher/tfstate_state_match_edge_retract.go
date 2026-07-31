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
//
// `s.uid IN $uids` (#5623 P1 review finding) additionally restricts this
// retract to state resources whose ownership was ACTUALLY RESOLVED this
// cycle -- see terraformStateMatchesConfigEdgeRetractStatements' doc comment
// for why "this generation upserted the node" is not the same fact as "we
// know its correct owner this cycle," and why conflating the two wiped a
// still-valid edge on an ordinary transient resolver error.
const canonicalTerraformStateMatchesConfigEdgeRetractCypher = `MATCH (s:TerraformStateResource)<-[e:MATCHES_STATE]-(:TerraformResource)
WHERE s.scope_id = $scope_id
  AND s.generation_id = $generation_id
  AND s.uid IN $uids
  AND e.evidence_source = 'projector/tfstate'
  AND e.generation_id <> $generation_id
DELETE e`

// terraformStateMatchesConfigEdgeRetractStatements builds the generation-gated
// stale-MATCHES_STATE-edge retraction described on
// canonicalTerraformStateMatchesConfigEdgeRetractCypher.
//
// Skipped ONLY on the scope's first generation, matching every other tfstate
// retraction in this package (terraformStateResourceRetractStatements): no
// prior generation ever wrote an edge for this scope, so nothing can be
// stale yet.
//
// UNLIKE terraformStateResourceRetractStatements (the node-level retract),
// this statement runs on delta cycles too (#5623 fix; an earlier version of
// this comment copied the node-level DeltaProjection skip here, reasoning
// that a delta cycle "carries none" of mat.TerraformStateResources -- that
// reasoning does not actually transfer, and skipping was a tenant-visibility
// bug, not a no-op):
//
//   - The node-level retract is a whole-current-label-population DETACH DELETE
//     bounded only by `generation_id <> $generation_id`; if it ran on a delta
//     cycle that carries a strict subset of the scope's state resources, it
//     would delete every resource this delta did NOT touch (mass deletion),
//     because it has no positive list of "backends untouched this cycle" to
//     exclude. THAT is the danger the DeltaProjection skip exists to prevent,
//     and it is real for the node-level retract.
//   - This edge-level retract's WHERE clause is differently shaped and does
//     NOT have that danger: `s.generation_id = $generation_id` restricts it to
//     edges attached to a state resource ALREADY confirmed upserted THIS EXACT
//     generation (buildTerraformStateStatements always runs the resource
//     upsert before this retract, on every cycle, delta or full -- see that
//     function's phase-order doc). A resource this delta cycle did not touch
//     still carries an OLDER generation_id and never matches the anchor, so
//     its edges (stale or not) are correctly left alone. There is no
//     "positive list" problem here: the anchor itself IS the positive list.
//   - Skipping this retract on delta cycles therefore did not prevent mass
//     deletion (nothing here can mass-delete); it only left a real window: if
//     a state resource's resolved OwningRepoID changes on a delta cycle,
//     terraformStateMatchesConfigEdgeStatements' MERGE (which has NO
//     DeltaProjection guard and unconditionally writes the new edge every
//     cycle) creates the new edge immediately, but the old edge to the
//     now-orphaned repo survived, undeleted, until the next full
//     reconciliation (mat.ReconciliationProjection, hours away per
//     ESHU_REPO_RECONCILE_INTERVAL_HOURS). During that window the state
//     resource carried two MATCHES_STATE edges to two different repos
//     simultaneously, and infra_scope_grant.go's scope predicate (#5623) --
//     which assumes at most one edge -- admitted the resource for EITHER
//     repo's scoped grant, including the one that no longer owns it: a
//     tenant-visibility leak. Running the retract every cycle (except the
//     first) keeps "at most one MATCHES_STATE edge per state resource" true
//     at the end of every generation, not just every full reconciliation.
//
// UID-FILTERED (#5623 P1 review, then its own P1 follow-up finding), not
// "every row this generation upserted": this retract only considers state
// resources whose OwnershipOutcome this cycle is NOT
// projector.TerraformStateOwnershipTransientFailure. "This generation
// upserted the node" and "we know its correct owner this cycle" are DIFFERENT
// facts -- resolveTerraformStateOwnership's resolver
// (TerraformStateOwnershipResolver.ResolveOwningRepoID) can fail for an
// ordinary transient Postgres timeout or pool exhaustion, and the node still
// gets upserted with an empty OwningRepoID that cycle regardless.
//
// The filter went through two shapes before landing here, and both matter to
// understand why it looks the way it does:
//
//  1. (pre-#5623-P1) No filter at all -- every row this generation upserted
//     was retract-eligible, including rows whose ownership resolution merely
//     failed transiently. This wiped a still-correct MATCHES_STATE edge on an
//     ordinary resolver hiccup: an accuracy regression (under-authorization,
//     never a leak, but wrong graph truth on every affected delta cycle
//     instead of only during genuine ownership changes).
//  2. (#5623 P1, first pass) `row.OwningRepoID != ""` -- correctly excluded
//     transient failures, but ALSO excluded the two AUTHORITATIVE
//     "not resolved" answers (tfstatebackend.ErrNoConfigRepoOwnsBackend,
//     tfstatebackend.ErrAmbiguousBackendOwner), because those also leave
//     OwningRepoID empty. A backend that previously resolved to repo A and
//     later became unowned or ambiguous kept repo A's MATCHES_STATE edge
//     indefinitely -- the #5623 P0 tenant-visibility leak, reintroduced
//     through this narrower door: infra_scope_grant.go's scope predicate kept
//     authorizing repo A even though it was no longer the unique owner.
//  3. (here) `row.OwnershipOutcome != TerraformStateOwnershipTransientFailure`
//     -- the outcome enum makes the three-way distinction explicit instead of
//     inferring it from OwningRepoID alone: TransientFailure (including "no
//     resolver wired" / "blank backend identity") is excluded (preserve);
//     Resolved, NoOwner, and AmbiguousOwner are all included (retract-
//     eligible), because all three are authoritative answers this cycle --
//     Resolved may point at a DIFFERENT repo than before, and NoOwner /
//     AmbiguousOwner both mean "not uniquely owned by the prior repo
//     anymore," which is exactly the condition that must invalidate a stale
//     edge.
//
// This is symmetric with terraformStateMatchesConfigEdgeStatements' own
// MERGE, which excludes OwningRepoID == "" rows from writing a new edge
// regardless of outcome -- NoOwner and AmbiguousOwner rows are retract-
// eligible here but never MERGE-eligible there, which is exactly "the edge
// disappears and nothing replaces it," the correct result for both.
//
// Batched by w.batchSize uids per statement, mirroring
// terraformStateResourceMigrationStatements' own uid-batching (same file
// family, tfstate_canonical_writer_retract.go) rather than inventing a new
// batching shape.
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
	if mat.FirstGeneration {
		return nil
	}

	uids := make([]string, 0, len(mat.TerraformStateResources))
	for _, row := range mat.TerraformStateResources {
		if row.OwnershipOutcome == projector.TerraformStateOwnershipTransientFailure {
			continue
		}
		uids = append(uids, row.UID)
	}
	if len(uids) == 0 {
		return nil
	}

	var statements []Statement
	for start := 0; start < len(uids); start += w.batchSize {
		end := start + w.batchSize
		if end > len(uids) {
			end = len(uids)
		}
		statements = append(statements, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    canonicalTerraformStateMatchesConfigEdgeRetractCypher,
			Parameters: map[string]any{
				"scope_id":                       mat.ScopeID,
				"generation_id":                  mat.GenerationID,
				"uids":                           uids[start:end],
				StatementMetadataPhaseKey:        canonicalPhaseTerraformState,
				StatementMetadataEntityLabelKey:  "MATCHES_STATE",
				StatementMetadataScopeIDKey:      mat.ScopeID,
				StatementMetadataGenerationIDKey: mat.GenerationID,
				StatementMetadataSummaryKey:      "retract_stale_matches_state_edge",
			},
			Drain: true,
		})
	}
	return statements
}
