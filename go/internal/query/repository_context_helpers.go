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
	return result
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
	if len(outgoing) == 0 {
		return incoming
	}
	return append(outgoing, incoming...)
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
	return result
}
