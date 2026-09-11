// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/impact"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func QueryRepoEntryPoints(ctx context.Context, reader querycontract.GraphQuery, content querycontract.ContentStore, params map[string]any) []map[string]any {
	repoID := querycontract.StringVal(params, "repo_id")
	if entryPoints := querycontract.LoadRepositoryEntryPoints(ctx, content, repoID); entryPoints != nil {
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
		if !querycontract.IsRepositoryEntryPointName(querycontract.StringVal(row, "name")) {
			continue
		}
		result = append(result, map[string]any{
			"name":          querycontract.StringVal(row, "name"),
			"relative_path": querycontract.StringVal(row, "relative_path"),
			"language":      querycontract.StringVal(row, "language"),
		})
	}
	return result
}

// queryRepoInfrastructure returns the repository infrastructure rows,
// whether the underlying read genuinely failed, and whether a healthy graph
// read landed past its LIMIT bound -- more rows exist beyond it (P2-2
// follow-up to #5764). It never
// returns an error to its own callers: infrastructure is a genuine auxiliary
// panel, so a graph-read failure degrades to an empty result rather than
// failing the whole context/story response. Callers that want to surface the
// degradation or the truncation do so via the returned bools (limitations /
// partial_reasons / stage-log failure_class and truncated); callers that do
// not care may discard them. degraded and truncated are never both true: a
// failed read returns an empty result with truncated forced false, since
// there is nothing to disclose a bound on.
func QueryRepoInfrastructure(ctx context.Context, reader querycontract.GraphQuery, content querycontract.ContentStore, params map[string]any) ([]map[string]any, bool, bool) {
	rows, truncated, err := queryRepoInfrastructureRows(ctx, reader, content, params)
	if err != nil {
		return make([]map[string]any, 0), true, false
	}
	return rows, false, truncated
}

func queryRepoLanguageDistribution(ctx context.Context, reader querycontract.GraphQuery, params map[string]any) []map[string]any {
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
			"language":   querycontract.StringVal(row, "language"),
			"file_count": querycontract.IntVal(row, "file_count"),
		})
	}
	return result
}

func QueryRepoDependencies(ctx context.Context, reader querycontract.GraphQuery, params map[string]any) []map[string]any {
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
			"type":        querycontract.StringVal(row, "type"),
			"target_name": querycontract.StringVal(row, "target_name"),
			"target_id":   querycontract.StringVal(row, "target_id"),
		}
		if evidenceType := querycontract.StringVal(row, "evidence_type"); evidenceType != "" {
			entry["evidence_type"] = evidenceType
		}
		copyRelationshipEvidenceMetadata(entry, row)
		result = append(result, entry)
	}
	return filterRepoRelationshipTargetRowsForAccess(result, "target_id", querycontract.RepositoryAccessFilterFromContext(ctx))
}

func queryRepoRelationshipOverview(ctx context.Context, reader querycontract.GraphQuery, params map[string]any) []map[string]any {
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
	return filterRepoRelationshipOverviewRowsForAccess(combined, querycontract.StringVal(params, "repo_id"), querycontract.RepositoryAccessFilterFromContext(ctx))
}

func queryRepoRelationshipOverviewDirection(ctx context.Context, reader querycontract.GraphQuery, params map[string]any, cypher string) []map[string]any {
	rows, err := reader.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return make([]map[string]any, 0)
	}

	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{
			"direction":   querycontract.StringVal(row, "direction"),
			"type":        querycontract.StringVal(row, "type"),
			"source_name": querycontract.StringVal(row, "source_name"),
			"source_id":   querycontract.StringVal(row, "source_id"),
			"target_name": querycontract.StringVal(row, "target_name"),
			"target_id":   querycontract.StringVal(row, "target_id"),
		}
		if evidenceType := querycontract.StringVal(row, "evidence_type"); evidenceType != "" {
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
func QueryRepoSourceToolBreakdown(ctx context.Context, reader querycontract.GraphQuery, params map[string]any) []map[string]any {
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
			"source_tool": querycontract.StringVal(row, "source_tool"),
			"edge_count":  querycontract.IntVal(row, "edge_count"),
		})
	}
	return result
}

