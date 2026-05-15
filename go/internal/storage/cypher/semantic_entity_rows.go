package cypher

import (
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func newSemanticRowsByLabel() map[string][]map[string]any {
	return map[string][]map[string]any{
		"Annotation":             nil,
		"Typedef":                nil,
		"TypeAlias":              nil,
		"TypeAnnotation":         nil,
		"Component":              nil,
		"Module":                 nil,
		"ImplBlock":              nil,
		"Protocol":               nil,
		"ProtocolImplementation": nil,
		"Variable":               nil,
		"Function":               nil,
	}
}

func semanticEntityPlans() []struct {
	label  string
	cypher string
} {
	return []struct {
		label  string
		cypher string
	}{
		{label: "Annotation", cypher: semanticAnnotationUpsertCypher},
		{label: "Typedef", cypher: semanticTypedefUpsertCypher},
		{label: "TypeAlias", cypher: semanticTypeAliasUpsertCypher},
		{label: "TypeAnnotation", cypher: semanticTypeAnnotationUpsertCypher},
		{label: "Component", cypher: semanticComponentUpsertCypher},
		{label: "Module", cypher: semanticModuleUpsertCypher},
		{label: "ImplBlock", cypher: semanticImplBlockUpsertCypher},
		{label: "Protocol", cypher: semanticProtocolUpsertCypher},
		{label: "ProtocolImplementation", cypher: semanticProtocolImplementationUpsertCypher},
		{label: "Variable", cypher: semanticVariableUpsertCypher},
		{label: "Function", cypher: semanticFunctionUpsertCypher},
	}
}

func buildParameterizedSemanticEntityStatement(row reducer.SemanticEntityRow) (Statement, bool) {
	rowMap, ok := buildSemanticEntityRowMap(row)
	if !ok {
		return Statement{}, false
	}

	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    semanticEntitySingleRowUpsertCypher(row.EntityType),
		Parameters: map[string]any{
			"file_path":                     rowMap["file_path"],
			"entity_id":                     rowMap["entity_id"],
			"properties":                    semanticEntityProperties(rowMap),
			StatementMetadataEntityLabelKey: row.EntityType,
			StatementMetadataSummaryKey:     fmt.Sprintf("semantic label=%s rows=1 entity_id=%v fallback=singleton_parameterized", row.EntityType, rowMap["entity_id"]),
		},
	}, true
}

func buildSemanticEntityPropertyRowMap(row reducer.SemanticEntityRow) (map[string]any, bool) {
	rowMap, ok := buildSemanticEntityRowMap(row)
	if !ok {
		return nil, false
	}
	return map[string]any{
		"file_path":  rowMap["file_path"],
		"entity_id":  rowMap["entity_id"],
		"properties": semanticEntityProperties(rowMap),
	}, true
}

func semanticEntityStatementSummary(label string, rows []map[string]any) string {
	if len(rows) == 0 {
		return fmt.Sprintf("semantic label=%s rows=0", label)
	}
	firstID := rows[0]["entity_id"]
	lastID := rows[len(rows)-1]["entity_id"]
	return fmt.Sprintf("semantic label=%s rows=%d first_id=%v last_id=%v", label, len(rows), firstID, lastID)
}

func semanticEntityRetractStatementSummary(label string, repoIDs []string) string {
	return fmt.Sprintf("semantic_retract label=%s repo_ids=%d", label, len(repoIDs))
}

func semanticEntityProperties(rowMap map[string]any) map[string]any {
	properties := map[string]any{
		"id":              rowMap["entity_id"],
		"name":            rowMap["entity_name"],
		"path":            rowMap["file_path"],
		"relative_path":   rowMap["relative_path"],
		"line_number":     rowMap["start_line"],
		"start_line":      rowMap["start_line"],
		"end_line":        rowMap["end_line"],
		"repo_id":         rowMap["repo_id"],
		"language":        rowMap["language"],
		"lang":            rowMap["language"],
		"evidence_source": rowMap["evidence_source"],
	}

	if semanticKind, ok := rowMap["semantic_kind"]; ok && semanticKind != nil {
		properties["semantic_kind"] = semanticKind
	} else {
		properties["semantic_kind"] = rowMap["entity_type"]
	}

	for _, key := range []string{
		"kind",
		"target_kind",
		"type",
		"type_alias_kind",
		"type_parameters",
		"framework",
		"module_kind",
		"declaration_merge_group",
		"declaration_merge_count",
		"declaration_merge_kinds",
		"jsx_fragment_shorthand",
		"component_type_assertion",
		"component_wrapper_kind",
		"impl_context",
		"trait",
		"target",
		"protocol",
		"implemented_for",
		"attribute_kind",
		"value",
		"docstring",
		"class_context",
		"method_kind",
		"constructor_kind",
		"annotation_kind",
		"context",
		"enclosing_function",
		"type_annotation_count",
		"type_annotation_kinds",
		"parameter_count",
		"decorators",
		"async",
	} {
		if value, ok := rowMap[key]; ok {
			properties[key] = value
		}
	}

	return properties
}

