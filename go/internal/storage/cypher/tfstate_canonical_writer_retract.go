// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// terraformStateResourceMigrationCypher relabels a pre-#5443
// TerraformResource node to TerraformStateResource when its uid reappears in
// the current tfstate materialization batch and it still carries
// evidence_source = 'projector/tfstate' (the state writer's own marker,
// never written by the config-side generic entity pipeline). This is the
// ONLY migration shape used: a single MATCH ... SET ... REMOVE statement, no
// UNWIND, no MERGE.
//
// Two backend facts were verified directly against the pinned NornicDB
// module (github.com/orneryd/nornicdb v1.0.45 per go/go.mod, resolved via
// `go list -m -f '{{.Dir}}'`, not a fork checkout) before writing this shape,
// mirroring the #5441 review round 9 tracing methodology this file's sibling
// (tfstate_canonical_writer.go) already documents:
//
//  1. Cypher-layer routing: pkg/cypher/executor.go's dispatch checks `hasSet`
//     (line ~2557) BEFORE the `REMOVE` check (line ~2581), so a MATCH ...
//     SET ... REMOVE statement with no MERGE/UNWIND routes to executeSet, not
//     executeRemove. Inside executeSet
//     (pkg/cypher/executor_mutations.go), firstPostSetClauseIndex explicitly
//     lists REMOVE as a SET-clause boundary keyword, and the trailing-clause
//     handler (~line 1191) recognizes a "REMOVE " trailing segment and routes
//     it through applyRemoveToMatchedRows on the SAME already-matched node
//     objects -- this is a genuinely different code path from the MERGE ...
//     SET ... REMOVE fusion #5441 proved corrupts data (that fusion has no
//     REMOVE awareness at all inside executeMerge's SET-clause parsing); a
//     MATCH-anchored SET+REMOVE is a distinct, supported, tested shape.
//  2. Storage-layer correctness: pkg/storage/badger_nodes.go's UpdateNode
//     unregisters unique-constraint values under the node's PRE-update labels
//     and re-registers them under its POST-update labels on every call
//     (lines ~338-349), and rewrites the label index (delete-then-recreate,
//     lines ~258-298) on every call. SET's label-add and REMOVE's
//     label-remove each call UpdateNode once, so after both run within one
//     executeSet invocation the node ends in the correct final state: label
//     index and every registered uid uniqueness constraint reflect
//     TerraformStateResource only. pkg/storage/badger_extra_test.go's
//     TestBadgerEngine_UpdateNode_LabelChange is the pinned module's own
//     proof that GetNodesByLabel reflects a label change immediately.
//
// Both facts are STATIC evidence from the pinned module's source and its own
// test suite; TestTerraformStateResourceMigrationLive
// (tfstate_canonical_writer_retract_live_test.go) is the empirical
// counterpart against a running NornicDB instance, required by this
// repository's Cypher rigor discipline before trusting a shape that "parses"
// as one that "executes as written."
const terraformStateResourceMigrationCypher = `MATCH (r:TerraformResource)
WHERE r.uid IN $uids AND r.evidence_source = 'projector/tfstate'
SET r:TerraformStateResource
REMOVE r:TerraformResource`

// canonicalTerraformStateResourceRetractCurrentLabelCypher deletes
// TerraformStateResource nodes this scope owns that were not refreshed by
// the current generation -- the steady-state case: a resource that no
// longer exists in state, whether it was created before or after the #5443
// label split.
const canonicalTerraformStateResourceRetractCurrentLabelCypher = `MATCH (r:TerraformStateResource)
WHERE r.scope_id = $scope_id AND r.evidence_source = 'projector/tfstate' AND r.generation_id <> $generation_id
DETACH DELETE r`

