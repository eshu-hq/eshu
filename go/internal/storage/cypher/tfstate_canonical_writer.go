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

// canonicalTerraformStateResourceUpsertBaseCypher sets every fixed
// TerraformResource field except the promoted-attribute merge. It is shared
// by canonicalTerraformStateResourceUpsertCypher (non-allowlisted resource
// types, unchanged shape) and every per-resource-type template
// terraformStateResourceUpsertCypherForType builds (#5441 review round 8,
// P1-a).
const canonicalTerraformStateResourceUpsertBaseCypher = `UNWIND $rows AS row
MERGE (r:TerraformResource {uid: row.uid})
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
    r.evidence_source = 'projector/tfstate'`

// canonicalTerraformStateResourceUpsertCypher is used for resource types with
// no entry in terraformAttributePromotionAllowlist: promoteTerraformResourceAttributes
// never writes a tf_attr_* property for them, so `r += row.attrs` (always an
// empty-map no-op here) cannot leave a stale property behind and no REMOVE
// clause is needed.
const canonicalTerraformStateResourceUpsertCypher = canonicalTerraformStateResourceUpsertBaseCypher + `,
    r += row.attrs`

// terraformStateResourceUpsertCypherByType holds one generated Cypher
// template per allowlisted resource type, each REMOVE-ing that type's full
// closed set of possible tf_attr_* properties before re-setting only the
// subset promoteTerraformResourceAttributes currently produces. Built once at
// package init from terraformAttributePromotionAllowlist via
// terraformAttributePromotionKeysForType, so the REMOVE list can never drift
// from the allowlist it must fully cover.
//
// Static per-property REMOVE names, not a dynamic REMOVE r[key] loop, are
// required: traced against the pinned NornicDB executor
// (pkg/cypher/ast_builder.go parseRemove) and confirmed it parses only the
// `variable.property` and `variable:Label` REMOVE forms -- there is no
// dynamic bracket-index property removal support, so a per-row variable key
// list cannot be REMOVE-d in one shared UNWIND template. Per-resource-type
// static templates, dispatched like batchCanonicalTypedRepoRelationshipUpsertCypher
// dispatches per relationship type, are the only executor-safe shape.
var terraformStateResourceUpsertCypherByType = buildTerraformStateResourceUpsertCypherByType()

func buildTerraformStateResourceUpsertCypherByType() map[string]string {
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
		out[resourceType] = canonicalTerraformStateResourceUpsertBaseCypher +
			"\nREMOVE " + strings.Join(removeItems, ", ") +
			"\nSET r += row.attrs"
	}
	return out
}

