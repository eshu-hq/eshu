package graph

import (
	"context"
	"fmt"
	"strings"
)

// DefaultBatchSize is the default number of rows per UNWIND batch.
const DefaultBatchSize = 500

// BatchEntityRow holds the properties for one entity in a batch UNWIND
// merge. All rows within a single batch must share the same Label.
type BatchEntityRow struct {
	// FilePath is the absolute path to the file containing the entity.
	FilePath string

	// Name is the entity's short name.
	Name string

	// LineNumber is the 1-based line where the entity is defined.
	LineNumber int

	// UID is the optional canonical unique identifier. When set, the row
	// uses uid-based merge identity.
	UID string

	// Extra holds additional properties to set on the node.
	Extra map[string]any
}

// BatchFileRow holds the properties for one file node in a batch merge.
type BatchFileRow struct {
	// FilePath is the absolute path to the file.
	FilePath string

	// Name is the file's base name.
	Name string

	// RelativePath is the repo-relative path.
	RelativePath string

	// Language is the detected language of the file.
	Language string

	// IsDependency flags whether this is a dependency file.
	IsDependency bool
}

// BatchRelationshipRow holds the properties for one relationship in a batch.
type BatchRelationshipRow struct {
	// SourceLabel is the Neo4j label of the source node.
	SourceLabel string

	// SourceKey holds the identity properties for the source node.
	SourceKey map[string]any

	// TargetLabel is the Neo4j label of the target node.
	TargetLabel string

	// TargetKey holds the identity properties for the target node.
	TargetKey map[string]any

	// RelType is the relationship type (e.g., "CALLS", "INHERITS").
	RelType string

	// RelProps holds optional properties to set on the relationship.
	RelProps map[string]any
}

// BatchMergeEntities executes batched UNWIND merges for entities of a single
// label. Rows are split into UID-identity and name-identity groups and
// merged separately so Neo4j can use indexes directly on the MERGE clause.
// This mirrors the Python run_entity_unwind in graph/persistence/unwind.py.
func BatchMergeEntities(
	ctx context.Context,
	executor CypherExecutor,
	label string,
	rows []BatchEntityRow,
	batchSize int,
) error {
	if executor == nil {
		return fmt.Errorf("batch entity executor is required")
	}
	if err := ValidateCypherLabel(label); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	// Collect all extra keys across all rows for uniform shape.
	allExtraKeys := collectExtraKeys(rows)
	if err := ValidateCypherPropertyKeys(allExtraKeys); err != nil {
		return err
	}

	// Split into UID-identity and name-identity rows.
	var uidRows, nameRows []map[string]any
	for i := range rows {
		rowMap := entityRowToMap(rows[i], allExtraKeys)
		if rows[i].UID != "" {
			uidRows = append(uidRows, rowMap)
		} else {
			nameRows = append(nameRows, rowMap)
		}
	}

	// Build SET clause shared by all queries.
	setClause := buildEntitySetClause(allExtraKeys)

	// Execute UID-identity batches.
	if len(uidRows) > 0 {
		cypher := fmt.Sprintf(`UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:%s {uid: row.uid})
SET %s
MERGE (f)-[:CONTAINS]->(n)`, label, setClause)

		if err := executeBatched(ctx, executor, cypher, uidRows, batchSize); err != nil {
			return fmt.Errorf("batch uid entity merge %s: %w", label, err)
		}
	}

	// Execute name-identity batches.
	if len(nameRows) > 0 {
		cypher := fmt.Sprintf(`UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:%s {name: row.name, path: row.file_path, line_number: row.line_number})
SET %s
MERGE (f)-[:CONTAINS]->(n)`, label, setClause)

		if err := executeBatched(ctx, executor, cypher, nameRows, batchSize); err != nil {
			return fmt.Errorf("batch name entity merge %s: %w", label, err)
		}
	}

	return nil
}

// BatchMergeFiles executes batched UNWIND merges for file nodes.
func BatchMergeFiles(
	ctx context.Context,
	executor CypherExecutor,
	rows []BatchFileRow,
	batchSize int,
) error {
	if executor == nil {
		return fmt.Errorf("batch file executor is required")
	}
	if len(rows) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	rowMaps := make([]map[string]any, len(rows))
	for i := range rows {
		rowMaps[i] = map[string]any{
			"file_path":     rows[i].FilePath,
			"name":          rows[i].Name,
			"relative_path": rows[i].RelativePath,
			"language":      rows[i].Language,
			"is_dependency": rows[i].IsDependency,
		}
	}

	cypher := `UNWIND $rows AS row
MERGE (f:File {path: row.file_path})
SET f.name = row.name,
    f.relative_path = row.relative_path,
    f.lang = row.language,
    f.is_dependency = row.is_dependency`

	if err := executeBatched(ctx, executor, cypher, rowMaps, batchSize); err != nil {
		return fmt.Errorf("batch file merge: %w", err)
	}

	return nil
}

