package cypher

import "strings"

const (
	semanticEntityEvidenceSource = "parser/semantic-entities"

	semanticAnnotationUpsertCypher = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:Annotation {uid: row.entity_id})
SET n.id = row.entity_id,
    n.name = row.entity_name,
    n.path = row.file_path,
    n.relative_path = row.relative_path,
    n.line_number = row.start_line,
    n.start_line = row.start_line,
    n.end_line = row.end_line,
    n.repo_id = row.repo_id,
    n.language = row.language,
    n.lang = row.language,
    n.kind = row.kind,
    n.target_kind = row.target_kind,
    n.semantic_kind = coalesce(row.semantic_kind, row.entity_type),
    n.evidence_source = row.evidence_source
MERGE (f)-[:CONTAINS]->(n)`

	semanticTypedefUpsertCypher = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:Typedef {uid: row.entity_id})
SET n.id = row.entity_id,
    n.name = row.entity_name,
    n.path = row.file_path,
    n.relative_path = row.relative_path,
    n.line_number = row.start_line,
    n.start_line = row.start_line,
    n.end_line = row.end_line,
    n.repo_id = row.repo_id,
    n.language = row.language,
    n.lang = row.language,
    n.type = row.type,
    n.semantic_kind = coalesce(row.semantic_kind, row.entity_type),
    n.evidence_source = row.evidence_source
MERGE (f)-[:CONTAINS]->(n)`

	semanticTypeAliasUpsertCypher = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:TypeAlias {uid: row.entity_id})
SET n.id = row.entity_id,
    n.name = row.entity_name,
    n.path = row.file_path,
    n.relative_path = row.relative_path,
    n.line_number = row.start_line,
    n.start_line = row.start_line,
    n.end_line = row.end_line,
    n.repo_id = row.repo_id,
    n.language = row.language,
    n.lang = row.language,
    n.type_alias_kind = row.type_alias_kind,
    n.type_parameters = row.type_parameters,
    n.semantic_kind = coalesce(row.semantic_kind, row.entity_type),
    n.evidence_source = row.evidence_source
MERGE (f)-[:CONTAINS]->(n)`

	semanticTypeAnnotationUpsertCypher = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:TypeAnnotation {uid: row.entity_id})
SET n.id = row.entity_id,
    n.name = row.entity_name,
    n.path = row.file_path,
    n.relative_path = row.relative_path,
    n.line_number = row.start_line,
    n.start_line = row.start_line,
    n.end_line = row.end_line,
    n.repo_id = row.repo_id,
    n.language = row.language,
    n.lang = row.language,
    n.annotation_kind = row.annotation_kind,
    n.context = row.context,
    n.type = row.type,
    n.semantic_kind = coalesce(row.semantic_kind, row.entity_type),
    n.evidence_source = row.evidence_source
MERGE (f)-[:CONTAINS]->(n)`

	semanticComponentUpsertCypher = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:Component {uid: row.entity_id})
SET n.id = row.entity_id,
    n.name = row.entity_name,
    n.path = row.file_path,
    n.relative_path = row.relative_path,
    n.line_number = row.start_line,
    n.start_line = row.start_line,
    n.end_line = row.end_line,
    n.repo_id = row.repo_id,
	n.language = row.language,
	n.lang = row.language,
	n.framework = row.framework,
	n.jsx_fragment_shorthand = row.jsx_fragment_shorthand,
	n.component_type_assertion = row.component_type_assertion,
	n.component_wrapper_kind = row.component_wrapper_kind,
	n.semantic_kind = coalesce(row.semantic_kind, row.entity_type),
	n.evidence_source = row.evidence_source
MERGE (f)-[:CONTAINS]->(n)`

	semanticImplBlockUpsertCypher = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:ImplBlock {uid: row.entity_id})
SET n.id = row.entity_id,
    n.name = row.entity_name,
    n.path = row.file_path,
    n.relative_path = row.relative_path,
    n.line_number = row.start_line,
    n.start_line = row.start_line,
    n.end_line = row.end_line,
    n.repo_id = row.repo_id,
    n.language = row.language,
    n.lang = row.language,
    n.kind = row.kind,
    n.trait = row.trait,
    n.target = row.target,
    n.semantic_kind = coalesce(row.semantic_kind, row.entity_type),
    n.evidence_source = row.evidence_source
MERGE (f)-[:CONTAINS]->(n)`

	semanticProtocolUpsertCypher = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:Protocol {uid: row.entity_id})
SET n.id = row.entity_id,
    n.name = row.entity_name,
    n.path = row.file_path,
    n.relative_path = row.relative_path,
    n.line_number = row.start_line,
    n.start_line = row.start_line,
    n.end_line = row.end_line,
    n.repo_id = row.repo_id,
    n.language = row.language,
    n.lang = row.language,
    n.module_kind = row.module_kind,
    n.semantic_kind = coalesce(row.semantic_kind, row.entity_type),
    n.evidence_source = row.evidence_source
