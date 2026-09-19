// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// listCatalogRepositoriesFromGraph returns the catalog's bounded repository
// page. The fourth return value reports whether the dependency-edge
// pre-pass backing is_dependency (see loadRepositoryDependencyEdges) was
// itself degraded (failed or truncated); the caller must fold that into the
// response's own limitations disclosure ONLY, never into truncated -- see
// logRepositoryDependencyEdgesDegradation's doc comment and #6786 review F1.
//
// #6786 review F6 proposed making this read PAGE-SCOPED
// (WHERE t.id IN $page_ids, bounded by page size instead of the whole
// graph's DEPENDS_ON edge count). Measured live on NornicDB v1.3.3 (schema
// applied, 5000 repos / 20000 edges): that shape's cold-path cost scales
// ~linearly with len($page_ids) at roughly 55-60ms/element (2000-element
// page: ~2 minutes) and is cached by exact PARAMETER VALUES, not statement
// text -- a different 2000-id set on the identical statement paid the full
// ~2-minute cost again. A real catalog page's id set drifts on ordinary
// repository churn, so this would cost roughly a full cold run on most
// requests. It was rejected in favour of the whole-graph read.
//
// The whole-graph read is NEW cost on this route: before #6786 the catalog
// issued no dependency-edge read at all (its per-row EXISTS was NornicDB's
// always-false shape). The catalog is unscoped, so it takes the grouped
// RepositoryDependencyGroupedEdgeCypher read, gated by the DEPENDS_ON
// cardinality probe and capped by the group-size read when the probe cannot
// prove the edges fit the bound (loadUnscopedRepositoryDependencyEdges). Measured on NornicDB v1.3.3 at 500 repositories with
// 200 files each: this function cost 0.045-0.056s before #6786 (with every
// is_dependency false), 0.60-0.66s with the per-edge read, and 0.013s with
// the grouped read; on Neo4j 0.006s, 0.011s and 0.011s (#6786 evidence doc).
func (h *Handler) listCatalogRepositoriesFromGraph(
	ctx context.Context,
	limit int,
) (repositories []catalogRepository, truncated bool, dependencyDegraded bool, err error) {
	cypher := fmt.Sprintf(`
		MATCH (r:Repository)
		RETURN %s
		ORDER BY r.name, r.id
		LIMIT $limit
	`, querycontract.RepoProjection("r"))
	rows, err := h.Neo4j.Run(ctx, cypher, map[string]any{"limit": limit + 1})
	if err != nil {
		return nil, false, false, err
	}
	rows, truncated = trimCatalogRows(rows, limit)

	// is_dependency is derived in Go from the same bounded, unscoped
	// dependency-edge pre-pass the repository list uses -- see
	// loadRepositoryDependencyEdges and issue #6786 defect 1. The catalog
	// endpoint has always been unscoped (AllScopes: true), matching the
	// EXISTS-based projection it replaces.
	// Timed as stage=dependency_cluster_edges with the same completion
	// attributes as the repository list route, minus cluster_count: the
	// catalog builds no clusters.
	edgeTimer := startRepositoryQueryStage(ctx, h.Logger, "catalog_list", "", "dependency_cluster_edges")
	dependencyRead := loadRepositoryDependencyEdges(ctx, h.Neo4j, querycontract.RepositoryAccessFilter{AllScopes: true})
	edgeTimer.Done(ctx, dependencyEdgeStageAttrs(dependencyRead)...)
	dependencyTargets := repositoryDependencyTargetSet(dependencyRead.Edges)
	dependencyDegraded = logRepositoryDependencyEdgesDegradation(ctx, h.Logger, "catalog_list", dependencyRead)
	logRepositoryDependencyClusterErrors(ctx, h.Logger, "catalog_list", dependencyRead)

	repositories = make([]catalogRepository, 0, len(rows))
	for _, row := range rows {
		id := querycontract.StringVal(row, "id")
		_, isDependency := dependencyTargets[id]
		repositories = append(repositories, catalogRepositoryFromRow(row, isDependency))
	}
	return repositories, truncated, dependencyDegraded, nil
}

// catalogRepositoryFromRow builds a catalogRepository from a graph or
// content row. isDependency is supplied by the caller rather than read from
// row["is_dependency"]: the graph path derives it from the dependency-edge
// pre-pass (issue #6786 defect 1) and the content path from the row's own
// field, so this stays a plain projection either way.
func catalogRepositoryFromRow(row map[string]any, isDependency bool) catalogRepository {
	return catalogRepository{
		ID:           querycontract.StringVal(row, "id"),
		Name:         querycontract.StringVal(row, "name"),
		Path:         querycontract.StringVal(row, "path"),
		LocalPath:    querycontract.StringVal(row, "local_path"),
		RemoteURL:    querycontract.StringVal(row, "remote_url"),
		RepoSlug:     querycontract.StringVal(row, "repo_slug"),
		HasRemote:    querycontract.BoolVal(row, "has_remote"),
		IsDependency: isDependency,
	}
}
