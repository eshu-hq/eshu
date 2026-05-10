package cypher

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

const canonicalPhaseTerraformState = "terraform_state"

const canonicalTerraformStateResourceUpsertCypher = `UNWIND $rows AS row
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
	statements = append(
		statements,
		tfstateBatchedStatements(
			canonicalTerraformStateResourceUpsertCypher,
			terraformStateResourceRows(mat),
			w.batchSize,
			"TerraformResource",
			mat,
		)...,
	)
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