MERGE (f)-[:CONTAINS]->(n)`

	semanticProtocolImplementationUpsertCypher = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:ProtocolImplementation {uid: row.entity_id})
SET n.id = row.entity_id,
    n.name = row.entity_name,
    n.path = row.file_path,
    n.relative_path = row.relative_path,
    n.line_number = row.start_line,
    n.start_line = row.start_line,
    n.end_line = row.end_line,
    n.repo_id = row.repo_id,
    n.language = row.language,
    n.lang = row.language,
    n.module_kind = row.module_kind,
    n.protocol = row.protocol,
    n.implemented_for = row.implemented_for,
    n.semantic_kind = coalesce(row.semantic_kind, row.entity_type),
    n.evidence_source = row.evidence_source
MERGE (f)-[:CONTAINS]->(n)`

	semanticVariableUpsertCypher = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:Variable {uid: row.entity_id})
SET n.id = row.entity_id,
    n.name = row.entity_name,
    n.path = row.file_path,
    n.relative_path = row.relative_path,
    n.line_number = row.start_line,
    n.start_line = row.start_line,
    n.end_line = row.end_line,
	    n.repo_id = row.repo_id,
	    n.language = row.language,
	    n.lang = row.language,
	    n.attribute_kind = row.attribute_kind,
	    n.value = row.value,
	    n.component_type_assertion = row.component_type_assertion,
	    n.semantic_kind = coalesce(row.semantic_kind, row.entity_type),
	    n.evidence_source = row.evidence_source
	MERGE (f)-[:CONTAINS]->(n)`

	semanticModuleUpsertCypher = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:Module {uid: row.entity_id})
SET n.id = row.entity_id,
    n.name = row.entity_name,
    n.path = row.file_path,
    n.relative_path = row.relative_path,
    n.line_number = row.start_line,
    n.start_line = row.start_line,
    n.end_line = row.end_line,
    n.repo_id = row.repo_id,
    n.language = row.language,
    n.lang = row.language,
    n.module_kind = row.module_kind,
    n.declaration_merge_group = row.declaration_merge_group,
    n.declaration_merge_count = row.declaration_merge_count,
    n.declaration_merge_kinds = row.declaration_merge_kinds,
    n.semantic_kind = coalesce(row.semantic_kind, row.entity_type),
    n.evidence_source = row.evidence_source
MERGE (f)-[:CONTAINS]->(n)`

	semanticFunctionUpsertCypher = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:Function {uid: row.entity_id})
SET n.id = row.entity_id,
    n.name = row.entity_name,
    n.path = row.file_path,
    n.relative_path = row.relative_path,
    n.line_number = row.start_line,
    n.start_line = row.start_line,
    n.end_line = row.end_line,
    n.repo_id = row.repo_id,
    n.language = row.language,
    n.lang = row.language,
    n.impl_context = row.impl_context,
    n.docstring = row.docstring,
    n.class_context = row.class_context,
    n.method_kind = row.method_kind,
    n.constructor_kind = row.constructor_kind,
    n.annotation_kind = row.annotation_kind,
    n.context = row.context,
    n.type_annotation_count = row.type_annotation_count,
    n.type_annotation_kinds = row.type_annotation_kinds,
    n.type_parameters = row.type_parameters,
    n.jsx_fragment_shorthand = row.jsx_fragment_shorthand,
    n.decorators = row.decorators,
    n.async = row.async,
    n.semantic_kind = coalesce(row.semantic_kind, row.entity_type),
    n.evidence_source = row.evidence_source
MERGE (f)-[:CONTAINS]->(n)`

	semanticRustImplBlockOwnershipCypher = `UNWIND $rows AS row
MATCH (impl:ImplBlock {uid: row.impl_block_id})
MATCH (fn:Function {uid: row.function_id})
MERGE (impl)-[:CONTAINS]->(fn)`

	semanticEntityRetractCypher = `MATCH (n:Annotation|Typedef|TypeAlias|TypeAnnotation|Component|Module|ImplBlock|Protocol|ProtocolImplementation|Variable|Function)
WHERE n.repo_id IN $repo_ids
  AND n.evidence_source = $evidence_source