func queryRepoConsumers(ctx context.Context, reader querycontract.GraphQuery, params map[string]any) []map[string]any {
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
			"name": querycontract.StringVal(row, "consumer_name"),
			"id":   querycontract.StringVal(row, "consumer_id"),
		})
	}
	return filterRepoRelationshipTargetRowsForAccess(result, "id", querycontract.RepositoryAccessFilterFromContext(ctx))
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
func filterRepoRelationshipTargetRowsForAccess(rows []map[string]any, idField string, access querycontract.RepositoryAccessFilter) []map[string]any {
	if !access.Scoped() {
		return rows
	}
	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if impact.ImpactRepoIDAllowed(querycontract.StringVal(row, idField), access) {
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
func FilterRepoRelationshipOverviewRowsForAccess(rows []map[string]any, anchorRepoID string, access querycontract.RepositoryAccessFilter) []map[string]any {
	if !access.Scoped() {
		return rows
	}
	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if repositoryRelationshipEndpointAllowed(querycontract.StringVal(row, "source_id"), anchorRepoID, access) &&
			repositoryRelationshipEndpointAllowed(querycontract.StringVal(row, "target_id"), anchorRepoID, access) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// repositoryRelationshipEndpointAllowed reports whether one repository endpoint
// of a relationship-overview row may be shown to the caller: the grant-verified
// anchor is always visible, and any other endpoint must be inside the grant
// (deny-by-default on empty when scoped, via impactRepoIDAllowed).
func repositoryRelationshipEndpointAllowed(repoID, anchorRepoID string, access querycontract.RepositoryAccessFilter) bool {
	if repoID != "" && repoID == anchorRepoID {
		return true
	}
	return impact.ImpactRepoIDAllowed(repoID, access)
}

// filterRepositoryRelationshipReadModelForAccess binds every related-repository
// endpoint carried by the read-model relationship rows and consumers to the
// caller's grant (#5167 W3 P0, fourth vector). The Postgres read-model path is
// the production-primary source for GET /repositories/{id}/context's
// relationships, relationship_overview, and consumers -- the read-model SQL
// anchors only on source_repo_id/target_repo_id = $anchor and applies no
// predicate to the FAR endpoint, and the deployable-unit merge
// (mergeRepositoryDeployableUnitRelationships) folds in graph rows through the
// unfiltered inner queryRepoRelationshipOverviewDirection. Filtering the merged
// read model here, before context.go derives any result[] field from
// it, closes all three emit sites at once: relationship_overview and the
// legacy dependencies both derive from the filtered Relationships (anchor-aware
// -- the grant-verified anchor endpoint stays, the far endpoint must be in
// grant), and Consumers are filtered by their sole repository id. Deny-by-
// default when scoped; an all-scopes/shared/admin caller is unaffected
// (returned unchanged), so non-scoped callers see no regression. Available is
// carried through unchanged: an in-grant read model stays authoritative even
// when every cross-tenant row is dropped, so the handler does not fall back to
// the graph and re-run the (already grant-bound) helpers.
func filterRepositoryRelationshipReadModelForAccess(
	readModel *querycontract.RepositoryRelationshipReadModel,
	anchorRepoID string,
	access querycontract.RepositoryAccessFilter,
) *querycontract.RepositoryRelationshipReadModel {
	if readModel == nil || !access.Scoped() {
		return readModel
	}
	return &querycontract.RepositoryRelationshipReadModel{
		Available:     readModel.Available,
		Relationships: filterRepoRelationshipOverviewRowsForAccess(readModel.Relationships, anchorRepoID, access),
		Consumers:     filterRepoRelationshipTargetRowsForAccess(readModel.Consumers, "id", access),
	}
}

// repositoryReadModelDependencies returns outgoing rows in the legacy
// repository dependency shape.
func repositoryReadModelDependencies(readModel *querycontract.RepositoryRelationshipReadModel) []map[string]any {
	if readModel == nil {
		return nil
	}
	dependencies := make([]map[string]any, 0, len(readModel.Relationships))
	for _, row := range readModel.Relationships {
		if querycontract.StringVal(row, "direction") != "outgoing" {
			continue
		}
		dependency := map[string]any{
			"type":        querycontract.StringVal(row, "type"),
			"target_name": querycontract.StringVal(row, "target_name"),
			"target_id":   querycontract.StringVal(row, "target_id"),
		}
		if evidenceType := querycontract.StringVal(row, "evidence_type"); evidenceType != "" {
			dependency["evidence_type"] = evidenceType
		}
		copyRelationshipEvidenceMetadata(dependency, row)
		dependencies = append(dependencies, dependency)
	}
	return dependencies
}

// The following lowercase twins keep every in-package caller unchanged after
// the #6060 export; root stayers outside the repository family name the
// exported spelling above.

func queryRepoDependencies(ctx context.Context, reader querycontract.GraphQuery, params map[string]any) []map[string]any {
	return QueryRepoDependencies(ctx, reader, params)
}

func queryRepoInfrastructure(ctx context.Context, reader querycontract.GraphQuery, content querycontract.ContentStore, params map[string]any) ([]map[string]any, bool, bool) {
	return QueryRepoInfrastructure(ctx, reader, content, params)
}

// queryRepoSourceToolBreakdown keeps the in-package spelling after the #6060
// export; root tests name QueryRepoSourceToolBreakdown.
func queryRepoSourceToolBreakdown(ctx context.Context, reader querycontract.GraphQuery, params map[string]any) []map[string]any {
	return QueryRepoSourceToolBreakdown(ctx, reader, params)
}

// queryRepoEntryPoints keeps the in-package spelling after the #6060 export;
// root tests name QueryRepoEntryPoints.
func queryRepoEntryPoints(ctx context.Context, reader querycontract.GraphQuery, content querycontract.ContentStore, params map[string]any) []map[string]any {
	return QueryRepoEntryPoints(ctx, reader, content, params)
}

// filterRepoRelationshipOverviewRowsForAccess keeps the in-package spelling
// after the #6060 export; root benchmarks name
// FilterRepoRelationshipOverviewRowsForAccess.
func filterRepoRelationshipOverviewRowsForAccess(rows []map[string]any, anchorRepoID string, access querycontract.RepositoryAccessFilter) []map[string]any {
	return FilterRepoRelationshipOverviewRowsForAccess(rows, anchorRepoID, access)
}
