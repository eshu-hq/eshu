// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

const canonicalPhaseTerraformState = "terraform_state"

// #5443: Terraform-state-observed resources MERGE under their own
// TerraformStateResource label, distinct from the config-declared
// TerraformResource label the HCL parser's generic content-entity pipeline
// writes (canonical_node_cypher.go's canonicalNodeEntityUpsertTemplate). The
// two identity domains were always structurally disjoint (a 64-hex sha256
// terraformStateUID vs a 12-hex blake2s content-entity ID) but shared one
// label, which meant no query could distinguish "declared in config",
// "applied in state", or "both" without inspecting evidence_source and
// guessing at property shape (see the now-simplified iac_resources.go). See
// tfstate_canonical_writer_retract.go for the migration that relabels
// pre-#5443 TerraformResource nodes carrying evidence_source =
// 'projector/tfstate' and the retraction that clears genuinely stale ones.
//
// canonicalTerraformStateResourceUpsertCypher is unchanged from before #5441
// review round 8's P1-a fix attempt: one unified UNWIND/MERGE/SET template
// for every resource type, ending in the additive `r += row.attrs` map-merge.
//
// #5441 review round 9, P0: an earlier version of this fix combined a
// REMOVE clause into this same MERGE...SET statement, per resource type, to
// clear stale tf_attr_* properties before re-setting the current subset.
// That shape does not execute as written on the pinned NornicDB executor.
// Traced and proven against the real backend (see
// terraformStateResourceAttributeRemoveCypherByType's doc comment below for
// the full trace and docs/internal/evidence/5441-edge-node-properties.md for
// the empirical reproduction): a MERGE statement with no WITH/RETURN routes
// to executeMerge (pkg/cypher/merge.go), whose standalone-SET boundary is
// delimited only by a following WITH or RETURN -- it has no REMOVE
// awareness at all, so the entire `REMOVE ...` clause and everything after
// it get swallowed into the SET clause text and corrupt the immediately
// preceding property assignment (r.evidence_source, in the shipped
// #5441 shape) with literal Cypher source text, on every write, not only
// refreshes. The stale-attribute bug this was meant to fix was ALSO not
// actually fixed by that shape.
//
// The fix is the two-statement design below: this upsert template stays
// exactly as it was (no REMOVE, no per-type variants), and
// terraformStateResourceAttributeRemoveStatements runs a genuinely separate
// REMOVE-only statement first.
//
// #5446 adds three more FIXED keys to this same SET clause --
// r.provider, r.provider_source_address, r.provider_alias -- sourced from
// the new provider-binding pre-pass (terraformStateProviderBindingsByResource,
// go/internal/projector/tfstate_canonical.go), not the dynamic tf_attr_*
// allowlist. These are ordinary UNCONDITIONAL SETs exactly like the
// pre-existing r.mode/r.provider_address/etc fixed keys immediately above
// them: row.provider/row.provider_source_address/row.provider_alias are
// always present as "" when no binding fact was observed (see
// terraformStateResourceRows below), so every write refreshes all three to
// the row's current value -- there is no REMOVE-before-upsert concern here
// the way there is for the dynamic, allowlist-driven tf_attr_* keys merged
// in by `r += row.attrs`: a fixed key with a guaranteed row value on every
// row can never go stale the way an ABSENT dynamic key can.
const canonicalTerraformStateResourceUpsertCypher = `UNWIND $rows AS row
MERGE (r:TerraformStateResource {uid: row.uid})
SET r.id = row.uid,
    r.name = row.address,
    r.address = row.address,
    r.mode = row.mode,
    r.resource_type = row.resource_type,
    r.resource_name = row.resource_name,
    r.module_address = row.module_address,
    r.provider_address = row.provider_address,
    r.lineage = row.lineage,
    r.serial = row.serial,
    r.backend_kind = row.backend_kind,
    r.locator_hash = row.locator_hash,
    r.path = row.path,
    r.line_number = 1,
    r.source_fact_id = row.source_fact_id,
    r.stable_fact_key = row.stable_fact_key,
    r.source_system = row.source_system,
    r.source_record_id = row.source_record_id,
    r.source_confidence = row.source_confidence,
    r.collector_kind = row.collector_kind,
    r.correlation_anchors = row.correlation_anchors,
    r.tag_key_hashes = row.tag_key_hashes,
    r.scope_id = row.scope_id,
    r.generation_id = row.generation_id,
    r.evidence_source = 'projector/tfstate',
    r.config_repo_id = row.config_repo_id,
    r.provider = row.provider,
    r.provider_source_address = row.provider_source_address,
    r.provider_alias = row.provider_alias,
    r += row.attrs`

