// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package taxonomy

import "github.com/eshu-hq/eshu/go/internal/query/querycontract/rowvalue"

// GraphResultMetadata projects the optional semantic-metadata columns
// GraphSemanticMetadataProjection (rows) selects into the
// "metadata" field of a language-query or entity result row, omitting keys
// whose value is absent or empty. The implementation moved from root's
// language_query_entities.go for #6060 so a handler-family subpackage can
// shape a graph row's metadata without importing root.
func GraphResultMetadata(row map[string]any) map[string]any {
	metadata := map[string]any{}
	if v := rowvalue.StringVal(row, "docstring"); v != "" {
		metadata["docstring"] = v
	}
	if v := rowvalue.StringVal(row, "class_context"); v != "" {
		metadata["class_context"] = v
	}
	if v := rowvalue.StringVal(row, "method_kind"); v != "" {
		metadata["method_kind"] = v
	}
	if v := rowvalue.StringVal(row, "constructor_kind"); v != "" {
		metadata["constructor_kind"] = v
	}
	if v := rowvalue.StringVal(row, "annotation_kind"); v != "" {
		metadata["annotation_kind"] = v
	}
	if v := rowvalue.StringVal(row, "context"); v != "" {
		metadata["context"] = v
	}
	if v := rowvalue.IntVal(row, "type_annotation_count"); v > 0 {
		metadata["type_annotation_count"] = v
	}
	if values := rowvalue.StringSliceVal(row, "type_annotation_kinds"); len(values) > 0 {
		typeAnnotationKinds := make([]any, 0, len(values))
		for _, value := range values {
			typeAnnotationKinds = append(typeAnnotationKinds, value)
		}
		metadata["type_annotation_kinds"] = typeAnnotationKinds
	}
	if values := rowvalue.StringSliceVal(row, "type_parameters"); len(values) > 0 {
		typeParameters := make([]any, 0, len(values))
		for _, value := range values {
			typeParameters = append(typeParameters, value)
		}
		metadata["type_parameters"] = typeParameters
	}
	if values := rowvalue.StringSliceVal(row, "dead_code_root_kinds"); len(values) > 0 {
		rootKinds := make([]any, 0, len(values))
		for _, value := range values {
			rootKinds = append(rootKinds, value)
		}
		metadata["dead_code_root_kinds"] = rootKinds
	}
	if v := rowvalue.StringVal(row, "type_alias_kind"); v != "" {
		metadata["type_alias_kind"] = v
	}
	if v := rowvalue.StringVal(row, "framework"); v != "" {
		metadata["framework"] = v
	}
	if v := rowvalue.StringVal(row, "module_kind"); v != "" {
		metadata["module_kind"] = v
	}
	if v, ok := row["jsx_fragment_shorthand"].(bool); ok {
		metadata["jsx_fragment_shorthand"] = v
	}
	if v := rowvalue.StringVal(row, "component_type_assertion"); v != "" {
		metadata["component_type_assertion"] = v
	}
	if v := rowvalue.StringVal(row, "component_wrapper_kind"); v != "" {
		metadata["component_wrapper_kind"] = v
	}
	if v := rowvalue.StringVal(row, "protocol"); v != "" {
		metadata["protocol"] = v
	}
	if v := rowvalue.StringVal(row, "implemented_for"); v != "" {
		metadata["implemented_for"] = v
	}
	if v := rowvalue.StringVal(row, "attribute_kind"); v != "" {
		metadata["attribute_kind"] = v
	}
	if v := rowvalue.StringVal(row, "value"); v != "" {
		metadata["value"] = v
	}
	if v := rowvalue.StringVal(row, "declaration_merge_group"); v != "" {
		metadata["declaration_merge_group"] = v
	}
	if v := rowvalue.IntVal(row, "declaration_merge_count"); v > 0 {
		metadata["declaration_merge_count"] = v
	}
	if values := rowvalue.StringSliceVal(row, "declaration_merge_kinds"); len(values) > 0 {
		declarationMergeKinds := make([]any, 0, len(values))
		for _, value := range values {
			declarationMergeKinds = append(declarationMergeKinds, value)
		}
		metadata["declaration_merge_kinds"] = declarationMergeKinds
	}
	if v := rowvalue.StringVal(row, "kind"); v != "" {
		metadata["kind"] = v
	}
	if v := rowvalue.StringVal(row, "target_kind"); v != "" {
		metadata["target_kind"] = v
	}
	if v := rowvalue.StringVal(row, "type"); v != "" {
		metadata["type"] = v
	}
	if values := rowvalue.StringSliceVal(row, "decorators"); len(values) > 0 {
		decorators := make([]any, 0, len(values))
		for _, value := range values {
			decorators = append(decorators, value)
		}
		metadata["decorators"] = decorators
	}
	if v, ok := row["async"].(bool); ok {
		metadata["async"] = v
	}
	if v := rowvalue.StringVal(row, "semantic_kind"); v != "" {
		metadata["semantic_kind"] = v
	}
	if v := rowvalue.StringVal(row, "metaclass"); v != "" {
		metadata["metaclass"] = v
	}
	if v := rowvalue.StringVal(row, "source"); v != "" {
		metadata["source"] = v
	}
	if v := rowvalue.StringVal(row, "terraform_source"); v != "" {
		metadata["terraform_source"] = v
	}
	if v := rowvalue.StringVal(row, "config_path"); v != "" {
		metadata["config_path"] = v
	}
	if values := rowvalue.StringSliceVal(row, "includes"); len(values) > 0 {
		includes := make([]any, 0, len(values))
		for _, value := range values {
			includes = append(includes, value)
		}
		metadata["includes"] = includes
	}
	if values := rowvalue.StringSliceVal(row, "inputs"); len(values) > 0 {
		inputs := make([]any, 0, len(values))
		for _, value := range values {
			inputs = append(inputs, value)
		}
		metadata["inputs"] = inputs
	}
	if values := rowvalue.StringSliceVal(row, "locals"); len(values) > 0 {
		locals := make([]any, 0, len(values))
		for _, value := range values {
			locals = append(locals, value)
		}
		metadata["locals"] = locals
	}
	if v := rowvalue.StringVal(row, "deployment_name"); v != "" {
		metadata["deployment_name"] = v
	}
	if v := rowvalue.StringVal(row, "entity_repo_name"); v != "" {
		metadata["repo_name"] = v
	}
	if v := rowvalue.StringVal(row, "create_deploy"); v != "" {
		metadata["create_deploy"] = v
	}
	if v := rowvalue.StringVal(row, "cluster_name"); v != "" {
		metadata["cluster_name"] = v
	}
	if v := rowvalue.StringVal(row, "zone_id"); v != "" {
		metadata["zone_id"] = v
	}
	if v := rowvalue.StringVal(row, "deploy_entry_point"); v != "" {
		metadata["deploy_entry_point"] = v
	}
	if v := rowvalue.StringVal(row, "qualified_name"); v != "" {
		metadata["qualified_name"] = v
	}
	if v := rowvalue.StringVal(row, "sql_entity_type"); v != "" {
		metadata["sql_entity_type"] = v
	}
	if v := rowvalue.StringVal(row, "schema"); v != "" {
		metadata["schema"] = v
	}
	if v := rowvalue.StringVal(row, "data_type"); v != "" {
		metadata["data_type"] = v
	}
	if v := rowvalue.StringVal(row, "table_name"); v != "" {
		metadata["table_name"] = v
	}
	if v := rowvalue.StringVal(row, "column_name"); v != "" {
		metadata["column_name"] = v
	}
	if v := rowvalue.StringVal(row, "routine_kind"); v != "" {
		metadata["routine_kind"] = v
	}
	if v := rowvalue.StringVal(row, "function_language"); v != "" {
		metadata["function_language"] = v
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}