// canonicalTerraformStateResourceRetractLegacyLabelCypher deletes legacy
// TerraformResource nodes this scope owns that were not refreshed by the
// current generation -- the migration-gap case: a resource that was removed
// from state before this scope's first post-#5443 write ever ran, so its
// uid never appeared in a migration batch (terraformStateResourceMigrationCypher
// only relabels uids present in the CURRENT batch) and it is still sitting
// under the old label. Scoped identically to the current-label statement so
// both halves of the sweep use the same generation gate.
const canonicalTerraformStateResourceRetractLegacyLabelCypher = `MATCH (r:TerraformResource)
WHERE r.scope_id = $scope_id AND r.evidence_source = 'projector/tfstate' AND r.generation_id <> $generation_id
DETACH DELETE r`

// terraformStateResourceMigrationStatements batches the migration relabel by
// w.batchSize UIDs, mirroring terraformStateResourceAttributeRemoveStatements's
// batching. Skipped on the scope's first generation (mat.FirstGeneration):
// nothing was ever written for this scope before, so no legacy node can
// exist to migrate.
func (w *CanonicalNodeWriter) terraformStateResourceMigrationStatements(mat projector.CanonicalMaterialization) []Statement {
	if mat.FirstGeneration || len(mat.TerraformStateResources) == 0 {
		return nil
	}
	uids := make([]string, 0, len(mat.TerraformStateResources))
	for _, row := range mat.TerraformStateResources {
		uids = append(uids, row.UID)
	}

	var statements []Statement
	for start := 0; start < len(uids); start += w.batchSize {
		end := start + w.batchSize
		if end > len(uids) {
			end = len(uids)
		}
		statements = append(statements, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    terraformStateResourceMigrationCypher,
			Parameters: map[string]any{
				"uids":                           uids[start:end],
				StatementMetadataPhaseKey:        canonicalPhaseTerraformState,
				StatementMetadataEntityLabelKey:  "TerraformStateResource",
				StatementMetadataScopeIDKey:      mat.ScopeID,
				StatementMetadataGenerationIDKey: mat.GenerationID,
				StatementMetadataSummaryKey:      "migrate_legacy_label",
			},
		})
	}
	return statements
}

// terraformStateResourceRetractStatements builds the two generation-gated
// DETACH DELETE statements described on
// canonicalTerraformStateResourceRetractCurrentLabelCypher and
// canonicalTerraformStateResourceRetractLegacyLabelCypher. Must run AFTER
// terraformStateResourceMigrationStatements in the phase order
// buildTerraformStateStatements assembles: migration first removes any
// currently-present resource from the legacy label's population, so the
// legacy-label retraction only ever deletes genuinely orphaned nodes rather
// than a resource this same batch is about to relabel. Skipped on the
// scope's first generation, matching buildRetractStatements's existing
// config-side skip (canonical_node_writer_retract.go).
func (w *CanonicalNodeWriter) terraformStateResourceRetractStatements(mat projector.CanonicalMaterialization) []Statement {
	if mat.FirstGeneration {
		return nil
	}
	return []Statement{
		{
			Operation: OperationCanonicalRetract,
			Cypher:    canonicalTerraformStateResourceRetractCurrentLabelCypher,
			Parameters: map[string]any{
				"scope_id":                       mat.ScopeID,
				"generation_id":                  mat.GenerationID,
				StatementMetadataPhaseKey:        canonicalPhaseTerraformState,
				StatementMetadataEntityLabelKey:  "TerraformStateResource",
				StatementMetadataScopeIDKey:      mat.ScopeID,
				StatementMetadataGenerationIDKey: mat.GenerationID,
				StatementMetadataSummaryKey:      "retract_stale_current_label",
			},
		},
		{
			Operation: OperationCanonicalRetract,
			Cypher:    canonicalTerraformStateResourceRetractLegacyLabelCypher,
			Parameters: map[string]any{
				"scope_id":                       mat.ScopeID,
				"generation_id":                  mat.GenerationID,
				StatementMetadataPhaseKey:        canonicalPhaseTerraformState,
				StatementMetadataEntityLabelKey:  "TerraformResource",
				StatementMetadataScopeIDKey:      mat.ScopeID,
				StatementMetadataGenerationIDKey: mat.GenerationID,
				StatementMetadataSummaryKey:      "retract_stale_legacy_label",
			},
		},
	}
}
