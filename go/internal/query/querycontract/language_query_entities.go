// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

// GraphBackedEntityTypes maps the user-facing entity type name to the Neo4j
// node label used in Cypher queries. The implementation moved from root's
// language_query_entities.go for #6060 so a handler-family subpackage can
// resolve an entity type's graph label without importing root.
var GraphBackedEntityTypes = map[string]string{
	"repository":      "Repository",
	"directory":       "Directory",
	"file":            "File",
	"module":          "Module",
	"function":        "Function",
	"class":           "Class",
	"struct":          "Struct",
	"enum":            "Enum",
	"union":           "Union",
	"macro":           "Macro",
	"type_annotation": "TypeAnnotation",
}

// ContentBackedEntityTypes maps user-facing entity types to content-entity
// labels that are already materialized in Postgres but not yet first-class in
// the graph query surface. The implementation moved from root's
// language_query_entities.go for #6060 so a handler-family subpackage can
// resolve an entity type's content label without importing root.
var ContentBackedEntityTypes = map[string]string{
	"type_alias":              "TypeAlias",
	"type_annotation":         "TypeAnnotation",
	"typedef":                 "Typedef",
	"annotation":              "Annotation",
	"protocol":                "Protocol",
	"impl_block":              "ImplBlock",
	"component":               "Component",
	"terragrunt_dependency":   "TerragruntDependency",
	"terragrunt_local":        "TerragruntLocal",
	"terragrunt_input":        "TerragruntInput",
	"guard":                   "guard",
	"protocol_implementation": "ProtocolImplementation",
	// "variable" is pure content-backed, not graph-first. The canonical-graph
	// skip in canonical_builder.go removes plain Variable nodes from the graph
	// projection but leaves ALL Variable rows (plain and semantic) in the
	// content store. A FEW semantic Variable graph nodes still exist (module
	// attributes, TSX/Elixir component-type-assertion variables), so routing
	// "variable" through the graph-first path would short-circuit on those few
	// non-empty graph rows and never fall back to content, silently omitting
	// the plain variables this entity type is meant to surface.
	"variable": "Variable",
}

// ResolveContentBackedEntityTypes maps entity_type filter values accepted by
// POST /api/v0/entities/resolve to the content-entity label
// resolveEntityFromContent (root) filters by. The implementation moved from
// root's entity_content_types.go for #6060 so a handler-family subpackage can
// resolve an entity type without importing root.
var ResolveContentBackedEntityTypes = map[string]string{
	"analytics_model":          "AnalyticsModel",
	"annotation":               "Annotation",
	"argocd_application":       "ArgoCDApplication",
	"argocd_applicationset":    "ArgoCDApplicationSet",
	"atlantis_project":         "AtlantisProject",
	"atlantis_workflow":        "AtlantisWorkflow",
	"component":                "Component",
	"cloudformation_condition": "CloudFormationCondition",
	"cloudformation_export":    "CloudFormationExport",
	"cloudformation_import":    "CloudFormationImport",
	"cloudformation_output":    "CloudFormationOutput",
	"cloudformation_parameter": "CloudFormationParameter",
	"cloudformation_resource":  "CloudFormationResource",
	"data_asset":               "DataAsset",
	"impl_block":               "ImplBlock",
	"k8s_resource":             "K8sResource",
	"kustomize_overlay":        "KustomizeOverlay",
	"protocol":                 "Protocol",
	"terraform_backend":        "TerraformBackend",
	"terraform_block":          "TerraformBlock",
	"terraform_check":          "TerraformCheck",
	"terraform_import":         "TerraformImport",
	"terraform_lock_provider":  "TerraformLockProvider",
	"terraform_moved_block":    "TerraformMovedBlock",
	"terraform_removed_block":  "TerraformRemovedBlock",
	"terragrunt_dependency":    "TerragruntDependency",
	"terragrunt_input":         "TerragruntInput",
	"terragrunt_local":         "TerragruntLocal",
	"type_alias":               "TypeAlias",
	"type_annotation":          "TypeAnnotation",
	"typedef":                  "Typedef",
	"variable":                 "Variable",
	"guard":                    "guard",
	"protocol_implementation":  "ProtocolImplementation",
	"module_attribute":         "module_attribute",
}

