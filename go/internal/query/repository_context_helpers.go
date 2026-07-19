// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

func queryRepoEntryPoints(ctx context.Context, reader GraphQuery, content ContentStore, params map[string]any) []map[string]any {
	repoID := StringVal(params, "repo_id")
	if entryPoints := loadRepositoryEntryPoints(ctx, content, repoID); entryPoints != nil {
		return entryPoints
	}

	rows, err := reader.Run(ctx, `
		MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(fn:Function)
		WHERE fn.name IN ['main', 'handler', 'app', 'create_app', 'lambda_handler',
		                   'Main', 'Handler', 'App', 'CreateApp', 'LambdaHandler']
		RETURN fn.name AS name, f.relative_path AS relative_path, fn.language AS language
		ORDER BY fn.name
	`, params)
	if err != nil || len(rows) == 0 {
		return make([]map[string]any, 0)
	}

	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if !isRepositoryEntryPointName(StringVal(row, "name")) {
			continue
		}
		result = append(result, map[string]any{
			"name":          StringVal(row, "name"),
			"relative_path": StringVal(row, "relative_path"),
			"language":      StringVal(row, "language"),
		})
	}
	return result
}

func isRepositoryEntryPointName(name string) bool {
	switch name {
	case "main", "handler", "app", "create_app", "lambda_handler",
		"Main", "Handler", "App", "CreateApp", "LambdaHandler":
		return true
	default:
		return false
	}
}

func queryRepoInfrastructure(ctx context.Context, reader GraphQuery, content ContentStore, params map[string]any) []map[string]any {
	return queryRepoInfrastructureRows(ctx, reader, content, params)
}

func queryRepoLanguageDistribution(ctx context.Context, reader GraphQuery, params map[string]any) []map[string]any {
	rows, err := reader.Run(ctx, `
		MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
		WHERE f.language IS NOT NULL
		RETURN f.language AS language, count(f) AS file_count
		ORDER BY file_count DESC
	`, params)
	if err != nil || len(rows) == 0 {
		return make([]map[string]any, 0)
	}

	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, map[string]any{
			"language":   StringVal(row, "language"),
			"file_count": IntVal(row, "file_count"),
		})
	}
	return result
}

func queryRepoDependencies(ctx context.Context, reader GraphQuery, params map[string]any) []map[string]any {
	rows, err := reader.Run(ctx, `
		MATCH (r:Repository {id: $repo_id})-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON|CORRELATES_DEPLOYABLE_UNIT]->(target:Repository)
		RETURN type(rel) AS type, target.name AS target_name,
		       target.id AS target_id, rel.evidence_type AS evidence_type,
		       rel.resolved_id AS resolved_id,
		       rel.generation_id AS generation_id,
		       rel.confidence AS confidence,
		       rel.evidence_count AS evidence_count,
		       rel.evidence_kinds AS evidence_kinds,
		       rel.resolution_source AS resolution_source,
		       rel.rationale AS rationale
		ORDER BY type, target_name
	`, params)
	if err != nil || len(rows) == 0 {
		return make([]map[string]any, 0)
	}

	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{
			"type":        StringVal(row, "type"),
			"target_name": StringVal(row, "target_name"),
			"target_id":   StringVal(row, "target_id"),
		}
		if evidenceType := StringVal(row, "evidence_type"); evidenceType != "" {
			entry["evidence_type"] = evidenceType
		}
		copyRelationshipEvidenceMetadata(entry, row)
		result = append(result, entry)
	}
	return filterRepoRelationshipTargetRowsForAccess(result, "target_id", repositoryAccessFilterFromContext(ctx))
}

func queryRepoRelationshipOverview(ctx context.Context, reader GraphQuery, params map[string]any) []map[string]any {
	outgoing := queryRepoRelationshipOverviewDirection(ctx, reader, params, `
		MATCH (r:Repository {id: $repo_id})-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON|CORRELATES_DEPLOYABLE_UNIT]->(target:Repository)
		RETURN 'outgoing' AS direction,
		       type(rel) AS type,
		       r.name AS source_name,
		       r.id AS source_id,
		       target.name AS target_name,
		       target.id AS target_id,
		       rel.evidence_type AS evidence_type,
		       rel.resolved_id AS resolved_id,
		       rel.generation_id AS generation_id,
		       rel.confidence AS confidence,
		       rel.evidence_count AS evidence_count,
		       rel.evidence_kinds AS evidence_kinds,
		       rel.resolution_source AS resolution_source,
		       rel.rationale AS rationale
		ORDER BY type, target_name
	`)
	incoming := queryRepoRelationshipOverviewDirection(ctx, reader, params, `
		MATCH (source:Repository)-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON|CORRELATES_DEPLOYABLE_UNIT]->(r:Repository {id: $repo_id})
		RETURN 'incoming' AS direction,
		       type(rel) AS type,
		       source.name AS source_name,
		       source.id AS source_id,
		       r.name AS target_name,
		       r.id AS target_id,
		       rel.evidence_type AS evidence_type,
		       rel.resolved_id AS resolved_id,
		       rel.generation_id AS generation_id,
		       rel.confidence AS confidence,
		       rel.evidence_count AS evidence_count,
		       rel.evidence_kinds AS evidence_kinds,
		       rel.resolution_source AS resolution_source,
		       rel.rationale AS rationale
		ORDER BY type, source_name
	`)
	var combined []map[string]any
	if len(outgoing) == 0 {
		combined = incoming
	} else {
		combined = append(outgoing, incoming...)
	}
	return filterRepoRelationshipOverviewRowsForAccess(combined, StringVal(params, "repo_id"), repositoryAccessFilterFromContext(ctx))
}