// terraformStateResourceAttributeRemoveCypherByType holds one generated
// Cypher template per allowlisted resource type: a standalone
// `MATCH ... WHERE r.uid IN $uids REMOVE ...` statement, no MERGE, no SET,
// no UNWIND. Built once at package init from terraformAttributePromotionAllowlist
// via terraformAttributePromotionKeysForType, so the REMOVE list can never
// drift from the allowlist it must fully cover.
//
// This is the ONLY property-removal shape proven to execute correctly
// against the pinned NornicDB executor, github.com/orneryd/nornicdb v1.0.45
// per go/go.mod, verified directly against that resolved module -- not a
// fork checkout -- after review round 9's P0 fix cited a fork-HEAD-only file
// that does not exist at the pinned version (#5441 review round 9 follow-up;
// see docs/internal/evidence/5441-edge-node-properties.md for the durable
// note on why the pinned module, not a local fork checkout, is the citation
// source of truth going forward):
//   - `pkg/cypher/ast_builder.go` parseRemove (v1.0.45 line 664) parses only
//     `variable.property` and `variable:Label` REMOVE items -- no dynamic
//     `REMOVE r[key]` bracket-index support, ruling out a per-row
//     FOREACH-based removal inside a shared UNWIND template.
//   - `pkg/cypher/executor.go` executeWithoutTransaction (v1.0.45 line 2446)
//     is the top-level dispatch; its REMOVE branch
//     (`containsKeywordOutsideStrings(cypher, "REMOVE")`, line 2581) routes
//     straight to `executeRemove` for any statement that reaches it with no
//     MERGE, UNWIND, or SET keyword -- before any of the MERGE/SET-oriented
//     routing branches can misparse it.
//   - `pkg/cypher/executor_mutations.go` executeRemove (v1.0.45 line 2032)
//     splits the statement into `MATCH ... WHERE ... RETURN *` (executed
//     through the general matcher, which supports `WHERE x IN $list`) and a
//     REMOVE item list parsed from the tail -- exactly the shape here.
//
// This also matches the actual precedent this repo already ships:
// rds_posture_node_writer.go and ec2_block_device_kms_posture_node_writer.go
// each pair a plain upsert SET statement with a SEPARATE, standalone
// `MATCH ... REMOVE ...` statement with no trailing SET -- never a REMOVE
// fused into the same MERGE statement as a SET.
var terraformStateResourceAttributeRemoveCypherByType = buildTerraformStateResourceAttributeRemoveCypherByType()

func buildTerraformStateResourceAttributeRemoveCypherByType() map[string]string {
	resourceTypes := make([]string, 0, len(terraformAttributePromotionAllowlist))
	for resourceType := range terraformAttributePromotionAllowlist {
		resourceTypes = append(resourceTypes, resourceType)
	}
	sort.Strings(resourceTypes)

	out := make(map[string]string, len(resourceTypes))
	for _, resourceType := range resourceTypes {
		keys := terraformAttributePromotionKeysForType(resourceType)
		if len(keys) == 0 {
			continue
		}
		removeItems := make([]string, len(keys))
		for i, key := range keys {
			removeItems[i] = "r." + key
		}
		out[resourceType] = "MATCH (r:TerraformStateResource) WHERE r.uid IN $uids\nREMOVE " +
			strings.Join(removeItems, ", ")
	}
	return out
}

const canonicalTerraformStateModuleUpsertCypher = `UNWIND $rows AS row
MERGE (m:TerraformModule {uid: row.uid})
SET m.id = row.uid,
    m.name = row.module_address,
    m.module_address = row.module_address,
    m.resource_count = row.resource_count,
    m.lineage = row.lineage,
    m.serial = row.serial,
    m.backend_kind = row.backend_kind,
    m.locator_hash = row.locator_hash,
    m.path = row.path,
    m.source_fact_id = row.source_fact_id,
    m.stable_fact_key = row.stable_fact_key,
    m.source_system = row.source_system,
    m.source_record_id = row.source_record_id,
    m.source_confidence = row.source_confidence,
    m.collector_kind = row.collector_kind,
    m.scope_id = row.scope_id,
    m.generation_id = row.generation_id,
    m.evidence_source = 'projector/tfstate'`