// BatchMergeRelationships executes batched relationship MERGE operations.
// All rows must share the same source label, target label, and relationship
// type.
func BatchMergeRelationships(
	ctx context.Context,
	executor CypherExecutor,
	rows []BatchRelationshipRow,
	batchSize int,
) error {
	if executor == nil {
		return fmt.Errorf("batch relationship executor is required")
	}
	if len(rows) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	// All rows must share the same labels and relationship type.
	sourceLabel := rows[0].SourceLabel
	targetLabel := rows[0].TargetLabel
	relType := rows[0].RelType

	if err := ValidateCypherLabel(sourceLabel); err != nil {
		return fmt.Errorf("source label: %w", err)
	}
	if err := ValidateCypherLabel(targetLabel); err != nil {
		return fmt.Errorf("target label: %w", err)
	}
	if err := ValidateCypherLabel(relType); err != nil {
		return fmt.Errorf("relationship type: %w", err)
	}

	// Collect source and target key names.
	sourceKeyNames := collectMapKeys(rows[0].SourceKey)
	targetKeyNames := collectMapKeys(rows[0].TargetKey)
	if err := ValidateCypherPropertyKeys(sourceKeyNames); err != nil {
		return fmt.Errorf("source key: %w", err)
	}
	if err := ValidateCypherPropertyKeys(targetKeyNames); err != nil {
		return fmt.Errorf("target key: %w", err)
	}

	// Build MATCH clauses.
	sourceMatch := buildMatchClause("src", sourceLabel, sourceKeyNames)
	targetMatch := buildMatchClause("tgt", targetLabel, targetKeyNames)

	// Build relationship SET clause if properties exist.
	relPropKeys := collectRelPropKeys(rows)
	var relSetClause string
	if len(relPropKeys) > 0 {
		if err := ValidateCypherPropertyKeys(relPropKeys); err != nil {
			return fmt.Errorf("relationship property: %w", err)
		}
		parts := make([]string, len(relPropKeys))
		for i, k := range relPropKeys {
			parts[i] = fmt.Sprintf("rel.`%s` = row.rel_%s", k, k)
		}
		relSetClause = "\nSET " + strings.Join(parts, ", ")
	}

	cypher := fmt.Sprintf(`UNWIND $rows AS row
%s
%s
MERGE (src)-[rel:%s]->(tgt)%s`, sourceMatch, targetMatch, relType, relSetClause)

	// Build row maps.
	rowMaps := make([]map[string]any, len(rows))
	for i := range rows {
		m := make(map[string]any)
		for _, k := range sourceKeyNames {
			m["src_"+k] = rows[i].SourceKey[k]
		}
		for _, k := range targetKeyNames {
			m["tgt_"+k] = rows[i].TargetKey[k]
		}
		for _, k := range relPropKeys {
			if rows[i].RelProps != nil {
				m["rel_"+k] = rows[i].RelProps[k]
			}
		}
		rowMaps[i] = m
	}

	if err := executeBatched(ctx, executor, cypher, rowMaps, batchSize); err != nil {
		return fmt.Errorf("batch relationship merge %s: %w", relType, err)
	}

	return nil
}

// executeBatched splits rows into chunks and executes each as an UNWIND query.
func executeBatched(
	ctx context.Context,
	executor CypherExecutor,
	cypher string,
	rows []map[string]any,
	batchSize int,
) error {
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[start:end]

		if err := executor.ExecuteCypher(ctx, CypherStatement{
			Cypher: cypher,
			Parameters: map[string]any{
				"rows": chunk,
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

// collectExtraKeys gathers all unique extra property keys across rows.
func collectExtraKeys(rows []BatchEntityRow) []string {
	seen := make(map[string]struct{})
	for i := range rows {
		for k := range rows[i].Extra {
			seen[k] = struct{}{}
		}
	}
	return sortStringSet(seen)
}

// entityRowToMap converts a BatchEntityRow into a flat map with uniform keys.
func entityRowToMap(row BatchEntityRow, allExtraKeys []string) map[string]any {
	m := map[string]any{
		"file_path":   row.FilePath,
		"name":        row.Name,
		"line_number": row.LineNumber,
		"uid":         row.UID,
	}
	for _, k := range allExtraKeys {
		if row.Extra != nil {
			m[k] = row.Extra[k]
		} else {
			m[k] = nil
		}
	}
	return m
}

// buildEntitySetClause builds the SET clause for entity UNWIND merges.
func buildEntitySetClause(extraKeys []string) string {
	parts := []string{
		"n.name = row.name",
		"n.path = row.file_path",
		"n.line_number = row.line_number",
	}
	for _, k := range extraKeys {
		parts = append(parts, fmt.Sprintf("n.`%s` = row.`%s`", k, k))
	}
	return strings.Join(parts, ", ")
}

// buildMatchClause builds a MATCH clause for a node with identity properties
// sourced from UNWIND row parameters.
func buildMatchClause(alias, label string, keyNames []string) string {
	parts := make([]string, len(keyNames))
	for i, k := range keyNames {
		parts[i] = fmt.Sprintf("%s: row.%s_%s", k, alias, k)
	}
	return fmt.Sprintf("MATCH (%s:%s {%s})", alias, label, strings.Join(parts, ", "))
}

// collectMapKeys returns sorted keys from a map.
func collectMapKeys(m map[string]any) []string {
	seen := make(map[string]struct{}, len(m))
	for k := range m {
		seen[k] = struct{}{}
	}
	return sortStringSet(seen)
}

// collectRelPropKeys returns sorted unique relationship property keys.
func collectRelPropKeys(rows []BatchRelationshipRow) []string {
	seen := make(map[string]struct{})
	for i := range rows {
		for k := range rows[i].RelProps {
			seen[k] = struct{}{}
		}
	}
	return sortStringSet(seen)
}

// sortStringSet returns a sorted slice from a set.
func sortStringSet(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	result := make([]string, 0, len(set))
	for k := range set {
		result = append(result, k)
	}
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && result[j] < result[j-1]; j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}
	return result
}