// ContentEntityTypeForResolve maps a resolve_entity entity_type filter value
// to its content-entity label, checking ResolveContentBackedEntityTypes,
// ContentBackedEntityTypes, and GraphBackedEntityTypes in that order, falling
// back to typeName itself when none match. The implementation moved from
// root's entity_content_types.go for #6060 so a handler-family subpackage can
// resolve an entity type without importing root.
func ContentEntityTypeForResolve(typeName string) string {
	if typeName == "" {
		return ""
	}
	if entityType, ok := ResolveContentBackedEntityTypes[typeName]; ok {
		return entityType
	}
	if entityType, ok := ContentBackedEntityTypes[typeName]; ok {
		return entityType
	}
	if entityType, ok := GraphBackedEntityTypes[typeName]; ok {
		return entityType
	}
	return typeName
}

// GraphResultMetadata projects the optional semantic-metadata columns
// GraphSemanticMetadataProjection (querygraphrows) selects into the
// "metadata" field of a language-query or entity result row, omitting keys
// whose value is absent or empty. The implementation moved from root's
// language_query_entities.go for #6060 so a handler-family subpackage can
// shape a graph row's metadata without importing root.
func GraphResultMetadata(row map[string]any) map[string]any {
	metadata := map[string]any{}
	if v := StringVal(row, "docstring"); v != "" {
		metadata["docstring"] = v
	}
	if v := StringVal(row, "class_context"); v != "" {
		metadata["class_context"] = v
	}
	if v := StringVal(row, "method_kind"); v != "" {
		metadata["method_kind"] = v
	}
	if v := StringVal(row, "constructor_kind"); v != "" {
		metadata["constructor_kind"] = v
	}
	if v := StringVal(row, "annotation_kind"); v != "" {
		metadata["annotation_kind"] = v
	}
	if v := StringVal(row, "context"); v != "" {
		metadata["context"] = v
	}
	if v := IntVal(row, "type_annotation_count"); v > 0 {
		metadata["type_annotation_count"] = v
	}
	if values := StringSliceVal(row, "type_annotation_kinds"); len(values) > 0 {
		typeAnnotationKinds := make([]any, 0, len(values))
		for _, value := range values {
			typeAnnotationKinds = append(typeAnnotationKinds, value)
		}
		metadata["type_annotation_kinds"] = typeAnnotationKinds
	}
	if values := StringSliceVal(row, "type_parameters"); len(values) > 0 {
		typeParameters := make([]any, 0, len(values))
		for _, value := range values {
			typeParameters = append(typeParameters, value)
		}
		metadata["type_parameters"] = typeParameters
	}
	if values := StringSliceVal(row, "dead_code_root_kinds"); len(values) > 0 {
		rootKinds := make([]any, 0, len(values))
		for _, value := range values {
			rootKinds = append(rootKinds, value)
		}
		metadata["dead_code_root_kinds"] = rootKinds
	}
	if v := StringVal(row, "type_alias_kind"); v != "" {
		metadata["type_alias_kind"] = v
	}
	if v := StringVal(row, "framework"); v != "" {
		metadata["framework"] = v
	}
	if v := StringVal(row, "module_kind"); v != "" {
		metadata["module_kind"] = v
	}
	if v, ok := row["jsx_fragment_shorthand"].(bool); ok {
		metadata["jsx_fragment_shorthand"] = v
	}
	if v := StringVal(row, "component_type_assertion"); v != "" {
		metadata["component_type_assertion"] = v
	}
	if v := StringVal(row, "component_wrapper_kind"); v != "" {
		metadata["component_wrapper_kind"] = v
	}
	if v := StringVal(row, "protocol"); v != "" {
		metadata["protocol"] = v
	}
	if v := StringVal(row, "implemented_for"); v != "" {
		metadata["implemented_for"] = v
	}
	if v := StringVal(row, "attribute_kind"); v != "" {
		metadata["attribute_kind"] = v
	}
	if v := StringVal(row, "value"); v != "" {
		metadata["value"] = v
	}
	if v := StringVal(row, "declaration_merge_group"); v != "" {
		metadata["declaration_merge_group"] = v
	}
	if v := IntVal(row, "declaration_merge_count"); v > 0 {
		metadata["declaration_merge_count"] = v
	}
	if values := StringSliceVal(row, "declaration_merge_kinds"); len(values) > 0 {
		declarationMergeKinds := make([]any, 0, len(values))
		for _, value := range values {
			declarationMergeKinds = append(declarationMergeKinds, value)
		}
		metadata["declaration_merge_kinds"] = declarationMergeKinds
	}
	if v := StringVal(row, "kind"); v != "" {
		metadata["kind"] = v
	}
	if v := StringVal(row, "target_kind"); v != "" {
		metadata["target_kind"] = v
	}
	if v := StringVal(row, "type"); v != "" {
		metadata["type"] = v
	}
	if values := StringSliceVal(row, "decorators"); len(values) > 0 {
		decorators := make([]any, 0, len(values))
		for _, value := range values {
			decorators = append(decorators, value)
		}
		metadata["decorators"] = decorators
	}
	if v, ok := row["async"].(bool); ok {
		metadata["async"] = v
	}
	if v := StringVal(row, "semantic_kind"); v != "" {
		metadata["semantic_kind"] = v
	}
	if v := StringVal(row, "metaclass"); v != "" {
		metadata["metaclass"] = v
	}
	if v := StringVal(row, "source"); v != "" {
		metadata["source"] = v
	}
	if v := StringVal(row, "terraform_source"); v != "" {
		metadata["terraform_source"] = v
	}
	if v := StringVal(row, "config_path"); v != "" {
		metadata["config_path"] = v
	}
	if values := StringSliceVal(row, "includes"); len(values) > 0 {
		includes := make([]any, 0, len(values))
		for _, value := range values {
			includes = append(includes, value)
		}
		metadata["includes"] = includes
	}
	if values := StringSliceVal(row, "inputs"); len(values) > 0 {
		inputs := make([]any, 0, len(values))
		for _, value := range values {
			inputs = append(inputs, value)
		}
		metadata["inputs"] = inputs
	}
	if values := StringSliceVal(row, "locals"); len(values) > 0 {
		locals := make([]any, 0, len(values))
		for _, value := range values {
			locals = append(locals, value)
		}
		metadata["locals"] = locals
	}
	if v := StringVal(row, "deployment_name"); v != "" {
		metadata["deployment_name"] = v
	}
	if v := StringVal(row, "entity_repo_name"); v != "" {
		metadata["repo_name"] = v
	}
	if v := StringVal(row, "create_deploy"); v != "" {
		metadata["create_deploy"] = v
	}
	if v := StringVal(row, "cluster_name"); v != "" {
		metadata["cluster_name"] = v
	}
	if v := StringVal(row, "zone_id"); v != "" {
		metadata["zone_id"] = v
	}
	if v := StringVal(row, "deploy_entry_point"); v != "" {
		metadata["deploy_entry_point"] = v
	}
	if v := StringVal(row, "qualified_name"); v != "" {
		metadata["qualified_name"] = v
	}
	if v := StringVal(row, "sql_entity_type"); v != "" {
		metadata["sql_entity_type"] = v
	}
	if v := StringVal(row, "schema"); v != "" {
		metadata["schema"] = v
	}
	if v := StringVal(row, "data_type"); v != "" {
		metadata["data_type"] = v
	}
	if v := StringVal(row, "table_name"); v != "" {
		metadata["table_name"] = v
	}
	if v := StringVal(row, "column_name"); v != "" {
		metadata["column_name"] = v
	}
	if v := StringVal(row, "routine_kind"); v != "" {
		metadata["routine_kind"] = v
	}
	if v := StringVal(row, "function_language"); v != "" {
		metadata["function_language"] = v
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

// GraphLabelToContentEntityType maps a graph node label to the content-entity
// type name used by the content-store fallback, or "" when the label has no
// content-store counterpart. The implementation moved from root's
// language_query_entities.go for #6060 so a handler-family subpackage can map
// a graph label without importing root.
func GraphLabelToContentEntityType(label string) string {
	switch label {
	case "Annotation":
		return "Annotation"
	case "Function", "Class", "Interface", "Module", "Variable", "Struct", "Enum", "Union", "Macro", "ImplBlock", "Typedef", "TypeAlias", "TypeAnnotation", "Component":
		return label
	case "SqlColumn", "SqlFunction", "SqlIndex", "SqlMigration", "SqlTable", "SqlTrigger", "SqlView":
		return label
	case "TerraformModule", "TerragruntConfig", "TerragruntDependency":
		return label
	case "FluxKustomization", "FluxGitRepository", "FluxOCIRepository", "FluxBucket",
		"FluxHelmRelease", "FluxHelmRepository":
		// Flux typed entities are entity_context-only (issue #5360 PR A;
		// FluxHelmRelease/FluxHelmRepository added issue #5483 C1). Their typed
		// fields (url, source_ref_*, chart_ref_*, ref_*, bucket_name, endpoint,
		// provider, source_path, target_namespace, chart, chart_version,
		// repo_type, generate_name) ride entity_metadata and are written as
		// graph node properties, but the fixed graph metadata projection does
		// not select them, so get_entity_context relies on this
		// content-enrichment bridge to surface them -- mirroring the #5346
		// SqlMigration read-surface fix. This mapping does NOT advertise them
		// as language-queryable: it feeds only the content-metadata enrichment
		// and relationship-label paths, never
		// allSupportedEntityTypes()/SupportedEntityTypes().
		return label
	default:
		return ""
	}
}