const canonicalTerraformStateOutputUpsertCypher = `UNWIND $rows AS row
MERGE (o:TerraformOutput {uid: row.uid})
SET o.id = row.uid,
    o.name = row.name,
    o.sensitive = row.sensitive,
    o.value_shape = row.value_shape,
    o.lineage = row.lineage,
    o.serial = row.serial,
    o.backend_kind = row.backend_kind,
    o.locator_hash = row.locator_hash,
    o.path = row.path,
    o.line_number = 1,
    o.source_fact_id = row.source_fact_id,
    o.stable_fact_key = row.stable_fact_key,
    o.source_system = row.source_system,
    o.source_record_id = row.source_record_id,
    o.source_confidence = row.source_confidence,
    o.collector_kind = row.collector_kind,
    o.scope_id = row.scope_id,
    o.generation_id = row.generation_id,
    o.evidence_source = 'projector/tfstate'`

// buildTerraformStateStatements returns every statement this writer emits for
// one tfstate materialization, in strict phase order:
//
//  1. Migration (terraformStateResourceMigrationStatements, #5443): relabels
//     any pre-#5443 TerraformResource node whose uid reappears in this
//     batch to TerraformStateResource. Must run before every other phase so
//     the phases below only ever see the current label.
//  2. REMOVE-before-upsert (terraformStateResourceAttributeRemoveStatements,
//     #5441 review round 9, P0): ordering here is load-bearing, not
//     cosmetic. It unconditionally clears each allowlisted type's FULL
//     closed set of possible tf_attr_* properties for every UID in this
//     batch, and the upsert's additive `r += row.attrs` merge re-establishes
//     only the subset the current row promotes. If the upsert ran first, the
//     REMOVE that follows it would immediately strip every tf_attr_*
//     property the upsert just wrote -- corrupting every write, not only
//     refreshes. REMOVE-then-SET is correct; SET-then-REMOVE is not.
//  3. Resource upsert: refreshes every resource this batch actually saw,
//     including its generation_id.
//  4. Retraction (terraformStateResourceRetractStatements, #5443):
//     generation-gated DETACH DELETE for resources genuinely no longer in
//     state, scoped to BOTH labels (TerraformStateResource for steady-state
//     staleness, TerraformResource for legacy nodes migration never touched
//     because their uid was absent from this batch). MUST run after BOTH
//     migration and the resource upsert -- this ordering is itself load-
//     bearing, not cosmetic (fixed after a P0 review finding: an earlier
//     version of this writer ran retraction before the upsert, so every
//     resource in the batch still carried the PREVIOUS cycle's generation_id
//     at the moment retraction's `generation_id <> $generation_id` predicate
//     evaluated it -- deleting the ENTIRE existing population every cycle,
//     not just genuinely stale nodes, with the upsert immediately
//     recreating everything). Running after the upsert instead matches this
//     writer's own "entities" -> "entity_retract" precedent
//     (canonical_node_writer.go's buildPhases): surviving nodes already
//     carry the current generation_id by the time retraction runs, so only
//     nodes this batch genuinely did not touch are deleted. Also running
//     after migration, so a still-present resource is relabeled rather than
//     deleted, since a legacy node still carries a stale generation_id until
//     migration or the upsert above touches it.
//  5. Module/output upserts (unchanged from before #5443).
//  6. MATCHES_STATE config-edge retract-then-write
//     (terraformStateMatchesConfigEdgeRetractStatements, then
//     terraformStateMatchesConfigEdgeStatements, #5443 P1 review finding):
//     the retract deletes a stale edge from a state resource refreshed this
//     generation whose resolved config match changed or became ambiguous --
//     node retraction alone never catches this because both endpoints
//     survive, only the relationship is wrong. Retract runs BEFORE the MERGE,
//     mirroring canonical_atlantis_edges.go's own retract-then-MERGE
//     ordering for the same reason: both are relationship DELETEs mixed into
//     a phase of otherwise-MERGE statements and need the Drain-marked
//     standalone-autocommit treatment (#4476 class).
//
// Partial-failure note: every phase above is a separate statement, not one
// atomic transaction (this repo's own precedent -- rds_posture_node_writer.go,
// ec2_block_device_kms_posture_node_writer.go -- does not bind its
// upsert/retract pair atomically either). Every phase is independently
// idempotent (a relabel of an already-relabeled node, a DETACH DELETE of an
// already-absent node, a REMOVE of an already-absent property, or a SET to
// an already-current value are all no-ops), so a retry of the whole
// generation after a partial failure is self-healing.
func (w *CanonicalNodeWriter) buildTerraformStateStatements(mat projector.CanonicalMaterialization) []Statement {
	var statements []Statement
	statements = append(statements, w.terraformStateResourceMigrationStatements(mat)...)
	statements = append(statements, w.terraformStateResourceAttributeRemoveStatements(mat)...)
	statements = append(
		statements,
		tfstateBatchedStatements(
			canonicalTerraformStateResourceUpsertCypher,
			terraformStateResourceRows(mat),
			w.batchSize,
			"TerraformStateResource",
			mat,
		)...,
	)
	statements = append(statements, w.terraformStateResourceRetractStatements(mat)...)
	statements = append(
		statements,
		tfstateBatchedStatements(
			canonicalTerraformStateModuleUpsertCypher,
			terraformStateModuleRows(mat),
			w.batchSize,
			"TerraformModule",
			mat,
		)...,
	)
	statements = append(
		statements,
		tfstateBatchedStatements(
			canonicalTerraformStateOutputUpsertCypher,
			terraformStateOutputRows(mat),
			w.batchSize,
			"TerraformOutput",
			mat,
		)...,
	)
	statements = append(statements, w.terraformStateMatchesConfigEdgeRetractStatements(mat)...)
	statements = append(statements, w.terraformStateMatchesConfigEdgeStatements(mat)...)
	return statements
}

