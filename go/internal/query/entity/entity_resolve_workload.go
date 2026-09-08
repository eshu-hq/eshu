// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

var errWorkloadGraphUnavailable = errors.New("authoritative graph workload resolution is unavailable")

func (h *EntityHandler) writeWorkloadEntityResolution(
	w http.ResponseWriter,
	r *http.Request,
	req ResolveEntityRequest,
	limit int,
) bool {
	if !strings.EqualFold(strings.TrimSpace(req.Type), "workload") {
		return false
	}

	entities, err := h.ResolveWorkloadEntities(r.Context(), req.Name, req.RepoID, limit+1)
	if err != nil {
		if errors.Is(err, errWorkloadGraphUnavailable) {
			querycontract.WriteError(w, http.StatusServiceUnavailable, err.Error())
			return true
		}
		if querycontract.WriteGraphReadError(w, r, err, "code_search.exact_symbol") {
			return true
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("resolve workload: %v", err))
		return true
	}
	for i := range entities {
		attachSemanticSummary(entities[i])
	}
	entities, truncated := trimResolvedEntityPage(entities, limit)
	if entities == nil {
		entities = []map[string]any{}
	}
	querycontract.WriteSuccess(
		w,
		r,
		http.StatusOK,
		resolvedEntityResponse(entities, limit, truncated),
		workloadEntityResolveTruthEnvelope(h.profile()),
	)
	return true
}

func workloadEntityResolveTruthEnvelope(profile querycontract.QueryProfile) *querycontract.TruthEnvelope {
	return querycontract.BuildTruthEnvelope(
		profile,
		"code_search.exact_symbol",
		querycontract.TruthBasisAuthoritativeGraph,
		"resolved by exact workload name from the authoritative graph",
	)
}

// ResolveWorkloadEntities resolves workload entities by name. Exported so the staying queryplan execution test keeps driving the handler; see #6060.
func (h *EntityHandler) ResolveWorkloadEntities(
	ctx context.Context,
	name string,
	repoID string,
	limit int,
) ([]map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, errWorkloadGraphUnavailable
	}

	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	propertyCypher, relationshipCypher, params := BuildResolveWorkloadQueries(name, repoID, limit, access)
	propertyRows, err := h.Neo4j.Run(ctx, propertyCypher, params)
	if err != nil {
		return nil, fmt.Errorf("query workloads by repository property: %w", err)
	}

	relationshipRows, err := h.Neo4j.Run(ctx, relationshipCypher, params)
	if err != nil {
		return nil, fmt.Errorf("query workloads by defining repository: %w", err)
	}

	rows := append(propertyRows, relationshipRows...)
	entities := make([]map[string]any, 0, len(rows))
	entitiesByID := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		entity := map[string]any{
			"id":        querycontract.StringVal(row, "id"),
			"labels":    querycontract.StringSliceVal(row, "labels"),
			"name":      querycontract.StringVal(row, "name"),
			"repo_id":   querycontract.StringVal(row, "repo_id"),
			"repo_name": querycontract.StringVal(row, "repo_name"),
		}
		id := entityString(entity, "id")
		if id == "" {
			continue
		}
		if existing, ok := entitiesByID[id]; ok {
			if entityString(existing, "repo_id") == "" && entityString(entity, "repo_id") != "" {
				existing["repo_id"] = entity["repo_id"]
			}
			continue
		}
		entities = append(entities, entity)
		entitiesByID[id] = entity
	}
	entities = normalizeResolvedEntities(entities, limit)
	if err := h.HydrateResolvedWorkloadRepoNames(ctx, entities); err != nil {
		return nil, err
	}
	return entities, nil
}

// HydrateResolvedWorkloadRepoNames fills repository names on resolved workload entities. Exported so the staying queryplan execution test keeps driving the handler; see #6060.
func (h *EntityHandler) HydrateResolvedWorkloadRepoNames(
	ctx context.Context,
	entities []map[string]any,
) error {
	repoIDs := make([]string, 0, len(entities))
	for _, entity := range entities {
		if entityString(entity, "repo_name") == "" {
			repoIDs = append(repoIDs, entityString(entity, "repo_id"))
		}
	}
	repoIDs = querycontract.UniqueSortedStrings(repoIDs)
	if len(repoIDs) == 0 {
		return nil
	}

	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	cypher, params := BuildHydrateResolvedWorkloadRepoNamesQuery(repoIDs, access)
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return fmt.Errorf("hydrate workload repository names: %w", err)
	}
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		names[querycontract.StringVal(row, "repo_id")] = querycontract.StringVal(row, "repo_name")
	}

	for _, entity := range entities {
		if entityString(entity, "repo_name") == "" {
			entity["repo_name"] = names[entityString(entity, "repo_id")]
		}
	}
	return nil
}
