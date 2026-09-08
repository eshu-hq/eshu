// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querygraphrows

import (
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// GraphPathNodeProps returns one projected path node's property map.
//
// It lives here because internal/query's depguard rule query-no-graph-driver
// keeps the Bolt driver's types in the handful of driver-owning files and
// packages, and its own message names the remedy: reach the graph through a
// port or an existing driver-owning file. Callers that only need a property
// off a node in a nodes(path) projection -- the NornicDB inheritance walk's
// interior grant filter, for one -- take this instead of importing the driver
// themselves. The implementation moved from root's neo4j.go for #6060 so a
// handler-family subpackage can decode a path node without importing root.
//
// The second result reports whether the value was a node shape at all, so a
// caller can fail closed on something it does not understand rather than
// treat it as a node with no properties.
func GraphPathNodeProps(node any) (map[string]any, bool) {
	switch value := node.(type) {
	case neo4jdriver.Node:
		return value.Props, true
	case map[string]any:
		if props, ok := value["properties"].(map[string]any); ok {
			return props, true
		}
		return value, true
	default:
		return nil, false
	}
}

// RouteToCallerEntityFromChain decodes the route-to-caller entity's identity
// fields from a nodes(path) value, reading the LAST node in the chain. The
// route-to-caller relationship reads anchor the known handler as the path
// start and project raw nodes(path) because NornicDB corrupts a
// CALL-subquery computed projection over path nodes (#5287); the far
// endpoint is the discovered caller/callee. nodes(path) is a neo4j.Node on
// both backends, with a map[string]any fallback. Returns nil when the chain
// is empty or its last element is not a node. The implementation moved from
// root's neo4j.go for #6060 so a handler-family subpackage can decode a
// route-to-caller chain without importing root.
func RouteToCallerEntityFromChain(chain any) map[string]any {
	props := lastChainNodeProps(chain)
	if props == nil {
		return nil
	}
	entityID := querycontract.StringVal(props, "id")
	if entityID == "" {
		entityID = querycontract.StringVal(props, "uid")
	}
	filePath := querycontract.StringVal(props, "file_path")
	if filePath == "" {
		filePath = querycontract.StringVal(props, "relative_path")
	}
	return map[string]any{
		"entity_id":  entityID,
		"name":       querycontract.StringVal(props, "name"),
		"file_path":  filePath,
		"repo_id":    querycontract.StringVal(props, "repo_id"),
		"language":   querycontract.StringVal(props, "language"),
		"start_line": querycontract.IntVal(props, "start_line"),
		"end_line":   querycontract.IntVal(props, "end_line"),
	}
}

// lastChainNodeProps returns the property map of the last node in a
// nodes(path) value, decoding both the neo4j.Node driver shape and a
// map[string]any fallback.
func lastChainNodeProps(chain any) map[string]any {
	items, ok := chain.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	switch node := items[len(items)-1].(type) {
	case neo4jdriver.Node:
		return node.Props
	case map[string]any:
		if props, ok := node["properties"].(map[string]any); ok {
			return props
		}
		return node
	default:
		return nil
	}
}

// GraphSemanticMetadataProjection returns the shared Cypher RETURN-clause
// projection fragment for an entity's optional semantic metadata columns,
// used by every entity read that populates graphResultMetadata-shaped rows.
// The implementation moved from root's language_query_entities.go for #6060;
// see doc.go for why it lives here rather than in querycontract.
func GraphSemanticMetadataProjection() string {
	return `
		       e.docstring as docstring,
		       e.class_context as class_context,
		       e.method_kind as method_kind,
		       e.constructor_kind as constructor_kind,
		       e.annotation_kind as annotation_kind,
		       e.context as context,
		       e.type_annotation_count as type_annotation_count,
		       e.type_annotation_kinds as type_annotation_kinds,
		       e.type_parameters as type_parameters,
		       e.dead_code_root_kinds as dead_code_root_kinds,
		       e.type_alias_kind as type_alias_kind,
		       e.framework as framework,
		       e.module_kind as module_kind,
		       e.jsx_fragment_shorthand as jsx_fragment_shorthand,
		       e.component_type_assertion as component_type_assertion,
		       e.component_wrapper_kind as component_wrapper_kind,
		       e.protocol as protocol,
		       e.implemented_for as implemented_for,
		       e.attribute_kind as attribute_kind,
		       e.value as value,
		       e.declaration_merge_group as declaration_merge_group,
		       e.declaration_merge_count as declaration_merge_count,
		       e.declaration_merge_kinds as declaration_merge_kinds,
		       e.kind as kind,
		       e.target_kind as target_kind,
		       e.type as type,
		       e.decorators as decorators,
		       e.async as async,
		       e.semantic_kind as semantic_kind,
		       e.metaclass as metaclass,
		       e.source as source,
		       e.terraform_source as terraform_source,
		       e.config_path as config_path,
		       e.includes as includes,
		       e.inputs as inputs,
		       e.locals as locals,
		       e.deployment_name as deployment_name,
		       e.repo_name as entity_repo_name,
		       e.create_deploy as create_deploy,
		       e.cluster_name as cluster_name,
		       e.zone_id as zone_id,
		       e.deploy_entry_point as deploy_entry_point,
		       e.qualified_name as qualified_name,
		       e.sql_entity_type as sql_entity_type,
		       e.schema as schema,
		       e.data_type as data_type,
		       e.table_name as table_name,
		       e.column_name as column_name,
		       e.routine_kind as routine_kind,
		       e.function_language as function_language`
}
