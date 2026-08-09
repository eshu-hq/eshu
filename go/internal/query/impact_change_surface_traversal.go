// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"cmp"
	"context"
	"fmt"
	"slices"
)

// changeSurfaceRepositoryConsumersCypher follows repository dependency edges
// against their stored direction. A repository that depends on the changed
// repository is the consumer affected by that change, so the traversal is
// incoming from the changed repository anchor.
const changeSurfaceRepositoryConsumersCypher = `MATCH path = (start:Repository {id: $target_id})<-[:DEPENDS_ON*1..%d]-(impacted:Repository)
WHERE impacted.id <> $target_id%s
RETURN impacted.id as id, impacted.name as name, labels(impacted) as labels,
       impacted.environment as environment, impacted.repo_id as repo_id,
       length(path) as depth, relationships(path) as rels
ORDER BY depth, name, id
LIMIT %d`

// changeSurfaceTraversalRows merges the bounded outward-causal and incoming
// repository-consumer reads. NornicDB cannot safely express the two branches as
// UNION or as separate MATCH clauses, so they are intentionally separate
// auto-commit reads with eventual-read consistency. A rejected wrong-direction
// row still contributes to raw truncation, making partial coverage explicit.
func (h *ImpactHandler) changeSurfaceTraversalRows(
	ctx context.Context,
	target changeSurfaceTargetCandidate,
	environment string,
	depth int,
	limit int,
	access repositoryAccessFilter,
) ([]map[string]any, bool, error) {
	startPattern, err := changeSurfaceTraversalStartPattern(target)
	if err != nil {
		return nil, false, err
	}
	params := map[string]any{"target_id": target.ID}
	if environment != "" {
		params["environment"] = environment
	}

	outgoing, err := h.runChangeSurfaceOutgoing(ctx, startPattern, environment, depth, limit, params)
	if err != nil {
		return nil, false, fmt.Errorf("query outgoing change surface impact: %w", err)
	}
	rawTruncated := len(outgoing) > limit
	rows := changeSurfaceFilterTraversalRows(outgoing, environment, access, false)

	if target.hasLabel("Repository") {
		consumers, consumerErr := h.runChangeSurfaceRepositoryConsumers(ctx, environment, depth, limit, params)
		if consumerErr != nil {
			return nil, false, fmt.Errorf("query repository consumer impact: %w", consumerErr)
		}
		rawTruncated = rawTruncated || len(consumers) > limit
		rows = append(rows, changeSurfaceFilterTraversalRows(consumers, environment, access, true)...)
	}

	changeSurfaceSortRows(rows)
	return rows, rawTruncated, nil
}

func (h *ImpactHandler) runChangeSurfaceOutgoing(
	ctx context.Context,
	startPattern string,
	environment string,
	depth int,
	limit int,
	params map[string]any,
) ([]map[string]any, error) {
	cypher := fmt.Sprintf(
		changeSurfaceInvestigateCypher,
		startPattern,
		depth,
		changeSurfaceEnvironmentClause(environment),
	)
	queryParams := make(map[string]any, len(params)+1)
	for key, value := range params {
		queryParams[key] = value
	}
	queryParams["limit"] = limit + 1
	return h.Neo4j.Run(ctx, cypher, queryParams)
}

func (h *ImpactHandler) runChangeSurfaceRepositoryConsumers(
	ctx context.Context,
	environment string,
	depth int,
	limit int,
	params map[string]any,
) ([]map[string]any, error) {
	cypher := fmt.Sprintf(
		changeSurfaceRepositoryConsumersCypher,
		depth,
		changeSurfaceEnvironmentClause(environment),
		limit+1,
	)
	return h.Neo4j.Run(ctx, cypher, params)
}

func changeSurfaceFilterTraversalRows(
	rows []map[string]any,
	environment string,
	access repositoryAccessFilter,
	consumerBranch bool,
) []map[string]any {
	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if StringVal(row, "id") == "" {
			continue
		}
		env := StringVal(row, "environment")
		if environment != "" && env != "" && env != environment {
			continue
		}
		if !impactRepoIDAllowed(changeSurfaceImpactedRowRepoID(row), access) {
			continue
		}
		edges := changeSurfaceRelEdges(row["rels"])
		if !changeSurfacePathMatchesBranch(edges, consumerBranch) {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func changeSurfacePathMatchesBranch(edges []changeSurfaceRelEdge, consumerBranch bool) bool {
	if len(edges) == 0 {
		return false
	}
	for _, edge := range edges {
		if edge.relType == "" {
			return false
		}
		if consumerBranch != (edge.relType == "DEPENDS_ON") {
			return false
		}
	}
	return true
}

func changeSurfaceSortRows(rows []map[string]any) {
	slices.SortStableFunc(rows, func(left, right map[string]any) int {
		if order := cmp.Compare(IntVal(left, "depth"), IntVal(right, "depth")); order != 0 {
			return order
		}
		if order := cmp.Compare(StringVal(left, "name"), StringVal(right, "name")); order != 0 {
			return order
		}
		return cmp.Compare(StringVal(left, "id"), StringVal(right, "id"))
	})
}