DETACH DELETE n`
)

func semanticEntityLabelRetractCypher(label string) string {
	return "MATCH (n:" + label + ")\n" +
		"WHERE n.repo_id IN $repo_ids\n" +
		"  AND n.evidence_source = $evidence_source\n" +
		"DETACH DELETE n"
}

func semanticEntitySingleRowUpsertCypher(label string) string {
	return "MATCH (f:File {path: $file_path})\n" +
		"MERGE (n:" + label + " {uid: $entity_id})\n" +
		"SET n += $properties\n" +
		"MERGE (f)-[:CONTAINS]->(n)"
}

func semanticEntityBatchedPropertiesUpsertCypher(label string) string {
	return "UNWIND $rows AS row\n" +
		"MATCH (f:File {path: row.file_path})\n" +
		"MERGE (n:" + label + " {uid: row.entity_id})\n" +
		"SET n += row.properties\n" +
		"MERGE (f)-[:CONTAINS]->(n)"
}

func semanticEntityMergeFirstRowsUpsertCypher(cypher string) string {
	const unwindLine = "UNWIND $rows AS row\n"
	const fileMatchLine = "MATCH (f:File {path: row.file_path})\n"
	const containmentMerge = "MERGE (f)-[:CONTAINS]->(n)"

	if !strings.HasPrefix(cypher, unwindLine+fileMatchLine) {
		return cypher
	}
	rewritten := unwindLine + strings.TrimPrefix(cypher, unwindLine+fileMatchLine)
	containmentIndex := strings.LastIndex(rewritten, containmentMerge)
	if containmentIndex < 0 {
		return rewritten
	}
	return rewritten[:containmentIndex] + fileMatchLine + rewritten[containmentIndex:]
}

func semanticEntityCanonicalNodeRowsUpsertCypher(label string, cypher string) string {
	if !semanticEntityCanonicalNodeOwnedLabel(label) {
		return semanticEntityMergeFirstRowsUpsertCypher(cypher)
	}
	const fileMatchLine = "MATCH (f:File {path: row.file_path})"
	const containmentMerge = "MERGE (f)-[:CONTAINS]->(n)"
	const evidenceSourceAssignment = "n.evidence_source = row.evidence_source"

	rewritten := semanticEntityMergeFirstRowsUpsertCypher(cypher)
	rewritten = strings.Replace(
		rewritten,
		"MERGE (n:"+label+" {uid: row.entity_id})",
		"MATCH (n:"+label+" {uid: row.entity_id})",
		1,
	)
	lines := strings.Split(rewritten, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == fileMatchLine ||
			trimmed == containmentMerge ||
			strings.Contains(trimmed, evidenceSourceAssignment) {
			continue
		}
		out = append(out, line)
	}
	for i := len(out) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(out[i])
		if trimmed == "" {
			continue
		}
		if strings.HasSuffix(trimmed, ",") {
			out[i] = strings.TrimRight(strings.TrimRight(out[i], " \t"), ",")
		}
		break
	}
	return strings.Join(out, "\n")
}

func semanticEntityCanonicalNodeOwnedLabel(label string) bool {
	_, ok := semanticEntityCanonicalNodeClearProperties[label]
	return ok
}

func semanticEntityClearPropertiesForLabel(label string) []string {
	props := semanticEntityCanonicalNodeClearProperties[label]
	return append([]string(nil), props...)
}

func semanticEntityCanonicalNodeClearCypher(label string, properties []string) string {
	if len(properties) == 0 {
		return ""
	}
	assignments := make([]string, 0, len(properties))
	for _, property := range properties {
		property = strings.TrimSpace(property)
		if property != "" {
			assignments = append(assignments, "n."+property)
		}
	}
	return "MATCH (n:" + label + ")\n" +
		"WHERE n.repo_id IN $repo_ids\n" +
		"REMOVE " + strings.Join(assignments, ", ")
}

var semanticEntityCanonicalNodeClearProperties = map[string][]string{
	"Annotation": {
		"kind",
		"target_kind",
		"semantic_kind",
	},
	"Typedef": {
		"type",
		"semantic_kind",
	},
	"TypeAlias": {
		"type_alias_kind",
		"type_parameters",
		"semantic_kind",
	},
	"TypeAnnotation": {
		"annotation_kind",
		"context",
		"type",
		"semantic_kind",
	},
	"Component": {
		"framework",
		"jsx_fragment_shorthand",
		"component_type_assertion",
		"component_wrapper_kind",
		"semantic_kind",
	},
	"ImplBlock": {
		"kind",
		"trait",
		"target",
		"semantic_kind",
	},
	"Protocol": {
		"module_kind",
		"semantic_kind",
	},
	"ProtocolImplementation": {
		"module_kind",
		"protocol",
		"implemented_for",
		"semantic_kind",
	},
	"Variable": {
		"attribute_kind",
		"value",
		"component_type_assertion",
		"semantic_kind",
	},
	"Function": {
		"impl_context",
		"docstring",
		"class_context",
		"method_kind",
		"constructor_kind",
		"annotation_kind",
		"context",
		"type_annotation_count",
		"type_annotation_kinds",
		"type_parameters",
		"jsx_fragment_shorthand",
		"decorators",
		"async",
		"semantic_kind",
	},
}