func buildSemanticEntityRowMap(row reducer.SemanticEntityRow) (map[string]any, bool) {
	if row.RepoID == "" || row.EntityID == "" || row.EntityName == "" || row.FilePath == "" {
		return nil, false
	}
	if row.EntityType != "Annotation" && row.EntityType != "Typedef" && row.EntityType != "TypeAlias" &&
		row.EntityType != "TypeAnnotation" && row.EntityType != "Component" && row.EntityType != "Module" && row.EntityType != "ImplBlock" &&
		row.EntityType != "Protocol" && row.EntityType != "ProtocolImplementation" &&
		row.EntityType != "Variable" && row.EntityType != "Function" {
		return nil, false
	}
	if row.StartLine <= 0 {
		return nil, false
	}

	rowMap := map[string]any{
		"repo_id":         row.RepoID,
		"entity_id":       row.EntityID,
		"entity_type":     row.EntityType,
		"entity_name":     row.EntityName,
		"file_path":       row.FilePath,
		"relative_path":   row.RelativePath,
		"language":        row.Language,
		"start_line":      row.StartLine,
		"end_line":        row.EndLine,
		"evidence_source": semanticEntityEvidenceSource,
	}
	if row.Metadata != nil {
		if kind := semanticMetadataString(row.Metadata, "kind"); kind != "" {
			rowMap["kind"] = kind
		}
		if targetKind := semanticMetadataString(row.Metadata, "target_kind"); targetKind != "" {
			rowMap["target_kind"] = targetKind
		}
		if typ := semanticMetadataString(row.Metadata, "type"); typ != "" {
			rowMap["type"] = typ
		}
		if aliasKind := semanticMetadataString(row.Metadata, "type_alias_kind"); aliasKind != "" {
			rowMap["type_alias_kind"] = aliasKind
		}
		if typeParameters := semanticMetadataStringSlice(row.Metadata, "type_parameters"); len(typeParameters) > 0 {
			rowMap["type_parameters"] = typeParameters
		}
		if framework := semanticMetadataString(row.Metadata, "framework"); framework != "" {
			rowMap["framework"] = framework
		}
		if moduleKind := semanticMetadataString(row.Metadata, "module_kind"); moduleKind != "" {
			rowMap["module_kind"] = moduleKind
		}
		if declarationMergeGroup := semanticMetadataString(row.Metadata, "declaration_merge_group"); declarationMergeGroup != "" {
			rowMap["declaration_merge_group"] = declarationMergeGroup
		}
		if declarationMergeCount := semanticMetadataInt(row.Metadata, "declaration_merge_count"); declarationMergeCount > 0 {
			rowMap["declaration_merge_count"] = declarationMergeCount
		}
		if declarationMergeKinds := semanticMetadataStringSlice(row.Metadata, "declaration_merge_kinds"); len(declarationMergeKinds) > 0 {
			rowMap["declaration_merge_kinds"] = declarationMergeKinds
		}
		if jsxFragment := semanticMetadataBool(row.Metadata, "jsx_fragment_shorthand"); jsxFragment {
			rowMap["jsx_fragment_shorthand"] = true
		}
		if componentAssertion := semanticMetadataString(row.Metadata, "component_type_assertion"); componentAssertion != "" {
			rowMap["component_type_assertion"] = componentAssertion
		}
		if componentWrapper := semanticMetadataString(row.Metadata, "component_wrapper_kind"); componentWrapper != "" {
			rowMap["component_wrapper_kind"] = componentWrapper
		}
		if implContext := semanticMetadataString(row.Metadata, "impl_context"); implContext != "" {
			rowMap["impl_context"] = implContext
		}
		if trait := semanticMetadataString(row.Metadata, "trait"); trait != "" {
			rowMap["trait"] = trait
		}
		if target := semanticMetadataString(row.Metadata, "target"); target != "" {
			rowMap["target"] = target
		}
		if protocol := semanticMetadataString(row.Metadata, "protocol"); protocol != "" {
			rowMap["protocol"] = protocol
		}
		if implementedFor := semanticMetadataString(row.Metadata, "implemented_for"); implementedFor != "" {
			rowMap["implemented_for"] = implementedFor
		}
		if attributeKind := semanticMetadataString(row.Metadata, "attribute_kind"); attributeKind != "" {
			rowMap["attribute_kind"] = attributeKind
		}
		if value := semanticMetadataString(row.Metadata, "value"); value != "" {
			rowMap["value"] = value
		}
		if docstring := semanticMetadataString(row.Metadata, "docstring"); docstring != "" {
			rowMap["docstring"] = docstring
		}
		if classContext := semanticMetadataString(row.Metadata, "class_context"); classContext != "" {
			rowMap["class_context"] = classContext
		}
		if methodKind := semanticMetadataString(row.Metadata, "method_kind"); methodKind != "" {
			rowMap["method_kind"] = methodKind
		}
		if constructorKind := semanticMetadataString(row.Metadata, "constructor_kind"); constructorKind != "" {
			rowMap["constructor_kind"] = constructorKind
		}
		if annotationKind := semanticMetadataString(row.Metadata, "annotation_kind"); annotationKind != "" {
			rowMap["annotation_kind"] = annotationKind
		}
		if context := semanticMetadataString(row.Metadata, "context"); context != "" {
			rowMap["context"] = context
		}
		if enclosingFunction := semanticMetadataString(row.Metadata, "enclosing_function"); enclosingFunction != "" {
			rowMap["enclosing_function"] = enclosingFunction
		}
		if count := semanticMetadataInt(row.Metadata, "type_annotation_count"); count > 0 {
			rowMap["type_annotation_count"] = count
		}
		if count := semanticMetadataInt(row.Metadata, "parameter_count"); count > 0 {
			rowMap["parameter_count"] = count
		}
		if kinds := semanticMetadataStringSlice(row.Metadata, "type_annotation_kinds"); len(kinds) > 0 {
			rowMap["type_annotation_kinds"] = kinds
		}
		if jsxFragment := semanticMetadataBool(row.Metadata, "jsx_fragment_shorthand"); jsxFragment {
			rowMap["jsx_fragment_shorthand"] = true
		}
		if decorators := semanticMetadataStringSlice(row.Metadata, "decorators"); len(decorators) > 0 {
			rowMap["decorators"] = decorators
		}
		if async := semanticMetadataBool(row.Metadata, "async"); async {
			rowMap["async"] = true
		}
		if semanticKind := semanticMetadataString(row.Metadata, "semantic_kind"); semanticKind != "" {
			rowMap["semantic_kind"] = semanticKind
		}
	}
	semanticEntityEnsureNullableKeys(rowMap)
	return rowMap, true
}