func queryRepoRelationshipOverviewDirection(ctx context.Context, reader GraphQuery, params map[string]any, cypher string) []map[string]any {
	rows, err := reader.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return make([]map[string]any, 0)
	}

	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{
			"direction":   StringVal(row, "direction"),
			"type":        StringVal(row, "type"),
			"source_name": StringVal(row, "source_name"),
			"source_id":   StringVal(row, "source_id"),
			"target_name": StringVal(row, "target_name"),
			"target_id":   StringVal(row, "target_id"),
		}
		if evidenceType := StringVal(row, "evidence_type"); evidenceType != "" {
			entry["evidence_type"] = evidenceType
		}
		copyRelationshipEvidenceMetadata(entry, row)
		result = append(result, entry)
	}
	return result
}

// queryRepoSourceToolBreakdown returns a per-source_tool edge count for the
// repository, anchored on the repository's id. It matches all outgoing edges
// from the repository node that carry a non-null source_tool property and
// returns a (source_tool, edge_count) aggregate. The anchor
// `(r:Repository {id: $repo_id})` is repository-id-indexed, and the expand is
// restricted to the six Tier-2 repo-outgoing verbs that actually carry
// source_tool (#3997/#3999). Typing the relationship is deliberate: a bare
// `-[rel]->()` would also traverse REPO_CONTAINS to every File node in the
// repository (a large fanout) only to discard them on the source_tool IS NOT
// NULL filter; the type list keeps the expand to the stamped edges.
func queryRepoSourceToolBreakdown(ctx context.Context, reader GraphQuery, params map[string]any) []map[string]any {
	rows, err := reader.Run(ctx, `
		MATCH (r:Repository {id: $repo_id})-[rel:DEPENDS_ON|DEPLOYS_FROM|USES_MODULE|READS_CONFIG_FROM|PROVISIONS_DEPENDENCY_FOR|DISCOVERS_CONFIG_IN]->()
		WHERE rel.source_tool IS NOT NULL
		RETURN rel.source_tool AS source_tool, count(rel) AS edge_count
		ORDER BY edge_count DESC, source_tool
	`, params)
	if err != nil || len(rows) == 0 {
		return make([]map[string]any, 0)
	}

	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, map[string]any{
			"source_tool": StringVal(row, "source_tool"),
			"edge_count":  IntVal(row, "edge_count"),
		})
	}
	return result
}

func queryRepoConsumers(ctx context.Context, reader GraphQuery, params map[string]any) []map[string]any {
	rows, err := reader.Run(ctx, `
		MATCH (consumer:Repository)-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON|CORRELATES_DEPLOYABLE_UNIT]->(r:Repository {id: $repo_id})
		RETURN consumer.name AS consumer_name, consumer.id AS consumer_id
		ORDER BY consumer_name
	`, params)
	if err != nil || len(rows) == 0 {
		return make([]map[string]any, 0)
	}

	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, map[string]any{
			"name": StringVal(row, "consumer_name"),
			"id":   StringVal(row, "consumer_id"),
		})
	}
	return filterRepoRelationshipTargetRowsForAccess(result, "id", repositoryAccessFilterFromContext(ctx))
}

// filterRepoRelationshipTargetRowsForAccess drops repository-relationship rows
// whose related repository (named by idField -- "target_id" for
// queryRepoDependencies, "id" for queryRepoConsumers) is outside the caller's
// grant (#5167 W3 P0, third vector). These helpers anchor only on the
// grant-verified repo (r {id:$repo_id}); the RELATED repository they name
// carries no grant predicate, so a scoped caller could otherwise read a
// cross-tenant repository's id/name through dependencies[]/consumers[] on
// /services/{name}/context, /workloads/{id}/context, and the repository
// context/story routes. Deny-by-default when scoped (empty related id is
// dropped), matching impactRepoIDAllowed and the rest of the W3 row filters. An
// all-scopes or shared-key caller is unaffected (rows returned unchanged), so
// non-scoped callers see no regression.
func filterRepoRelationshipTargetRowsForAccess(rows []map[string]any, idField string, access repositoryAccessFilter) []map[string]any {
	if !access.scoped() {
		return rows
	}
	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if impactRepoIDAllowed(StringVal(row, idField), access) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// filterRepoRelationshipOverviewRowsForAccess drops relationship-overview rows
// whose NON-anchor repository endpoint is outside the caller's grant (#5167 W3
// P0, third vector). queryRepoRelationshipOverview returns both directions of
// each edge: outgoing rows anchor on source (r {id:$repo_id}) and name the
// related repo in target_id, incoming rows anchor on target and name the
// related repo in source_id. The anchor endpoint (== anchorRepoID) is always
// kept -- the caller reached this repo -- while the other endpoint must be in
// grant, so a scoped caller never sees a cross-tenant repository's id/name via
// relationship_overview. An all-scopes or shared-key caller is unaffected.
func filterRepoRelationshipOverviewRowsForAccess(rows []map[string]any, anchorRepoID string, access repositoryAccessFilter) []map[string]any {
	if !access.scoped() {
		return rows
	}
	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if repositoryRelationshipEndpointAllowed(StringVal(row, "source_id"), anchorRepoID, access) &&
			repositoryRelationshipEndpointAllowed(StringVal(row, "target_id"), anchorRepoID, access) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// repositoryRelationshipEndpointAllowed reports whether one repository endpoint
// of a relationship-overview row may be shown to the caller: the grant-verified
// anchor is always visible, and any other endpoint must be inside the grant
// (deny-by-default on empty when scoped, via impactRepoIDAllowed).
func repositoryRelationshipEndpointAllowed(repoID, anchorRepoID string, access repositoryAccessFilter) bool {
	if repoID != "" && repoID == anchorRepoID {
		return true
	}
	return impactRepoIDAllowed(repoID, access)
}
