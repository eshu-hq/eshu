package cypher

import (
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func canonicalEntityRowsByLabel(mat projector.CanonicalMaterialization) map[string][]map[string]any {
	if len(mat.Entities) == 0 {
		return nil
	}

	byLabel := make(map[string][]map[string]any, len(mat.Entities))
	for _, entity := range mat.Entities {
		row := map[string]any{
			"entity_id":     entity.EntityID,
			"generation_id": mat.GenerationID,
			"props": canonicalEntityProperties(
				entity,
				mat.ScopeID,
				mat.GenerationID,
			),
		}
		byLabel[entity.Label] = append(byLabel[entity.Label], row)
	}

	return byLabel
}

func canonicalEntityRowsByLabelWithFile(mat projector.CanonicalMaterialization) map[string][]map[string]any {
	if len(mat.Entities) == 0 {
		return nil
	}

	byLabel := make(map[string][]map[string]any, len(mat.Entities))
	for _, entity := range mat.Entities {
		row := map[string]any{
			"entity_id":     entity.EntityID,
			"file_path":     entity.FilePath,
			"generation_id": mat.GenerationID,
			"props": canonicalEntityProperties(
				entity,
				mat.ScopeID,
				mat.GenerationID,
			),
		}
		byLabel[entity.Label] = append(byLabel[entity.Label], row)
	}

	return byLabel
}

func canonicalEntityRowsByLabelAndFile(mat projector.CanonicalMaterialization) map[string]map[string][]map[string]any {
	if len(mat.Entities) == 0 {
		return nil
	}

	byLabel := make(map[string]map[string][]map[string]any, len(mat.Entities))
	for _, entity := range mat.Entities {
		byFile := byLabel[entity.Label]
		if byFile == nil {
			byFile = make(map[string][]map[string]any)
			byLabel[entity.Label] = byFile
		}
		byFile[entity.FilePath] = append(byFile[entity.FilePath], map[string]any{
			"entity_id":     entity.EntityID,
			"generation_id": mat.GenerationID,
			"props": canonicalEntityProperties(
				entity,
				mat.ScopeID,
				mat.GenerationID,
			),
		})
	}

	return byLabel
}

func canonicalEntityContainmentRowsByLabelAndFile(mat projector.CanonicalMaterialization) map[string]map[string][]map[string]any {
	if len(mat.Entities) == 0 {
		return nil
	}

	byLabel := make(map[string]map[string][]map[string]any, len(mat.Entities))
	for _, entity := range mat.Entities {
		byFile := byLabel[entity.Label]
		if byFile == nil {
			byFile = make(map[string][]map[string]any)
			byLabel[entity.Label] = byFile
		}
		byFile[entity.FilePath] = append(byFile[entity.FilePath], map[string]any{
			"entity_id":     entity.EntityID,
			"generation_id": mat.GenerationID,
		})
	}
	return byLabel
}

func sortedCanonicalEntityLabels(byLabel map[string][]map[string]any) []string {
	labels := make([]string, 0, len(byLabel))
	for label := range byLabel {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

func sortedCanonicalEntityContainmentLabels(byLabel map[string]map[string][]map[string]any) []string {
	labels := make([]string, 0, len(byLabel))
	for label := range byLabel {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

func sortedCanonicalEntityContainmentFiles(byFile map[string][]map[string]any) []string {
	files := make([]string, 0, len(byFile))
	for filePath := range byFile {
		files = append(files, filePath)
	}
	sort.Strings(files)
	return files
}

// buildEntityStatements writes entity nodes first so backend-specific edge
// creation can happen in a later, separately timed phase.
func (w *CanonicalNodeWriter) buildEntityStatements(mat projector.CanonicalMaterialization) []Statement {
	if w.entityContainmentInEntityUpsert {
		if w.entityContainmentBatchAcrossFiles {
			return w.buildEntityStatementsWithBatchedContainment(mat)
		}
		return w.buildEntityStatementsWithContainment(mat)
	}

	byLabel := canonicalEntityRowsByLabel(mat)
	if len(byLabel) == 0 {
		return nil
	}

	var stmts []Statement
	for _, label := range sortedCanonicalEntityLabels(byLabel) {
		batchSize := 0
		if w.entityLabelBatchSizes != nil {
			batchSize = w.entityLabelBatchSizes[label]
		}
		if batchSize <= 0 {
			batchSize = w.entityBatchSize
		}
		if batchSize <= 0 {
			batchSize = w.batchSize
		}
		if batchSize <= 0 {
			batchSize = DefaultBatchSize
		}
		batchRows := make([]map[string]any, 0, batchSize)
		flushBatch := func() {
			if len(batchRows) == 0 {
				return
			}
			statementSummary := fmt.Sprintf(
				"label=%s rows=%d first_id=%v last_id=%v",
				label,
				len(batchRows),
				batchRows[0]["entity_id"],
				batchRows[len(batchRows)-1]["entity_id"],
			)
			stmts = append(stmts, Statement{
				Operation: OperationCanonicalUpsert,
				Cypher:    fmt.Sprintf(canonicalNodeEntityUpsertTemplate, label),
				Parameters: map[string]any{
					"rows":                           append([]map[string]any(nil), batchRows...),
					StatementMetadataPhaseKey:        CanonicalPhaseEntities,
					StatementMetadataEntityLabelKey:  label,
					StatementMetadataSummaryKey:      statementSummary,
					StatementMetadataScopeIDKey:      mat.ScopeID,
					StatementMetadataGenerationIDKey: mat.GenerationID,
				},
			})
			batchRows = batchRows[:0]
		}
		for _, row := range byLabel[label] {
			if canonicalEntityRowNeedsSingletonFallback(label, row) {
				flushBatch()
				mode := canonicalEntitySingletonFallbackMode(label, row)
				stmts = append(stmts, Statement{
					Operation: OperationCanonicalUpsert,
					Cypher:    fmt.Sprintf(canonicalNodeEntitySingletonUpsertTemplate, label),
					Parameters: map[string]any{
						"entity_id":                        row["entity_id"],
						"props":                            row["props"],
						"generation_id":                    row["generation_id"],
						StatementMetadataPhaseKey:          CanonicalPhaseEntities,
						StatementMetadataEntityLabelKey:    label,
						StatementMetadataPhaseGroupModeKey: mode,
						StatementMetadataScopeIDKey:        mat.ScopeID,
						StatementMetadataGenerationIDKey:   mat.GenerationID,
						StatementMetadataSummaryKey: fmt.Sprintf(
							"label=%s rows=1 entity_id=%v fallback=%s",
							label,
							row["entity_id"],
							canonicalEntitySingletonFallbackName(mode),
						),
					},
				})
				continue
			}
			batchRows = append(batchRows, row)
			if len(batchRows) >= batchSize {
				flushBatch()
			}
		}
		flushBatch()
	}

	return stmts
}

func (w *CanonicalNodeWriter) buildEntityStatementsWithContainment(mat projector.CanonicalMaterialization) []Statement {
	byLabel := canonicalEntityRowsByLabelAndFile(mat)
	if len(byLabel) == 0 {
		return nil
	}

	var stmts []Statement
	for _, label := range sortedCanonicalEntityContainmentLabels(byLabel) {
		batchSize := 0
		if w.entityLabelBatchSizes != nil {
			batchSize = w.entityLabelBatchSizes[label]
		}
		if batchSize <= 0 {
			batchSize = w.entityBatchSize
		}
		if batchSize <= 0 {
			batchSize = w.batchSize
		}
		if batchSize <= 0 {
			batchSize = DefaultBatchSize
		}

		byFile := byLabel[label]
		for _, filePath := range sortedCanonicalEntityContainmentFiles(byFile) {
			batchRows := make([]map[string]any, 0, batchSize)
			flushBatch := func() {
				if len(batchRows) == 0 {
					return
				}
				statementSummary := fmt.Sprintf(
					"label=%s file=%s rows=%d first_id=%v last_id=%v containment=inline",
					label,
					filePath,
					len(batchRows),
					batchRows[0]["entity_id"],
					batchRows[len(batchRows)-1]["entity_id"],
				)
				stmts = append(stmts, Statement{
					Operation: OperationCanonicalUpsert,
					Cypher:    fmt.Sprintf(canonicalNodeEntityFileScopedUpsertWithContainmentTemplate, label),
					Parameters: map[string]any{
						"file_path":                      filePath,
						"rows":                           append([]map[string]any(nil), batchRows...),
						StatementMetadataPhaseKey:        CanonicalPhaseEntities,
						StatementMetadataEntityLabelKey:  label,
						StatementMetadataSummaryKey:      statementSummary,
						StatementMetadataScopeIDKey:      mat.ScopeID,
						StatementMetadataGenerationIDKey: mat.GenerationID,
					},
				})
				batchRows = batchRows[:0]
			}
			for _, row := range byFile[filePath] {
				if canonicalEntityRowNeedsSingletonFallback(label, row) {
					flushBatch()
					mode := canonicalEntitySingletonFallbackMode(label, row)
					stmts = append(stmts, canonicalNodeEntitySingletonWithContainmentStatement(
						label,
						filePath,
						row,
						fmt.Sprintf(
							"label=%s rows=1 entity_id=%v fallback=%s containment=inline",
							label,
							row["entity_id"],
							canonicalEntitySingletonFallbackName(mode),
						),
						mat.ScopeID,
						mat.GenerationID,
					))
					continue
				}
				batchRows = append(batchRows, row)
				if len(batchRows) >= batchSize {
					flushBatch()
				}
			}
			flushBatch()
		}
	}

	return stmts
}

func (w *CanonicalNodeWriter) buildEntityStatementsWithBatchedContainment(mat projector.CanonicalMaterialization) []Statement {
	byLabel := canonicalEntityRowsByLabelWithFile(mat)
	if len(byLabel) == 0 {
		return nil
	}

	var stmts []Statement
	for _, label := range sortedCanonicalEntityLabels(byLabel) {
		batchSize := 0
		if w.entityLabelBatchSizes != nil {
			batchSize = w.entityLabelBatchSizes[label]
		}
		if batchSize <= 0 {
			batchSize = w.entityBatchSize
		}
		if batchSize <= 0 {
			batchSize = w.batchSize
		}
		if batchSize <= 0 {
			batchSize = DefaultBatchSize
		}

		batchRows := make([]map[string]any, 0, batchSize)
		flushBatch := func() {
			if len(batchRows) == 0 {
				return
			}
			statementSummary := fmt.Sprintf(
				"label=%s rows=%d first_id=%v last_id=%v containment=inline batch_across_files=true",
				label,
				len(batchRows),
				batchRows[0]["entity_id"],
				batchRows[len(batchRows)-1]["entity_id"],
			)
			stmts = append(stmts, Statement{
				Operation: OperationCanonicalUpsert,
				Cypher:    fmt.Sprintf(canonicalNodeEntityUpsertWithContainmentTemplate, label),
				Parameters: map[string]any{
					"rows":                           append([]map[string]any(nil), batchRows...),
					StatementMetadataPhaseKey:        CanonicalPhaseEntities,
					StatementMetadataEntityLabelKey:  label,
					StatementMetadataSummaryKey:      statementSummary,
					StatementMetadataScopeIDKey:      mat.ScopeID,
					StatementMetadataGenerationIDKey: mat.GenerationID,
				},
			})
			batchRows = batchRows[:0]
		}
		for _, row := range byLabel[label] {
			if canonicalEntityRowNeedsSingletonFallback(label, row) {
				flushBatch()
				filePath, _ := row["file_path"].(string)
				mode := canonicalEntitySingletonFallbackMode(label, row)
				stmts = append(stmts, canonicalNodeEntitySingletonWithContainmentStatement(
					label,
					filePath,
					row,
					fmt.Sprintf(
						"label=%s rows=1 entity_id=%v fallback=%s containment=inline",
						label,
						row["entity_id"],
						canonicalEntitySingletonFallbackName(mode),
					),
					mat.ScopeID,
					mat.GenerationID,
				))
				continue
			}
			batchRows = append(batchRows, row)
			if len(batchRows) >= batchSize {
				flushBatch()
			}
		}
		flushBatch()
	}

	return stmts
}

func canonicalEntityProperties(
	entity projector.EntityRow,
	scopeID string,
	generationID string,
) map[string]any {
	properties := map[string]any{
		"id":              entity.EntityID,
		"name":            entity.EntityName,
		"path":            entity.FilePath,
		"relative_path":   entity.RelativePath,
		"line_number":     entity.StartLine,
		"start_line":      entity.StartLine,
		"end_line":        entity.EndLine,
		"repo_id":         entity.RepoID,
		"language":        entity.Language,
		"lang":            entity.Language,
		"scope_id":        scopeID,
		"generation_id":   generationID,
		"evidence_source": "projector/canonical",
	}

	row := map[string]any{
		"entity_metadata": entity.Metadata,
		"language":        entity.Language,
		"label":           entity.Label,
	}
	if metadata := canonicalEntityMetadataProperties(row); len(metadata) > 0 {
		for key, value := range metadata {
			properties[key] = value
		}
	}
	if metadata := canonicalTypeScriptClassFamilyMetadata(row); len(metadata) > 0 {
		for key, value := range metadata {
			properties[key] = value
		}
	}

	return properties
}

func (w *CanonicalNodeWriter) buildEntityContainmentStatements(mat projector.CanonicalMaterialization) []Statement {
	if w.entityContainmentInEntityUpsert {
		return nil
	}

	byLabel := canonicalEntityContainmentRowsByLabelAndFile(mat)
	if len(byLabel) == 0 {
		return nil
	}

	var stmts []Statement
	for _, label := range sortedCanonicalEntityContainmentLabels(byLabel) {
		batchSize := 0
		if w.entityLabelBatchSizes != nil {
			batchSize = w.entityLabelBatchSizes[label]
		}
		if batchSize <= 0 {
			batchSize = w.entityBatchSize
		}
		if batchSize <= 0 {
			batchSize = w.batchSize
		}
		if batchSize <= 0 {
			batchSize = DefaultBatchSize
		}

		byFile := byLabel[label]
		for _, filePath := range sortedCanonicalEntityContainmentFiles(byFile) {
			rows := byFile[filePath]
			for start := 0; start < len(rows); start += batchSize {
				end := start + batchSize
				if end > len(rows) {
					end = len(rows)
				}
				batchRows := rows[start:end]
				statementSummary := fmt.Sprintf(
					"label=%s containment file=%s rows=%d first_id=%v last_id=%v",
					label,
					filePath,
					len(batchRows),
					batchRows[0]["entity_id"],
					batchRows[len(batchRows)-1]["entity_id"],
				)
				stmts = append(stmts, Statement{
					Operation: OperationCanonicalUpsert,
					Cypher:    fmt.Sprintf(canonicalNodeEntityContainmentEdgeTemplate, label),
					Parameters: map[string]any{
						"file_path":                      filePath,
						"rows":                           append([]map[string]any(nil), batchRows...),
						StatementMetadataPhaseKey:        CanonicalPhaseEntityContainment,
						StatementMetadataEntityLabelKey:  label,
						StatementMetadataSummaryKey:      statementSummary,
						StatementMetadataScopeIDKey:      mat.ScopeID,
						StatementMetadataGenerationIDKey: mat.GenerationID,
					},
				})
			}
		}
	}

	return stmts
}