func semanticEntityEnsureNullableKeys(rowMap map[string]any) {
	for _, key := range semanticEntityNullableRowKeys {
		if _, ok := rowMap[key]; !ok {
			rowMap[key] = nil
		}
	}
}

var semanticEntityNullableRowKeys = []string{
	"kind",
	"target_kind",
	"type",
	"type_alias_kind",
	"type_parameters",
	"framework",
	"module_kind",
	"declaration_merge_group",
	"declaration_merge_count",
	"declaration_merge_kinds",
	"jsx_fragment_shorthand",
	"component_type_assertion",
	"component_wrapper_kind",
	"impl_context",
	"trait",
	"target",
	"protocol",
	"implemented_for",
	"attribute_kind",
	"value",
	"docstring",
	"class_context",
	"method_kind",
	"constructor_kind",
	"annotation_kind",
	"context",
	"enclosing_function",
	"type_annotation_count",
	"type_annotation_kinds",
	"parameter_count",
	"decorators",
	"async",
	"semantic_kind",
}

func buildRustImplBlockOwnershipRows(rows []reducer.SemanticEntityRow) []map[string]any {
	if len(rows) == 0 {
		return nil
	}

	functionIDsByFileAndContext := make(map[string][]string)
	for _, row := range rows {
		if row.EntityType != "Function" {
			continue
		}
		implContext := semanticMetadataString(row.Metadata, "impl_context")
		if implContext == "" || row.FilePath == "" || row.EntityID == "" {
			continue
		}
		key := row.FilePath + "|" + implContext
		functionIDsByFileAndContext[key] = append(functionIDsByFileAndContext[key], row.EntityID)
	}

	seen := make(map[string]struct{})
	ownershipRows := make([]map[string]any, 0)
	for _, row := range rows {
		if row.EntityType != "ImplBlock" || row.FilePath == "" || row.EntityID == "" || row.EntityName == "" {
			continue
		}
		functionIDs := functionIDsByFileAndContext[row.FilePath+"|"+row.EntityName]
		for _, functionID := range functionIDs {
			dedupKey := row.EntityID + "->" + functionID
			if _, ok := seen[dedupKey]; ok {
				continue
			}
			seen[dedupKey] = struct{}{}
			ownershipRows = append(ownershipRows, map[string]any{
				"impl_block_id": row.EntityID,
				"function_id":   functionID,
			})
		}
	}

	return ownershipRows
}

func uniqueSemanticRepoIDs(repoIDs []string) []string {
	seen := make(map[string]struct{})
	unique := make([]string, 0, len(repoIDs))
	for _, repoID := range repoIDs {
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		unique = append(unique, repoID)
	}
	sort.Strings(unique)
	return unique
}