// terraformStateResourceAttributeRemoveStatements builds one standalone
// REMOVE-only statement per allowlisted-resource-type batch of UIDs (#5441
// review round 9, P0). See terraformStateResourceAttributeRemoveCypherByType
// for why this must be a separate statement, never fused with the upsert.
// Batched the same way as the upsert rows (w.batchSize UIDs per statement)
// so a single materialization with many resources of one allowlisted type
// does not send an unbounded parameter list in one statement.
func (w *CanonicalNodeWriter) terraformStateResourceAttributeRemoveStatements(mat projector.CanonicalMaterialization) []Statement {
	byType := make(map[string][]string, len(terraformStateResourceAttributeRemoveCypherByType))
	for _, row := range mat.TerraformStateResources {
		if _, ok := terraformStateResourceAttributeRemoveCypherByType[row.ResourceType]; !ok {
			continue
		}
		byType[row.ResourceType] = append(byType[row.ResourceType], row.UID)
	}
	if len(byType) == 0 {
		return nil
	}

	resourceTypes := make([]string, 0, len(byType))
	for resourceType := range byType {
		resourceTypes = append(resourceTypes, resourceType)
	}
	sort.Strings(resourceTypes)

	var statements []Statement
	for _, resourceType := range resourceTypes {
		uids := byType[resourceType]
		cypher := terraformStateResourceAttributeRemoveCypherByType[resourceType]
		for start := 0; start < len(uids); start += w.batchSize {
			end := start + w.batchSize
			if end > len(uids) {
				end = len(uids)
			}
			statements = append(statements, Statement{
				Operation: OperationCanonicalRetract,
				Cypher:    cypher,
				Parameters: map[string]any{
					"uids":                           uids[start:end],
					StatementMetadataPhaseKey:        canonicalPhaseTerraformState,
					StatementMetadataEntityLabelKey:  "TerraformStateResource",
					StatementMetadataScopeIDKey:      mat.ScopeID,
					StatementMetadataGenerationIDKey: mat.GenerationID,
					StatementMetadataSummaryKey: fmt.Sprintf(
						"resource_type=%s remove_stale_attrs uids=%d",
						resourceType,
						end-start,
					),
				},
			})
		}
	}
	return statements
}

func tfstateBatchedStatements(
	cypher string,
	rows []map[string]any,
	batchSize int,
	label string,
	mat projector.CanonicalMaterialization,
) []Statement {
	statements := buildBatchedStatements(cypher, rows, batchSize)
	for index := range statements {
		batchRows := statements[index].Parameters["rows"].([]map[string]any)
		statements[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseTerraformState
		statements[index].Parameters[StatementMetadataEntityLabelKey] = label
		statements[index].Parameters[StatementMetadataScopeIDKey] = mat.ScopeID
		statements[index].Parameters[StatementMetadataGenerationIDKey] = mat.GenerationID
		statements[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"label=%s rows=%d",
			label,
			len(batchRows),
		)
	}
	return statements
}
