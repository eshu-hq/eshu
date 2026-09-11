// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// repositoryAccessFilter mirrors root's repository_authz.go alias so the
// queryplan-pinned builders below relocate byte-identical: qualifying
// the filter to querycontract in their signatures would change their
// declaration bytes and break the source_sha256 pins. Do not qualify.
type repositoryAccessFilter = querycontract.RepositoryAccessFilter

// BuildSearchGraphEntitiesQuery returns the bounded entity-search Cypher and its params.
func BuildSearchGraphEntitiesQuery(
	repoID string,
	query string,
	language string,
	limit int,
	exact bool,
	access repositoryAccessFilter,
) (string, map[string]any) {
	if repoID == "" {
		return "", nil
	}
	cypher := `
		MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
	`
	params := map[string]any{
		"query": query,
		"limit": limit,
	}
	if repoID != "" {
		cypher = `
			MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e)
		`
		params["repo_id"] = repoID
	}
	if exact {
		cypher += " WHERE e.name = $query"
	} else {
		cypher += " WHERE e.name CONTAINS $query"
	}
	if repoID == "" && access.Scoped() {
		cypher += access.GraphPredicate("r")
		params = access.GraphParams(params)
	}

	if language != "" {
		cypher += " AND (e.language = $language OR f.language = $language)"
		params["language"] = language
	}

	cypher += `
		RETURN e.id as entity_id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       r.id as repo_id, r.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + graphSemanticMetadataProjection() + `
		ORDER BY e.name
		LIMIT $limit
	`
	return cypher, params
}

// graphSemanticMetadataProjection is a family-local copy of root's
// language/entities.go helper of the same name. The entity
// semantic-metadata projection is shared with staying root builders
// (language-query, dead-code, complexity, entity) that cannot cross
// the package boundary, so the leaf carries this byte-identical copy
// instead of importing root. Keep it behavior-identical to its root
// source: the queryplan-pinned builders that embed it emit Cypher
// text covered by cypher_sha256 pins.
func graphSemanticMetadataProjection() string {
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