// terraformStateResourceUpsertCypherForType returns the Cypher template to
// use for a resource row of the given resourceType: the per-type
// REMOVE-then-SET template for an allowlisted type, or the shared
// no-REMOVE-needed template otherwise.
func terraformStateResourceUpsertCypherForType(resourceType string) string {
	if cypher, ok := terraformStateResourceUpsertCypherByType[resourceType]; ok {
		return cypher
	}
	return canonicalTerraformStateResourceUpsertCypher
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

func (w *CanonicalNodeWriter) buildTerraformStateStatements(mat projector.CanonicalMaterialization) []Statement {
	var statements []Statement
	for _, group := range terraformStateResourceRowGroups(mat) {
		statements = append(
			statements,
			tfstateBatchedStatements(
				terraformStateResourceUpsertCypherForType(group.resourceType),
				group.rows,
				w.batchSize,
				"TerraformResource",
				mat,
			)...,
		)
	}
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

func terraformStateResourceRows(mat projector.CanonicalMaterialization) []map[string]any {
	rows := make([]map[string]any, 0, len(mat.TerraformStateResources))
	for _, row := range mat.TerraformStateResources {
		// #5441: reduce the raw classified Attributes object to a bounded,
		// allowlisted, redaction-safe subset of prefixed scalar node
		// properties. attrs is always a non-nil map (never Go nil / Cypher
		// null) so the additive `r += row.attrs` merge in
		// canonicalTerraformStateResourceUpsertCypher is always a well-typed
		// map-merge — an empty map is a safe no-op on the pinned NornicDB
		// executor (pkg/cypher/set_helpers.go applySetMapMergeToNode ranges
		// over the resolved map's entries), matching the same
		// always-present-map convention canonicalEntityProperties already
		// uses for the code-entity `SET n += row.props` writer.
		attrs := promoteTerraformResourceAttributes(row.ResourceType, row.Attributes)
		if attrs == nil {
			attrs = map[string]any{}
		}
		rows = append(rows, map[string]any{
			"uid":                 row.UID,
			"address":             row.Address,
			"mode":                row.Mode,
			"resource_type":       row.ResourceType,
			"resource_name":       row.Name,
			"module_address":      row.ModuleAddress,
			"provider_address":    row.ProviderAddress,
			"lineage":             row.Lineage,
			"serial":              row.Serial,
			"backend_kind":        row.BackendKind,
			"locator_hash":        row.LocatorHash,
			"path":                row.StatePath,
			"source_fact_id":      row.SourceFactID,
			"stable_fact_key":     row.StableFactKey,
			"source_system":       row.SourceSystem,
			"source_record_id":    row.SourceRecordID,
			"source_confidence":   row.SourceConfidence,
			"collector_kind":      row.CollectorKind,
			"correlation_anchors": row.CorrelationAnchors,
			"tag_key_hashes":      row.TagKeyHashes,
			"scope_id":            mat.ScopeID,
			"generation_id":       mat.GenerationID,
			"attrs":               attrs,
		})
	}
	return rows
}

// terraformStateResourceRowGroup bundles the rows that share one Cypher
// template. resourceType is "" for the shared bucket of non-allowlisted
// resource types (no REMOVE clause needed); otherwise it names the
// allowlisted resource type whose REMOVE-then-SET template applies.
type terraformStateResourceRowGroup struct {
	resourceType string
	rows         []map[string]any
}

// terraformStateResourceRowGroups buckets terraformStateResourceRows(mat)'s
// output by resource type so each allowlisted type gets its own
// REMOVE-then-SET Cypher template (#5441 review round 8, P1-a) instead of
// every resource sharing one additive-only template. Every non-allowlisted
// type still shares a single no-REMOVE-needed template, exactly as before
// this fix. Returned in sorted-resourceType order with the shared bucket
// last, for deterministic statement ordering across runs.
func terraformStateResourceRowGroups(mat projector.CanonicalMaterialization) []terraformStateResourceRowGroup {
	byType := make(map[string][]map[string]any)
	for _, row := range terraformStateResourceRows(mat) {
		resourceType, _ := row["resource_type"].(string)
		key := resourceType
		if _, allowlisted := terraformStateResourceUpsertCypherByType[resourceType]; !allowlisted {
			key = ""
		}
		byType[key] = append(byType[key], row)
	}

	types := make([]string, 0, len(byType))
	for resourceType := range byType {
		if resourceType == "" {
			continue
		}
		types = append(types, resourceType)
	}
	sort.Strings(types)

	groups := make([]terraformStateResourceRowGroup, 0, len(byType))
	for _, resourceType := range types {
		groups = append(groups, terraformStateResourceRowGroup{resourceType: resourceType, rows: byType[resourceType]})
	}
	if rows, ok := byType[""]; ok {
		groups = append(groups, terraformStateResourceRowGroup{resourceType: "", rows: rows})
	}
	return groups
}

func terraformStateModuleRows(mat projector.CanonicalMaterialization) []map[string]any {
	rows := make([]map[string]any, 0, len(mat.TerraformStateModules))
	for _, row := range mat.TerraformStateModules {
		rows = append(rows, map[string]any{
			"uid":               row.UID,
			"module_address":    row.ModuleAddress,
			"resource_count":    row.ResourceCount,
			"lineage":           row.Lineage,
			"serial":            row.Serial,
			"backend_kind":      row.BackendKind,
			"locator_hash":      row.LocatorHash,
			"path":              row.StatePath,
			"source_fact_id":    row.SourceFactID,
			"stable_fact_key":   row.StableFactKey,
			"source_system":     row.SourceSystem,
			"source_record_id":  row.SourceRecordID,
			"source_confidence": row.SourceConfidence,
			"collector_kind":    row.CollectorKind,
			"scope_id":          mat.ScopeID,
			"generation_id":     mat.GenerationID,
		})
	}
	return rows
}

func terraformStateOutputRows(mat projector.CanonicalMaterialization) []map[string]any {
	rows := make([]map[string]any, 0, len(mat.TerraformStateOutputs))
	for _, row := range mat.TerraformStateOutputs {
		rows = append(rows, map[string]any{
			"uid":               row.UID,
			"name":              row.Name,
			"sensitive":         row.Sensitive,
			"value_shape":       row.ValueShape,
			"lineage":           row.Lineage,
			"serial":            row.Serial,
			"backend_kind":      row.BackendKind,
			"locator_hash":      row.LocatorHash,
			"path":              row.StatePath,
			"source_fact_id":    row.SourceFactID,
			"stable_fact_key":   row.StableFactKey,
			"source_system":     row.SourceSystem,
			"source_record_id":  row.SourceRecordID,
			"source_confidence": row.SourceConfidence,
			"collector_kind":    row.CollectorKind,
			"scope_id":          mat.ScopeID,
			"generation_id":     mat.GenerationID,
		})
	}
	return rows
}
