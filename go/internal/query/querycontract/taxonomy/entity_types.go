// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package taxonomy

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

// GraphFirstContentBackedEntityTypes maps user-facing entity types that
// resolve through the graph first and fall back to content entities. The
// implementation moved from root's language_query_entities.go for #6060 so
// a handler-family subpackage can resolve an entity type's graph label
// without importing root.
var GraphFirstContentBackedEntityTypes = map[string]string{
	"annotation":              "Annotation",
	"component":               "Component",
	"impl_block":              "ImplBlock",
	"protocol":                "Protocol",
	"protocol_implementation": "ProtocolImplementation",
	"module_attribute":        "Variable",
	"terraform_backend":       "TerraformBackend",
	"terraform_check":         "TerraformCheck",
	"terraform_import":        "TerraformImport",
	"terraform_lock_provider": "TerraformLockProvider",
	"terraform_module":        "TerraformModule",
	"terraform_moved_block":   "TerraformMovedBlock",
	"terraform_removed_block": "TerraformRemovedBlock",
	"terragrunt_config":       "TerragruntConfig",
	"terragrunt_dependency":   "TerragruntDependency",
	"sql_column":              "SqlColumn",
	"sql_function":            "SqlFunction",
	"sql_index":               "SqlIndex",
	"sql_migration":           "SqlMigration",
	"sql_table":               "SqlTable",
	"sql_trigger":             "SqlTrigger",
	"sql_view":                "SqlView",
	"type_alias":              "TypeAlias",
	"typedef":                 "Typedef",
}

// ElixirSemanticEntityType maps an Elixir semantic entity type to the graph
// label and metadata predicate the entity resolve path filters by. The
// implementation moved from root's elixir_semantic_types.go for #6060 so a
// handler-family subpackage can resolve an Elixir entity type without
// importing root.
type ElixirSemanticEntityType struct {
	BaseType      string
	GraphLabel    string
	MetadataKey   string
	MetadataValue string
}

// ElixirSemanticEntityTypes maps Elixir semantic entity types to their
// graph/metadata resolution. The implementation moved from root's
// elixir_semantic_types.go for #6060; see ElixirSemanticEntityType.
var ElixirSemanticEntityTypes = map[string]ElixirSemanticEntityType{
	"guard": {
		BaseType:      "Function",
		GraphLabel:    "Function",
		MetadataKey:   "semantic_kind",
		MetadataValue: "guard",
	},
	"protocol_implementation": {
		BaseType:      "Module",
		GraphLabel:    "Module",
		MetadataKey:   "module_kind",
		MetadataValue: "protocol_implementation",
	},
	"module_attribute": {
		BaseType:      "Variable",
		GraphLabel:    "Variable",
		MetadataKey:   "attribute_kind",
		MetadataValue: "module_attribute",
	},
}

// ElixirGraphSemanticEntityType resolves an Elixir semantic entity type to
// its graph label and metadata predicate. The implementation moved from
// root's elixir_semantic_types.go for #6060; see ElixirSemanticEntityType.
func ElixirGraphSemanticEntityType(entityType string) (string, string, string, bool) {
	semanticType, ok := ElixirSemanticEntityTypes[entityType]
	if !ok || semanticType.GraphLabel == "" {
		return "", "", "", false
	}
	return semanticType.GraphLabel, semanticType.MetadataKey, semanticType.MetadataValue, true
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
