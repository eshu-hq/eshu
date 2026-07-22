// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "github.com/eshu-hq/eshu/go/internal/projector"

// This file builds the per-batch row-map payloads consumed by the
// terraform_state canonical upsert statements in tfstate_canonical_writer.go
// (canonicalTerraformStateResourceUpsertCypher,
// canonicalTerraformStateModuleUpsertCypher, and
// canonicalTerraformStateOutputUpsertCypher), one builder per node label.

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
			"uid":                     row.UID,
			"address":                 row.Address,
			"mode":                    row.Mode,
			"resource_type":           row.ResourceType,
			"resource_name":           row.Name,
			"module_address":          row.ModuleAddress,
			"provider_address":        row.ProviderAddress,
			"lineage":                 row.Lineage,
			"serial":                  row.Serial,
			"backend_kind":            row.BackendKind,
			"locator_hash":            row.LocatorHash,
			"path":                    row.StatePath,
			"source_fact_id":          row.SourceFactID,
			"stable_fact_key":         row.StableFactKey,
			"source_system":           row.SourceSystem,
			"source_record_id":        row.SourceRecordID,
			"source_confidence":       row.SourceConfidence,
			"collector_kind":          row.CollectorKind,
			"correlation_anchors":     row.CorrelationAnchors,
			"tag_key_hashes":          row.TagKeyHashes,
			"scope_id":                mat.ScopeID,
			"generation_id":           mat.GenerationID,
			"config_repo_id":          terraformStateOwningRepoIDValue(row.OwningRepoID),
			"provider":                row.Provider,
			"provider_source_address": row.ProviderSourceAddress,
			"provider_alias":          row.ProviderAlias,
			"attrs":                   attrs,
		})
	}
	return rows
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
